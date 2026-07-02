package tool

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type DestroySandboxInput struct {
	ConversationID string
	RequestKey     string
}

type DestroySandbox struct {
	Sandboxes ConversationSandboxStore
	Runtime   SandboxManager
}

func (uc DestroySandbox) Execute(ctx context.Context, input DestroySandboxInput) (*domain.ConversationSandbox, error) {
	if uc.Sandboxes == nil {
		return nil, errors.New("destroy sandbox use case requires sandbox store")
	}
	if uc.Runtime == nil {
		return nil, errors.New("destroy sandbox use case requires sandbox runtime")
	}

	sandbox, err := uc.Sandboxes.GetActiveConversationSandbox(ctx, input.ConversationID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			latest, latestErr := uc.Sandboxes.GetLatestConversationSandbox(ctx, input.ConversationID)
			if latestErr == nil && latest.Status == domain.SandboxStatusDestroyed {
				return latest, nil
			}
		}
		return nil, err
	}

	destroyed, err := uc.Sandboxes.DestroyConversationSandbox(ctx, sandbox.ID, sandbox.RuntimeMetadata)
	if err != nil {
		return nil, err
	}
	requestKey := sandboxRequestKey(input.RequestKey, "destroy", input.ConversationID)
	_, err = uc.Runtime.DestroySandbox(ctx, domain.SandboxHandle{
		Provider:  sandbox.Provider,
		RuntimeID: sandbox.RuntimeID,
		Metadata:  sandbox.RuntimeMetadata,
	}, requestKey)
	if err != nil {
		if _, compensateErr := uc.Sandboxes.RestoreConversationSandbox(ctx, sandbox.ID, sandbox.RuntimeMetadata); compensateErr != nil {
			return nil, errors.Join(err, fmt.Errorf("compensate sandbox database destruction: %w", compensateErr))
		}
		return nil, err
	}

	return destroyed, nil
}
