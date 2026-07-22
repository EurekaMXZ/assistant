package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

func (o *ToolOrchestrator) recordObservedRemoteCalls(ctx context.Context, scope tool.ToolScope, run *domain.TurnRun, items []llm.ModelItem) error {
	for _, item := range items {
		record, err := o.recordRemoteToolCall(ctx, scope, run, item)
		if err != nil {
			return err
		}
		o.publishRemoteToolCallEvent(ctx, scope, record, item)
	}
	return nil
}

func (o *ToolOrchestrator) ensureStepCapabilities(functionCalls []llm.ModelItem) error {
	switch {
	case len(functionCalls) > 0 && o.executor == nil:
		return errors.New("tool orchestrator requires tool executor for function calls")
	default:
		return nil
	}
}

func (o *ToolOrchestrator) executeLocalToolCalls(ctx context.Context, run *domain.TurnRun, input []llm.ModelItem, scope tool.ToolScope, calls []tool.ToolCall, totalOutputBudgetTokens int) ([]llm.ModelItem, tool.ToolScope, error) {
	currentInput := cloneModelItems(input)
	currentScope := cloneToolScope(scope)
	remainingTokens := totalOutputBudgetTokens

	for _, call := range calls {
		record, acquired, err := o.recordToolCallStart(ctx, currentScope, run, call)
		if err != nil {
			return currentInput, currentScope, err
		}

		call.RequestKey = run.ID + ":" + call.CallID
		execution, err := o.executeRecordedLocalToolCall(ctx, currentScope, record, acquired, call)
		if err != nil {
			return currentInput, currentScope, err
		}
		if execution != nil {
			outputLimit := o.modelToolOutputTokenLimit()
			if remainingTokens >= 0 {
				outputLimit = min(outputLimit, remainingTokens)
			}
			modelItem := truncateModelContextItem(execution.OutputItem, outputLimit)
			currentInput = append(currentInput, modelItem)
			if remainingTokens >= 0 {
				remainingTokens = max(0, remainingTokens-domain.EstimateTokens(modelItem.Output))
			}
		}
		if execution == nil || !execution.Failed {
			currentScope = applyToolScopeDelta(currentScope, call)
		}
	}

	return currentInput, currentScope, nil
}

func localToolCallsAwaitingInputLast(items []llm.ModelItem) ([]tool.ToolCall, error) {
	calls := modelItemsToToolCalls(items)
	ordered := make([]tool.ToolCall, 0, len(calls))
	var awaiting []tool.ToolCall
	for _, call := range calls {
		if normalizedToolName(call) == tool.AskUser {
			awaiting = append(awaiting, call)
			continue
		}
		ordered = append(ordered, call)
	}
	if len(awaiting) > 1 {
		return nil, domain.NewValidationError("ask_user may only be called once per response")
	}
	return append(ordered, awaiting...), nil
}

func (o *ToolOrchestrator) executeRecordedLocalToolCall(ctx context.Context, scope tool.ToolScope, record *domain.ToolCallRecord, acquired bool, call tool.ToolCall) (*tool.ToolExecutionResult, error) {
	if record != nil {
		switch record.Status {
		case domain.ToolCallStatusCompleted:
			output := ""
			if record.OutputBlobKey != "" && o.artifacts != nil {
				payload, err := o.artifacts.GetBytes(ctx, record.OutputBlobKey)
				if err != nil {
					return nil, fmt.Errorf("load completed tool output: %w", err)
				}
				output = string(payload)
			}
			return &tool.ToolExecutionResult{OutputItem: llm.ModelItem{
				Type: llm.ModelItemFunctionCallOutput, CallID: call.CallID, Output: output,
			}}, nil
		case domain.ToolCallStatusFailed:
			output := ""
			if record.OutputBlobKey != "" && o.artifacts != nil {
				payload, err := o.artifacts.GetBytes(ctx, record.OutputBlobKey)
				if err != nil {
					return nil, fmt.Errorf("load failed tool output: %w", err)
				}
				output = string(payload)
			}
			if strings.TrimSpace(output) == "" {
				output = modelVisibleToolFailure(call, errors.New(record.ErrorMessage))
			}
			return &tool.ToolExecutionResult{Failed: true, OutputItem: llm.ModelItem{
				Type: llm.ModelItemFunctionCallOutput, CallID: call.CallID, Output: output,
			}}, nil
		case domain.ToolCallStatusCancelled:
			return nil, fmt.Errorf("tool call %s is already %s", describeToolCall(call), record.Status)
		case domain.ToolCallStatusAwaitingInput:
			prompt, err := tool.DecodeAskUserPrompt(call.Arguments)
			if err != nil {
				return nil, err
			}
			prompt.CallID = call.CallID
			prompt.ToolCallID = record.ID
			return nil, &AwaitingInputSignal{ToolCall: record, Prompt: prompt}
		case domain.ToolCallStatusRunning:
			if !acquired {
				return nil, fmt.Errorf("tool call %s is already executing", describeToolCall(call))
			}
		}
	}
	if normalizedToolName(call) != tool.AskUser {
		o.publishToolStarted(ctx, scope, record, call)
	}

	execution, err := o.executor.Execute(ctx, scope, call)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("execute tool %s: %w", describeToolCall(call), ctx.Err())
		}
		visibleOutput := modelVisibleToolFailure(call, err)
		o.publishToolFailed(ctx, scope, record, call, err.Error(), nil)
		if failErr := o.recordToolCallFailure(ctx, scope, record, call, err.Error(), visibleOutput); failErr != nil {
			return nil, failErr
		}
		return &tool.ToolExecutionResult{Failed: true, OutputItem: llm.ModelItem{
			Type: llm.ModelItemFunctionCallOutput, CallID: call.CallID, Output: visibleOutput,
		}}, nil
	}
	if execution == nil {
		if err := o.recordToolCallSuccess(ctx, scope, record, call, nil); err != nil {
			return nil, err
		}
		o.publishToolCompleted(ctx, scope, record, call, nil)
		return nil, nil
	}
	if execution.AwaitingInput != nil {
		if normalizedToolName(call) != tool.AskUser {
			return nil, fmt.Errorf("tool %s cannot await user input", describeToolCall(call))
		}
		if record == nil || o.calls == nil {
			return nil, errors.New("awaiting input requires a durable tool call")
		}
		prompt := execution.AwaitingInput
		prompt.CallID = call.CallID
		prompt.ToolCallID = record.ID
		return nil, &AwaitingInputSignal{ToolCall: record, Prompt: prompt}
	}

	if err := o.recordToolCallSuccess(ctx, scope, record, call, execution); err != nil {
		return nil, err
	}
	o.publishToolCompleted(ctx, scope, record, call, []byte(execution.OutputItem.Output))
	for _, event := range execution.StreamEvents {
		o.publish(ctx, event)
	}
	return execution, nil
}
func modelVisibleToolFailure(call tool.ToolCall, err error) string {
	message := "tool execution failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = boundedToolFailureMessage(err.Error(), 2048)
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"ok": false,
		"error": map[string]any{
			"type":    "tool_execution_failed",
			"tool":    describeToolCall(call),
			"message": message,
		},
		"next_action": "Adjust the arguments, try a narrower request, use another tool, or continue without this tool.",
	})
	if marshalErr != nil {
		return `{"ok":false,"error":{"type":"tool_execution_failed","message":"tool execution failed"}}`
	}
	return string(payload)
}

