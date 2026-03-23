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
	"rag_robot/internal/pkg/metrics"
)

// QACache 问答结果缓存
// 核心思路：
// 1. 使用问题的 MD5 哈希作为缓存键（确保相同问题命中缓存）
// 2. TTL 设置为 1 小时（热点问题缓存）
// 3. 序列化为 JSON 存储
type QACache struct {
	cache *Client
	ttl   time.Duration
}

func NewQACache(cache *Client) *QACache {
	return &QACache{
		cache: cache,
		ttl:   1 * time.Hour, // 默认1小时过期
	}
}

// GetCacheKey 生成缓存键
// 格式：qa:{knowledge_base_id}:{question_hash}
// 为什么这样设计：
// 1. 加上 kb_id：同一问题在不同知识库中答案不同
// 2. 使用 MD5：相同问题的哈希相同，能命中缓存
// 3. 前缀 qa：区分不同类型的缓存
func (c *QACache) GetCacheKey(kbID int64, question string) string {
	hash := md5.Sum([]byte(question))
	return fmt.Sprintf("qa:%d:%x", kbID, hash)
}

// Get 从缓存获取问答结果
// 参数：
//   - ctx: 上下文
//   - kbID: 知识库 ID
//   - question: 用户问题
//
// 返回：
//   - *model.QAResult: 问答结果
//   - bool: 是否命中缓存
//   - error: 错误信息
func (c *QACache) Get(ctx context.Context, kbID int64, question string) (*model.QAResult, bool, error) {
	key := c.GetCacheKey(kbID, question)

	val, err := c.cache.Get(ctx, key)
	if err == redis.Nil {
		// 缓存未命中
		metrics.CacheHitsTotal.WithLabelValues("qa", "miss").Inc()
		return nil, false, nil
	}
	if err != nil {
		// Redis错误，不影响主流程
		metrics.CacheHitsTotal.WithLabelValues("qa", "miss").Inc()
		logger.Warn("获取QA缓存失败", zap.Error(err))
		return nil, false, nil
	}

	// 反序列化
	var result model.QAResult
	if err = json.Unmarshal([]byte(val), &result); err != nil {
		logger.Warn("反序列化QA缓存失败", zap.Error(err))
		// 删除损坏的缓存
		_ = c.cache.Del(ctx, key)
		metrics.CacheHitsTotal.WithLabelValues("qa", "miss").Inc()
		return nil, false, nil
	}

	metrics.CacheHitsTotal.WithLabelValues("qa", "hit").Inc()
	logger.Debug("QA缓存命中", zap.String("key", key))
	return &result, true, nil
}

// Set 设置问答结果缓存
func (c *QACache) Set(ctx context.Context, kbID int64, question string, result *model.QAResult) error {
	key := c.GetCacheKey(kbID, question)

	// 序列化
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("序列化QA结果失败: %w", err)
	}

	// 写入缓存
	if err = c.cache.Set(ctx, key, data, c.ttl); err != nil {
		logger.Warn("设置QA缓存失败", zap.Error(err))
		return err
	}

	logger.Debug("QA缓存已设置", zap.String("key", key))
	return nil
}

// Invalidate 主动失效缓存
// 使用场景：文档更新后，需要清除相关问题的缓存
func (c *QACache) Invalidate(ctx context.Context, kbID int64, question string) error {
	key := c.GetCacheKey(kbID, question)
	return c.cache.Del(ctx, key)
}

// InvalidateByKB 清除某个知识库的所有QA缓存
// 使用场景：知识库文档大量更新后
func (c *QACache) InvalidateByKB(ctx context.Context, kbID int64) error {
	// TODO: 使用 SCAN 命令遍历删除（避免阻塞）
	// 这里简化实现，实际生产环境需要分批删除
	pattern := fmt.Sprintf("qa:%d:*", kbID)
	logger.Info("清除知识库QA缓存", zap.String("pattern", pattern))
	return nil
}
