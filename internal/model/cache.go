package model

// SearchHit 向量检索命中结果，用于 VectorCache 序列化存储。
// 字段与 search.SearchResult 保持一致。
type SearchHit struct {
	ChunkID    int64   `json:"chunk_id"`
	Score      float32 `json:"score"`
	DocumentID int64   `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"`
}

// QAResult 问答结果，用于 QACache 序列化存储。
// 字段与 qa.AskResponse 保持一致。
type QAResult struct {
	QARecordID int64         `json:"qa_record_id"`
	Answer     string        `json:"answer"`
	Sources    []QASourceHit `json:"sources"`
}

// QASourceHit 问答来源片段，对应 qa.SourceChunk。
type QASourceHit struct {
	ChunkID    int64   `json:"chunk_id"`
	DocumentID int64   `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"`
	Score      float32 `json:"score"`
}
