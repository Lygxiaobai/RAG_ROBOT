package main

import (
	"log"
	"time"

	"rag_robot/internal/pkg/circuitbreaker"
	"rag_robot/internal/pkg/metrics"
	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/pkg/pool"
	"rag_robot/internal/pkg/tracing"
	"rag_robot/internal/repository/cache"
	"rag_robot/internal/repository/qdrant"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"rag_robot/internal/api/router"
	"rag_robot/internal/pkg/config"
	"rag_robot/internal/pkg/logger"
	"rag_robot/internal/repository/database"
)

func main() {
	cfg, err := config.LoadConfig(config.GetConfigPath())
	if err != nil {
		log.Fatal("配置文件加载失败: ", err)
	}

	if err = logger.Init(logger.Config{
		Level:      cfg.Log.Level,
		Format:     cfg.Log.Format,
		OutputPath: cfg.Log.OutputPath,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,
	}); err != nil {
		log.Fatal("日志初始化失败: ", err)
	}
	defer logger.Sync()

	// 初始化 OpenTelemetry 链路追踪，从配置读取 service_name 和 exporter
	// shutdown 必须在进程退出前调用，确保所有 Span 都被 flush
	tracingShutdown, err := tracing.Init(tracing.Config{
		Enabled:     cfg.Tracing.Enabled,
		ServiceName: cfg.Tracing.ServiceName,
		Exporter:    cfg.Tracing.Exporter,
	})
	if err != nil {
		logger.Error("链路追踪初始化失败", zap.Error(err))
		log.Fatal("链路追踪初始化失败: ", err)
	}
	defer func() {
		if err := tracingShutdown(nil); err != nil {
			logger.Error("链路追踪关闭失败", zap.Error(err))
		}
	}()
	logger.Info("链路追踪初始化成功",
		zap.Bool("enabled", cfg.Tracing.Enabled),
		zap.String("exporter", cfg.Tracing.Exporter),
	)

	db, err := database.NewMySQLDB(cfg.Database)
	if err != nil {
		logger.Error("MySQL数据库连接池初始化失败", zap.Error(err))
		log.Fatal("MySQL数据库连接池初始化失败: ", err)
	}
	defer db.Close()
	logger.Info("数据库连接池初始化成功")

	gin.SetMode(cfg.Server.Mode)
	logger.Info("配置加载成功",
		zap.String("mode", cfg.Server.Mode),
		zap.String("port", cfg.Server.Port),
	)

	// 从配置解析熔断器参数，统一在 circuit_breaker 块管理，不再散落在代码里
	openaiCBCfg := cfg.CircuitBreaker.OpenAI
	openaiWindow, _ := time.ParseDuration(openaiCBCfg.WindowSize)
	openaiReset, _ := time.ParseDuration(openaiCBCfg.ResetTimeout)
	if openaiWindow == 0 {
		openaiWindow = 60 * time.Second // 配置缺失时的安全默认值
	}
	if openaiReset == 0 {
		openaiReset = 30 * time.Second
	}
	minReqOpenAI := openaiCBCfg.MinRequests
	if minReqOpenAI == 0 {
		minReqOpenAI = 5
	}
	failRateOpenAI := openaiCBCfg.FailureRate
	if failRateOpenAI == 0 {
		failRateOpenAI = 0.5
	}

	qdrantCBCfg := cfg.CircuitBreaker.Qdrant
	qdrantWindow, _ := time.ParseDuration(qdrantCBCfg.WindowSize)
	qdrantReset, _ := time.ParseDuration(qdrantCBCfg.ResetTimeout)
	if qdrantWindow == 0 {
		qdrantWindow = 30 * time.Second
	}
	if qdrantReset == 0 {
		qdrantReset = 15 * time.Second
	}
	minReqQdrant := qdrantCBCfg.MinRequests
	if minReqQdrant == 0 {
		minReqQdrant = 3
	}
	failRateQdrant := qdrantCBCfg.FailureRate
	if failRateQdrant == 0 {
		failRateQdrant = 0.6
	}

	embeddingBreaker := circuitbreaker.NewCircuitBreaker(openaiWindow, int(minReqOpenAI), failRateOpenAI, openaiReset)
	embeddingClient := openai.NewEmbeddingClient(cfg.OpenAI).WithBreaker(embeddingBreaker)
	logger.Info("初始化 EmbeddingClient 成功")

	qdrantClient, err := qdrant.NewClient(cfg.Qdrant.Host, cfg.Qdrant.Port)
	if err != nil {
		logger.Error("qdrantClient初始化失败", zap.Error(err))
		log.Fatal("qdrantClient初始化失败: ", err)
	}
	qdrantBreaker := circuitbreaker.NewCircuitBreaker(qdrantWindow, int(minReqQdrant), failRateQdrant, qdrantReset)
	qdrantClient.WithBreaker(qdrantBreaker)
	defer qdrantClient.Close()
	logger.Info("初始化 QdrantClient 成功")

	// chat 熔断器复用 openai 的配置参数（同一个服务）
	chatBreaker := circuitbreaker.NewCircuitBreaker(openaiWindow, int(minReqOpenAI), failRateOpenAI, openaiReset)
	openAIClient := openai.NewChatClient(cfg.OpenAI).WithBreaker(chatBreaker)
	logger.Info("初始化 ChatClient 成功")

	// 后台定时上报熔断器状态到 Prometheus（每 5 秒采样一次）
	// circuitbreaker.State: 0=Closed(正常) 1=Open(熔断) 2=HalfOpen(半开)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			metrics.CircuitBreakerState.WithLabelValues("embedding").Set(float64(embeddingBreaker.GetState()))
			metrics.CircuitBreakerState.WithLabelValues("qdrant").Set(float64(qdrantBreaker.GetState()))
			metrics.CircuitBreakerState.WithLabelValues("chat").Set(float64(chatBreaker.GetState()))
		}
	}()
	var redisClient *cache.Client
	if cfg.Redis.Enabled {
		redisClient, err = cache.NewClient(cfg.Redis)
		if err != nil {
			logger.Error("初始化redisClient失败", zap.Error(err))
			redisClient.IsEnabled(false)
			//log.Fatal("初始化redisClient失败: ", err)
		}
		logger.Info("初始化 RedisClient 成功")
	} else {
		redisClient = &cache.Client{
			Enabled: false,
		}

	}

	workerPool := pool.NewWorkerPool(20, 1000)
	defer workerPool.Shutdown()
	logger.Info("初始化 WorkerPool 成功")

	// 将三个熔断器打包成 map 传给健康检查 handler
	breakers := map[string]*circuitbreaker.CircuitBreaker{
		"embedding": embeddingBreaker,
		"qdrant":    qdrantBreaker,
		"chat":      chatBreaker,
	}

	// 限流器（100 rps，burst=200）在 SetupRouter 内部注册，路由前生效
	r := router.SetupRouter(db, embeddingClient, qdrantClient, openAIClient, redisClient, workerPool, breakers, &cfg.Metrics)

	addr := ":" + cfg.Server.Port
	logger.Info("HTTP 服务启动", zap.String("addr", addr))
	if err = r.Run(addr); err != nil {
		logger.Error("服务启动失败", zap.Error(err))
		log.Fatal("服务启动失败: ", err)
	}
}
