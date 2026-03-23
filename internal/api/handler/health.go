package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"rag_robot/internal/pkg/circuitbreaker"
	"rag_robot/internal/pkg/logger"
)

// pinger 是可以做连通性探测的依赖组件接口。
// db、redis、qdrant 都实现这个接口，方便统一处理。
type pinger interface {
	Ping(ctx context.Context) error
}

// dbPinger 包装 *sql.DB 使其满足 pinger 接口。
type dbPinger struct{ db *sql.DB }

func (d *dbPinger) Ping(ctx context.Context) error { return d.db.PingContext(ctx) }

// HealthHandler 增强版健康检查 Handler。
// 持有所有外部依赖的引用，可以逐一探测。
type HealthHandler struct {
	db       *sql.DB
	redis    pinger
	qdrant   pinger
	breakers map[string]*circuitbreaker.CircuitBreaker
}

// NewHealthHandler 创建 HealthHandler。
//
//   - db:       MySQL 连接池（必须，挂了返回 503）
//   - redis:    Redis 客户端（降级依赖，挂了继续运行）
//   - qdrant:   Qdrant 客户端（降级依赖，挂了走全文检索）
//   - breakers: 熔断器 map，key 是服务名（embedding/qdrant/chat）
func NewHealthHandler(
	db *sql.DB,
	redis pinger,
	qdrant pinger,
	breakers map[string]*circuitbreaker.CircuitBreaker,
) *HealthHandler {
	return &HealthHandler{
		db:       db,
		redis:    redis,
		qdrant:   qdrant,
		breakers: breakers,
	}
}

// componentStatus 单个组件的健康状态。
type componentStatus struct {
	Status  string `json:"status"`            // ok / degraded / error
	Latency string `json:"latency"`           // 探测耗时
	Message string `json:"message,omitempty"` // 异常时的说明
}

// healthResponse 完整健康检查响应体。
type healthResponse struct {
	Status     string                     `json:"status"` // healthy / degraded / unhealthy
	Timestamp  string                     `json:"timestamp"`
	Components map[string]componentStatus `json:"components"`
}

// Check GET /health
// 检查所有组件，返回整体状态和各组件详情。
//
// 状态判定规则：
//   - MySQL 不可用 → unhealthy（503），负载均衡会摘除节点
//   - Redis/Qdrant/熔断器异常 → degraded（200），仍能提供降级服务
func (h *HealthHandler) Check(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	components := make(map[string]componentStatus)
	overallHealthy := true
	hasDegraded := false

	// ---- MySQL（关键依赖）----
	start := time.Now()
	if err := h.db.PingContext(ctx); err != nil {
		logger.Error("健康检查：MySQL 不可用", zap.Error(err))
		components["mysql"] = componentStatus{
			Status:  "error",
			Latency: time.Since(start).String(),
			Message: "database unavailable",
		}
		overallHealthy = false
	} else {
		components["mysql"] = componentStatus{
			Status:  "ok",
			Latency: time.Since(start).String(),
		}
	}

	// ---- Redis（降级依赖）----
	start = time.Now()
	if err := h.redis.Ping(ctx); err != nil {
		logger.Warn("健康检查：Redis 不可用，缓存已降级", zap.Error(err))
		components["redis"] = componentStatus{
			Status:  "degraded",
			Latency: time.Since(start).String(),
			Message: "cache unavailable, running without cache",
		}
		hasDegraded = true
	} else {
		components["redis"] = componentStatus{
			Status:  "ok",
			Latency: time.Since(start).String(),
		}
	}

	// ---- Qdrant（降级依赖）----
	start = time.Now()
	if err := h.qdrant.Ping(ctx); err != nil {
		logger.Warn("健康检查：Qdrant 不可用，向量检索已降级", zap.Error(err))
		components["qdrant"] = componentStatus{
			Status:  "degraded",
			Latency: time.Since(start).String(),
			Message: "vector search degraded to fulltext",
		}
		hasDegraded = true
	} else {
		components["qdrant"] = componentStatus{
			Status:  "ok",
			Latency: time.Since(start).String(),
		}
	}

	// ---- 熔断器状态 ----
	// State: 0=Closed(正常) 1=Open(熔断中) 2=HalfOpen(探测中)
	stateNames := map[circuitbreaker.State]string{
		circuitbreaker.StateClosed:   "closed",
		circuitbreaker.StateOpen:     "open",
		circuitbreaker.StateHalfOpen: "half-open",
	}
	for name, cb := range h.breakers {
		state := cb.GetState()
		cs := componentStatus{
			Status:  "ok",
			Message: stateNames[state],
		}
		if state == circuitbreaker.StateOpen {
			cs.Status = "degraded"
			hasDegraded = true
		} else if state == circuitbreaker.StateHalfOpen {
			cs.Status = "degraded"
			hasDegraded = true
		}
		components["circuit:"+name] = cs
	}

	// ---- 汇总整体状态 ----
	overallStatus := "healthy"
	httpCode := http.StatusOK
	if !overallHealthy {
		overallStatus = "unhealthy"
		httpCode = http.StatusServiceUnavailable // 503 让负载均衡摘除节点
	} else if hasDegraded {
		overallStatus = "degraded" // 200，仍可提供降级服务
	}

	c.JSON(httpCode, healthResponse{
		Status:     overallStatus,
		Timestamp:  time.Now().Format(time.RFC3339),
		Components: components,
	})
}

// Ready GET /ready
// 就绪探针（K8s readinessProbe）：只检查关键依赖 MySQL。
// 返回非 200 时，K8s 停止向该 Pod 转发流量。
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// Live GET /live
// 存活探针（K8s livenessProbe）：只要进程还在跑就返回 200。
// 返回非 200 时，K8s 会重启容器。
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}
