package database

import (
	"context"
	"database/sql"
	"fmt"
	"rag_robot/internal/model"
	"strings"
	"time"
)

type DocumentRepo struct {
	db *sql.DB
}

func NewDocumentRepo(db *sql.DB) *DocumentRepo {
	return &DocumentRepo{db: db}
}

// CreateDocument 创建文档记录，返回文档 ID 和是否为新建记录。
// 若 content_hash 已存在（重复上传同一文件），返回已有记录的 ID，并将 created 置为 false。
func (r *DocumentRepo) CreateDocument(ctx context.Context, doc *model.Document) (int64, bool, error) {
	query := `
          INSERT INTO documents (knowledge_base_id, name, file_type, file_size, file_path, status, content_hash, created_at, updated_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
      `
	now := time.Now()
	result, err := r.db.ExecContext(ctx, query,
		doc.KnowledgeBaseID,
		doc.Name,
		doc.FileType,
		doc.FileSize,
		doc.FilePath,
		"pending",
		doc.ContentHash,
		now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			// 同知识库下重复上传相同内容时，直接复用已有文档记录。
			var existID int64
			row := r.db.QueryRowContext(ctx,
				`SELECT id FROM documents WHERE knowledge_base_id = ? AND content_hash = ? LIMIT 1`,
				doc.KnowledgeBaseID,
				doc.ContentHash,
			)
			if scanErr := row.Scan(&existID); scanErr == nil {
				return existID, false, nil
			}

			// 兼容当前旧索引仍是全局 content_hash 唯一的情况，给出更明确的报错。
			var existingKBID int64
			row = r.db.QueryRowContext(ctx,
				`SELECT knowledge_base_id FROM documents WHERE content_hash = ? LIMIT 1`,
				doc.ContentHash,
			)
			if scanErr := row.Scan(&existingKBID); scanErr == nil {
				return 0, false, fmt.Errorf("相同内容的文档已存在于知识库 %d，请先调整 documents 唯一索引为 (knowledge_base_id, content_hash)", existingKBID)
			}
		}
		return 0, false, fmt.Errorf("创建文档记录失败: %w", err)
	}

	// 走到这里说明本次是新建文档记录，需要把自增 ID 返回给上层继续处理。
	id, idErr := result.LastInsertId()
	if idErr != nil {
		return 0, false, fmt.Errorf("获取文档 ID 失败: %w", idErr)
	}
	return id, true, nil
}

// isUniqueViolation 判断是否为 MySQL 唯一键冲突错误（Error 1062）。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "1062") || strings.Contains(err.Error(), "Duplicate entry")
}

// UpdateDocumentStatus 更新文档状态和分块数量。
func (r *DocumentRepo) UpdateDocumentStatus(ctx context.Context, id int64, status string, chunkCount int) error {
	query := `UPDATE documents SET status = ?, chunk_count = ?, updated_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, status, chunkCount, time.Now(), id)
	return err
}

// BatchCreateChunks 批量插入文档分块，并回填每个分块的自增 ID。
func (r *DocumentRepo) BatchCreateChunks(ctx context.Context, chunks []*model.DocumentChunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
          INSERT INTO document_chunks (document_id, knowledge_base_id, chunk_index, content, content_length, created_at)
          VALUES (?, ?, ?, ?, ?, ?)
      `)
	if err != nil {
		return fmt.Errorf("准备 SQL 失败: %w", err)
	}
	defer stmt.Close()

	for _, chunk := range chunks {
		result, execErr := stmt.ExecContext(ctx,
			chunk.DocumentID,
			chunk.KnowledgeBaseID,
			chunk.ChunkIndex,
			chunk.Content,
			len([]rune(chunk.Content)),
			time.Now(),
		)
		if execErr != nil {
			return fmt.Errorf("插入分块失败: %w", execErr)
		}

		chunkID, idErr := result.LastInsertId()
		if idErr != nil {
			return fmt.Errorf("获取分块 ID 失败: %w", idErr)
		}
		chunk.ID = chunkID
	}

	return tx.Commit()
}

