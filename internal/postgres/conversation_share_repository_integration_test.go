package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConversationShareCreationIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	ownerID := uuid.NewString()
	otherUserID := uuid.NewString()
	conversationID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, username, password_hash, role, email_verified_at)
		VALUES
			($1::uuid, $2, $3, 'integration-hash', 'user', now()),
			($4::uuid, $5, $6, 'integration-hash', 'user', now())
	`, ownerID, ownerID+"@example.com", "share-owner-"+ownerID, otherUserID, otherUserID+"@example.com", "share-other-"+otherUserID); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM conversations WHERE id = $1::uuid`, conversationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1::uuid, $2::uuid)`, ownerID, otherUserID)
	}()
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO conversations (id, owner_user_id, title)
		VALUES ($1::uuid, $2::uuid, 'Snapshot title')
	`, conversationID, ownerID); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO messages (conversation_id, seq, role, content_text)
		VALUES ($1::uuid, 1, 'user', 'first'), ($1::uuid, 2, 'assistant', 'second')
	`, conversationID); err != nil {
		t.Fatalf("insert messages: %v", err)
	}

	repository := NewConversationShareRepository(pool)
	first, replayed, err := repository.CreateConversationShare(t.Context(), CreateConversationShareParams{
		ConversationID: conversationID, CreatedByUserID: ownerID, IdempotencyKey: "share-operation-1",
	})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	if replayed || first.Title != "Snapshot title" || first.LastMessageSeq != 2 {
		t.Fatalf("unexpected first share: share=%#v replayed=%t", first, replayed)
	}
	parsedID, err := uuid.Parse(first.ID)
	if err != nil || parsedID.Version() != 4 {
		t.Fatalf("share id = %q, want UUIDv4", first.ID)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE conversations SET title = 'Private renamed title' WHERE id = $1::uuid`, conversationID); err != nil {
		t.Fatalf("rename conversation: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO messages (conversation_id, seq, role, content_text) VALUES ($1::uuid, 3, 'user', 'third')
	`, conversationID); err != nil {
		t.Fatalf("extend conversation: %v", err)
	}

	second, replayed, err := repository.CreateConversationShare(t.Context(), CreateConversationShareParams{
		ConversationID: conversationID, CreatedByUserID: ownerID, IdempotencyKey: "share-operation-1",
	})
	if err != nil {
		t.Fatalf("replay share: %v", err)
	}
	if !replayed || second.ID != first.ID || second.Title != first.Title || second.LastMessageSeq != first.LastMessageSeq {
		t.Fatalf("replayed share changed: first=%#v second=%#v replayed=%t", first, second, replayed)
	}

	latest, replayed, err := repository.CreateConversationShare(t.Context(), CreateConversationShareParams{
		ConversationID: conversationID, CreatedByUserID: ownerID, IdempotencyKey: "share-operation-2",
	})
	if err != nil {
		t.Fatalf("create latest share: %v", err)
	}
	if replayed || latest.ID == first.ID || latest.Title != "Private renamed title" || latest.LastMessageSeq != 3 {
		t.Fatalf("unexpected latest share: share=%#v replayed=%t", latest, replayed)
	}

	type createResult struct {
		share    *domain.ConversationShare
		replayed bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan createResult, 2)
	for range 2 {
		go func() {
			<-start
			share, replayed, err := repository.CreateConversationShare(t.Context(), CreateConversationShareParams{
				ConversationID: conversationID, CreatedByUserID: ownerID, IdempotencyKey: "concurrent-share",
			})
			results <- createResult{share: share, replayed: replayed, err: err}
		}()
	}
	close(start)
	concurrent := []createResult{<-results, <-results}
	if concurrent[0].err != nil || concurrent[1].err != nil {
		t.Fatalf("concurrent create errors = (%v, %v)", concurrent[0].err, concurrent[1].err)
	}
	if concurrent[0].share.ID != concurrent[1].share.ID || concurrent[0].replayed == concurrent[1].replayed {
		t.Fatalf("unexpected concurrent results: %#v", concurrent)
	}

	_, _, err = repository.CreateConversationShare(t.Context(), CreateConversationShareParams{
		ConversationID: conversationID, CreatedByUserID: otherUserID, IdempotencyKey: "unowned-share",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unowned create error = %v, want not found", err)
	}
	if _, _, err := repository.CreateConversationShare(t.Context(), CreateConversationShareParams{
		ConversationID: "not-a-uuid", CreatedByUserID: ownerID, IdempotencyKey: "malformed-share",
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("malformed conversation error = %v, want not found", err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE conversations SET owner_user_id = $2::uuid WHERE id = $1::uuid`, conversationID, otherUserID); err != nil {
		t.Fatalf("transfer conversation ownership: %v", err)
	}
	if _, _, err := repository.CreateConversationShare(t.Context(), CreateConversationShareParams{
		ConversationID: conversationID, CreatedByUserID: ownerID, IdempotencyKey: "share-operation-1",
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("former owner replay error = %v, want not found", err)
	}
}
