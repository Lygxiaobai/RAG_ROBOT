package qa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"rag_robot/internal/model"
	"rag_robot/internal/repository/cache"
	"strings"
	"time"

	"github.com/jinzhu/copier"
	goOpenAI "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
	"rag_robot/internal/pkg/logger"
	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/pkg/pool"
	"rag_robot/internal/repository/database"
	"rag_robot/internal/service/search"
)

// systemPrompt 限定模型只基于检索出的文档内容回答。
const systemPrompt = `你是一个企业文档智能助手。你的任务是根据提供的文档内容回答用户的问题。规则：1. 只基于提供的文档内容回答，不要使用外部知识。2. 如果文档中没有相关信息，请明确告知用户“文档中未找到相关内容”。3. 回答要准确、简洁、专业。4. 如有必要，可以引用原文片段。`

var fewShotExamples = []struct {
	Question string
	Context  string
	Answer   string
}{
	{
		Question: "试用期多久？",
		Context:  "[片段1]\n员工入职后的试用期为3个月，试用期内按照劳动合同约定执行。",
		Answer:   "根据文档，员工入职后的试用期为3个月。",
	},
	{
		Question: "公司有多少天陪产假？",
		Context:  "[片段1]\n本文档介绍报销流程、审批要求和差旅标准，未提及陪产假制度。",
		Answer:   "文档中未找到关于陪产假的相关内容。",
	},
}

type Service struct {
	searchSvc  *search.Service
	chatClient *openai.ChatClient
	qaRepo     *database.QARepo
	qaCache    *cache.QACache
	pool       *pool.WorkerPool
}

func NewService(searchSvc *search.Service, chatClient *openai.ChatClient, qaRepo *database.QARepo, qaCache *cache.QACache, workerPool *pool.WorkerPool) *Service {
	return &Service{
		searchSvc:  searchSvc,
		chatClient: chatClient,
		qaRepo:     qaRepo,
		qaCache:    qaCache,
		pool:       workerPool,
	}
}

type AskRequest struct {
	Question        string
	KnowledgeBaseID int64
	TopK            int
}

type AskResponse struct {
	QARecordID int64         `json:"qa_record_id"`
	Answer     string        `json:"answer"`
	Sources    []SourceChunk `json:"sources"`
}

type SourceChunk struct {
	ChunkID    int64   `json:"chunk_id"`
	DocumentID int64   `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"`
	Score      float32 `json:"score"`
}

