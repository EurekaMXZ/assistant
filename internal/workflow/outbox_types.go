package workflow

import "time"

type OutboxEvent struct {
	ID             string     `json:"id"`
	EventType      string     `json:"event_type"`
	ConversationID string     `json:"conversation_id,omitempty"`
	TurnID         string     `json:"turn_id,omitempty"`
	TurnRunID      string     `json:"turn_run_id,omitempty"`
	ClaimToken     string     `json:"-"`
	ClaimedAt      *time.Time `json:"-"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}
