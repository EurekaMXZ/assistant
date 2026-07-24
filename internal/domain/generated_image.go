package domain

import "time"

const (
	GeneratedImageKindPartial = "partial"
	GeneratedImageKindFinal   = "final"

	GeneratedImageStatusReady    = "ready"
	GeneratedImageStatusDeleting = "deleting"
)

type GeneratedImageAsset struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	TurnID         string     `json:"turn_id"`
	TurnRunID      string     `json:"turn_run_id"`
	ResponseID     string     `json:"response_id,omitempty"`
	ItemID         string     `json:"item_id"`
	Kind           string     `json:"kind"`
	Revision       int        `json:"revision"`
	Status         string     `json:"status"`
	ObjectKey      string     `json:"-"`
	ContentType    string     `json:"content_type"`
	SizeBytes      int64      `json:"size_bytes"`
	SHA256         string     `json:"sha256"`
	Width          int        `json:"width"`
	Height         int        `json:"height"`
	AttachmentID   string     `json:"attachment_id,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type UpsertGeneratedImageAssetParams struct {
	ID             string
	ConversationID string
	TurnID         string
	TurnRunID      string
	ResponseID     string
	ItemID         string
	Kind           string
	Revision       int
	ObjectKey      string
	ContentType    string
	SizeBytes      int64
	SHA256         string
	Width          int
	Height         int
	AttachmentID   string
	ExpiresAt      *time.Time
}
