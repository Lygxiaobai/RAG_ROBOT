package search

import (
	"context"
	"fmt"

	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/repository/qdrant"
)

type Service struct {
	embedClient  *openai.EmbeddingClient
	qdrantClient *qdrant.Client
}

func NewService(embedClient *openai.EmbeddingClient, qdrantClient *qdrant.Client) *Service {
	return &Service{
		embedClient:  embedClient,
		qdrantClient: qdrantClient,
	}
}

// SearchResult 检索结果，返回给 Handler 层
type SearchResult struct {
	Score      float32 `json:"score"`
	DocumentID int64   `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"`
}

// Search 对用户查询文本做向量检索，返回 Top-K 相关片段
func (s *Service) Search(ctx context.Context, query string, kbID int64, topK int) ([]*SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("查询内容不能为空")
	}
	if topK <= 0 || topK > 20 {
		topK = 5
	}

	// 1. 对查询文本生成向量
	vector, err := s.embedClient.GetEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询向量化失败: %w", err)
	}

	// 2. 在 Qdrant 中检索
	hits, err := s.qdrantClient.Search(ctx, vector, kbID, uint64(topK))
	if err != nil {
		return nil, err
	}

	// 3. 组装结果
	results := make([]*SearchResult, 0, len(hits))
	for _, hit := range hits {
		results = append(results, &SearchResult{
			Score:      hit.Score,
			DocumentID: hit.DocumentID,
			ChunkIndex: hit.ChunkIndex,
			Content:    hit.Content,
		})
	}
	return results, nil
}
