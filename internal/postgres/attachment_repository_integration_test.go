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

func TestDeleteGeneratedImageAttachmentsIntegration(t *testing.T) {
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
	`, userID, userID+"@example.com", "generated-image-"+userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, userID)
	})
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO conversations (id, owner_user_id, title) VALUES ($1::uuid, $2::uuid, 'generated image cleanup')
	`, conversationID, userID); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}

	prefix := "conversations/" + conversationID + "/turns/" + uuid.NewString() + "/generated-images/"
	deletableKey := prefix + "run-1/image-1.png"
	protectedKey := prefix + "run-2/image-2.png"
	repository := NewAttachmentRepository(pool)
	deletable, err := repository.UpsertAttachment(t.Context(), assistantattachment.CreateAttachmentParams{
		ID: uuid.NewString(), ConversationID: conversationID, UploadedByUserID: userID,
		Filename: "generated-image-1.png", ContentType: "image/png", Category: "image", SizeBytes: 1,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObjectKey: deletableKey,
		Metadata: json.RawMessage(`{"source":"image_generation"}`),
	})
	if err != nil {
		t.Fatalf("insert deletable generated image attachment: %v", err)
	}
	protected, err := repository.UpsertAttachment(t.Context(), assistantattachment.CreateAttachmentParams{
		ID: uuid.NewString(), ConversationID: conversationID, UploadedByUserID: userID,
		Filename: "generated-image-2.png", ContentType: "image/png", Category: "image", SizeBytes: 1,
		SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ObjectKey: protectedKey,
		Metadata: json.RawMessage(`{"source":"image_generation"}`),
	})
	if err != nil {
		t.Fatalf("insert protected generated image attachment: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO messages (conversation_id, seq, role, metadata)
		VALUES ($1::uuid, 1, 'assistant', jsonb_build_object('attachment_ids', jsonb_build_array($2::text)))
	`, conversationID, protected.ID); err != nil {
		t.Fatalf("insert generated image message reference: %v", err)
	}

	deleted, err := repository.DeleteGeneratedImageAttachments(t.Context(), prefix)
	if err != nil {
		t.Fatalf("delete generated image attachments: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != deletableKey {
		t.Fatalf("deleted object keys = %#v", deleted)
	}
	for _, expected := range []struct {
		id    string
		count int
	}{
		{id: deletable.ID, count: 0},
		{id: protected.ID, count: 1},
	} {
		var count int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM attachments WHERE id = $1::uuid`, expected.id).Scan(&count); err != nil {
			t.Fatalf("count attachments: %v", err)
		}
		if count != expected.count {
			t.Fatalf("attachment %s count = %d, want %d", expected.id, count, expected.count)
		}
	}
}
