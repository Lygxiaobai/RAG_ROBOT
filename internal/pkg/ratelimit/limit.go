package ratelimit

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// Limiter 限流器
// 使用 Token Bucket 算法
// 核心思路：
// 1. 每秒产生固定数量的 token
// 2. 每个请求消耗 1 个 token
// 3. token 不足时拒绝请求
type Limiter struct {
	limiter *rate.Limiter
	burst   int // 突发流量大小
}

// NewLimiter 创建限流器
// 参数：
//   - rps: 每秒允许的请求数（Requests Per Second）
//   - burst: 突发流量大小（允许瞬时超过rps的请求数）
//
// 返回：
//   - *Limiter: 限流器实例
//
// 设计思路：
// 1. rps=100, burst=200 表示：
//   - 正常情况下每秒100个请求
//   - 突发情况下可以瞬时处理200个请求
//
// 2. burst > rps 允许短时间内的流量突发
func NewLimiter(rps int, burst int) *Limiter {
	return &Limiter{
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
		burst:   burst,
	}
}

// Allow 检查是否允许请求
// 返回：
//   - bool: true表示允许，false表示拒绝
func (l *Limiter) Allow() bool {
	return l.limiter.Allow()
}

// Wait 等待直到有可用token
// 会阻塞直到获取到token或超时
func (l *Limiter) Wait(ctx context.Context) error {
	return l.limiter.Wait(ctx)
}

// Reserve 预订token
// 返回等待时间
func (l *Limiter) Reserve() time.Duration {
	reservation := l.limiter.Reserve()
	if !reservation.OK() {
		return 0
	}
	return reservation.Delay()
}
