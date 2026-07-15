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
	OutputItem   llm.ModelItem
	StreamEvents []stream.Event
	Failed       bool
}

type ToolExecutor interface {
	Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error)
}
