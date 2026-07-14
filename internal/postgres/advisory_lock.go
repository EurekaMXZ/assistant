package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockRetry = 100 * time.Millisecond

type lockedConnectionContextKey struct{}

func WithConversationLock(ctx context.Context, pool *pgxpool.Pool, conversationID string, fn func(context.Context) error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire db connection for advisory lock: %w", err)
	}
	defer conn.Release()

	for {
		var locked bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1, 0))", conversationID).Scan(&locked); err != nil {
			return fmt.Errorf("acquire advisory lock: %w", err)
		}
		if locked {
			break
		}

		timer := time.NewTimer(advisoryLockRetry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", conversationID)
	}()

	return fn(context.WithValue(ctx, lockedConnectionContextKey{}, conn))
}

func lockedConnection(ctx context.Context) *pgxpool.Conn {
	conn, _ := ctx.Value(lockedConnectionContextKey{}).(*pgxpool.Conn)
	return conn
}
