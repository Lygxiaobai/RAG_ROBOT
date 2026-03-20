package router

import (
	"database/sql"
	"github.com/gin-gonic/gin"
	"rag_robot/internal/repository/qdrant"
	"rag_robot/internal/service/search"

	"rag_robot/internal/api/handler"
	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/repository/database"
	"rag_robot/internal/service/document"
)

func SetupRouter(db *sql.DB, embedClient *openai.EmbeddingClient, qdrantClient *qdrant.Client) *gin.Engine {
	r := gin.Default()
	//设置文件上传大小 50MB
	r.MaxMultipartMemory = 50 << 20

	//健康检查
	healthHandler := handler.NewHealthHandler(db)
	r.GET("/health", healthHandler.Check)

	//创建路由组
	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", healthHandler.Check)

		//文档相关路由
		docRepo := database.NewDocumentRepo(db)
		docSvc := document.NewService(docRepo, embedClient, qdrantClient)
		docHandler := handler.NewDocumentHandler(docSvc)
		v1.POST("/document/upload", docHandler.Upload)

		//检索路由
		searchSvc := search.NewService(embedClient, qdrantClient)
		searchHandler := handler.NewSearchHandler(searchSvc)
		v1.POST("/search", searchHandler.Search)
	}
	return r
}
