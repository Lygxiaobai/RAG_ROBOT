package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"rag_robot/internal/service/qa"
)

type QAHandler struct {
	svc *qa.Service
}

func NewQAHandler(svc *qa.Service) *QAHandler {
	return &QAHandler{svc: svc}
}

type askRequest struct {
	Question        string `json:"question"         binding:"required"`
	KnowledgeBaseID int64  `json:"knowledge_base_id" binding:"required,min=1"`
	TopK            int    `json:"top_k"`
}

func (h *QAHandler) Ask(c *gin.Context) {
	var req askRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}

	//单次会话 使用Cache
	resp, err := h.svc.AskQuestion(c.Request.Context(), &qa.AskRequest{
		Question:        req.Question,
		KnowledgeBaseID: req.KnowledgeBaseID,
		TopK:            req.TopK,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "success", "data": resp})
}

type feedbackRequest struct {
	QARecordID int64  `json:"qa_record_id" binding:"required,min=1"`
	Rating     int8   `json:"rating"       binding:"required,min=1,max=3"`
	Comment    string `json:"comment"`
}

func (h *QAHandler) Feedback(c *gin.Context) {
	var req feedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}

	if err := h.svc.SubmitFeedback(c.Request.Context(), &qa.FeedbackRequest{
		QARecordID: req.QARecordID,
		Rating:     req.Rating,
		Comment:    req.Comment,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "success"})
}
