package handler

import (
	"go.uber.org/zap"
	"net/http"
	"rag_robot/internal/pkg/errors"
	"rag_robot/internal/pkg/logger"

	"github.com/gin-gonic/gin"
)

// Response 统一响应结构（复用已有的，这里仅做示例）
type Response struct {
	Code    int         `json:"code"` // 业务错误码，200=成功
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Success 成功响应
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{Code: 200, Message: "success", Data: data})
}

// Fail 失败响应
// 自动识别 AppError，提取 HTTPCode 和业务 Code
func Fail(c *gin.Context, err error) {
	if appErr, ok := errors.IsAppError(err); ok {
		// 记录内部原因（Cause）到日志，不暴露给前端
		if appErr.Cause != nil {
			logger.Error("request failed",
				zap.Int("code", appErr.Code),
				zap.Error(appErr.Cause),
				zap.String("path", c.Request.URL.Path),
			)
		}
		c.JSON(appErr.HTTPCode, Response{
			Code:    appErr.Code,
			Message: appErr.Message,
		})
		return
	}
	// 未知错误，兜底返回 500
	logger.Error("unknown error", zap.Error(err),
		zap.String("path", c.Request.URL.Path))
	c.JSON(http.StatusInternalServerError, Response{
		Code:    errors.ErrInternalServer.Code,
		Message: errors.ErrInternalServer.Message,
	})
}
