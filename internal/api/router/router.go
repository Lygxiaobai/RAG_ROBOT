package router

import (
	"database/sql"
	"rag_robot/internal/pkg/pool"
	"rag_robot/internal/pkg/ratelimit"
	"rag_robot/internal/repository/cache"

	"github.com/gin-gonic/gin"
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

func SetupRouter(db *sql.DB, embedClient *openai.EmbeddingClient, qdrantClient *qdrant.Client, openAIClient *openai.ChatClient, redisClient *cache.Client, workerPool *pool.WorkerPool) *gin.Engine {
	r := gin.Default()
	r.MaxMultipartMemory = 50 << 20
	r.Use(middleware.CORS())
	r.Use(middleware.RateLimitMiddleware(ratelimit.NewLimiter(100, 200)))

	healthHandler := handler.NewHealthHandler(db)
	r.GET("/health", healthHandler.Check)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", healthHandler.Check)

		docRepo := database.NewDocumentRepo(db)

		searchSvc := search.NewService(embedClient, qdrantClient, cache.NewVectorCache(redisClient))

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
