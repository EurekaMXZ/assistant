package workflow

import (
	"context"
	"encoding/json"
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
