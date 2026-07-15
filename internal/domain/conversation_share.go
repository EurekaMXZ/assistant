package domain

import "time"

type ConversationShare struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversation_id"`
	CreatedByUserID string    `json:"created_by_user_id"`
	Title           string    `json:"title,omitempty"`
	LastMessageSeq  int64     `json:"last_message_seq"`
	CreatedAt       time.Time `json:"created_at"`
}

type ConversationShareSnapshot struct {
	ID             string    `json:"id"`
	Title          string    `json:"title,omitempty"`
	LastMessageSeq int64     `json:"last_message_seq"`
	CreatedAt      time.Time `json:"created_at"`
	Messages       []Message `json:"messages"`
}
