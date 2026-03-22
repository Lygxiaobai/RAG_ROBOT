package cache

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"rag_robot/internal/model"
	"rag_robot/internal/pkg/logger"
)

// VectorCache 向量检索结果缓存
// 核心思路：
// 1. 缓存向量检索的Top-K结果
// 2. TTL设置较短（30分钟），因为向量检索结果可能变化
// 3. 减少Qdrant的查询压力
type VectorCache struct {
	cache *Client
	ttl   time.Duration
}

func NewVectorCache(cache *Client) *VectorCache {
	return &VectorCache{
		cache: cache,
		ttl:   30 * time.Minute, // 30分钟过期
	}
}

// GetCacheKey 生成缓存键
// 格式：vector:{knowledge_base_id}:{query_hash}:{top_k}
// 为什么包含 top_k：
// 1. 同一问题，top_k不同，结果不同
// 2. 保证缓存精确性
func (c *VectorCache) GetCacheKey(kbID int64, query string, topK int) string {
	hash := md5.Sum([]byte(query))
	return fmt.Sprintf("vector:%d:%x:%d", kbID, hash, topK)
}

// Get 从缓存获取向量检索结果
func (c *VectorCache) Get(ctx context.Context, kbID int64, query string, topK int) ([]model.SearchHit, bool, error) {
	key := c.GetCacheKey(kbID, query, topK)

	val, err := c.cache.Get(ctx, key)
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		logger.Warn("获取向量缓存失败", zap.Error(err))
		return nil, false, nil
	}

	var hits []model.SearchHit
	if err = json.Unmarshal([]byte(val), &hits); err != nil {
		logger.Warn("反序列化向量缓存失败", zap.Error(err))
		_ = c.cache.Del(ctx, key)
		return nil, false, nil
	}

	logger.Debug("向量缓存命中", zap.String("key", key), zap.Int("hit_count", len(hits)))
	return hits, true, nil
}

// Set 设置向量检索结果缓存
func (c *VectorCache) Set(ctx context.Context, kbID int64, query string, topK int, hits []model.SearchHit) error {
	key := c.GetCacheKey(kbID, query, topK)

	data, err := json.Marshal(hits)
	if err != nil {
		return fmt.Errorf("序列化向量检索结果失败: %w", err)
	}

	if err = c.cache.Set(ctx, key, data, c.ttl); err != nil {
		logger.Warn("设置向量缓存失败", zap.Error(err))
		return err
	}

	logger.Debug("向量缓存已设置", zap.String("key", key))
	return nil
}

// InvalidateByKB 清除某个知识库的所有向量缓存
// 使用场景：文档更新后，向量数据变化
func (c *VectorCache) InvalidateByKB(ctx context.Context, kbID int64) error {
	pattern := fmt.Sprintf("vector:%d:*", kbID)
	logger.Info("清除知识库向量缓存", zap.String("pattern", pattern))
	// TODO: 使用 SCAN 命令遍历删除
	return nil
}
