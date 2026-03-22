package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	goOpenAI "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
	"rag_robot/internal/model"
	"rag_robot/internal/pkg/logger"
	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/pkg/pool"
	"rag_robot/internal/repository/database"
	qaService "rag_robot/internal/service/qa"
	"rag_robot/internal/service/search"
)

const (
	systemPrompt = `你是一个企业文档智能助手，根据提供的文档内容回答问题。只基于文档内容回答，文档中没有则告知用户。`
	maxHistory   = 10
)

type Message struct {
	Role    string
	Content string
}

type Session struct {
	ID              string
	ConversationID  int64 // 数据库 conversations.id
	KnowledgeBaseID int64
	History         []Message
	UpdatedAt       time.Time
}

type Service struct {
	mu         sync.RWMutex
	sessions   map[string]*Session
	searchSvc  *search.Service
	chatClient *openai.ChatClient
	qaRepo     *database.QARepo
	convRepo   *database.ConversationRepo
	pool       *pool.WorkerPool
}

func NewService(searchSvc *search.Service, chatClient *openai.ChatClient, qaRepo *database.QARepo, convRepo *database.ConversationRepo, workerPool *pool.WorkerPool) *Service {
	return &Service{
		sessions:   make(map[string]*Session),
		searchSvc:  searchSvc,
		chatClient: chatClient,
		qaRepo:     qaRepo,
		convRepo:   convRepo,
		pool:       workerPool,
	}
}

func (s *Service) CreateSession(kbID int64) string {
	id := fmt.Sprintf("%d-%d", kbID, time.Now().UnixNano())

	var dbID int64
	// 持久化到数据库
	if s.convRepo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		conv := &model.Conversation{
			KnowledgeBaseID: kbID,
			SessionID:       id,
			Status:          "active",
		}
		var err error
		dbID, err = s.convRepo.Create(ctx, conv)
		if err != nil {
			logger.Error("创建会话记录失败", zap.Error(err), zap.String("session_id", id))
		}
	}

	s.mu.Lock()
	s.sessions[id] = &Session{
		ID:              id,
		ConversationID:  dbID,
		KnowledgeBaseID: kbID,
		History:         []Message{},
		UpdatedAt:       time.Now(),
	}
	s.mu.Unlock()
	return id
}

// getSession 先查内存缓存，未命中则从数据库恢复。
func (s *Service) getSession(ctx context.Context, sessionID string) (*Session, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if ok {
		return session, nil
	}

	// 内存未命中，尝试从数据库恢复
	if s.convRepo == nil {
		return nil, fmt.Errorf("会话不存在: %s", sessionID)
	}

	conv, err := s.convRepo.GetBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("查询会话失败: %w", err)
	}
	if conv == nil {
		return nil, fmt.Errorf("会话不存在: %s", sessionID)
	}

	// 从数据库加载历史消息
	dbMsgs, err := s.convRepo.ListMessages(ctx, conv.ID, maxHistory*2)
	if err != nil {
		logger.Error("加载会话历史失败", zap.Error(err), zap.String("session_id", sessionID))
		dbMsgs = nil
	}

	history := make([]Message, 0, len(dbMsgs))
	for _, m := range dbMsgs {
		history = append(history, Message{Role: m.Role, Content: m.Content})
	}

	session = &Session{
		ID:              sessionID,
		ConversationID:  conv.ID,
		KnowledgeBaseID: conv.KnowledgeBaseID,
		History:         history,
		UpdatedAt:       time.Now(),
	}

	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()

	return session, nil
}

func (s *Service) Chat(ctx context.Context, sessionID, userMsg string, onChunk func(string) error) error {
	startedAt := time.Now()
	//计算问题hash值
	questionHash := hashQuestion(userMsg)

	session, err := s.getSession(ctx, sessionID)
	if err != nil {
		s.submitAsync(func() {
			s.persistRecord(nil, nil, userMsg, questionHash, "", nil, startedAt, "failed", err.Error())
		})
		return err
	}

	kbID := session.KnowledgeBaseID
	conversationID := optionalInt64(session.ConversationID)
	hits, err := s.searchSvc.Search(ctx, userMsg, kbID, 5)
	if err != nil {
		kbIDCopy, convIDCopy, msgCopy, hashCopy, startCopy, errMsg := kbID, conversationID, userMsg, questionHash, startedAt, err.Error()
		s.submitAsync(func() {
			s.persistRecord(&kbIDCopy, convIDCopy, msgCopy, hashCopy, "", nil, startCopy, "failed", errMsg)
		})
		return fmt.Errorf("检索失败: %w", err)
	}

	docContext := buildContext(hits)
	messages := buildMessages(session.History, userMsg, docContext)

	var fullAnswer strings.Builder
	err = s.chatClient.StreamComplete(ctx, messages, func(chunk string) error {
		fullAnswer.WriteString(chunk)
		return onChunk(chunk)
	})
	if err != nil {
		status := "failed"
		if fullAnswer.Len() > 0 {
			status = "partial"
		}
		kbIDCopy, convIDCopy, msgCopy, hashCopy, ans, startCopy, st, errMsg := kbID, conversationID, userMsg, questionHash, fullAnswer.String(), startedAt, status, err.Error()
		s.submitAsync(func() {
			s.persistRecord(&kbIDCopy, convIDCopy, msgCopy, hashCopy, ans, hits, startCopy, st, errMsg)
		})
		return err
	}

	answerText := fullAnswer.String()
	s.mu.Lock()
	session.History = append(session.History,
		Message{Role: "user", Content: userMsg},
		Message{Role: "assistant", Content: answerText},
	)
	if len(session.History) > maxHistory*2 {
		session.History = session.History[2:]
	}
	session.UpdatedAt = time.Now()
	s.mu.Unlock()

	// 异步持久化消息和 QA 记录，不阻塞 SSE 响应返回
	convID, ans := session.ConversationID, answerText
	kbIDCopy, convIDCopy, msgCopy, hashCopy, startCopy := kbID, conversationID, userMsg, questionHash, startedAt
	s.submitAsync(func() {
		s.persistMessages(convID, userMsg, ans)
		s.persistRecord(&kbIDCopy, convIDCopy, msgCopy, hashCopy, ans, hits, startCopy, "success", "")
	})
	return nil
}