// UpdateChunkQdrantID 回写 Qdrant Point ID 到分块记录。
func (r *DocumentRepo) UpdateChunkQdrantID(ctx context.Context, chunkID int64, qdrantPointID string) error {
	query := `UPDATE document_chunks SET qdrant_point_id = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, qdrantPointID, chunkID)
	return err
}

// GetDocumentByID 查询单条文档记录。
func (r *DocumentRepo) GetDocumentByID(ctx context.Context, id int64) (*model.Document, error) {
	query := `SELECT id, knowledge_base_id, name, file_type, file_size, file_path, status, chunk_count, content_hash, created_at, updated_at FROM documents WHERE id = ?`
	row := r.db.QueryRowContext(ctx, query, id)
	doc := &model.Document{}
	err := row.Scan(
		&doc.ID, &doc.KnowledgeBaseID, &doc.Name, &doc.FileType,
		&doc.FileSize, &doc.FilePath, &doc.Status, &doc.ChunkCount,
		&doc.ContentHash, &doc.CreatedAt, &doc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询文档失败: %w", err)
	}
	return doc, nil
}

// GetChunkByDocumentAndIndex 根据 document_id 和 chunk_index 查询分块。
func (r *DocumentRepo) GetChunkByDocumentAndIndex(ctx context.Context, documentID int64, chunkIndex int) (*model.DocumentChunk, error) {
	query := `
		SELECT id, document_id, knowledge_base_id, chunk_index, content, content_length, qdrant_point_id, created_at
		FROM document_chunks
		WHERE document_id = ? AND chunk_index = ?
		LIMIT 1
	`
	row := r.db.QueryRowContext(ctx, query, documentID, chunkIndex)

	chunk := &model.DocumentChunk{}
	err := row.Scan(
		&chunk.ID,
		&chunk.DocumentID,
		&chunk.KnowledgeBaseID,
		&chunk.ChunkIndex,
		&chunk.Content,
		&chunk.ContentLength,
		&chunk.QdrantPointID,
		&chunk.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询分块失败: %w", err)
	}
	return chunk, nil
}

// DeleteDocument 删除文档记录及其所有分块。
func (r *DocumentRepo) DeleteDocument(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, `DELETE FROM document_chunks WHERE document_id = ?`, id); err != nil {
		return fmt.Errorf("删除分块失败: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除文档失败: %w", err)
	}
	return tx.Commit()
}

// DeleteChunksByDocumentID 删除指定文档的所有分块（用于重新处理文档）
func (r *DocumentRepo) DeleteChunksByDocumentID(ctx context.Context, documentID int64) error {
	query := `DELETE FROM document_chunks WHERE document_id = ?`
	_, err := r.db.ExecContext(ctx, query, documentID)
	if err != nil {
		return fmt.Errorf("删除文档分块失败: %w", err)
	}
	return nil
}

// FullTextSearchChunk 是全文检索返回的分块结构，与 SearchResult 字段对应。
type FullTextSearchChunk struct {
	ID         int64
	DocumentID int64
	ChunkIndex int
	Content    string
}

// FullTextSearch 对 document_chunks 表按 content LIKE 做全文检索兜底。
// 仅在 Qdrant 不可用时作为降级方案使用。
// 按 content_length 升序（内容越短越精准），最多返回 limit 条。
func (r *DocumentRepo) FullTextSearch(ctx context.Context, kbID int64, query string, limit int) ([]*FullTextSearchChunk, error) {
	// LIKE 检索：%keyword%，简单有效，无需额外索引
	// kbID 过滤保证只检索当前知识库的内容
	sql := `
		SELECT id, document_id, chunk_index, content
		FROM document_chunks
		WHERE knowledge_base_id = ?
		  AND content LIKE ?
		ORDER BY content_length ASC
		LIMIT ?
	`
	// 构造 LIKE 参数，关键词两侧加 %
	keyword := "%" + query + "%"
	rows, err := r.db.QueryContext(ctx, sql, kbID, keyword, limit)
	if err != nil {
		return nil, fmt.Errorf("全文检索失败: %w", err)
	}
	defer rows.Close()

	var results []*FullTextSearchChunk
	for rows.Next() {
		chunk := &FullTextSearchChunk{}
		if err := rows.Scan(&chunk.ID, &chunk.DocumentID, &chunk.ChunkIndex, &chunk.Content); err != nil {
			return nil, fmt.Errorf("扫描检索结果失败: %w", err)
		}
		results = append(results, chunk)
	}
	return results, rows.Err()
}
