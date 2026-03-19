package model

import "time"

// ========== 数据库模型 ==========
// Document 对应数据库 documents 表
type Document struct {
	ID              int64     `json:"id"`
	KnowledgeBaseID int64     `json:"knowledge_base_id"`
	Name            string    `json:"name"`
	FileType        string    `json:"file_type"`    // pdf, word, txt, md
	FileSize        int64     `json:"file_size"`    // 字节数
	FilePath        string    `json:"file_path"`    // 存储路径
	Status          string    `json:"status"`       // pending/processing/completed/failed
	ChunkCount      int       `json:"chunk_count"`  // 分块数量
	ContentHash     string    `json:"content_hash"` // MD5，防重复
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// DocumentChunk 对应数据库 document_chunks 表
type DocumentChunk struct {
	ID              int64     `json:"id"`
	DocumentID      int64     `json:"document_id"`
	KnowledgeBaseID int64     `json:"knowledge_base_id"`
	ChunkIndex      int       `json:"chunk_index"` // 第几块，从0开始
	Content         string    `json:"content"`     // 文本内容
	ContentLength   int       `json:"content_length"`
	QdrantPointID   string    `json:"qdrant_point_id"` // 对应Qdrant的点ID
	CreatedAt       time.Time `json:"created_at"`
}

// ========== 请求/响应结构体 ==========

// UploadDocumentRequest 上传文档请求（通过 multipart/form-data）
type UploadDocumentRequest struct {
	KnowledgeBaseID int64 `form:"knowledge_base_id" binding:"required"`
}

// UploadDocumentResponse 上传文档响应
type UploadDocumentResponse struct {
	DocumentID int64  `json:"document_id"`
	Name       string `json:"name"`
	FileType   string `json:"file_type"`
	FileSize   int64  `json:"file_size"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
}

// ChunkConfig 分块配置
type ChunkConfig struct {
	ChunkSize    int // 每块最大字符数，建议 500
	ChunkOverlap int // 重叠字符数，建议 100
}
