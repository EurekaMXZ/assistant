package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
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

const (
	sandboxExecutionRenewInterval = 20 * time.Second
	sandboxExecutionLeaseBuffer   = 5 * time.Minute
)

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

	var sandbox *domain.ConversationSandbox
	executionToken := uuid.NewString()
	leaseDuration := timeout + sandboxExecutionLeaseBuffer
	err := withConversationSandboxLock(ctx, uc.Locker, input.ConversationID, func(lockCtx context.Context) error {
		var err error
		sandbox, err = uc.Sandboxes.GetUsableConversationSandbox(lockCtx, input.ConversationID)
		if err != nil {
			return err
		}
		if sandbox.Status == domain.SandboxStatusStopped {
			handle, resumeErr := uc.Runtime.ResumeSandbox(lockCtx, sandboxHandle(sandbox), sandboxRequestKey(input.RequestKey, "resume", input.ConversationID))
			if resumeErr != nil {
				return resumeErr
			}
			if handle == nil {
				return errors.New("sandbox runtime returned an empty resume handle")
			}
			resumed, resumeErr := uc.Sandboxes.ResumeConversationSandbox(lockCtx, sandbox.ID, handle.Metadata)
			if resumeErr != nil {
				_, stopErr := uc.Runtime.StopSandbox(context.WithoutCancel(lockCtx), *handle, sandboxRequestKey(input.RequestKey, "resume", input.ConversationID)+":compensate")
				return errors.Join(resumeErr, stopErr)
			}
			sandbox = resumed
		}
		return uc.Sandboxes.AcquireConversationSandboxExecution(lockCtx, sandbox.ID, executionToken, leaseDuration)
	})
	if err != nil {
		return nil, err
	}

	execCtx, cancelExec := context.WithCancel(ctx)
	renewCtx, cancelRenew := context.WithCancel(ctx)
	renewErr := make(chan error, 1)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(sandboxExecutionRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				attemptCtx, cancel := context.WithTimeout(renewCtx, 5*time.Second)
				err := withConversationSandboxLock(attemptCtx, uc.Locker, input.ConversationID, func(lockCtx context.Context) error {
					return uc.Sandboxes.RenewConversationSandboxExecution(lockCtx, sandbox.ID, executionToken, leaseDuration)
				})
				cancel()
				if err != nil {
					if renewCtx.Err() != nil {
						return
					}
					renewErr <- err
					cancelExec()
					return
				}
			}
		}
	}()

	result, runErr := uc.Runtime.ExecSandboxCommand(execCtx, sandboxHandle(sandbox), domain.SandboxCommandRequest{
		Command:          command,
		Args:             append([]string(nil), input.Args...),
		WorkingDirectory: strings.TrimSpace(input.WorkingDirectory),
		TimeoutSeconds:   timeoutSeconds,
	}, input.RequestKey)
	cancelExec()
	cancelRenew()
	<-renewDone
	var leaseErr error
	select {
	case leaseErr = <-renewErr:
	default:
	}
	completeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	completeErr := uc.Sandboxes.CompleteConversationSandboxExecution(completeCtx, sandbox.ID, executionToken)
	cancel()
	if err := errors.Join(runErr, leaseErr, completeErr); err != nil {
		return result, err
	}
	return result, nil
}
