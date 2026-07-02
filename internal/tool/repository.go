package tool

import (
	"context"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type ConversationTitleUpdater interface {
	UpdateConversationTitle(ctx context.Context, conversationID string, title string) (*domain.Conversation, error)
}
