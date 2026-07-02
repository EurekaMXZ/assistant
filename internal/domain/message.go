package domain

import (
	"encoding/json"
	"time"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	TurnID         string          `json:"turn_id,omitempty"`
	Seq            int64           `json:"seq"`
	Role           string          `json:"role"`
	ContentText    string          `json:"content_text,omitempty"`
	TokenCount     *int            `json:"token_count,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"created_at"`
}

type AssistantMessageDraft struct {
	ContentText string          `json:"content_text"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}
