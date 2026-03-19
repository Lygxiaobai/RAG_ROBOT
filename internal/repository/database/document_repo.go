package database

import (
	"context"
	"database/sql"
	"fmt"
	"rag_robot/internal/model"
	"time"
)

type DocumentRepo struct {
	db *sql.DB
}

func NewDocumentRepo(db *sql.DB) *DocumentRepo {
	return &DocumentRepo{db: db}
}

// CreateDocument 创建文档记录，返回新记录的ID
func (r *DocumentRepo) CreateDocument(ctx context.Context, doc *model.Document) (int64, error) {
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
		return 0, fmt.Errorf("创建文档记录失败: %w", err)
	}
	return result.LastInsertId()
}

// UpdateDocumentStatus 更新文档状态  chunkCount分块数量
func (r *DocumentRepo) UpdateDocumentStatus(ctx context.Context, id int64, status string, chunkCount int) error {
	query := `UPDATE documents SET status = ?, chunk_count = ?, updated_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, status, chunkCount, time.Now(), id)
	return err
}

// BatchCreateChunks 批量插入文档分块
func (r *DocumentRepo) BatchCreateChunks(ctx context.Context, chunks []*model.DocumentChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	// 使用事务保证原子性
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback() // 如果没有 Commit，自动回滚

	stmt, err := tx.PrepareContext(ctx, `
          INSERT INTO document_chunks (document_id, knowledge_base_id, chunk_index, content, content_length, created_at)
          VALUES (?, ?, ?, ?, ?, ?)
      `)
	if err != nil {
		return fmt.Errorf("准备SQL失败: %w", err)
	}
	defer stmt.Close()

	for _, chunk := range chunks {
		_, err = stmt.ExecContext(ctx,
			chunk.DocumentID,
			chunk.KnowledgeBaseID,
			chunk.ChunkIndex,
			chunk.Content,
			len([]rune(chunk.Content)),
			time.Now(),
		)
		if err != nil {
			return fmt.Errorf("插入分块失败: %w", err)
		}
	}

	return tx.Commit()
}
