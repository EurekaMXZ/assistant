package domain

import (
	"encoding/json"
	"time"
)

const (
	TurnStatusAccepted     = "accepted"
	TurnStatusContextReady = "context_ready"
	TurnStatusProcessing   = "processing"
	TurnStatusCompleted    = "completed"
	TurnStatusFailed       = "failed"
)

type Turn struct {
	ID               string          `json:"id"`
	ConversationID   string          `json:"conversation_id"`
	Seq              int64           `json:"seq"`
	Status           string          `json:"status"`
	RequestBlobKey   string          `json:"request_blob_key,omitempty"`
	ResponseBlobKey  string          `json:"response_blob_key,omitempty"`
	StreamBlobKey    string          `json:"stream_blob_key,omitempty"`
	OpenAIResponseID string          `json:"openai_response_id,omitempty"`
	ErrorCode        string          `json:"error_code,omitempty"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	Metadata         json.RawMessage `json:"metadata"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
	FailedAt         *time.Time      `json:"failed_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type EnqueuedTurn struct {
	ConversationID string  `json:"conversation_id"`
	Message        Message `json:"message"`
	Turn           Turn    `json:"turn"`
}

type TurnRunSummary struct {
	RequestBlobKey  string
	ResponseBlobKey string
	StreamBlobKey   string
	ResponseID      string
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	Model           string
}
