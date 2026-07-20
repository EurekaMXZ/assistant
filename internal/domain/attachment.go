package domain

import (
	"encoding/json"
	"time"
)

const (
	AttachmentCategoryImage    = "image"
	AttachmentCategoryText     = "text"
	AttachmentCategoryDocument = "document"
	AttachmentCategoryBinary   = "binary"
	AttachmentStatusPending    = "pending"
	AttachmentStatusReady      = "ready"
	AttachmentStatusDeleting   = "deleting"
)

type Attachment struct {
	ID                string          `json:"id"`
	ConversationID    string          `json:"conversation_id"`
	UploadedByUserID  string          `json:"uploaded_by_user_id"`
	Filename          string          `json:"filename"`
	ContentType       string          `json:"content_type"`
	Category          string          `json:"category"`
	SizeBytes         int64           `json:"size_bytes"`
	SHA256            string          `json:"sha256"`
	ContentMD5        string          `json:"-"`
	Status            string          `json:"status"`
	ObjectKey         string          `json:"-"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	UploadCompletedAt *time.Time      `json:"upload_completed_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}