// Ask 单轮 RAG 问答。  未使用缓存
func (s *Service) Ask(ctx context.Context, req *AskRequest) (*AskResponse, error) {
	startedAt := time.Now()
	kbID := req.KnowledgeBaseID
	questionHash := hashQuestion(req.Question)

	hits, err := s.searchSvc.Search(ctx, req.Question, req.KnowledgeBaseID, req.TopK)
	if err != nil {
		question, kbIDCopy, hashCopy, startCopy, errMsg := req.Question, kbID, questionHash, startedAt, err.Error()
		s.submitAsync(func() {
			s.persistRecord(question, &kbIDCopy, hashCopy, "", nil, startCopy, "failed", errMsg)
		})
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	ragContext, sources := buildContext(hits)

	messages := []goOpenAI.ChatCompletionMessage{
		{Role: goOpenAI.ChatMessageRoleSystem, Content: systemPrompt},
	}
	messages = append(messages, BuildFewShotMessagesForChat()...)
	messages = append(messages, goOpenAI.ChatCompletionMessage{
		Role:    goOpenAI.ChatMessageRoleUser,
		Content: buildUserPrompt(req.Question, ragContext),
	})

	answer, err := s.chatClient.Complete(ctx, messages)
	if err != nil {
		question, kbIDCopy, hashCopy, startCopy, errMsg := req.Question, kbID, questionHash, startedAt, err.Error()
		s.submitAsync(func() {
			s.persistRecord(question, &kbIDCopy, hashCopy, "", sources, startCopy, "failed", errMsg)
		})
		return nil, fmt.Errorf("GPT 调用失败: %w", err)
	}

	// success 路径保持同步：QARecordID 需要返回给调用方用于反馈提交
	resp := &AskResponse{Answer: answer, Sources: sources}
	resp.QARecordID = s.persistRecord(req.Question, &kbID, questionHash, resp.Answer, resp.Sources, startedAt, "success", "")
	return resp, nil
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

// QA 单论问答 使用缓存
func (s *Service) AskQuestion(ctx context.Context, req *AskRequest) (*AskResponse, error) {
	// 1. 尝试从缓存获取
	if s.qaCache != nil {
		cached, hit, err := s.qaCache.Get(ctx, req.KnowledgeBaseID, req.Question)
		if err == nil && hit {
			logger.Info("QA缓存命中", zap.String("question", req.Question))

			// 如果 cached 是 model.QAResult，需要转换
			var askResp AskResponse
			if err := copier.Copy(&askResp, cached); err != nil {
				logger.Warn("缓存数据转换失败", zap.Error(err))
				// 转换失败时，不返回缓存，继续正常流程
			} else {
				return &askResp, nil
			}
		} else if err != nil {
			// 缓存读取失败，记录但不影响主流程
			logger.Warn("读取缓存失败", zap.Error(err))
		}
	}

	// 2. 缓存未命中，执行正常流程
	askResponse, err := s.Ask(ctx, req)
	if err != nil {
		logger.Error("回答失败", zap.Error(err))
		return nil, err // 关键：出错时应该返回错误，而不是继续
	}

	// 防御性检查
	if askResponse == nil {
		logger.Error("回答结果为空")
		return nil, errors.New("empty response")
	}

	// 3. 异步保存结果到缓存（避免阻塞主流程）
	if s.qaCache != nil {
		go func() {
			// 使用新的 context，避免主流程取消影响缓存写入
			cacheCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			var result model.QAResult
			if err := copier.Copy(&result, askResponse); err != nil {
				logger.Warn("缓存数据转换失败", zap.Error(err))
				// 转换失败时，不返回缓存，继续正常流程
			}
			// 直接缓存 AskResponse，避免类型转换
			if err := s.qaCache.Set(cacheCtx, req.KnowledgeBaseID, req.Question, &result); err != nil {
				logger.Warn("保存QA缓存失败", zap.Error(err))
			}
		}()
	}

	return askResponse, nil
}

// buildContext 把检索到的片段拼成上下文文本，并返回来源信息。
func buildContext(hits []*search.SearchResult) (string, []SourceChunk) {
	var sb strings.Builder
	sources := make([]SourceChunk, 0, len(hits))

	for i, hit := range hits {
		sb.WriteString(fmt.Sprintf("[片段%d]\n%s\n\n", i+1, hit.Content))
		sources = append(sources, SourceChunk{
			ChunkID:    hit.ChunkID,
			DocumentID: hit.DocumentID,
			ChunkIndex: hit.ChunkIndex,
			Content:    hit.Content,
			Score:      hit.Score,
		})
	}
	return sb.String(), sources
}

// buildUserPrompt 把问题和上下文拼成最终发给 GPT 的 prompt。
func buildUserPrompt(question, context string) string {
	return fmt.Sprintf("文档内容：\n%s\n用户问题：%s", context, question)
}

// BuildFewShotMessagesForChat 构造最简单的 few-shot 示例消息，供单轮问答和多轮聊天共用。
func BuildFewShotMessagesForChat() []goOpenAI.ChatCompletionMessage {
	msgs := make([]goOpenAI.ChatCompletionMessage, 0, len(fewShotExamples)*2)
	for _, example := range fewShotExamples {
		msgs = append(msgs,
			goOpenAI.ChatCompletionMessage{
				Role:    goOpenAI.ChatMessageRoleUser,
				Content: buildUserPrompt(example.Question, example.Context),
			},
			goOpenAI.ChatCompletionMessage{
				Role:    goOpenAI.ChatMessageRoleAssistant,
				Content: example.Answer,
			},
		)
	}
	return msgs
}

func (s *Service) persistRecord(
	question string,
	knowledgeBaseID *int64,
	questionHash string,
	answer string,
	sources []SourceChunk,
	startedAt time.Time,
	status string,
	errorMessage string,
) int64 {
	if s.qaRepo == nil {
		return 0
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
		QuestionText:    question,
		QuestionHash:    hashPtr,
		AnswerText:      answer,
		AnswerModel:     s.chatClient.Model(),
		AnswerLatencyMS: int(time.Since(startedAt).Milliseconds()),
		RetrievalCount:  len(sources),
		TopScore:        topScoreFromSources(sources),
		Status:          status,
		ErrorMessage:    errorPtr,
	}

	sourceRows := make([]database.QASource, 0, len(sources))
	for i, source := range sources {
		score := float64(source.Score)
		sourceRows = append(sourceRows, database.QASource{
			DocumentID: source.DocumentID,
			ChunkID:    source.ChunkID,
			ChunkIndex: source.ChunkIndex,
			RankNo:     i + 1,
			Score:      &score,
		})
	}

	id, err := s.qaRepo.CreateRecordWithSources(recordCtx, record, sourceRows)
	if err != nil {
		logger.Error("写入 qa_records 失败",
			zap.Error(err),
			zap.String("status", status),
			zap.String("question_hash", questionHash),
		)
		return 0
	}
	return id
}

// topScoreFromSources 返回最高分数（Qdrant 结果已按 score 降序，取首个即可）。
func topScoreFromSources(sources []SourceChunk) *float64 {
	if len(sources) == 0 {
		return nil
	}
	score := float64(sources[0].Score)
	return &score

}

func hashQuestion(question string) string {
	sum := sha256.Sum256([]byte(question))
	return hex.EncodeToString(sum[:])
}

// FeedbackRequest 用户反馈请求。
type FeedbackRequest struct {
	QARecordID int64
	Rating     int8
	Comment    string
}

// SubmitFeedback 提交用户对回答的评价。
func (s *Service) SubmitFeedback(ctx context.Context, req *FeedbackRequest) error {
	var commentPtr *string
	if req.Comment != "" {
		commentPtr = &req.Comment
	}
	return s.qaRepo.CreateFeedback(ctx, &database.QAFeedback{
		QARecordID: req.QARecordID,
		Rating:     req.Rating,
		Comment:    commentPtr,
	})
}

//QAresult --->AskResponse
