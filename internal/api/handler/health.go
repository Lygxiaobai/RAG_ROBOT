package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"rag_robot/internal/pkg/logger"
)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Check(c *gin.Context) {
	if h.db == nil {
		logger.Error("数据库连接池未初始化")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "DOWN",
			"message": "database pool is not initialized",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		logger.Error("数据库连接失败", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "DOWN",
			"message": "database unavailable",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "OK",
		"message": "success",
	})
}
