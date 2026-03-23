package handler

import (
	"github.com/gin-gonic/gin"
	"rag_robot/internal/pkg/errors"
	"rag_robot/internal/service/search"
)

type SearchHandler struct {
	svc *search.Service
}

func NewSearchHandler(svc *search.Service) *SearchHandler {
	return &SearchHandler{svc: svc}
}

type searchRequest struct {
	Query           string `json:"query"            binding:"required"`
	KnowledgeBaseID int64  `json:"knowledge_base_id" binding:"required,min=1"`
	TopK            int    `json:"top_k"`
}

// Search 处理向量检索请求
func (h *SearchHandler) Search(c *gin.Context) {
	var req searchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, errors.ErrInvalidParam.Wrap(err))
		return
	}

	results, err := h.svc.Search(c.Request.Context(), req.Query, req.KnowledgeBaseID, req.TopK)
	if err != nil {
		Fail(c, err)
		return
	}

	Success(c, results)
}
