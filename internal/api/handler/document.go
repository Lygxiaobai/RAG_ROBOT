package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "knowledge_base_id 无效",
		})
		return
	}

	// 2. 获取上传的文件（最大 50MB）
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "文件获取失败: " + err.Error(),
		})
		return
	}
	defer file.Close()

	//3. 调用 Service 处理
	result, err := h.svc.ProcessDocument(
		c.Request.Context(),
		file,
		header.Filename,
		header.Size,
		kbID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "success",
		"data":    result,
	})
}

// Delete 处理文档删除请求
func (h *DocumentHandler) Delete(c *gin.Context) {
	docID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || docID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "document id 无效",
		})
		return
	}

	if err = h.svc.DeleteDocument(c.Request.Context(), docID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "success",
	})
}
