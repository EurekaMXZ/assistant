package tool

import (
	"context"
	"encoding/json"

	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

type ToolScope struct {
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
	OwnerUserID    string `json:"owner_user_id"`
	HasSandbox     bool   `json:"has_sandbox,omitempty"`
}

type ToolCatalog interface {
	ListTools(ctx context.Context, scope ToolScope) ([]llm.ModelTool, error)
}

type ToolCall struct {
	Type        string
	CallID      string
	Name        string
	Namespace   string
	ServerLabel string
	Arguments   json.RawMessage
	Raw         json.RawMessage
	RequestKey  string
}

type ToolExecutionResult struct {
	OutputItem    llm.ModelItem
	StreamEvents  []stream.Event
	Failed        bool
	AwaitingInput *AskUserPrompt
}

type AskUserOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Tone  string `json:"tone"`
}

type AskUserAction struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type AskUserPrompt struct {
	ToolCallID string          `json:"tool_call_id"`
	CallID     string          `json:"call_id"`
	Prompt     string          `json:"prompt"`
	Kind       string          `json:"kind"`
	Options    []AskUserOption `json:"options"`
	Action     *AskUserAction  `json:"action,omitempty"`
}

type AskUserInteraction struct {
	ID         string          `json:"id"`
	ToolCallID string          `json:"tool_call_id"`
	Prompt     string          `json:"prompt"`
	Kind       string          `json:"kind"`
	Options    []AskUserOption `json:"options"`
	Action     *AskUserAction  `json:"action,omitempty"`
	Answer     *AskUserAnswer  `json:"answer,omitempty"`
	Status     string          `json:"status"`
}

type AskUserAnswer struct {
	Status       string `json:"status"`
	OptionID     string `json:"option_id"`
	Label        string `json:"label"`
	UserReported bool   `json:"user_reported"`
}

type ToolExecutor interface {
	Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error)
}
