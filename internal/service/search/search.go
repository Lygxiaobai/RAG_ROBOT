package search

import (
	"context"
	"fmt"
	"github.com/jinzhu/copier"
	"go.uber.org/zap"
	"rag_robot/internal/model"
	"rag_robot/internal/pkg/logger"
	"rag_robot/internal/pkg/metrics"
	"rag_robot/internal/repository/cache"
	"rag_robot/internal/repository/database"
	"time"

	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/repository/qdrant"
)

type Service struct {
	embedClient  *openai.EmbeddingClient
	qdrantClient *qdrant.Client
	vectorCache  *cache.VectorCache
	docRepo      *database.DocumentRepo // 全文检索降级用
}

func NewService(embedClient *openai.EmbeddingClient, qdrantClient *qdrant.Client, vectorCache *cache.VectorCache, docRepo *database.DocumentRepo) *Service {
	return &Service{
		embedClient:  embedClient,
		qdrantClient: qdrantClient,
		vectorCache:  vectorCache,
		docRepo:      docRepo,
	}
}

// SearchResult 检索结果，返回给上层服务使用。
type SearchResult struct {
	ChunkID    int64   `json:"chunk_id"`
	Score      float32 `json:"score"`
	DocumentID int64   `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"`
	// IsFallback=true 表示该结果来自 MySQL 全文检索降级，而非 Qdrant 向量检索
	IsFallback bool `json:"is_fallback,omitempty"`
}

// Search 对查询文本做向量检索，返回 Top-K 相关片段。
// 当 Qdrant 不可用时，自动降级到 MySQL LIKE 全文检索。
func (s *Service) Search(ctx context.Context, query string, kbID int64, topK int) ([]*SearchResult, error) {
	// 缓存命中
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
	// 缓存未命中

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

	searchStart := time.Now()
	hits, err := s.qdrantClient.Search(ctx, vector, kbID, uint64(topK))
	if err != nil {
		// Qdrant 检索失败（超时/熔断/不可用），降级到 MySQL LIKE 全文检索
		logger.Warn("Qdrant 检索失败，降级到全文检索",
			zap.Error(err),
			zap.Int64("kb_id", kbID),
			zap.String("query", query),
		)
		return s.fallbackFullTextSearch(ctx, kbID, query, topK)
	}
	// 记录 Qdrant 检索耗时
	metrics.ObserveVectorSearch("qdrant", searchStart)

	// 将结果保存到缓存中
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

// fallbackFullTextSearch 当 Qdrant 不可用时，用 MySQL LIKE 检索兜底。
// 返回的结果 IsFallback=true，分数固定为 0（无相似度语义）。
func (s *Service) fallbackFullTextSearch(ctx context.Context, kbID int64, query string, limit int) ([]*SearchResult, error) {
	if s.docRepo == nil {
		return nil, fmt.Errorf("向量检索不可用且未配置全文检索降级")
	}

	searchStart := time.Now()
	chunks, err := s.docRepo.FullTextSearch(ctx, kbID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("全文检索降级也失败: %w", err)
	}
	// 记录全文检索耗时
	metrics.ObserveVectorSearch("fallback", searchStart)

	results := make([]*SearchResult, 0, len(chunks))
	for _, chunk := range chunks {
		results = append(results, &SearchResult{
			ChunkID:    chunk.ID,
			Score:      0, // LIKE 检索无相似度分数
			DocumentID: chunk.DocumentID,
			ChunkIndex: chunk.ChunkIndex,
			Content:    chunk.Content,
			IsFallback: true,
		})
	}
	return results, nil
}
