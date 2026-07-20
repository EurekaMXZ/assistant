package domain

import (
	"encoding/json"
	"time"
)

type Conversation struct {
	ID          string          `json:"id"`
	OwnerUserID string          `json:"owner_user_id,omitempty"`
	Title       string          `json:"title,omitempty"`
	Status      string          `json:"status"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	ArchivedAt  *time.Time      `json:"archived_at,omitempty"`
	DeletedAt   *time.Time      `json:"deleted_at,omitempty"`
}
