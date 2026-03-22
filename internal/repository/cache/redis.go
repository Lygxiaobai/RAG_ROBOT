package cache

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"rag_robot/internal/pkg/config"
	"time"
)

// Client Redis客户端封装
// 核心职责：
// 1. 提供统一的Redis操作接口
// 2. 处理连接失败的降级逻辑
// 3. 统计缓存命中率
type Client struct {
	rdb       *redis.Client
	hitCount  int64 // 缓存命中次数
	missCount int64 // 缓存未命中次数
	enabled   bool  // 是否启用缓存（降级开关）
}

// NewClient 创建Redis客户端
// 参数：
//   - cfg: Redis配置
//
// 返回：
//   - *Client: Redis客户端
//   - error: 错误信息
//
// 设计思路：
// 1. 连接Redis服务器
// 2. Ping检查连接是否正常
// 3. 如果连接失败，不报错，而是禁用缓存（降级策略）
func NewClient(cfg config.RedisConfig) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second, // 连接超时
		ReadTimeout:  3 * time.Second, // 读超时
		WriteTimeout: 3 * time.Second, // 写超时
		PoolSize:     50,              // 连接池大小
		MinIdleConns: 10,              // 最小空闲连接数
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		// Redis连接失败，启用降级模式（禁用缓存）
		return &Client{
			rdb:     nil,
			enabled: false,
		}, nil // 不返回错误，允许系统继续运行
	}

	return &Client{
		rdb:     rdb,
		enabled: true,
	}, nil
}

// Set 设置缓存
// 参数：
//   - ctx: 上下文
//   - key: 缓存键
//   - value: 缓存值
//   - ttl: 过期时间（0表示永不过期）
//
// 返回：
//   - error: 错误信息
func (c *Client) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if !c.enabled || c.rdb == nil {
		return nil // 缓存未启用，直接返回
	}

	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// Get 获取缓存
// 参数：
//   - ctx: 上下文
//   - key: 缓存键
//
// 返回：
//   - string: 缓存值
//   - error: 错误信息（redis.Nil 表示键不存在）
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	if !c.enabled || c.rdb == nil {
		return "", redis.Nil // 缓存未启用，返回未命中
	}

	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		c.missCount++
		return "", err
	}
	if err != nil {
		return "", err
	}

	c.hitCount++
	return val, nil
}

// Del 删除缓存
func (c *Client) Del(ctx context.Context, keys ...string) error {
	if !c.enabled || c.rdb == nil {
		return nil
	}

	return c.rdb.Del(ctx, keys...).Err()
}

// Exists 检查键是否存在
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	if !c.enabled || c.rdb == nil {
		return false, nil
	}

	count, err := c.rdb.Exists(ctx, key).Result()
	return count > 0, err
}

// Expire 设置过期时间
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if !c.enabled || c.rdb == nil {
		return nil
	}

	return c.rdb.Expire(ctx, key, ttl).Err()
}

// GetHitRate 获取缓存命中率
func (c *Client) GetHitRate() float64 {
	total := c.hitCount + c.missCount
	if total == 0 {
		return 0
	}
	return float64(c.hitCount) / float64(total)
}

// Close 关闭Redis连接
func (c *Client) Close() error {
	if c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}
