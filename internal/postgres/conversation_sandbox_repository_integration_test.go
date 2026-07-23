package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConversationSandboxLifecycleIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	userID := uuid.NewString()
	conversationID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, username, password_hash, role, email_verified_at)
		VALUES ($1::uuid, $2, $3, 'integration-hash', 'user', now())
	`, userID, userID+"@example.com", "sandbox-"+userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO conversations (id, owner_user_id, title) VALUES ($1::uuid, $2::uuid, 'sandbox lifecycle integration')
	`, conversationID, userID); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}

	repository := NewConversationSandboxRepository(pool)
	sandbox, err := repository.CreateConversationSandbox(t.Context(), conversationID, "cubesandbox", "runtime-1", json.RawMessage(`{"state":"active"}`))
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	providers, err := repository.ListNonDestroyedSandboxProviders(t.Context())
	if err != nil {
		t.Fatalf("list non-destroyed providers: %v", err)
	}
	if len(providers) != 1 || providers[0] != "cubesandbox" {
		t.Fatalf("providers = %#v, want cubesandbox", providers)
	}
	firstToken := uuid.NewString()
	if err := repository.AcquireConversationSandboxExecution(t.Context(), sandbox.ID, firstToken, time.Minute); err != nil {
		t.Fatalf("acquire execution lease: %v", err)
	}
	if err := repository.AcquireConversationSandboxExecution(t.Context(), sandbox.ID, uuid.NewString(), time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second execution lease error = %v, want conflict", err)
	}
	if _, err := repository.StopConversationSandbox(t.Context(), sandbox.ID, sandbox.RuntimeMetadata); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stop leased sandbox error = %v, want conflict", err)
	}
	if _, err := repository.BeginConversationSandboxRelease(t.Context(), sandbox.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("release leased sandbox error = %v, want conflict", err)
	}
	if err := repository.RenewConversationSandboxExecution(t.Context(), sandbox.ID, firstToken, 2*time.Minute); err != nil {
		t.Fatalf("renew execution lease: %v", err)
	}
	if err := repository.CompleteConversationSandboxExecution(t.Context(), sandbox.ID, firstToken); err != nil {
		t.Fatalf("complete execution lease: %v", err)
	}

	stopped, err := repository.StopConversationSandbox(t.Context(), sandbox.ID, json.RawMessage(`{"state":"stopped"}`))
	if err != nil {
		t.Fatalf("stop sandbox: %v", err)
	}
	if stopped.Status != domain.SandboxStatusStopped || stopped.StoppedAt == nil {
		t.Fatalf("unexpected stopped sandbox: %#v", stopped)
	}
	resumed, err := repository.ResumeConversationSandbox(t.Context(), sandbox.ID, json.RawMessage(`{"state":"active"}`))
	if err != nil {
		t.Fatalf("resume sandbox: %v", err)
	}
	if resumed.Status != domain.SandboxStatusActive || resumed.StoppedAt != nil {
		t.Fatalf("unexpected resumed sandbox: %#v", resumed)
	}
	releasing, err := repository.BeginConversationSandboxRelease(t.Context(), sandbox.ID)
	if err != nil {
		t.Fatalf("begin sandbox release: %v", err)
	}
	if releasing.Status != domain.SandboxStatusReleasing || releasing.ReleasePreviousStatus != domain.SandboxStatusActive {
		t.Fatalf("unexpected releasing sandbox: %#v", releasing)
	}
	releaseToken := uuid.NewString()
	if _, err := repository.ClaimConversationSandboxRelease(t.Context(), sandbox.ID, releaseToken, time.Minute); err != nil {
		t.Fatalf("claim sandbox release: %v", err)
	}
	if _, err := repository.ClaimConversationSandboxRelease(t.Context(), sandbox.ID, uuid.NewString(), time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second release claim error = %v, want conflict", err)
	}
	if err := repository.RenewConversationSandboxReleaseClaim(t.Context(), sandbox.ID, releaseToken, 2*time.Minute); err != nil {
		t.Fatalf("renew sandbox release claim: %v", err)
	}
	destroyed, err := repository.CompleteConversationSandboxRelease(t.Context(), sandbox.ID, releaseToken, json.RawMessage(`{"state":"destroyed"}`))
	if err != nil {
		t.Fatalf("complete sandbox release: %v", err)
	}
	if destroyed.Status != domain.SandboxStatusDestroyed || destroyed.DestroyedAt == nil {
		t.Fatalf("unexpected destroyed sandbox: %#v", destroyed)
	}
	providers, err = repository.ListNonDestroyedSandboxProviders(t.Context())
	if err != nil {
		t.Fatalf("list providers after destroy: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("providers after destroy = %#v, want empty", providers)
	}
}

func TestConversationSandboxQuotaIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	userID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, username, password_hash, role, email_verified_at, sandbox_quota)
		VALUES ($1::uuid, $2, $3, 'integration-hash', 'user', now(), 2)
	`, userID, userID+"@example.com", "sandbox-quota-"+userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, userID)
	})

	conversationIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, conversationID := range conversationIDs {
		if _, err := pool.Exec(t.Context(), `
			INSERT INTO conversations (id, owner_user_id, title) VALUES ($1::uuid, $2::uuid, 'sandbox quota integration')
		`, conversationID, userID); err != nil {
			t.Fatalf("insert conversation: %v", err)
		}
	}

	repository := NewConversationSandboxRepository(pool)
	first, err := repository.CreateConversationSandbox(t.Context(), conversationIDs[0], "cubesandbox", "runtime-1", nil)
	if err != nil {
		t.Fatalf("create first sandbox: %v", err)
	}
	if _, err := repository.CreateConversationSandbox(t.Context(), conversationIDs[1], "cubesandbox", "runtime-2", nil); err != nil {
		t.Fatalf("create second sandbox: %v", err)
	}
	if _, err := repository.CreateConversationSandbox(t.Context(), conversationIDs[2], "cubesandbox", "runtime-3", nil); !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "sandbox quota exceeded") {
		t.Fatalf("third sandbox error = %v", err)
	}
	if _, err := repository.StopConversationSandbox(t.Context(), first.ID, nil); err != nil {
		t.Fatalf("stop first sandbox: %v", err)
	}
	if _, err := repository.CreateConversationSandbox(t.Context(), conversationIDs[2], "cubesandbox", "runtime-3", nil); err != nil {
		t.Fatalf("create sandbox after releasing slot: %v", err)
	}
	if _, err := repository.ResumeConversationSandbox(t.Context(), first.ID, nil); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("resume over quota error = %v", err)
	}
}
