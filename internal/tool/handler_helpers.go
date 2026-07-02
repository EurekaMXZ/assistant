package tool

import (
	"encoding/json"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

func decodeToolArguments(call ToolCall, toolName string, target any) error {
	if err := json.Unmarshal(call.Arguments, target); err != nil {
		return fmt.Errorf("decode %s arguments: %w", toolName, err)
	}
	return nil
}

func marshalToolOutput(toolName string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s output: %w", toolName, err)
	}
	return raw, nil
}

func outputOnlyExecutionResult(callID string, payload []byte) *ToolExecutionResult {
	return &ToolExecutionResult{
		OutputItem: llm.ModelItem{
			Type:   llm.ModelItemFunctionCallOutput,
			CallID: callID,
			Output: string(payload),
		},
	}
}

func eventedExecutionResult(scope ToolScope, callID string, payload []byte, event stream.Event) *ToolExecutionResult {
	result := outputOnlyExecutionResult(callID, payload)
	result.StreamEvents = []stream.Event{event}
	if result.StreamEvents[0].ConversationID == "" {
		result.StreamEvents[0].ConversationID = scope.ConversationID
	}
	if result.StreamEvents[0].TurnID == "" {
		result.StreamEvents[0].TurnID = scope.TurnID
	}
	if result.StreamEvents[0].Payload == "" {
		result.StreamEvents[0].Payload = string(payload)
	}
	return result
}

func sandboxExecutionResult(scope ToolScope, callID string, toolName string, sandbox *domain.ConversationSandbox) (*ToolExecutionResult, error) {
	payload, err := marshalToolOutput(toolName, map[string]any{
		"conversation_id": scope.ConversationID,
		"sandbox":         sandbox,
	})
	if err != nil {
		return nil, err
	}

	return eventedExecutionResult(scope, callID, payload, stream.Event{
		Type:     stream.EventSandboxUpdated,
		ToolName: toolName,
	}), nil
}
