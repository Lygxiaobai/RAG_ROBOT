package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"rag_robot/internal/pkg/errors"
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
		Fail(c, errors.ErrInvalidParam.Wrap(err))
		return
	}
	sessionID := h.svc.CreateSession(req.KnowledgeBaseID)
	Success(c, gin.H{"session_id": sessionID})
}

type chatRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Message   string `json:"message"    binding:"required"`
}

// Chat 多轮对话（SSE 流式响应）
// 注意：SSE 一旦写入响应头就无法再走 JSON 错误响应，
// 参数校验必须在写 SSE headers 之前完成。
func (h *ChatHandler) Chat(c *gin.Context) {
	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 此时 headers 还未写出，可以正常返回 JSON 错误
		Fail(c, errors.ErrInvalidParam.Wrap(err))
		return
	}

	// 设置 SSE 响应头（写完之后就不能再改状态码了）
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
		// SSE 已开流，只能在流中发送错误事件，无法改 HTTP 状态码
		fmt.Fprintf(c.Writer, "data: [ERROR] %s\n\n", err.Error())
		c.Writer.Flush()
		return
	}

	// 流结束标志
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
