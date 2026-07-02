package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAttachmentIdempotencyIntegration(t *testing.T) {
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
	`, userID, userID+"@example.com", "attachment-"+userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO conversations (id, owner_user_id, title) VALUES ($1::uuid, $2::uuid, 'attachment integration')
	`, conversationID, userID); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}

	repository := NewAttachmentRepository(pool)
	first, err := repository.CreateAttachment(t.Context(), assistantattachment.CreateAttachmentParams{
		ID: uuid.NewString(), ConversationID: conversationID, UploadedByUserID: userID,
		IdempotencyKey: "attachment-operation-1", Filename: "first.txt", ContentType: "text/plain",
		Category: "text", SizeBytes: 5, SHA256: "first", ObjectKey: "integration/" + uuid.NewString(), Metadata: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create first attachment: %v", err)
	}
	secondObjectKey := "integration/" + uuid.NewString()
	second, err := repository.CreateAttachment(t.Context(), assistantattachment.CreateAttachmentParams{
		ID: uuid.NewString(), ConversationID: conversationID, UploadedByUserID: userID,
		IdempotencyKey: "attachment-operation-1", Filename: "second.txt", ContentType: "text/plain",
		Category: "text", SizeBytes: 6, SHA256: "second", ObjectKey: secondObjectKey, Metadata: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("replay attachment: %v", err)
	}
	if second.ID != first.ID || second.ObjectKey != first.ObjectKey {
		t.Fatalf("replay = (%q, %q), want (%q, %q)", second.ID, second.ObjectKey, first.ID, first.ObjectKey)
	}
	if second.ObjectKey == secondObjectKey {
		t.Fatal("idempotent replay stored the second object key")
	}

	replayed, err := repository.GetAttachmentByIdempotencyKey(t.Context(), conversationID, userID, "attachment-operation-1")
	if err != nil {
		t.Fatalf("get replayed attachment: %v", err)
	}
	if replayed.ID != first.ID {
		t.Fatalf("replayed id = %q, want %q", replayed.ID, first.ID)
	}
}
