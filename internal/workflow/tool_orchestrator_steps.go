package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func (o *ToolOrchestrator) executeLocalToolCalls(ctx context.Context, run *domain.TurnRun, input []llm.ModelItem, scope tool.ToolScope, calls []tool.ToolCall) ([]llm.ModelItem, tool.ToolScope, error) {
	currentInput := cloneModelItems(input)
	currentScope := cloneToolScope(scope)

	for _, call := range calls {
		record, acquired, err := o.recordToolCallStart(ctx, currentScope, run, call)
		if err != nil {
			return nil, currentScope, err
		}

		call.RequestKey = run.ID + ":" + call.CallID
		execution, err := o.executeRecordedLocalToolCall(ctx, currentScope, record, acquired, call)
		if err != nil {
			return nil, currentScope, err
		}
		if execution != nil {
			currentInput = append(currentInput, execution.OutputItem)
		}
		currentScope = applyToolScopeDelta(currentScope, call)
	}

	return currentInput, currentScope, nil
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
		case domain.ToolCallStatusFailed, domain.ToolCallStatusAmbiguous:
			return nil, fmt.Errorf("tool call %s is already %s", describeToolCall(call), record.Status)
		case domain.ToolCallStatusRunning:
			if !acquired {
				return nil, fmt.Errorf("tool call %s is already executing", describeToolCall(call))
			}
		}
	}
	o.publishToolStarted(ctx, scope, record, call)

	execution, err := o.executor.Execute(ctx, scope, call)
	if err != nil {
		o.publishToolFailed(ctx, scope, record, call, err.Error(), nil)
		if failErr := o.recordToolCallFailure(ctx, scope, record, call, err.Error()); failErr != nil {
			return nil, failErr
		}
		return nil, fmt.Errorf("execute tool %s: %w", describeToolCall(call), err)
	}
	if execution == nil {
		if err := o.recordToolCallSuccess(ctx, scope, record, call, nil); err != nil {
			return nil, err
		}
		o.publishToolCompleted(ctx, scope, record, call, nil)
		return nil, nil
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
