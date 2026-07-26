package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWithConversationLockReusesMatchingLockContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), lockedConnectionContextKey{}, &pgxpool.Conn{})
	ctx = context.WithValue(ctx, lockedConversationContextKey{}, "conv-1")
	called := false
	if err := WithConversationLock(ctx, nil, "conv-1", func(context.Context) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("reuse matching conversation lock: %v", err)
	}
	if !called {
		t.Fatal("matching conversation lock did not invoke callback")
	}
}
