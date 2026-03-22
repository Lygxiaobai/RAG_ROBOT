package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// 一条问答 会有一条记录 （不论是qa 还是chat）
type QARecord struct {
	KnowledgeBaseID *int64
	ConversationID  *int64
	UserID          *int64
	QuestionText    string
	QuestionHash    *string
	AnswerText      string
	AnswerModel     string
	AnswerLatencyMS int
	RetrievalCount  int //   检索的分片数量 topk
	TopScore        *float64
	Status          string
	ErrorMessage    *string
}

// 每条topk对应一条source  一条记录对应多条source
type QASource struct {
	DocumentID int64
	ChunkID    int64
	ChunkIndex int
	RankNo     int //排名第几
	Score      *float64
}

type QARepo struct {
	db      *sql.DB
	docRepo *DocumentRepo
}

func NewQARepo(db *sql.DB) *QARepo {
	return &QARepo{
		db:      db,
		docRepo: NewDocumentRepo(db),
	}
}

// CreateRecordWithSources 创建问答记录并落来源片段。
func (r *QARepo) CreateRecordWithSources(ctx context.Context, record *QARecord, sources []QASource) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开启问答记录事务失败: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO qa_records (
			knowledge_base_id, conversation_id, user_id, question_text, question_hash,
			answer_text, answer_model, answer_latency_ms, retrieval_count, top_score,
			status, error_message, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		nullInt64(record.KnowledgeBaseID),
		nullInt64(record.ConversationID),
		nullInt64(record.UserID),
		record.QuestionText,
		record.QuestionHash,
		record.AnswerText,
		record.AnswerModel,
		record.AnswerLatencyMS,
		record.RetrievalCount,
		record.TopScore,
		record.Status,
		record.ErrorMessage,
		time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("创建问答记录失败: %w", err)
	}

	qaRecordID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取问答记录 ID 失败: %w", err)
	}

	if len(sources) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO qa_sources (qa_record_id, document_id, chunk_id, rank_no, score, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return 0, fmt.Errorf("准备答案来源写入失败: %w", err)
		}
		defer stmt.Close()

		now := time.Now()
		for _, source := range sources {
			chunkID := source.ChunkID
			if chunkID <= 0 {
				chunk, findErr := r.docRepo.GetChunkByDocumentAndIndex(ctx, source.DocumentID, source.ChunkIndex)
				if findErr != nil {
					return 0, fmt.Errorf("回查来源分块失败: %w", findErr)
				}
				if chunk == nil {
					continue
				}
				chunkID = chunk.ID
			}

			if _, err = stmt.ExecContext(ctx,
				qaRecordID,
				source.DocumentID,
				chunkID,
				source.RankNo,
				source.Score,
				now,
			); err != nil {
				return 0, fmt.Errorf("写入答案来源失败: %w", err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交问答记录事务失败: %w", err)
	}
	return qaRecordID, nil
}

// QAFeedback 用户对问答结果的反馈。
type QAFeedback struct {
	QARecordID int64
	UserID     *int64
	Rating     int8
	Comment    *string
}

// CreateFeedback 写入用户反馈记录。
func (r *QARepo) CreateFeedback(ctx context.Context, fb *QAFeedback) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO qa_feedback (qa_record_id, user_id, rating, comment, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, fb.QARecordID, nullInt64(fb.UserID), fb.Rating, fb.Comment, time.Now())
	if err != nil {
		return fmt.Errorf("创建用户反馈失败: %w", err)
	}
	return nil
}

func nullInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
