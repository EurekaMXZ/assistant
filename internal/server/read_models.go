package server

import (
	"encoding/json"
	"time"

	"github.com/EurekaMXZ/assistant/internal/tool"
)

type TurnExecutionTrace struct {
	TurnID           string         `json:"turn_id"`
	ConversationID   string         `json:"conversation_id"`
	Status           string         `json:"status"`
	RequestBlobKey   string         `json:"request_blob_key,omitempty"`
	ResponseBlobKey  string         `json:"response_blob_key,omitempty"`
	OpenAIResponseID string         `json:"openai_response_id,omitempty"`
	ErrorCode        string         `json:"error_code,omitempty"`
	ErrorMessage     string         `json:"error_message,omitempty"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	CompletedAt      *time.Time     `json:"completed_at,omitempty"`
	FailedAt         *time.Time     `json:"failed_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	Runs             []TurnRunTrace `json:"runs"`
}

type TurnRunTrace struct {
	ID                       string          `json:"id"`
	TurnID                   string          `json:"turn_id"`
	StepIndex                int             `json:"step_index"`
	Provider                 string          `json:"provider"`
	Model                    string          `json:"model,omitempty"`
	Status                   string          `json:"status"`
	RequestBlobKey           string          `json:"request_blob_key"`
	ResponseBlobKey          string          `json:"response_blob_key,omitempty"`
	ResponseID               string          `json:"response_id,omitempty"`
	InputTokens              int             `json:"input_tokens,omitempty"`
	CacheReadInputTokens     int             `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int             `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens             int             `json:"output_tokens,omitempty"`
	ReasoningOutputTokens    int             `json:"reasoning_output_tokens,omitempty"`
	TotalTokens              int             `json:"total_tokens,omitempty"`
	BillingCurrency          string          `json:"billing_currency,omitempty"`
	BillingAmountNanos       *int64          `json:"billing_amount_nanos,omitempty"`
	ErrorMessage             string          `json:"error_message,omitempty"`
	StartedAt                time.Time       `json:"started_at"`
	CompletedAt              *time.Time      `json:"completed_at,omitempty"`
	FailedAt                 *time.Time      `json:"failed_at,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
	OutputItems              json.RawMessage `json:"output_items,omitempty"`
	ToolCalls                []ToolCallTrace `json:"tool_calls"`
}

type ToolCallTrace struct {
	ID           string     `json:"id"`
	TurnID       string     `json:"turn_id"`
	TurnRunID    string     `json:"turn_run_id"`
	CallID       string     `json:"call_id"`
	ToolType     string     `json:"tool_type"`
	Namespace    string     `json:"namespace,omitempty"`
	ToolName     string     `json:"tool_name"`
	Status       string     `json:"status"`
	Summary      string     `json:"summary,omitempty"`
	Details      []string   `json:"details,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	FailedAt     *time.Time `json:"failed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type TurnTimeline struct {
	TurnID         string             `json:"turn_id"`
	ConversationID string             `json:"conversation_id"`
	Status         string             `json:"status"`
	Items          []TurnTimelineItem `json:"items"`
	LastEventIndex int64              `json:"-"`
}

type TurnStreamSnapshot struct {
	TurnID         string             `json:"turn_id"`
	ConversationID string             `json:"conversation_id"`
	Status         string             `json:"status"`
	Items          []TurnTimelineItem `json:"items"`
	StartedAt      *time.Time         `json:"started_at,omitempty"`
	CompletedAt    *time.Time         `json:"completed_at,omitempty"`
	FailedAt       *time.Time         `json:"failed_at,omitempty"`
}

type TurnStreamItemDelta struct {
	ItemID         string    `json:"item_id"`
	ItemType       string    `json:"item_type"`
	Delta          string    `json:"delta"`
	SequenceNumber *int      `json:"sequence_number,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type TurnStreamDone struct {
	TurnID         string `json:"turn_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Status         string `json:"status"`
	ErrorCode      string `json:"error_code,omitempty"`
	Error          string `json:"error,omitempty"`
}

type TurnTimelineItem struct {
	ID               string               `json:"id"`
	Type             string               `json:"type"`
	Title            string               `json:"title,omitempty"`
	Status           string               `json:"status,omitempty"`
	Summary          string               `json:"summary,omitempty"`
	ContentText      string               `json:"content_text,omitempty"`
	Details          []string             `json:"details,omitempty"`
	InputLabel       string               `json:"input_label,omitempty"`
	InputText        string               `json:"input_text,omitempty"`
	Links            []TurnTimelineLink   `json:"links,omitempty"`
	Command          string               `json:"command,omitempty"`
	WorkingDirectory string               `json:"working_directory,omitempty"`
	CommandOutput    string               `json:"command_output,omitempty"`
	ExitCode         *int                 `json:"exit_code,omitempty"`
	TimedOut         bool                 `json:"timed_out,omitempty"`
	Raw              json.RawMessage      `json:"raw,omitempty"`
	Arguments        json.RawMessage      `json:"arguments,omitempty"`
	Output           json.RawMessage      `json:"output,omitempty"`
	ToolCallID       string               `json:"tool_call_id,omitempty"`
	Prompt           string               `json:"prompt,omitempty"`
	Kind             string               `json:"kind,omitempty"`
	Options          []tool.AskUserOption `json:"options,omitempty"`
	Action           *tool.AskUserAction  `json:"action,omitempty"`
	Answer           *tool.AskUserAnswer  `json:"answer,omitempty"`
	Metadata         map[string]any       `json:"metadata,omitempty"`
	Image            *TurnTimelineImage   `json:"image,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
}

type TurnTimelineImage struct {
	AssetID      string `json:"asset_id"`
	Kind         string `json:"kind"`
	Revision     int    `json:"revision"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	AttachmentID string `json:"attachment_id,omitempty"`
}

type TurnTimelineLink struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}
