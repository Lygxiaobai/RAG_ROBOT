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

// SearchResult 检索结果，返回给上层服务使用。
type SearchResult struct {
	ChunkID    int64   `json:"chunk_id"`
	Score      float32 `json:"score"`
	DocumentID int64   `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"`
}

// Search 对查询文本做向量检索，返回 Top-K 相关片段。
func (s *Service) Search(ctx context.Context, query string, kbID int64, topK int) ([]*SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("查询内容不能为空")
	}
	if topK <= 0 || topK > 20 {
		topK = 5
	}

	vector, err := s.embedClient.GetEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询向量化失败: %w", err)
	}

	hits, err := s.qdrantClient.Search(ctx, vector, kbID, uint64(topK))
	if err != nil {
		return nil, err
	}

	results := make([]*SearchResult, 0, len(hits))
	for _, hit := range hits {
		results = append(results, &SearchResult{
			ChunkID:    hit.ChunkID,
			Score:      hit.Score,
			DocumentID: hit.DocumentID,
			ChunkIndex: hit.ChunkIndex,
			Content:    hit.Content,
		})
	}
	return results, nil
}
