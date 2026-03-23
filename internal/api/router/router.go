package router

import (
	"database/sql"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"rag_robot/internal/pkg/circuitbreaker"
	"rag_robot/internal/pkg/config"
	"rag_robot/internal/pkg/pool"
	"rag_robot/internal/pkg/ratelimit"
	"rag_robot/internal/repository/cache"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"rag_robot/internal/api/handler"
	"rag_robot/internal/api/middleware"
	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/repository/database"
	"rag_robot/internal/repository/qdrant"
	"rag_robot/internal/service/chat"
	"rag_robot/internal/service/document"
	"rag_robot/internal/service/qa"
	"rag_robot/internal/service/search"
)

func SetupRouter(
	db *sql.DB,
	embedClient *openai.EmbeddingClient,
	qdrantClient *qdrant.Client,
	openAIClient *openai.ChatClient,
	redisClient *cache.Client,
	workerPool *pool.WorkerPool,
	breakers map[string]*circuitbreaker.CircuitBreaker,
	metricConfig *config.MetricsConfig,
) *gin.Engine {
	// 用 gin.New() 代替 gin.Default()，避免 gin 默认 Logger 和我们的 TraceLogger 重复打日志
	r := gin.New()
	r.Use(gin.Recovery()) // 保留 panic recover
	r.MaxMultipartMemory = 50 << 20

	// 中间件注册顺序很重要：
	// 1. PrometheusMetrics：最先，确保 429 也能被统计
	// 2. CORS：跨域头需要在任何响应之前设置
	// 3. otelgin：创建根 Span，后续中间件才能从 ctx 里取到 trace_id
	// 4. TraceLogger：在 otelgin 之后，才能读到有效的 trace_id 写入日志
	// 5. RateLimit：限流
	//指标统计
	if metricConfig.Enabled {
		r.Use(middleware.PrometheusMetrics(metricConfig.Path))
		// Prometheus 指标拉取端点
		r.GET(metricConfig.Path, gin.WrapH(promhttp.Handler()))
	}
	r.Use(middleware.CORS())
	//链路跟踪
	r.Use(otelgin.Middleware("rag_robot"))
	r.Use(middleware.TraceLogger())
	//限流器
	r.Use(middleware.RateLimitMiddleware(ratelimit.NewLimiter(100, 200)))

	// 健康检查（三个端点职责不同）
	healthHandler := handler.NewHealthHandler(db, redisClient, qdrantClient, breakers)
	r.GET("/health", healthHandler.Check) // 完整检查，含各组件状态
	r.GET("/ready", healthHandler.Ready)  // 就绪探针，K8s readinessProbe
	r.GET("/live", healthHandler.Live)    // 存活探针，K8s livenessProbe

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", healthHandler.Check)

		docRepo := database.NewDocumentRepo(db)

		searchSvc := search.NewService(embedClient, qdrantClient, cache.NewVectorCache(redisClient), docRepo)

		//文档上传与删除
		docSvc := document.NewService(docRepo, embedClient, qdrantClient, workerPool)
		docHandler := handler.NewDocumentHandler(docSvc)
		v1.POST("/document/upload", docHandler.Upload)
		v1.DELETE("/document/:id", docHandler.Delete)
		//检索topk
		searchHandler := handler.NewSearchHandler(searchSvc)
		v1.POST("/search", searchHandler.Search)

		//qa问答
		qaRepo := database.NewQARepo(db)
		qaSvc := qa.NewService(searchSvc, openAIClient, qaRepo, cache.NewQACache(redisClient), workerPool)
		qaHandler := handler.NewQAHandler(qaSvc)
		v1.POST("/qa", qaHandler.Ask)
		v1.POST("/qa/feedback", qaHandler.Feedback)

		//chat聊天 流式对话
		convRepo := database.NewConversationRepo(db)
		chatService := chat.NewService(searchSvc, openAIClient, qaRepo, convRepo, workerPool)
		chatHandler := handler.NewChatHandler(chatService)
		v1.POST("/chat/session", chatHandler.CreateSession)
		v1.POST("/chat", chatHandler.Chat)
	}
	return r
}
