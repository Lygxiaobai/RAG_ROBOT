package qa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	goOpenAI "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
	"rag_robot/internal/pkg/logger"
	"rag_robot/internal/pkg/openai"
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
}

func NewService(searchSvc *search.Service, chatClient *openai.ChatClient, qaRepo *database.QARepo) *Service {
	return &Service{
		searchSvc:  searchSvc,
		chatClient: chatClient,
		qaRepo:     qaRepo,
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

// Ask 单轮 RAG 问答。
func (s *Service) Ask(ctx context.Context, req *AskRequest) (*AskResponse, error) {
	startedAt := time.Now()
	kbID := req.KnowledgeBaseID
	questionHash := hashQuestion(req.Question)

	hits, err := s.searchSvc.Search(ctx, req.Question, req.KnowledgeBaseID, req.TopK)
	if err != nil {
		//入库
		s.persistRecord(req.Question, &kbID, questionHash, "", nil, startedAt, "failed", err.Error())
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
		//入库
		s.persistRecord(req.Question, &kbID, questionHash, "", sources, startedAt, "failed", err.Error())
		return nil, fmt.Errorf("GPT 调用失败: %w", err)
	}

	resp := &AskResponse{Answer: answer, Sources: sources}
	//入库
	resp.QARecordID = s.persistRecord(req.Question, &kbID, questionHash, resp.Answer, resp.Sources, startedAt, "success", "")
	return resp, nil
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
