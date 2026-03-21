package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"rag_robot/internal/service/chat"
)

type ChatHandler struct {
	svc *chat.Service
}

func NewChatHandler(svc *chat.Service) *ChatHandler {
	return &ChatHandler{svc: svc}
}

type createSessionRequest struct {
	KnowledgeBaseID int64 `json:"knowledge_base_id" binding:"required,min=1"`
}

// CreateSession 创建新会话
func (h *ChatHandler) CreateSession(c *gin.Context) {
	var req createSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	sessionID := h.svc.CreateSession(req.KnowledgeBaseID)
	c.JSON(http.StatusOK, gin.H{
		"code": 200, "message": "success",
		"data": gin.H{"session_id": sessionID},
	})
}

type chatRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Message   string `json:"message"    binding:"required"`
}

// Chat 多轮对话（SSE 流式响应）
func (h *ChatHandler) Chat(c *gin.Context) {
	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	err := h.svc.Chat(c.Request.Context(), req.SessionID, req.Message,
		func(chunk string) error {
			// 每个 token 以 SSE 格式写出
			_, werr := fmt.Fprintf(c.Writer, "data: %s\n\n", chunk)
			c.Writer.Flush()
			return werr
		},
	)

	if err != nil {
		fmt.Fprintf(c.Writer, "data: [ERROR] %s\n\n", err.Error())
		c.Writer.Flush()
		return
	}

	// 流结束标志
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
