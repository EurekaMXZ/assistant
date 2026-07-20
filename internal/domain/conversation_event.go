package domain

import (
	"encoding/json"
	"time"
)

type ConversationEventInput struct {
	ConversationID  string
	TurnID          string
	TurnRunID       string
	EventKey        string
	SchemaVersion   int
	EventType       string
	Payload         json.RawMessage
	ContextIncluded bool
}

const (
	ConversationEventUserMessageCreated    = "user_message.created"
	ConversationEventReasoningSummaryDone  = "reasoning_summary.completed"
	ConversationEventToolCallStarted       = "tool_call.started"
	ConversationEventToolCallCompleted     = "tool_call.completed"
	ConversationEventToolCallFailed        = "tool_call.failed"
	ConversationEventOutputTextCompleted   = "output_text.completed"
	ConversationEventOutputTextInterrupted = "output_text.interrupted"
	ConversationEventRunStarted            = "run.started"
	ConversationEventRunCompleted          = "run.completed"
	ConversationEventRunFailed             = "run.failed"
	ConversationEventRunCancelled          = "run.cancelled"
	ConversationEventTurnCompleted         = "turn.completed"
	ConversationEventTurnFailed            = "turn.failed"
	ConversationEventTurnCancelled         = "turn.cancelled"
)

type ConversationEvent struct {
	ID              string          `json:"id"`
	ConversationID  string          `json:"conversation_id"`
	TurnID          string          `json:"turn_id,omitempty"`
	TurnRunID       string          `json:"turn_run_id,omitempty"`
	EventSeq        int64           `json:"event_seq"`
	EventKey        string          `json:"event_key"`
	SchemaVersion   int             `json:"schema_version"`
	EventType       string          `json:"event_type"`
	Payload         json.RawMessage `json:"payload"`
	ContextIncluded bool            `json:"context_included"`
	CreatedAt       time.Time       `json:"created_at"`
}
