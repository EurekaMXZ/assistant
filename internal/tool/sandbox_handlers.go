package tool

import (
	"context"
)

type CreateSandboxHandler struct {
	UseCase CreateSandbox
}

func (h CreateSandboxHandler) ToolName() string {
	return SandboxCreate
}

func (h CreateSandboxHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	sandbox, err := h.UseCase.Execute(ctx, CreateSandboxInput{
		ConversationID: scope.ConversationID,
		RequestKey:     call.RequestKey,
	})
	if err != nil {
		return nil, err
	}

	return sandboxExecutionResult(scope, call.CallID, SandboxCreate, sandbox)
}

type DestroySandboxHandler struct {
	UseCase DestroySandbox
}

func (h DestroySandboxHandler) ToolName() string {
	return SandboxDestroy
}

func (h DestroySandboxHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	sandbox, err := h.UseCase.Execute(ctx, DestroySandboxInput{
		ConversationID: scope.ConversationID,
		RequestKey:     call.RequestKey,
	})
	if err != nil {
		return nil, err
	}

	return sandboxExecutionResult(scope, call.CallID, SandboxDestroy, sandbox)
}

type ExecSandboxCommandHandler struct {
	UseCase ExecSandboxCommand
}

func (h ExecSandboxCommandHandler) ToolName() string {
	return SandboxExec
}

func (h ExecSandboxCommandHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		Command          string   `json:"command"`
		Args             []string `json:"args"`
		WorkingDirectory string   `json:"working_directory"`
		TimeoutSeconds   int      `json:"timeout_seconds"`
	}
	if err := decodeToolArguments(call, SandboxExec, &input); err != nil {
		return nil, err
	}

	result, err := h.UseCase.Execute(ctx, ExecSandboxCommandInput{
		ConversationID:   scope.ConversationID,
		Command:          input.Command,
		Args:             input.Args,
		WorkingDirectory: input.WorkingDirectory,
		TimeoutSeconds:   input.TimeoutSeconds,
		RequestKey:       call.RequestKey,
	})
	if err != nil {
		return nil, err
	}

	payload, err := marshalToolOutput(SandboxExec, map[string]any{
		"conversation_id": scope.ConversationID,
		"result":          result,
	})
	if err != nil {
		return nil, err
	}

	return outputOnlyExecutionResult(call.CallID, payload), nil
}
