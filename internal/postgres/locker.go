package postgres

import (
	"context"

	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ workflow.ConversationLocker = (*ConversationLocker)(nil)

type ConversationLocker struct {
	pool *pgxpool.Pool
}

func NewConversationLocker(pool *pgxpool.Pool) *ConversationLocker {
	return &ConversationLocker{
		pool: pool,
	}
}

func (l *ConversationLocker) WithConversationLock(ctx context.Context, conversationID string, fn func(context.Context) error) error {
	return WithConversationLock(ctx, l.pool, conversationID, fn)
}
