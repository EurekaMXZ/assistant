package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

const maxSandboxShellCommandBytes = 16 << 10

type CreateSandboxShellInput struct {
	ConversationID   string
	WorkingDirectory string
	RequestKey       string
}

type ConnectSandboxShellInput struct {
	ConversationID string
	SessionID      string
	Command        string
	TimeoutSeconds int
	RequestKey     string
}

type DestroySandboxShellInput struct {
	ConversationID string
	SessionID      string
	RequestKey     string
}

type CreateSandboxShell struct {
	Sandboxes ConversationSandboxStore
	Runtime   SandboxManager
	Shells    SandboxShellManager
	Locker    ConversationLocker
}

type ConnectSandboxShell struct {
	Sandboxes      ConversationSandboxStore
	Runtime        SandboxManager
	Shells         SandboxShellManager
	Locker         ConversationLocker
	DefaultTimeout time.Duration
	MaximumTimeout time.Duration
}

type DestroySandboxShell struct {
	Sandboxes ConversationSandboxStore
	Runtime   SandboxManager
	Shells    SandboxShellManager
	Locker    ConversationLocker
}

func (uc CreateSandboxShell) Execute(ctx context.Context, input CreateSandboxShellInput) (*domain.SandboxShellSession, error) {
	if uc.Sandboxes == nil || uc.Runtime == nil || uc.Shells == nil {
		return nil, errors.New("create sandbox shell is not configured")
	}
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		return nil, domain.NewValidationError("conversation id is required")
	}
	workingDirectory, err := normalizeSandboxShellWorkingDirectory(input.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	sessionID := sandboxShellSessionID(input.RequestKey)
	return runConversationSandboxExecution(ctx, uc.Sandboxes, uc.Runtime, uc.Locker, conversationID, input.RequestKey, time.Minute+sandboxExecutionLeaseBuffer, func(shellCtx context.Context, handle domain.SandboxHandle) (*domain.SandboxShellSession, error) {
		return uc.Shells.CreateSandboxShell(shellCtx, handle, domain.SandboxShellCreateRequest{
			SessionID: sessionID, WorkingDirectory: workingDirectory,
		}, input.RequestKey)
	})
}

func (uc ConnectSandboxShell) Execute(ctx context.Context, input ConnectSandboxShellInput) (*domain.SandboxShellCommandResult, error) {
	if uc.Sandboxes == nil || uc.Runtime == nil || uc.Shells == nil {
		return nil, errors.New("connect sandbox shell is not configured")
	}
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		return nil, domain.NewValidationError("conversation id is required")
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if !validSandboxShellSessionID(sessionID) {
		return nil, domain.NewValidationError("invalid shell session_id")
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return nil, domain.NewValidationError("command is required")
	}
	if strings.ContainsAny(command, "\r\n") {
		return nil, domain.NewValidationError("command must be one line; write multi-line scripts with sandbox.write_file")
	}
	if len(command) > maxSandboxShellCommandBytes {
		return nil, domain.NewValidationError(fmt.Sprintf("command exceeds %d bytes", maxSandboxShellCommandBytes))
	}
	defaultTimeout := uc.DefaultTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	maximumTimeout := uc.MaximumTimeout
	if maximumTimeout <= 0 {
		maximumTimeout = 5 * time.Minute
	}
	if input.TimeoutSeconds < 0 {
		return nil, domain.NewValidationError("timeout_seconds must not be negative")
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout > maximumTimeout {
		return nil, domain.NewValidationError(fmt.Sprintf("timeout_seconds must be %d or less", int(maximumTimeout/time.Second)))
	}
	timeoutSeconds := int(timeout / time.Second)
	return runConversationSandboxExecution(ctx, uc.Sandboxes, uc.Runtime, uc.Locker, conversationID, input.RequestKey, timeout+sandboxExecutionLeaseBuffer, func(shellCtx context.Context, handle domain.SandboxHandle) (*domain.SandboxShellCommandResult, error) {
		return uc.Shells.ExecSandboxShell(shellCtx, handle, domain.SandboxShellCommandRequest{
			SessionID: sessionID, Command: command, TimeoutSeconds: timeoutSeconds,
		}, input.RequestKey)
	})
}

func (uc DestroySandboxShell) Execute(ctx context.Context, input DestroySandboxShellInput) (*domain.SandboxShellSession, error) {
	if uc.Sandboxes == nil || uc.Runtime == nil || uc.Shells == nil {
		return nil, errors.New("destroy sandbox shell is not configured")
	}
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		return nil, domain.NewValidationError("conversation id is required")
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if !validSandboxShellSessionID(sessionID) {
		return nil, domain.NewValidationError("invalid shell session_id")
	}
	return runConversationSandboxExecution(ctx, uc.Sandboxes, uc.Runtime, uc.Locker, conversationID, input.RequestKey, time.Minute+sandboxExecutionLeaseBuffer, func(shellCtx context.Context, handle domain.SandboxHandle) (*domain.SandboxShellSession, error) {
		return uc.Shells.DestroySandboxShell(shellCtx, handle, sessionID, input.RequestKey)
	})
}

func normalizeSandboxShellWorkingDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/workspace", nil
	}
	if !path.IsAbs(value) {
		value = path.Join("/workspace", value)
	}
	value = path.Clean(value)
	if value != "/workspace" && !strings.HasPrefix(value, "/workspace/") {
		return "", domain.NewValidationError("working_directory must be inside /workspace")
	}
	return value, nil
}

func sandboxShellSessionID(requestKey string) string {
	requestKey = strings.TrimSpace(requestKey)
	if requestKey == "" {
		return "shell-" + uuid.NewString()
	}
	digest := sha256.Sum256([]byte(requestKey))
	return "shell-" + hex.EncodeToString(digest[:12])
}

func validSandboxShellSessionID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
