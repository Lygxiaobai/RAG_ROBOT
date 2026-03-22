package database

import (
	"context"
	"database/sql"
	"fmt"
	"rag_robot/internal/model"
	"time"
)

type ConversationRepo struct {
	db *sql.DB
}

func NewConversationRepo(db *sql.DB) *ConversationRepo {
	return &ConversationRepo{db: db}
}

// Create 创建会话记录，返回自增 ID。
func (r *ConversationRepo) Create(ctx context.Context, conv *model.Conversation) (int64, error) {
	now := time.Now()
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO conversations (user_id, knowledge_base_id, session_id, title, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		nullInt64(conv.UserID),
		conv.KnowledgeBaseID,
		conv.SessionID,
		conv.Title,
		conv.Status,
		now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("创建会话记录失败: %w", err)
	}
	return result.LastInsertId()
}

// GetBySessionID 根据 session_id 查询会话，不存在返回 nil, nil。
func (r *ConversationRepo) GetBySessionID(ctx context.Context, sessionID string) (*model.Conversation, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, knowledge_base_id, session_id, title, status, last_question_at, created_at, updated_at
		FROM conversations
		WHERE session_id = ?
	`, sessionID)

	conv := &model.Conversation{}
	err := row.Scan(
		&conv.ID,
		&conv.UserID,
		&conv.KnowledgeBaseID,
		&conv.SessionID,
		&conv.Title,
		&conv.Status,
		&conv.LastQuestionAt,
		&conv.CreatedAt,
		&conv.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询会话失败: %w", err)
	}
	return conv, nil
}

// UpdateLastQuestionAt 更新会话最后提问时间。
func (r *ConversationRepo) UpdateLastQuestionAt(ctx context.Context, id int64, t time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE conversations SET last_question_at = ?, updated_at = ? WHERE id = ?`, t, time.Now(), id)
	return err
}

// CreateMessage 创建消息记录。
func (r *ConversationRepo) CreateMessage(ctx context.Context, msg *model.ConversationMessage) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO messages (conversation_id, role, content, token_count, created_at)
		VALUES (?, ?, ?, ?, ?)
	`,
		msg.ConversationID,
		msg.Role,
		msg.Content,
		msg.TokenCount,
		time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("创建消息记录失败: %w", err)
	}
	return result.LastInsertId()
}

// ListMessages 查询会话最近的消息，按时间正序返回。
func (r *ConversationRepo) ListMessages(ctx context.Context, conversationID int64, limit int) ([]*model.ConversationMessage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at
		FROM messages
		WHERE conversation_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询消息列表失败: %w", err)
	}
	defer rows.Close()

	var msgs []*model.ConversationMessage
	for rows.Next() {
		m := &model.ConversationMessage{}
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.TokenCount, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("扫描消息记录失败: %w", err)
		}
		msgs = append(msgs, m)
	}

	// 反转为时间正序
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}
