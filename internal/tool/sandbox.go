package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

type ConversationSandboxReader interface {
	GetActiveConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error)
	GetUsableConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error)
}

type ConversationSandboxStore interface {
	ConversationSandboxReader
	GetLatestConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error)
	CreateConversationSandbox(ctx context.Context, conversationID string, provider string, runtimeID string, metadata json.RawMessage) (*domain.ConversationSandbox, error)
	StopConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error)
	ResumeConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error)
	TouchConversationSandbox(ctx context.Context, sandboxID string) error
	AcquireConversationSandboxExecution(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) error
	RenewConversationSandboxExecution(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) error
	CompleteConversationSandboxExecution(ctx context.Context, sandboxID string, token string) error
	ListIdleConversationSandboxes(ctx context.Context, inactiveBefore time.Time, limit int) ([]*domain.ConversationSandbox, error)
	ListStoppedConversationSandboxes(ctx context.Context, stoppedBefore time.Time, limit int) ([]*domain.ConversationSandbox, error)
	ListReleasingConversationSandboxes(ctx context.Context, limit int) ([]*domain.ConversationSandbox, error)
	BeginConversationSandboxRelease(ctx context.Context, sandboxID string) (*domain.ConversationSandbox, error)
	ClaimConversationSandboxRelease(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) (*domain.ConversationSandbox, error)
	RenewConversationSandboxReleaseClaim(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) error
	CompleteConversationSandboxRelease(ctx context.Context, sandboxID string, token string, metadata json.RawMessage) (*domain.ConversationSandbox, error)
}

const (
	SandboxReleaseLeaseDuration   = 10 * time.Minute
	sandboxReleaseRenewInterval   = 2 * time.Minute
	sandboxExecutionRenewInterval = 20 * time.Second
	sandboxExecutionLeaseBuffer   = 5 * time.Minute
)

func RunSandboxReleaseOperation(ctx context.Context, store ConversationSandboxStore, sandboxID string, token string, operation func(context.Context) (*domain.SandboxHandle, error)) (*domain.SandboxHandle, error) {
	operationCtx, cancelOperation := context.WithCancel(ctx)
	renewCtx, cancelRenew := context.WithCancel(ctx)
	renewErr := make(chan error, 1)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(sandboxReleaseRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				attemptCtx, cancel := context.WithTimeout(renewCtx, 5*time.Second)
				err := store.RenewConversationSandboxReleaseClaim(attemptCtx, sandboxID, token, SandboxReleaseLeaseDuration)
				cancel()
				if err != nil {
					if renewCtx.Err() != nil {
						return
					}
					renewErr <- err
					cancelOperation()
					return
				}
			}
		}
	}()

	handle, operationErr := operation(operationCtx)
	cancelOperation()
	cancelRenew()
	<-renewDone
	if operationErr == nil {
		return handle, nil
	}
	select {
	case err := <-renewErr:
		return handle, errors.Join(operationErr, err)
	default:
		return handle, operationErr
	}
}

type ConversationLocker interface {
	WithConversationLock(ctx context.Context, conversationID string, fn func(context.Context) error) error
}

func withConversationSandboxLock(ctx context.Context, locker ConversationLocker, conversationID string, fn func(context.Context) error) error {
	if locker == nil {
		return fn(ctx)
	}
	return locker.WithConversationLock(ctx, conversationID, fn)
}

func runConversationSandboxExecution[T any](ctx context.Context, store ConversationSandboxStore, runtime SandboxManager, locker ConversationLocker, conversationID string, requestKey string, leaseDuration time.Duration, operation func(context.Context, domain.SandboxHandle) (T, error)) (T, error) {
	var zero T
	var sandbox *domain.ConversationSandbox
	executionToken := uuid.NewString()
	err := withConversationSandboxLock(ctx, locker, conversationID, func(lockCtx context.Context) error {
		var err error
		sandbox, err = store.GetUsableConversationSandbox(lockCtx, conversationID)
		if err != nil {
			return err
		}
		if sandbox.Status == domain.SandboxStatusStopped {
			handle, resumeErr := runtime.ResumeSandbox(lockCtx, sandboxHandle(sandbox), sandboxRequestKey(requestKey, "resume", conversationID))
			if resumeErr != nil {
				return resumeErr
			}
			if handle == nil {
				return errors.New("sandbox runtime returned an empty resume handle")
			}
			resumed, resumeErr := store.ResumeConversationSandbox(lockCtx, sandbox.ID, handle.Metadata)
			if resumeErr != nil {
				_, stopErr := runtime.StopSandbox(context.WithoutCancel(lockCtx), *handle, sandboxRequestKey(requestKey, "resume", conversationID)+":compensate")
				return errors.Join(resumeErr, stopErr)
			}
			sandbox = resumed
		}
		return store.AcquireConversationSandboxExecution(lockCtx, sandbox.ID, executionToken, leaseDuration)
	})
	if err != nil {
		return zero, err
	}

	operationCtx, cancelOperation := context.WithCancel(ctx)
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
				err := withConversationSandboxLock(attemptCtx, locker, conversationID, func(lockCtx context.Context) error {
					return store.RenewConversationSandboxExecution(lockCtx, sandbox.ID, executionToken, leaseDuration)
				})
				cancel()
				if err != nil {
					if renewCtx.Err() != nil {
						return
					}
					renewErr <- err
					cancelOperation()
					return
				}
			}
		}
	}()

	result, operationErr := operation(operationCtx, sandboxHandle(sandbox))
	cancelOperation()
	cancelRenew()
	<-renewDone
	var leaseErr error
	select {
	case leaseErr = <-renewErr:
	default:
	}
	completeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	completeErr := store.CompleteConversationSandboxExecution(completeCtx, sandbox.ID, executionToken)
	cancel()
	return result, errors.Join(operationErr, leaseErr, completeErr)
}

type SandboxManager interface {
	CreateSandbox(ctx context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error)
	StopSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error)
	ResumeSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error)
	DestroySandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error)
	ExecSandboxCommand(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, requestKey string) (*domain.SandboxCommandResult, error)
	WriteSandboxFile(ctx context.Context, handle domain.SandboxHandle, path string, reader io.Reader, size int64, requestKey string) error
}

type SandboxFileReader interface {
	ReadSandboxFile(ctx context.Context, handle domain.SandboxHandle, path string) (io.ReadCloser, int64, error)
}

type SandboxShellManager interface {
	CreateSandboxShell(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxShellCreateRequest, requestKey string) (*domain.SandboxShellSession, error)
	ExecSandboxShell(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxShellCommandRequest, requestKey string) (*domain.SandboxShellCommandResult, error)
	DestroySandboxShell(ctx context.Context, handle domain.SandboxHandle, sessionID string, requestKey string) (*domain.SandboxShellSession, error)
}
