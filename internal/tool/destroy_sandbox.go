package tool

import (
	"context"
	"errors"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
)

type DestroySandboxInput struct {
	ConversationID string
	RequestKey     string
}

type DestroySandbox struct {
	Sandboxes ConversationSandboxStore
	Runtime   SandboxManager
	Locker    ConversationLocker
}

func (uc DestroySandbox) Execute(ctx context.Context, input DestroySandboxInput) (*domain.ConversationSandbox, error) {
	if uc.Sandboxes == nil {
		return nil, errors.New("destroy sandbox use case requires sandbox store")
	}
	if uc.Runtime == nil {
		return nil, errors.New("destroy sandbox use case requires sandbox runtime")
	}

	var releasing *domain.ConversationSandbox
	claimToken := uuid.NewString()
	err := withConversationSandboxLock(ctx, uc.Locker, input.ConversationID, func(lockCtx context.Context) error {
		var prepareErr error
		releasing, prepareErr = uc.prepareReleaseLocked(lockCtx, input)
		if prepareErr != nil {
			return prepareErr
		}
		if releasing == nil {
			return errors.New("sandbox release preparation returned no sandbox")
		}
		if releasing.Status == domain.SandboxStatusDestroyed {
			return nil
		}
		releasing, prepareErr = uc.Sandboxes.ClaimConversationSandboxRelease(lockCtx, releasing.ID, claimToken, SandboxReleaseLeaseDuration)
		return prepareErr
	})
	if err != nil {
		return releasing, err
	}
	if releasing == nil {
		return nil, errors.New("sandbox release preparation returned no sandbox")
	}
	if releasing.Status == domain.SandboxStatusDestroyed {
		return releasing, nil
	}

	requestKey := sandboxRequestKey(input.RequestKey, "destroy", input.ConversationID)
	handle, err := RunSandboxReleaseOperation(ctx, uc.Sandboxes, releasing.ID, claimToken, func(operationCtx context.Context) (*domain.SandboxHandle, error) {
		return uc.Runtime.DestroySandbox(operationCtx, sandboxHandle(releasing), requestKey)
	})
	if err != nil {
		return nil, err
	}
	metadata := releasing.RuntimeMetadata
	if handle != nil && len(handle.Metadata) > 0 {
		metadata = handle.Metadata
	}
	completeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	destroyed, completeErr := uc.Sandboxes.CompleteConversationSandboxRelease(completeCtx, releasing.ID, claimToken, metadata)
	if completeErr != nil {
		latest, latestErr := uc.Sandboxes.GetLatestConversationSandbox(completeCtx, input.ConversationID)
		if latestErr == nil && latest.Status == domain.SandboxStatusDestroyed {
			return latest, nil
		}
		return nil, errors.Join(completeErr, latestErr)
	}
	return destroyed, completeErr
}

func (uc DestroySandbox) prepareReleaseLocked(ctx context.Context, input DestroySandboxInput) (*domain.ConversationSandbox, error) {
	sandbox, err := uc.Sandboxes.GetUsableConversationSandbox(ctx, input.ConversationID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			latest, latestErr := uc.Sandboxes.GetLatestConversationSandbox(ctx, input.ConversationID)
			if latestErr == nil {
				switch latest.Status {
				case domain.SandboxStatusDestroyed:
					return latest, nil
				case domain.SandboxStatusReleasing:
					sandbox = latest
					err = nil
				}
			}
		}
		if err != nil {
			return nil, err
		}
	}

	if sandbox.Status == domain.SandboxStatusReleasing {
		return sandbox, nil
	}
	return uc.Sandboxes.BeginConversationSandboxRelease(ctx, sandbox.ID)
}
