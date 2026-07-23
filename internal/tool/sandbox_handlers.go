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

type CreateSandboxShellHandler struct {
	UseCase CreateSandboxShell
}

type ConnectSandboxShellHandler struct {
	UseCase ConnectSandboxShell
}

type DestroySandboxShellHandler struct {
	UseCase DestroySandboxShell
}

type ImportSandboxAttachmentHandler struct {
	UseCase ImportSandboxAttachment
}

type WriteSandboxFileHandler struct {
	UseCase WriteSandboxFile
}

type EditSandboxFileHandler struct {
	UseCase EditSandboxFile
}

func (h WriteSandboxFileHandler) ToolName() string {
	return SandboxWriteFile
}

func (h WriteSandboxFileHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeToolArguments(call, SandboxWriteFile, &input); err != nil {
		return nil, err
	}
	result, err := h.UseCase.Execute(ctx, WriteSandboxFileInput{
		ConversationID: scope.ConversationID,
		Path:           input.Path,
		Content:        input.Content,
		RequestKey:     call.RequestKey,
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(SandboxWriteFile, map[string]any{
		"conversation_id": scope.ConversationID,
		"file":            result,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}

func (h EditSandboxFileHandler) ToolName() string {
	return SandboxEditFile
}

func (h EditSandboxFileHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		Path       string `json:"path"`
		OldText    string `json:"old_text"`
		NewText    string `json:"new_text"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := decodeToolArguments(call, SandboxEditFile, &input); err != nil {
		return nil, err
	}
	result, err := h.UseCase.Execute(ctx, EditSandboxFileInput{
		ConversationID: scope.ConversationID,
		Path:           input.Path,
		OldText:        input.OldText,
		NewText:        input.NewText,
		ReplaceAll:     input.ReplaceAll,
		RequestKey:     call.RequestKey,
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(SandboxEditFile, map[string]any{
		"conversation_id": scope.ConversationID,
		"file":            result,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}

func (h ImportSandboxAttachmentHandler) ToolName() string {
	return SandboxImportAttachment
}

func (h ImportSandboxAttachmentHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		AttachmentID string `json:"attachment_id"`
	}
	if err := decodeToolArguments(call, SandboxImportAttachment, &input); err != nil {
		return nil, err
	}

	result, err := h.UseCase.Execute(ctx, ImportSandboxAttachmentInput{
		ConversationID: scope.ConversationID,
		AttachmentID:   input.AttachmentID,
		RequestKey:     call.RequestKey,
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(SandboxImportAttachment, map[string]any{
		"conversation_id": scope.ConversationID,
		"attachment":      result,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}

func (h ExecSandboxCommandHandler) ToolName() string {
	return SandboxExec
}

func (h CreateSandboxShellHandler) ToolName() string {
	return SandboxShellCreate
}

func (h CreateSandboxShellHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		WorkingDirectory string `json:"working_directory"`
	}
	if err := decodeToolArguments(call, SandboxShellCreate, &input); err != nil {
		return nil, err
	}
	session, err := h.UseCase.Execute(ctx, CreateSandboxShellInput{
		ConversationID: scope.ConversationID, WorkingDirectory: input.WorkingDirectory, RequestKey: call.RequestKey,
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(SandboxShellCreate, map[string]any{
		"conversation_id": scope.ConversationID, "session": session,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}

func (h ConnectSandboxShellHandler) ToolName() string {
	return SandboxShellConnect
}

func (h ConnectSandboxShellHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		SessionID      string `json:"session_id"`
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := decodeToolArguments(call, SandboxShellConnect, &input); err != nil {
		return nil, err
	}
	result, err := h.UseCase.Execute(ctx, ConnectSandboxShellInput{
		ConversationID: scope.ConversationID, SessionID: input.SessionID, Command: input.Command,
		TimeoutSeconds: input.TimeoutSeconds, RequestKey: call.RequestKey,
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(SandboxShellConnect, map[string]any{
		"conversation_id": scope.ConversationID, "result": result,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}

func (h DestroySandboxShellHandler) ToolName() string {
	return SandboxShellDestroy
}

func (h DestroySandboxShellHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeToolArguments(call, SandboxShellDestroy, &input); err != nil {
		return nil, err
	}
	session, err := h.UseCase.Execute(ctx, DestroySandboxShellInput{
		ConversationID: scope.ConversationID, SessionID: input.SessionID, RequestKey: call.RequestKey,
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(SandboxShellDestroy, map[string]any{
		"conversation_id": scope.ConversationID, "session": session,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
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
