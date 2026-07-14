package tool

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
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
	SandboxReleaseLeaseDuration = 10 * time.Minute
	sandboxReleaseRenewInterval = 2 * time.Minute
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

type SandboxManager interface {
	CreateSandbox(ctx context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error)
	StopSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error)
	ResumeSandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error)
	DestroySandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error)
	ExecSandboxCommand(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, requestKey string) (*domain.SandboxCommandResult, error)
}
