package workflow

import "context"

type ConversationLocker interface {
	WithConversationLock(ctx context.Context, conversationID string, fn func(context.Context) error) error
}

type ConversationLockerFunc func(ctx context.Context, conversationID string, fn func(context.Context) error) error

func (f ConversationLockerFunc) WithConversationLock(ctx context.Context, conversationID string, fn func(context.Context) error) error {
	return f(ctx, conversationID, fn)
}
