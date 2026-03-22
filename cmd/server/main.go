package main

import (
	"log"
	"time"

	"rag_robot/internal/pkg/circuitbreaker"
	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/pkg/pool"
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

	embeddingClient := openai.NewEmbeddingClient(cfg.OpenAI).WithBreaker(
		circuitbreaker.NewCircuitBreaker(60*time.Second, 5, 0.5, 30*time.Second),
	)
	logger.Info("初始化 EmbeddingClient 成功")

	qdrantClient, err := qdrant.NewClient(cfg.Qdrant.Host, cfg.Qdrant.Port)
	if err != nil {
		logger.Error("qdrantClient初始化失败", zap.Error(err))
		log.Fatal("qdrantClient初始化失败: ", err)
	}
	qdrantClient.WithBreaker(circuitbreaker.NewCircuitBreaker(30*time.Second, 3, 0.5, 15*time.Second))
	defer qdrantClient.Close()
	logger.Info("初始化 QdrantClient 成功")

	openAIClient := openai.NewChatClient(cfg.OpenAI).WithBreaker(
		circuitbreaker.NewCircuitBreaker(60*time.Second, 5, 0.5, 30*time.Second),
	)
	logger.Info("初始化 ChatClient 成功")

	redisClient, err := cache.NewClient(cfg.Redis)
	if err != nil {
		logger.Error("初始化redisClient失败", zap.Error(err))
		log.Fatal("初始化redisClient失败: ", err)
	}
	logger.Info("初始化 RedisClient 成功")

	workerPool := pool.NewWorkerPool(20, 1000)
	defer workerPool.Shutdown()
	logger.Info("初始化 WorkerPool 成功")

	// 限流器（100 rps，burst=200）在 SetupRouter 内部注册，路由前生效
	r := router.SetupRouter(db, embeddingClient, qdrantClient, openAIClient, redisClient, workerPool)

	addr := ":" + cfg.Server.Port
	logger.Info("HTTP 服务启动", zap.String("addr", addr))
	if err = r.Run(addr); err != nil {
		logger.Error("服务启动失败", zap.Error(err))
		log.Fatal("服务启动失败: ", err)
	}
}
