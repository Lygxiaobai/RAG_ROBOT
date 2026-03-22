package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"rag_robot/internal/model"
	"rag_robot/internal/pkg/logger"
)

// DocCache 文档元数据缓存
// 核心思路：
// 1. 缓存文档的基本信息（不缓存内容，只缓存元数据）
// 2. TTL较长（24小时），因为文档信息变化不频繁
// 3. 减少数据库查询
type DocCache struct {
	cache *Client
	ttl   time.Duration
}

func NewDocCache(cache *Client) *DocCache {
	return &DocCache{
		cache: cache,
		ttl:   24 * time.Hour, // 24小时过期
	}
}

// GetCacheKey 生成缓存键
// 格式：doc:{document_id}
func (c *DocCache) GetCacheKey(docID int64) string {
	return fmt.Sprintf("doc:%d", docID)
}

// Get 从缓存获取文档信息
func (c *DocCache) Get(ctx context.Context, docID int64) (*model.Document, bool, error) {
	key := c.GetCacheKey(docID)

	val, err := c.cache.Get(ctx, key)
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		logger.Warn("获取文档缓存失败", zap.Error(err))
		return nil, false, nil
	}

	var doc model.Document
	if err = json.Unmarshal([]byte(val), &doc); err != nil {
		logger.Warn("反序列化文档缓存失败", zap.Error(err))
		_ = c.cache.Del(ctx, key)
		return nil, false, nil
	}

	logger.Debug("文档缓存命中", zap.String("key", key))
	return &doc, true, nil
}

// Set 设置文档信息缓存
func (c *DocCache) Set(ctx context.Context, doc *model.Document) error {
	key := c.GetCacheKey(doc.ID)

	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化文档信息失败: %w", err)
	}

	if err = c.cache.Set(ctx, key, data, c.ttl); err != nil {
		logger.Warn("设置文档缓存失败", zap.Error(err))
		return err
	}

	logger.Debug("文档缓存已设置", zap.String("key", key))
	return nil
}

// Invalidate 主动失效文档缓存
// 使用场景：文档信息更新后（状态变化、重新处理等）
func (c *DocCache) Invalidate(ctx context.Context, docID int64) error {
	key := c.GetCacheKey(docID)
	return c.cache.Del(ctx, key)
}
