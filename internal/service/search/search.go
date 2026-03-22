package search

import (
	"context"
	"fmt"
	"github.com/jinzhu/copier"
	"go.uber.org/zap"
	"rag_robot/internal/model"
	"rag_robot/internal/pkg/logger"
	"rag_robot/internal/repository/cache"

	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/repository/qdrant"
)

type Service struct {
	embedClient  *openai.EmbeddingClient
	qdrantClient *qdrant.Client
	vectorCache  *cache.VectorCache
}

func NewService(embedClient *openai.EmbeddingClient, qdrantClient *qdrant.Client, vectorCache *cache.VectorCache) *Service {
	return &Service{
		embedClient:  embedClient,
		qdrantClient: qdrantClient,
		vectorCache:  vectorCache,
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
	//缓存命中
	if s.vectorCache != nil {
		vectorCacheache, has, err := s.vectorCache.Get(ctx, kbID, query, topK)
		if err != nil {
			logger.Error("search vector cache failed", zap.Error(err))
			return nil, err
		}
		if has {
			var searchResults []*SearchResult
			err := copier.Copy(&searchResults, vectorCacheache)
			if err != nil {
				logger.Error("转换失败", zap.Error(err))
				return nil, err
			}
			return searchResults, nil
		}
	}
	//缓存未命中

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

	//将结果保存到缓冲中
	if s.vectorCache != nil && len(hits) > 0 {
		var searchHits []model.SearchHit
		if err := copier.Copy(&searchHits, hits); err != nil {
			logger.Error("转换失败", zap.Error(err))
		}
		if err := s.vectorCache.Set(ctx, kbID, query, topK, searchHits); err != nil {
			logger.Error("保存向量缓冲失败", zap.Error(err))
		}

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
