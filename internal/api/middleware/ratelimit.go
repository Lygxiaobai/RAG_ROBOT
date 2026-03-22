package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"rag_robot/internal/pkg/ratelimit"
)

// RateLimitMiddleware 限流中间件
// 使用场景：
// 1. 防止单个用户过度请求
// 2. 保护后端服务不被压垮
// 3. 保证服务的公平性
func RateLimitMiddleware(limiter *ratelimit.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否允许请求
		if !limiter.Allow() {
			// 触发限流，返回 429 Too Many Requests
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "请求过于频繁，请稍后再试",
				"code":  "RATE_LIMIT_EXCEEDED",
			})
			c.Abort()
			return
		}

		// 允许请求继续
		c.Next()
	}
}
