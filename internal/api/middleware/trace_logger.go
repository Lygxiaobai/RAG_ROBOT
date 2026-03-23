package middleware

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"rag_robot/internal/pkg/logger"
	"time"
)

// TraceLogger 在每条请求日志中注入 trace_id 和 span_id。
// 必须放在 otelgin.Middleware 之后，这样 span context 已经存在于 request context 中。
//
// 日志字段说明：
//   - trace_id: W3C TraceContext 格式，32位十六进制，可在 Jaeger/Tempo 中搜索
//   - span_id:  当前根 Span 的 ID，16位十六进制
//   - method/path/status/latency: 基本请求信息
func TraceLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		// otelgin 已将 span 注入 request context，直接从中提取
		spanCtx := trace.SpanFromContext(c.Request.Context()).SpanContext()

		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("client_ip", c.ClientIP()),
		}

		// 只有当 trace 有效时才附加追踪字段（无 otel 初始化时不会崩溃）
		if spanCtx.IsValid() {
			fields = append(fields,
				zap.String("trace_id", spanCtx.TraceID().String()),
				zap.String("span_id", spanCtx.SpanID().String()),
			)
		}

		if c.Writer.Status() >= 500 {
			logger.Error("HTTP请求", fields...)
		} else {
			logger.Info("HTTP请求", fields...)
		}
	}
}
