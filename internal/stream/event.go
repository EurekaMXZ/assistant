package stream

import (
	"context"
	"encoding/json"
)

const (
	EventResponseStarted     = "response.started"
	EventResponseCreated     = "response.created"
	EventResponseCompleted   = "response.completed"
	EventResponseFailed      = "response.failed"
	EventReasoningSummary    = "reasoning.summary"
	EventToolStarted         = "tool.started"
	EventToolCompleted       = "tool.completed"
	EventToolFailed          = "tool.failed"
	EventTurnDone            = "turn.done"
	EventConversationUpdated = "conversation.updated"
	EventSandboxUpdated      = "sandbox.updated"
)

const (
	ToolEventStatusStarted   = "started"
	ToolEventStatusCompleted = "completed"
	ToolEventStatusFailed    = "failed"
)

type Event struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversation_id,omitempty"`
	TurnID         string `json:"turn_id,omitempty"`
	ResponseID     string `json:"response_id,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	Payload        string `json:"payload,omitempty"`
	Delta          string `json:"delta,omitempty"`
	Text           string `json:"text,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	Error          string `json:"error,omitempty"`
}

type ToolStreamPayload struct {
	ToolCallRecordID string          `json:"tool_call_record_id,omitempty"`
	TurnRunID        string          `json:"turn_run_id,omitempty"`
	CallID           string          `json:"call_id,omitempty"`
	ToolName         string          `json:"tool_name,omitempty"`
	ToolType         string          `json:"tool_type,omitempty"`
	Namespace        string          `json:"namespace,omitempty"`
	ServerLabel      string          `json:"server_label,omitempty"`
	Status           string          `json:"status,omitempty"`
	Arguments        json.RawMessage `json:"arguments,omitempty"`
	Output           json.RawMessage `json:"output,omitempty"`
	Summary          string          `json:"summary,omitempty"`
	Details          []string        `json:"details,omitempty"`
	Error            string          `json:"error,omitempty"`
}

type ReasoningStreamPayload struct {
	TurnRunID  string            `json:"turn_run_id,omitempty"`
	ResponseID string            `json:"response_id,omitempty"`
	StepIndex  int               `json:"step_index,omitempty"`
	Items      []json.RawMessage `json:"items,omitempty"`
	Summary    string            `json:"summary,omitempty"`
}

type Publisher interface {
	Publish(ctx context.Context, event Event) error
}
