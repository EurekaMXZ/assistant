package server

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestHandleUploadConversationAttachmentReturnsCreatedAttachment(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Attachments: AttachmentUseCases{UploadConversationAttachment: func(_ context.Context, ownerUserID string, conversationID string, input UploadConversationAttachmentInput) (*domain.Attachment, error) {
			if ownerUserID != "user-1" || conversationID != "conv-1" {
				t.Fatalf("unexpected owner/conversation: %q %q", ownerUserID, conversationID)
			}
			if input.Filename != "notes.txt" {
				t.Fatalf("filename = %q, want %q", input.Filename, "notes.txt")
			}
			if input.IdempotencyKey != "attachment-1" {
				t.Fatalf("idempotency key = %q, want %q", input.IdempotencyKey, "attachment-1")
			}
			data, err := io.ReadAll(input.File)
			if err != nil {
				t.Fatalf("read upload: %v", err)
			}
			if string(data) != "hello" {
				t.Fatalf("data = %q, want %q", data, "hello")
			}
			return &domain.Attachment{
				ID:               "att-1",
				ConversationID:   conversationID,
				UploadedByUserID: ownerUserID,
				Filename:         input.Filename,
				ContentType:      input.ContentType,
				Category:         domain.AttachmentCategoryText,
				SizeBytes:        input.SizeBytes,
			}, nil
		}},
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conv-1/attachments", body)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Idempotency-Key", "attachment-1")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"id":"att-1"`)) {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestHandleUploadConversationAttachmentRejectsMissingFile(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Attachments: AttachmentUseCases{UploadConversationAttachment: func(context.Context, string, string, UploadConversationAttachmentInput) (*domain.Attachment, error) {
			t.Fatal("unexpected UploadConversationAttachment call")
			return nil, nil
		}},
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conv-1/attachments", body)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
