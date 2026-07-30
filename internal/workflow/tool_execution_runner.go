package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type ToolExecutionRunner struct {
	executor  tool.ToolExecutor
	artifacts ToolArtifactStore
	store     ToolExecutionWorkflowStore
	publisher stream.Publisher
}

func NewToolExecutionRunner(executor tool.ToolExecutor, artifacts ToolArtifactStore, store ToolExecutionWorkflowStore, publisher stream.Publisher) *ToolExecutionRunner {
	return &ToolExecutionRunner{executor: executor, artifacts: artifacts, store: store, publisher: publisher}
}

func (r *ToolExecutionRunner) ExecuteQueuedToolCall(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, lease ToolCallLease, leaseTimeout time.Duration) (*ToolCallSettlement, *AwaitingInputSignal, error) {
	if r == nil || r.executor == nil || r.artifacts == nil || r.store == nil || record == nil {
		return nil, nil, errors.New("durable tool execution is not configured")
	}
	arguments, err := r.artifacts.GetBytes(ctx, record.ArgumentsBlobKey)
	if err != nil {
		return nil, nil, fmt.Errorf("load queued tool arguments: %w", err)
	}
	call := tool.ToolCall{
		Type: record.ToolType, CallID: record.CallID, Namespace: record.Namespace,
		Name: record.ToolName, Arguments: append(json.RawMessage(nil), arguments...),
		RequestKey: record.StableOperationID,
	}
	executionCtx, stopLease := startToolCallLeaseHeartbeat(ctx, r.store, lease, leaseTimeout)
	defer stopLease()
	if normalizedToolName(call) != tool.AskUser {
		publishToolStarted(executionCtx, r.publisher, scope, record, call)
	}
	execution, err := r.executor.Execute(executionCtx, scope, call)
	if cause := context.Cause(executionCtx); cause != nil {
		return nil, nil, cause
	}
	if err != nil {
		visibleOutput := modelVisibleToolFailure(call, err)
		if errors.Is(err, tool.ErrOutcomeUncertain) {
			visibleOutput = modelVisibleToolUncertainOutcome(call, err)
		}
		outputKey, persistErr := r.persistOutput(executionCtx, scope, call, visibleOutput)
		if persistErr != nil {
			return nil, nil, persistErr
		}
		var settled *ToolCallSettlement
		if errors.Is(err, tool.ErrOutcomeUncertain) {
			settled, err = r.store.MarkQueuedToolCallUncertain(executionCtx, lease, outputKey, err.Error())
		} else {
			settled, err = r.store.FailQueuedToolCall(executionCtx, lease, outputKey, err.Error())
		}
		if err != nil {
			return nil, nil, err
		}
		publishToolFailed(executionCtx, r.publisher, scope, record, call, settled.Record.ErrorMessage, []byte(visibleOutput))
		return settled, nil, nil
	}
	if execution != nil && execution.AwaitingInput != nil {
		if normalizedToolName(call) != tool.AskUser {
			return nil, nil, fmt.Errorf("tool %s cannot await user input", describeToolCall(call))
		}
		prompt := execution.AwaitingInput
		prompt.CallID = call.CallID
		prompt.ToolCallID = record.ID
		return nil, &AwaitingInputSignal{ToolCall: record, Prompt: prompt}, nil
	}
	output := ""
	var streamEvents []stream.Event
	if execution != nil {
		output = execution.OutputItem.Output
		streamEvents = execution.StreamEvents
	}
	outputKey, err := r.persistOutput(executionCtx, scope, call, output)
	if err != nil {
		return nil, nil, err
	}
	if cause := context.Cause(executionCtx); cause != nil {
		return nil, nil, cause
	}
	if execution != nil && execution.Failed {
		message := "tool execution failed"
		settled, settleErr := r.store.FailQueuedToolCall(executionCtx, lease, outputKey, message)
		if settleErr != nil {
			return nil, nil, settleErr
		}
		publishToolFailed(executionCtx, r.publisher, scope, record, call, message, []byte(output))
		return settled, nil, nil
	}
	settled, err := r.store.CompleteQueuedToolCall(executionCtx, lease, outputKey)
	if err != nil {
		return nil, nil, err
	}
	publishToolCompleted(executionCtx, r.publisher, scope, record, call, []byte(output))
	for _, event := range streamEvents {
		if r.publisher != nil {
			_ = r.publisher.Publish(executionCtx, event)
		}
	}
	return settled, nil, nil
}

func (r *ToolExecutionRunner) persistOutput(ctx context.Context, scope tool.ToolScope, call tool.ToolCall, output string) (string, error) {
	if strings.TrimSpace(output) == "" {
		return "", nil
	}
	key := r.artifacts.ToolCallOutputKey(scope.ConversationID, scope.TurnID, call.CallID)
	if err := r.artifacts.PutBytes(ctx, key, []byte(output), "application/json"); err != nil {
		return "", fmt.Errorf("persist queued tool output: %w", err)
	}
	return key, nil
}

func modelVisibleToolUncertainOutcome(call tool.ToolCall, err error) string {
	message := "tool outcome is uncertain"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = boundedToolFailureMessage(err.Error(), 2048)
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"ok": false,
		"error": map[string]any{
			"type":    "tool_execution_uncertain",
			"tool":    describeToolCall(call),
			"message": message,
		},
		"next_action": "Do not assume the tool action failed or succeeded. Reconcile the external system before retrying.",
	})
	if marshalErr != nil {
		return `{"ok":false,"error":{"type":"tool_execution_uncertain","message":"tool outcome is uncertain"}}`
	}
	return string(payload)
}
