package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"rag_robot/internal/pkg/errors"
	"rag_robot/internal/service/document"
)

type DocumentHandler struct {
	svc *document.Service
}

func NewDocumentHandler(svc *document.Service) *DocumentHandler {
	return &DocumentHandler{svc: svc}
}

// Upload 处理文档上传请求
func (h *DocumentHandler) Upload(c *gin.Context) {
	// 1. 解析 knowledge_base_id
	kbIDStr := c.PostForm("knowledge_base_id")
	kbID, err := strconv.ParseInt(kbIDStr, 10, 64)
	if err != nil || kbID <= 0 {
		Fail(c, errors.ErrInvalidKBID)
		return
	}

	// 2. 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		Fail(c, errors.ErrFileGetFailed.Wrap(err))
		return
	}
	defer file.Close()

	// 3. 调用 Service 处理
	result, err := h.svc.ProcessDocument(
		c.Request.Context(),
		file,
		header.Filename,
		header.Size,
		kbID,
	)
	if err != nil {
		Fail(c, err)
		return
	}

	Success(c, result)
}

// Delete 处理文档删除请求
func (h *DocumentHandler) Delete(c *gin.Context) {
	docID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || docID <= 0 {
		Fail(c, errors.ErrInvalidDocumentID)
		return
	}

	if err = h.svc.DeleteDocument(c.Request.Context(), docID); err != nil {
		Fail(c, err)
		return
	}

	Success(c, nil)
}
