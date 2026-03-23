package handler

import (
	"github.com/gin-gonic/gin"
	"rag_robot/internal/pkg/errors"
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
		Fail(c, errors.ErrInvalidParam.Wrap(err))
		return
	}

	// 单次问答，使用 Cache
	resp, err := h.svc.AskQuestion(c.Request.Context(), &qa.AskRequest{
		Question:        req.Question,
		KnowledgeBaseID: req.KnowledgeBaseID,
		TopK:            req.TopK,
	})
	if err != nil {
		Fail(c, err)
		return
	}

	Success(c, resp)
}

type feedbackRequest struct {
	QARecordID int64  `json:"qa_record_id" binding:"required,min=1"`
	Rating     int8   `json:"rating"       binding:"required,min=1,max=3"`
	Comment    string `json:"comment"`
}

func (h *QAHandler) Feedback(c *gin.Context) {
	var req feedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, errors.ErrInvalidParam.Wrap(err))
		return
	}

	if err := h.svc.SubmitFeedback(c.Request.Context(), &qa.FeedbackRequest{
		QARecordID: req.QARecordID,
		Rating:     req.Rating,
		Comment:    req.Comment,
	}); err != nil {
		Fail(c, err)
		return
	}

	Success(c, nil)
}
