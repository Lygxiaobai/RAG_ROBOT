package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 所有指标用 promauto 注册，包加载时自动注入到默认 Registry。
// Prometheus 会定期 GET /metrics 拉取这些数据。

var (
	// ---- HTTP 层指标 ----

	// HTTPRequestsTotal 请求计数，按方法/路由模板/状态码分组。
	// 用途：rate(http_requests_total[1m]) 计算 QPS
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP 请求总计数",
		},
		[]string{"method", "path", "status_code"},
	)

	// HTTPRequestDuration 请求延迟直方图，按方法/路由模板分组。
	// 用途：histogram_quantile(0.99, ...) 计算 P99 延迟
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "HTTP 请求延迟（秒）",
			// 桶边界覆盖从 10ms 到 5s 的范围，适合 RAG 场景
			Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1, 2, 5},
		},
		[]string{"method", "path"},
	)

	// ---- 问答业务指标 ----

	// QARequestsTotal 问答请求计数，按类型（qa/chat）和结果状态分组。
	// status 取值：success / fallback / error
	QARequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "qa_requests_total",
			Help: "问答请求总计数",
		},
		[]string{"type", "status"},
	)

	// QADuration 问答端到端延迟（从收到请求到返回答案）。
	QADuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "qa_duration_seconds",
			Help:    "问答响应延迟（秒）",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30},
		},
		[]string{"type"}, // qa / chat
	)

	// ---- 向量检索指标 ----

	// VectorSearchDuration 向量检索耗时（不含向量化）。
	VectorSearchDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vector_search_duration_seconds",
			Help:    "向量检索延迟（秒）",
			Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1},
		},
		// source: qdrant（正常）/ fallback（MySQL 全文检索降级）
		[]string{"source"},
	)

	// ---- 外部服务指标 ----

	// OpenAICallsTotal OpenAI API 调用计数，按模型和结果分组。
	// status 取值：success / error / timeout / fallback
	OpenAICallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_calls_total",
			Help: "OpenAI API 调用总计数",
		},
		[]string{"operation", "status"}, // operation: embedding / chat
	)

	// ---- 熔断器状态指标 ----

	// CircuitBreakerState 熔断器当前状态（Gauge，随时间变化）。
	// 值：0=closed（正常），1=open（熔断中），2=half-open（探测中）
	// 告警规则：circuit_breaker_state{service="openai"} == 1 触发告警
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "熔断器当前状态（0=正常，1=熔断，2=半开）",
		},
		[]string{"service"}, // openai / qdrant / embedding
	)

	// ---- 缓存指标 ----

	// CacheHitsTotal 缓存命中/未命中计数。
	// 用途：rate(cache_hits_total{result="hit"}[5m]) / rate(cache_hits_total[5m]) 计算命中率
	CacheHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "缓存操作计数",
		},
		[]string{"type", "result"}, // type: qa/vector/doc, result: hit/miss
	)

	// ---- 文档处理指标 ----

	// DocumentsProcessed 文档处理计数。
	DocumentsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "documents_processed_total",
			Help: "文档处理总计数",
		},
		[]string{"status"}, // success / error
	)

	// ---- 系统资源指标 ----

	// WorkerPoolQueueSize 工作池当前待处理任务数（Gauge）。
	// 用途：监控异步任务积压情况，过高说明处理能力跟不上请求速度
	WorkerPoolQueueSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "worker_pool_queue_size",
			Help: "工作池待处理任务数",
		},
	)
)

// ObserveQADuration 记录问答耗时的辅助函数。
// 用法：defer metrics.ObserveQADuration("qa", time.Now())
func ObserveQADuration(qtype string, start time.Time) {
	QADuration.WithLabelValues(qtype).Observe(time.Since(start).Seconds())
}

// ObserveVectorSearch 记录向量检索耗时。
// source 传 "qdrant" 或 "fallback"。
func ObserveVectorSearch(source string, start time.Time) {
	VectorSearchDuration.WithLabelValues(source).Observe(time.Since(start).Seconds())
}
