package domain

import "time"

const (
	TurnRunStatusQueued    = "queued"
	TurnRunStatusRunning   = "running"
	TurnRunStatusCompleted = "completed"
	TurnRunStatusFailed    = "failed"

	ToolCallStatusRunning   = "running"
	ToolCallStatusCompleted = "completed"
	ToolCallStatusFailed    = "failed"
	ToolCallStatusAmbiguous = "ambiguous"
)

type TurnRun struct {
	ID                       string     `json:"id"`
	TurnID                   string     `json:"turn_id"`
	StepIndex                int        `json:"step_index"`
	Provider                 string     `json:"provider"`
	Model                    string     `json:"model,omitempty"`
	Status                   string     `json:"status"`
	Attempt                  int        `json:"attempt"`
	RequestBlobKey           string     `json:"request_blob_key"`
	StateBlobKey             string     `json:"state_blob_key,omitempty"`
	ResultBlobKey            string     `json:"result_blob_key,omitempty"`
	ResponseBlobKey          string     `json:"response_blob_key,omitempty"`
	ResponseID               string     `json:"response_id,omitempty"`
	InputTokens              int        `json:"input_tokens,omitempty"`
	CacheReadInputTokens     int        `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int        `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens             int        `json:"output_tokens,omitempty"`
	ReasoningOutputTokens    int        `json:"reasoning_output_tokens,omitempty"`
	TotalTokens              int        `json:"total_tokens,omitempty"`
	BillingCurrency          string     `json:"billing_currency,omitempty"`
	BillingAmountNanos       *int64     `json:"billing_amount_nanos,omitempty"`
	ErrorMessage             string     `json:"error_message,omitempty"`
	StartedAt                time.Time  `json:"started_at"`
	CompletedAt              *time.Time `json:"completed_at,omitempty"`
	FailedAt                 *time.Time `json:"failed_at,omitempty"`
	HeartbeatAt              *time.Time `json:"heartbeat_at,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type ToolCallRecord struct {
	ID               string     `json:"id"`
	TurnID           string     `json:"turn_id"`
	TurnRunID        string     `json:"turn_run_id"`
	CallID           string     `json:"call_id"`
	ToolType         string     `json:"tool_type"`
	Namespace        string     `json:"namespace,omitempty"`
	ToolName         string     `json:"tool_name"`
	Status           string     `json:"status"`
	ExecutionAttempt int        `json:"execution_attempt"`
	ArgumentsBlobKey string     `json:"arguments_blob_key"`
	OutputBlobKey    string     `json:"output_blob_key,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	FailedAt         *time.Time `json:"failed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
