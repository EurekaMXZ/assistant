package tool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

const (
	sandboxFileWriteTimeout         = 2 * time.Minute
	maxSandboxFileWriteContentBytes = 1 << 20
)

type WriteSandboxFileInput struct {
	ConversationID string
	Path           string
	Content        string
	RequestKey     string
}

type SandboxFileWriteResult struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type WriteSandboxFile struct {
	Sandboxes ConversationSandboxStore
	Runtime   SandboxManager
	Locker    ConversationLocker
}

func (uc WriteSandboxFile) Execute(ctx context.Context, input WriteSandboxFileInput) (*SandboxFileWriteResult, error) {
	if uc.Sandboxes == nil || uc.Runtime == nil {
		return nil, errors.New("write sandbox file is not configured")
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
	if !utf8.ValidString(input.Content) {
		return nil, domain.NewValidationError("content must be valid UTF-8")
	}
	size := int64(len(input.Content))
	if size > maxSandboxFileWriteContentBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("content exceeds %d bytes", maxSandboxFileWriteContentBytes))
	}

	content := []byte(input.Content)
	operationCtx, cancel := context.WithTimeout(ctx, sandboxFileWriteTimeout)
	defer cancel()
	return runConversationSandboxExecution(operationCtx, uc.Sandboxes, uc.Runtime, uc.Locker, conversationID, input.RequestKey, sandboxFileWriteTimeout+sandboxExecutionLeaseBuffer, func(writeCtx context.Context, handle domain.SandboxHandle) (*SandboxFileWriteResult, error) {
		resolvedPath, err := resolveSandboxWritePath(writeCtx, uc.Runtime, handle, sandboxPath, input.RequestKey)
		if err != nil {
			return nil, err
		}
		return writeSandboxFileAtomically(writeCtx, uc.Runtime, handle, resolvedPath, content, input.RequestKey)
	})
}

func writeSandboxFileAtomically(ctx context.Context, runtime SandboxManager, handle domain.SandboxHandle, resolvedPath string, content []byte, requestKey string) (*SandboxFileWriteResult, error) {
	digest := sha256.Sum256(content)
	mkdir, err := runtime.ExecSandboxCommand(ctx, handle, domain.SandboxCommandRequest{
		Command: "mkdir", Args: []string{"-p", "--", path.Dir(resolvedPath)}, WorkingDirectory: "/workspace", TimeoutSeconds: 30,
	}, requestKey+":mkdir")
	if err != nil {
		return nil, fmt.Errorf("create sandbox file directory: %w", err)
	}
	if mkdir == nil || mkdir.ExitCode != 0 {
		output := ""
		if mkdir != nil {
			output = strings.TrimSpace(mkdir.Output)
		}
		return nil, fmt.Errorf("create sandbox file directory failed: %s", output)
	}
	temporaryPath := resolvedPath + ".assistant-write-" + safeAssistantAttachmentKeyPart(requestKey)
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cleanupCancel()
		_, _ = runtime.ExecSandboxCommand(cleanupCtx, handle, domain.SandboxCommandRequest{
			Command: "rm", Args: []string{"-f", "--", temporaryPath}, WorkingDirectory: "/workspace", TimeoutSeconds: 10,
		}, requestKey+":cleanup")
	}()
	size := int64(len(content))
	if err := runtime.WriteSandboxFile(ctx, handle, temporaryPath, bytes.NewReader(content), size, requestKey+":stream"); err != nil {
		return nil, fmt.Errorf("write sandbox file content: %w", err)
	}
	move, err := runtime.ExecSandboxCommand(ctx, handle, domain.SandboxCommandRequest{
		Command: "mv", Args: []string{"--", temporaryPath, resolvedPath}, WorkingDirectory: "/workspace", TimeoutSeconds: 30,
	}, requestKey+":commit")
	if err != nil {
		return nil, fmt.Errorf("commit sandbox file: %w", err)
	}
	if move == nil || move.ExitCode != 0 {
		output := ""
		if move != nil {
			output = strings.TrimSpace(move.Output)
		}
		return nil, fmt.Errorf("commit sandbox file failed: %s", output)
	}
	committed = true
	return &SandboxFileWriteResult{
		Path: resolvedPath, SizeBytes: size, SHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func resolveSandboxWritePath(ctx context.Context, runtime SandboxManager, handle domain.SandboxHandle, value string, requestKey string) (string, error) {
	result, err := runtime.ExecSandboxCommand(ctx, handle, domain.SandboxCommandRequest{
		Command: "readlink", Args: []string{"-m", "--", value}, WorkingDirectory: "/workspace", TimeoutSeconds: 30,
	}, requestKey+":resolve")
	if err != nil {
		return "", fmt.Errorf("resolve sandbox write path: %w", err)
	}
	if result == nil || result.ExitCode != 0 {
		return "", domain.NewValidationError("sandbox write path could not be resolved")
	}
	resolved := strings.TrimSpace(result.Output)
	if resolved == "/workspace" || !strings.HasPrefix(resolved, "/workspace/") || strings.Contains(resolved, "\n") {
		return "", domain.NewValidationError("resolved sandbox write path must be inside /workspace")
	}
	return resolved, nil
}
