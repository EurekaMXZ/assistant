package domain

import (
	"encoding/json"
	"time"
)

type TurnStreamEvent struct {
	ID             string          `json:"id"`
	TurnID         string          `json:"turn_id"`
	ConversationID string          `json:"conversation_id"`
	EventIndex     int64           `json:"event_index"`
	EventType      string          `json:"event_type"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
}
