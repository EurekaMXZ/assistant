package tool

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type CreateSandboxInput struct {
	ConversationID string
	RequestKey     string
}

type CreateSandbox struct {
	Sandboxes ConversationSandboxStore
	Runtime   SandboxManager
	Locker    ConversationLocker
}

func (uc CreateSandbox) Execute(ctx context.Context, input CreateSandboxInput) (*domain.ConversationSandbox, error) {
	if uc.Sandboxes == nil {
		return nil, errors.New("create sandbox use case requires sandbox store")
	}
	if uc.Runtime == nil {
		return nil, errors.New("create sandbox use case requires sandbox runtime")
	}

	var result *domain.ConversationSandbox
	err := withConversationSandboxLock(ctx, uc.Locker, input.ConversationID, func(lockCtx context.Context) error {
		var executeErr error
		result, executeErr = uc.executeLocked(lockCtx, input)
		return executeErr
	})
	return result, err
}

func (uc CreateSandbox) executeLocked(ctx context.Context, input CreateSandboxInput) (*domain.ConversationSandbox, error) {
	if sandbox, err := uc.Sandboxes.GetUsableConversationSandbox(ctx, input.ConversationID); err == nil {
		if sandbox.Status == domain.SandboxStatusStopped {
			return uc.resume(ctx, sandbox, input.RequestKey)
		}
		if err := uc.Sandboxes.TouchConversationSandbox(ctx, sandbox.ID); err != nil {
			return nil, err
		}
		return uc.Sandboxes.GetUsableConversationSandbox(ctx, input.ConversationID)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	requestKey := sandboxRequestKey(input.RequestKey, "create", input.ConversationID)
	handle, err := uc.Runtime.CreateSandbox(ctx, input.ConversationID, requestKey)
	if err != nil {
		return nil, err
	}

	sandbox, err := uc.Sandboxes.CreateConversationSandbox(ctx, input.ConversationID, handle.Provider, handle.RuntimeID, handle.Metadata)
	if err != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		var activeSandbox *domain.ConversationSandbox
		if existing, getErr := uc.Sandboxes.GetUsableConversationSandbox(cleanupCtx, input.ConversationID); getErr == nil {
			if existing.Provider == handle.Provider && existing.RuntimeID == handle.RuntimeID {
				return existing, nil
			}
			activeSandbox = existing
		}
		if !handle.Reused {
			_, compensateErr := uc.Runtime.DestroySandbox(cleanupCtx, *handle, requestKey+":compensate")
			if compensateErr != nil {
				return nil, errors.Join(err, fmt.Errorf("compensate sandbox runtime creation: %w", compensateErr))
			}
		}
		if activeSandbox != nil {
			return activeSandbox, nil
		}
		if errors.Is(err, domain.ErrConflict) {
			if existing, getErr := uc.Sandboxes.GetUsableConversationSandbox(cleanupCtx, input.ConversationID); getErr == nil {
				return existing, nil
			}
		}
		return nil, err
	}

	return sandbox, nil
}

func (uc CreateSandbox) resume(ctx context.Context, sandbox *domain.ConversationSandbox, requestKey string) (*domain.ConversationSandbox, error) {
	key := sandboxRequestKey(requestKey, "resume", sandbox.ConversationID)
	handle, err := uc.Runtime.ResumeSandbox(ctx, sandboxHandle(sandbox), key)
	if err != nil {
		return nil, err
	}
	resumed, err := uc.Sandboxes.ResumeConversationSandbox(ctx, sandbox.ID, handle.Metadata)
	if err == nil {
		return resumed, nil
	}
	cleanupCtx := context.WithoutCancel(ctx)
	_, stopErr := uc.Runtime.StopSandbox(cleanupCtx, *handle, key+":compensate")
	if stopErr != nil {
		return nil, errors.Join(err, fmt.Errorf("compensate sandbox runtime resume: %w", stopErr))
	}
	return nil, err
}

func sandboxHandle(sandbox *domain.ConversationSandbox) domain.SandboxHandle {
	if sandbox == nil {
		return domain.SandboxHandle{}
	}
	return domain.SandboxHandle{Provider: sandbox.Provider, RuntimeID: sandbox.RuntimeID, Metadata: sandbox.RuntimeMetadata}
}

func sandboxRequestKey(requestKey string, operation string, conversationID string) string {
	if requestKey != "" {
		return requestKey
	}
	return "sandbox:" + operation + ":" + conversationID
}