// submitAsync 将函数投递到协程池异步执行；pool 未初始化时降级为 goroutine。
func (s *Service) submitAsync(fn func()) {
	if s.pool != nil {
		_ = s.pool.Submit(func(_ context.Context) error {
			fn()
			return nil
		})
		return
	}
	go fn()
}

// persistMessages 将用户消息和助手回答写入 messages 表，并更新会话最后提问时间。
func (s *Service) persistMessages(conversationID int64, userMsg, assistantMsg string) {
	if s.convRepo == nil || conversationID == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	now := time.Now()

	if _, err := s.convRepo.CreateMessage(ctx, &model.ConversationMessage{
		ConversationID: conversationID,
		Role:           "user",
		Content:        userMsg,
	}); err != nil {
		logger.Error("写入用户消息失败", zap.Error(err), zap.Int64("conversation_id", conversationID))
	}

	if _, err := s.convRepo.CreateMessage(ctx, &model.ConversationMessage{
		ConversationID: conversationID,
		Role:           "assistant",
		Content:        assistantMsg,
	}); err != nil {
		logger.Error("写入助手消息失败", zap.Error(err), zap.Int64("conversation_id", conversationID))
	}

	if err := s.convRepo.UpdateLastQuestionAt(ctx, conversationID, now); err != nil {
		logger.Error("更新会话最后提问时间失败", zap.Error(err), zap.Int64("conversation_id", conversationID))
	}
}

func (s *Service) persistRecord(
	knowledgeBaseID *int64,
	conversationID *int64,
	question string,
	questionHash string,
	answer string,
	hits []*search.SearchResult,
	startedAt time.Time,
	status string,
	errorMessage string,
) {
	if s.qaRepo == nil {
		return
	}

	recordCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var hashPtr *string
	if questionHash != "" {
		hashPtr = &questionHash
	}

	var errorPtr *string
	if errorMessage != "" {
		errorPtr = &errorMessage
	}

	record := &database.QARecord{
		KnowledgeBaseID: knowledgeBaseID,
		ConversationID:  conversationID,
		QuestionText:    question,
		QuestionHash:    hashPtr,
		AnswerText:      answer,
		AnswerModel:     s.chatClient.Model(),
		AnswerLatencyMS: int(time.Since(startedAt).Milliseconds()),
		RetrievalCount:  len(hits),
		TopScore:        topScoreFromHits(hits),
		Status:          status,
		ErrorMessage:    errorPtr,
	}

	sourceRows := make([]database.QASource, 0, len(hits))
	for i, hit := range hits {
		score := float64(hit.Score)
		sourceRows = append(sourceRows, database.QASource{
			DocumentID: hit.DocumentID,
			ChunkID:    hit.ChunkID,
			ChunkIndex: hit.ChunkIndex,
			RankNo:     i + 1,
			Score:      &score,
		})
	}

	if _, err := s.qaRepo.CreateRecordWithSources(recordCtx, record, sourceRows); err != nil {
		logger.Error("写入 qa_records 失败",
			zap.Error(err),
			zap.String("status", status),
			zap.String("question_hash", questionHash),
		)
	}
}

func optionalInt64(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func topScoreFromHits(hits []*search.SearchResult) *float64 {
	if len(hits) == 0 {
		return nil
	}

	maxScore := float64(hits[0].Score)
	for _, hit := range hits[1:] {
		if float64(hit.Score) > maxScore {
			maxScore = float64(hit.Score)
		}
	}

	return &maxScore
}

func hashQuestion(question string) string {
	sum := sha256.Sum256([]byte(question))
	return hex.EncodeToString(sum[:])
}

func buildContext(hits []*search.SearchResult) string {
	var sb strings.Builder
	for i, hit := range hits {
		sb.WriteString(fmt.Sprintf("[片段%d]\n%s\n\n", i+1, hit.Content))
	}
	return sb.String()
}

func buildMessages(history []Message, userMsg, docContext string) []goOpenAI.ChatCompletionMessage {
	msgs := []goOpenAI.ChatCompletionMessage{
		{Role: goOpenAI.ChatMessageRoleSystem, Content: systemPrompt},
	}
	msgs = append(msgs, qaService.BuildFewShotMessagesForChat()...)

	for _, h := range history {
		role := goOpenAI.ChatMessageRoleUser
		if h.Role == "assistant" {
			role = goOpenAI.ChatMessageRoleAssistant
		}
		msgs = append(msgs, goOpenAI.ChatCompletionMessage{Role: role, Content: h.Content})
	}

	content := fmt.Sprintf("文档内容：\n%s\n用户问题：%s", docContext, userMsg)
	msgs = append(msgs, goOpenAI.ChatCompletionMessage{Role: goOpenAI.ChatMessageRoleUser, Content: content})
	return msgs
}
