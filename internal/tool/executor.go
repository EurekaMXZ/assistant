package tool

import (
	"context"
	"fmt"
	"strings"
)

type LocalExecutor struct {
	handlers map[string]LocalToolHandler
}

type LocalToolHandler interface {
	ToolName() string
	Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error)
}

func NewLocalExecutor(handlers ...LocalToolHandler) (LocalExecutor, error) {
	registry := make(map[string]LocalToolHandler, len(handlers))
	for _, handler := range handlers {
		if handler == nil {
			return LocalExecutor{}, fmt.Errorf("local tool handler is nil")
		}

		name := strings.TrimSpace(handler.ToolName())
		if name == "" {
			return LocalExecutor{}, fmt.Errorf("local tool handler %T returned empty tool name", handler)
		}
		if _, exists := registry[name]; exists {
			return LocalExecutor{}, fmt.Errorf("duplicate local tool handler %q", name)
		}

		registry[name] = handler
	}

	return LocalExecutor{handlers: registry}, nil
}

func (e LocalExecutor) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	name := normalizedToolName(call)
	handler, ok := e.handlers[name]
	if !ok {
		return nil, RecoverableError(fmt.Errorf("unsupported local tool %q", name))
	}
	return handler.Execute(ctx, scope, call)
}

func normalizedToolName(call ToolCall) string {
	name := strings.TrimSpace(call.Name)
	if call.Namespace == "" {
		return name
	}
	if name == "" {
		return strings.TrimSpace(call.Namespace)
	}
	return strings.TrimSpace(call.Namespace) + "." + name
}
