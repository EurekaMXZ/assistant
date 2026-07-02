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
}

func (uc CreateSandbox) Execute(ctx context.Context, input CreateSandboxInput) (*domain.ConversationSandbox, error) {
	if uc.Sandboxes == nil {
		return nil, errors.New("create sandbox use case requires sandbox store")
	}
	if uc.Runtime == nil {
		return nil, errors.New("create sandbox use case requires sandbox runtime")
	}

	if sandbox, err := uc.Sandboxes.GetActiveConversationSandbox(ctx, input.ConversationID); err == nil {
		return sandbox, nil
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
		_, compensateErr := uc.Runtime.DestroySandbox(ctx, *handle, requestKey+":compensate")
		if compensateErr != nil {
			return nil, errors.Join(err, fmt.Errorf("compensate sandbox runtime creation: %w", compensateErr))
		}
		if errors.Is(err, domain.ErrConflict) {
			existing, getErr := uc.Sandboxes.GetActiveConversationSandbox(ctx, input.ConversationID)
			if getErr == nil {
				return existing, nil
			}
		}
		return nil, err
	}

	return sandbox, nil
}

func sandboxRequestKey(requestKey string, operation string, conversationID string) string {
	if requestKey != "" {
		return requestKey
	}
	return "sandbox:" + operation + ":" + conversationID
}
