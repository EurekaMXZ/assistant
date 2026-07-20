package workflow

import (
	"time"
)

const (
	EventTurnAccepted              = "turn.accepted"
	EventTurnContextReady          = "turn.context_ready"
	EventTurnRunRequested          = "turn_run.requested"
	EventTurnCancellationRequested = "turn.cancel_requested"
	EventContextCompactionRequest  = "context.compaction.requested"
)

type WorkflowEvent struct {
	ID             string    `json:"id"`
	EventType      string    `json:"event_type"`
	ConversationID string    `json:"conversation_id,omitempty"`
	TurnID         string    `json:"turn_id,omitempty"`
	TurnRunID      string    `json:"turn_run_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
