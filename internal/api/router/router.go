package router

import (
	"database/sql"
	"github.com/gin-gonic/gin"
	"rag_robot/internal/api/handler"
)

func SetupRouter(db *sql.DB) *gin.Engine {
	r := gin.Default()

	//健康检查
	healthHandler := handler.NewHealthHandler(db)
	r.GET("/health", healthHandler.Check)

	//创建路由组
	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", healthHandler.Check)
	}
	return r
}
