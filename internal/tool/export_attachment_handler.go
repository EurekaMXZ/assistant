package tool

import (
	"context"
	"errors"
	"strings"
)

type SandboxExportFileArguments struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
}

type SandboxExportFileHandler struct {
	UseCase ExportSandboxFile
}

func (h SandboxExportFileHandler) ToolName() string {
	return SandboxExportFileTool
}

func (h SandboxExportFileHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var args SandboxExportFileArguments
	if err := decodeToolArguments(call, SandboxExportFileTool, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(scope.OwnerUserID) == "" {
		return nil, errors.New("sandbox.export_file requires an owner user scope")
	}
	attachment, err := h.UseCase.Execute(ctx, ExportSandboxFileInput{
		ConversationID: scope.ConversationID,
		TurnID:         scope.TurnID,
		OwnerUserID:    scope.OwnerUserID,
		CallID:         call.CallID,
		SandboxPath:    args.Path,
		Filename:       args.Filename,
		RequestKey:     call.RequestKey + ":export-file",
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(SandboxExportFileTool, assistantAttachmentReference(scope, "sandbox_export", attachment))
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}

type ConversationExportTextArguments struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type ConversationExportTextHandler struct {
	UseCase ExportTextAttachment
}

func (h ConversationExportTextHandler) ToolName() string {
	return ConversationExportText
}

func (h ConversationExportTextHandler) Execute(ctx context.Context, scope ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var args ConversationExportTextArguments
	if err := decodeToolArguments(call, ConversationExportText, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(scope.OwnerUserID) == "" {
		return nil, errors.New("conversation.export_text requires an owner user scope")
	}
	attachment, err := h.UseCase.Execute(ctx, ExportTextAttachmentInput{
		ConversationID: scope.ConversationID,
		TurnID:         scope.TurnID,
		OwnerUserID:    scope.OwnerUserID,
		CallID:         call.CallID,
		Filename:       args.Filename,
		Content:        args.Content,
	})
	if err != nil {
		return nil, err
	}
	payload, err := marshalToolOutput(ConversationExportText, assistantAttachmentReference(scope, "text_export", attachment))
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}
