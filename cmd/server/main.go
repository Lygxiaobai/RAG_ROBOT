package main

import (
	"log"
	"rag_robot/internal/pkg/openai"
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
	//初始化openAIClient
	embeddingClient := openai.NewEmbeddingClient(cfg.OpenAI)
	logger.Info("初始化openAIClient 成功")

	//初始化qdrantClient
	qdrantClient, err := qdrant.NewClient(cfg.Qdrant.Host, cfg.Qdrant.Port)
	if err != nil {
		logger.Error("qdrantClient初始化失败", zap.Error(err))
		log.Fatal("qdrantClient初始化失败: ", err)
	}
	defer qdrantClient.Close()
	logger.Info("初始化qdrantClient 成功")

	r := router.SetupRouter(db, embeddingClient, qdrantClient)

	addr := ":" + cfg.Server.Port
	logger.Info("HTTP 服务启动", zap.String("addr", addr))
	if err = r.Run(addr); err != nil {
		logger.Error("服务启动失败", zap.Error(err))
		log.Fatal("服务启动失败: ", err)
	}
}
