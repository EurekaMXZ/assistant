package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type EditSandboxFileInput struct {
	ConversationID string
	Path           string
	OldText        string
	NewText        string
	ReplaceAll     bool
	RequestKey     string
}

type SandboxFileEditResult struct {
	Path         string `json:"path"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	Replacements int    `json:"replacements"`
}

type EditSandboxFile struct {
	Sandboxes ConversationSandboxStore
	Runtime   SandboxManager
	Files     SandboxFileReader
	Locker    ConversationLocker
}

func (uc EditSandboxFile) Execute(ctx context.Context, input EditSandboxFileInput) (*SandboxFileEditResult, error) {
	if uc.Sandboxes == nil || uc.Runtime == nil || uc.Files == nil {
		return nil, errors.New("edit sandbox file is not configured")
	}
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		return nil, domain.NewValidationError("conversation id is required")
	}
	sandboxPath, err := normalizeSandboxWorkspacePath(input.Path)
	if err != nil {
		return nil, err
	}
	if len(sandboxPath) > 4096 {
		return nil, domain.NewValidationError("path is too long")
	}
	if input.OldText == "" {
		return nil, domain.NewValidationError("old_text is required")
	}
	if !utf8.ValidString(input.OldText) || !utf8.ValidString(input.NewText) {
		return nil, domain.NewValidationError("old_text and new_text must be valid UTF-8")
	}
	if len(input.OldText) > maxSandboxFileWriteContentBytes || len(input.NewText) > maxSandboxFileWriteContentBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("replacement text exceeds %d bytes", maxSandboxFileWriteContentBytes))
	}

	operationCtx, cancel := context.WithTimeout(ctx, sandboxFileWriteTimeout)
	defer cancel()
	return runConversationSandboxExecution(operationCtx, uc.Sandboxes, uc.Runtime, uc.Locker, conversationID, input.RequestKey, sandboxFileWriteTimeout+sandboxExecutionLeaseBuffer, func(editCtx context.Context, handle domain.SandboxHandle) (*SandboxFileEditResult, error) {
		resolvedPath, err := resolveSandboxWritePath(editCtx, uc.Runtime, handle, sandboxPath, input.RequestKey)
		if err != nil {
			return nil, err
		}
		reader, reportedSize, err := uc.Files.ReadSandboxFile(editCtx, handle, resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("read sandbox file for editing: %w", err)
		}
		if reader == nil {
			return nil, errors.New("read sandbox file for editing returned no content")
		}
		if reportedSize > maxSandboxFileWriteContentBytes {
			_ = reader.Close()
			return nil, domain.NewValidationError(fmt.Sprintf("file exceeds editable size of %d bytes", maxSandboxFileWriteContentBytes))
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, maxSandboxFileWriteContentBytes+1))
		if closeErr := reader.Close(); readErr == nil {
			readErr = closeErr
		}
		if readErr != nil {
			return nil, fmt.Errorf("read sandbox file for editing: %w", readErr)
		}
		if len(content) > maxSandboxFileWriteContentBytes {
			return nil, domain.NewValidationError(fmt.Sprintf("file exceeds editable size of %d bytes", maxSandboxFileWriteContentBytes))
		}
		if reportedSize >= 0 && reportedSize != int64(len(content)) {
			return nil, errors.New("sandbox file size changed while reading")
		}
		if !utf8.Valid(content) {
			return nil, domain.NewValidationError("sandbox file must contain valid UTF-8 text")
		}

		existing := string(content)
		replacements := strings.Count(existing, input.OldText)
		if replacements == 0 {
			return nil, domain.NewValidationError("old_text was not found in the sandbox file")
		}
		if replacements > 1 && !input.ReplaceAll {
			return nil, domain.NewValidationError(fmt.Sprintf("old_text occurs %d times; provide more context or set replace_all", replacements))
		}
		limit := 1
		if input.ReplaceAll {
			limit = -1
		}
		edited := []byte(strings.Replace(existing, input.OldText, input.NewText, limit))
		if len(edited) > maxSandboxFileWriteContentBytes {
			return nil, domain.NewValidationError(fmt.Sprintf("edited file exceeds %d bytes", maxSandboxFileWriteContentBytes))
		}
		written, err := writeSandboxFileAtomically(editCtx, uc.Runtime, handle, resolvedPath, edited, input.RequestKey)
		if err != nil {
			return nil, err
		}
		if !input.ReplaceAll {
			replacements = 1
		}
		return &SandboxFileEditResult{
			Path: written.Path, SizeBytes: written.SizeBytes, SHA256: written.SHA256, Replacements: replacements,
		}, nil
	})
}
