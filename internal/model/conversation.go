package model

import "time"

// Conversation 对应数据库 conversations 表
type Conversation struct {
	ID              int64      `json:"id"`
	UserID          *int64     `json:"user_id"`
	KnowledgeBaseID int64      `json:"knowledge_base_id"`
	SessionID       string     `json:"session_id"`
	Title           *string    `json:"title"`
	Status          string     `json:"status"` // active/archived/closed
	LastQuestionAt  *time.Time `json:"last_question_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ConversationMessage 对应数据库 messages 表
type ConversationMessage struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Role           string    `json:"role"` // user/assistant
	Content        string    `json:"content"`
	TokenCount     int       `json:"token_count"`
	CreatedAt      time.Time `json:"created_at"`
}