func boundedToolFailureMessage(message string, maxBytes int) string {
	message = strings.ToValidUTF8(strings.TrimSpace(message), "\ufffd")
	if maxBytes <= 0 || len(message) <= maxBytes {
		return message
	}
	for maxBytes > 0 && !utf8.ValidString(message[:maxBytes]) {
		maxBytes--
	}
	return message[:maxBytes] + "..."
}

func modelItemsToToolCalls(items []llm.ModelItem) []tool.ToolCall {
	if len(items) == 0 {
		return nil
	}

	calls := make([]tool.ToolCall, 0, len(items))
	for _, item := range items {
		calls = append(calls, toolCallFromModelItem(item))
	}
	return calls
}

func normalizeFunctionCallItems(items []llm.ModelItem, tools []llm.ModelTool) []llm.ModelItem {
	if len(items) == 0 {
		return nil
	}

	normalized := cloneModelItems(items)
	for index := range normalized {
		item := &normalized[index]
		if strings.TrimSpace(item.Namespace) != "" {
			continue
		}

		name := strings.TrimSpace(item.Name)
		if name == "" || strings.Contains(name, ".") {
			continue
		}

		if namespace, ok := uniqueFunctionToolNamespace(tools, name); ok && namespace != "" {
			item.Namespace = namespace
			continue
		}

		if namespace, toolName, ok := uniqueSafeFunctionTool(tools, name); ok {
			item.Namespace = namespace
			item.Name = toolName
		}
	}

	return normalized
}

type functionToolMatch struct {
	namespace string
	name      string
}

func uniqueSafeFunctionTool(tools []llm.ModelTool, name string) (string, string, bool) {
	var matches []functionToolMatch
	collectSafeFunctionTools(tools, "", strings.TrimSpace(name), &matches)
	if len(matches) != 1 {
		return "", "", false
	}
	return matches[0].namespace, matches[0].name, true
}

func collectSafeFunctionTools(tools []llm.ModelTool, namespace string, name string, matches *[]functionToolMatch) {
	for _, modelTool := range tools {
		toolName := strings.TrimSpace(modelTool.Name)
		switch modelTool.Type {
		case llm.ModelToolTypeNamespace:
			collectSafeFunctionTools(modelTool.Tools, joinToolNamespace(namespace, toolName), name, matches)
		case llm.ModelToolTypeFunction:
			if llm.SafeToolName(joinToolNamespace(namespace, toolName)) != name {
				continue
			}
			*matches = append(*matches, functionToolMatch{
				namespace: strings.TrimSpace(namespace),
				name:      toolName,
			})
		}
	}
}

func uniqueFunctionToolNamespace(tools []llm.ModelTool, name string) (string, bool) {
	matches := map[string]struct{}{}
	collectFunctionToolNamespaces(tools, "", strings.TrimSpace(name), matches)
	if len(matches) != 1 {
		return "", false
	}

	for namespace := range matches {
		return namespace, true
	}
	return "", false
}

func collectFunctionToolNamespaces(tools []llm.ModelTool, namespace string, name string, matches map[string]struct{}) {
	for _, modelTool := range tools {
		toolName := strings.TrimSpace(modelTool.Name)
		switch modelTool.Type {
		case llm.ModelToolTypeNamespace:
			collectFunctionToolNamespaces(modelTool.Tools, joinToolNamespace(namespace, toolName), name, matches)
		case llm.ModelToolTypeFunction:
			if toolName == name {
				matches[strings.TrimSpace(namespace)] = struct{}{}
				continue
			}

			if dot := strings.LastIndex(toolName, "."); dot > 0 && dot < len(toolName)-1 && toolName[dot+1:] == name {
				matches[joinToolNamespace(namespace, toolName[:dot])] = struct{}{}
			}
		}
	}
}

func joinToolNamespace(parent string, child string) string {
	parent = strings.TrimSpace(parent)
	child = strings.TrimSpace(child)
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	return parent + "." + child
}
