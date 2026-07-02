package postgres

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutboxClaimsHaveExclusiveLeaseOwnershipIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	eventID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO outbox_events (id, event_type, created_at)
		VALUES ($1::uuid, 'integration.claim', now() - interval '100 years')
	`, eventID); err != nil {
		t.Fatalf("insert outbox fixture: %v", err)
	}
	repository := NewWorkflowOutboxRepository(pool)
	type claimResult struct {
		id    string
		token string
		err   error
	}
	results := make(chan claimResult, 2)
	for range 2 {
		go func() {
			items, err := repository.ClaimPendingOutboxEvents(t.Context(), time.Minute, 1)
			result := claimResult{err: err}
			if len(items) == 1 {
				result.id = items[0].ID
				result.token = items[0].ClaimToken
			}
			results <- result
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent outbox claims: first=%#v second=%#v", first, second)
	}
	if first.id == eventID && second.id == eventID {
		t.Fatalf("outbox event claimed by two workers: first=%#v second=%#v", first, second)
	}
	owner := first
	if second.id == eventID {
		owner = second
	}
	if owner.id != eventID || owner.token == "" {
		t.Fatalf("fixture event was not leased: first=%#v second=%#v", first, second)
	}
	if err := repository.MarkOutboxPublished(t.Context(), eventID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("non-owner publish error = %v, want not found", err)
	}
	if err := repository.MarkOutboxPublished(t.Context(), eventID, owner.token); err != nil {
		t.Fatalf("owner publish: %v", err)
	}
	for _, result := range []claimResult{first, second} {
		if result.id != "" && result.id != eventID {
			_ = repository.MarkOutboxPublishError(t.Context(), result.id, result.token, "release integration claim")
		}
	}

	expiredID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO outbox_events (id, event_type, created_at)
		VALUES ($1::uuid, 'integration.expiry', now() - interval '99 years')
	`, expiredID); err != nil {
		t.Fatalf("insert expiring outbox fixture: %v", err)
	}
	initial, err := repository.ClaimPendingOutboxEvents(t.Context(), time.Minute, 1)
	if err != nil || len(initial) != 1 || initial[0].ID != expiredID {
		t.Fatalf("initial expiry claim=%#v err=%v", initial, err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE outbox_events SET claimed_at = now() - interval '2 minutes' WHERE id = $1::uuid`, expiredID); err != nil {
		t.Fatalf("expire outbox claim: %v", err)
	}
	reclaimed, err := repository.ClaimPendingOutboxEvents(t.Context(), time.Minute, 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != expiredID {
		t.Fatalf("reclaimed expiry event=%#v err=%v", reclaimed, err)
	}
	if reclaimed[0].ClaimToken == initial[0].ClaimToken {
		t.Fatal("expired outbox lease reused claim token")
	}
	if err := repository.MarkOutboxPublished(t.Context(), expiredID, reclaimed[0].ClaimToken); err != nil {
		t.Fatalf("publish reclaimed event: %v", err)
	}
}
