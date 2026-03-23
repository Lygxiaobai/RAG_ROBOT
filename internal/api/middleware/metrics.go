package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"rag_robot/internal/pkg/metrics"
)

// PrometheusMetrics 是 Gin 全局中间件，自动为每个 HTTP 请求采集指标。
// 记录两类数据：
//  1. 请求计数（按方法 / 路由模板 / 状态码）
//  2. 请求延迟直方图（按方法 / 路由模板）
//
// 注意：使用 c.FullPath() 而非 c.Request.URL.Path，
// 这样 /api/v1/document/123 和 /api/v1/document/456 会聚合到同一个 label，
// 避免高基数（high cardinality）导致 Prometheus 内存爆炸。
func PrometheusMetrics(metricsPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 先执行后续 handler
		c.Next()

		// /metrics 自身的请求不统计，避免自我干扰
		if c.Request.URL.Path == metricsPath {
			return
		}

		duration := time.Since(start).Seconds()
		path := c.FullPath() // 路由模板，如 /api/v1/document/:id
		if path == "" {
			// 未匹配到任何路由（404），统一归到 "unknown"
			path = "unknown"
		}

		statusCode := strconv.Itoa(c.Writer.Status())

		// 累加请求计数
		metrics.HTTPRequestsTotal.WithLabelValues(
			c.Request.Method,
			path,
			statusCode,
		).Inc()

		// 记录延迟到直方图
		metrics.HTTPRequestDuration.WithLabelValues(
			c.Request.Method,
			path,
		).Observe(duration)
	}
}
