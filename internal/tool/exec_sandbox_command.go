package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type ExecSandboxCommandInput struct {
	ConversationID   string
	Command          string
	Args             []string
	WorkingDirectory string
	TimeoutSeconds   int
	RequestKey       string
}

type ExecSandboxCommand struct {
	Sandboxes      ConversationSandboxStore
	Runtime        SandboxManager
	Locker         ConversationLocker
	DefaultTimeout time.Duration
	MaximumTimeout time.Duration
}

func (uc ExecSandboxCommand) Execute(ctx context.Context, input ExecSandboxCommandInput) (*domain.SandboxCommandResult, error) {
	if uc.Sandboxes == nil {
		return nil, errors.New("exec sandbox command use case requires sandbox store")
	}
	if uc.Runtime == nil {
		return nil, errors.New("exec sandbox command use case requires sandbox runtime")
	}

	command := strings.TrimSpace(input.Command)
	if command == "" {
		return nil, errors.New("command is required")
	}
	defaultTimeout := uc.DefaultTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	maximumTimeout := uc.MaximumTimeout
	if maximumTimeout <= 0 {
		maximumTimeout = 5 * time.Minute
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	if input.TimeoutSeconds < 0 {
		return nil, domain.NewValidationError("timeout_seconds must not be negative")
	}
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout > maximumTimeout {
		return nil, domain.NewValidationError(fmt.Sprintf("timeout_seconds must be %d or less", int(maximumTimeout/time.Second)))
	}
	timeoutSeconds := int(timeout / time.Second)

	leaseDuration := timeout + sandboxExecutionLeaseBuffer
	return runConversationSandboxExecution(ctx, uc.Sandboxes, uc.Runtime, uc.Locker, input.ConversationID, input.RequestKey, leaseDuration, func(execCtx context.Context, handle domain.SandboxHandle) (*domain.SandboxCommandResult, error) {
		return uc.Runtime.ExecSandboxCommand(execCtx, handle, domain.SandboxCommandRequest{
			Command:          command,
			Args:             append([]string(nil), input.Args...),
			WorkingDirectory: strings.TrimSpace(input.WorkingDirectory),
			TimeoutSeconds:   timeoutSeconds,
		}, input.RequestKey)
	})
}
