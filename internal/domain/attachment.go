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
)

type Attachment struct {
	ID               string          `json:"id"`
	ConversationID   string          `json:"conversation_id"`
	UploadedByUserID string          `json:"uploaded_by_user_id"`
	Filename         string          `json:"filename"`
	ContentType      string          `json:"content_type"`
	Category         string          `json:"category"`
	SizeBytes        int64           `json:"size_bytes"`
	SHA256           string          `json:"sha256"`
	ObjectKey        string          `json:"object_key,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}
