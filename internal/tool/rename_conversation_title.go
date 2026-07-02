package tool

import (
	"context"
	"errors"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type RenameConversationTitleInput struct {
	ConversationID string
	Title          string
}

type RenameConversationTitle struct {
	Conversations ConversationTitleUpdater
}

func (uc RenameConversationTitle) Execute(ctx context.Context, input RenameConversationTitleInput) (*domain.Conversation, error) {
	if uc.Conversations == nil {
		return nil, errors.New("rename conversation title use case requires conversation title updater")
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, errors.New("title is required")
	}

	return uc.Conversations.UpdateConversationTitle(ctx, input.ConversationID, title)
}
