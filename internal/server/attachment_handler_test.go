package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

const testAttachmentSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func attachmentTestAuth() AuthUseCases {
	return AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
		return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
	}}
}

func TestHandleCreateConversationAttachmentUploadReturnsIntent(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: attachmentTestAuth(),
		Attachments: AttachmentUseCases{CreateConversationAttachmentUpload: func(_ context.Context, ownerUserID string, conversationID string, input CreateConversationAttachmentUploadInput) (*ConversationAttachmentUpload, error) {
			if ownerUserID != "user-1" || conversationID != "conv-1" || input.Filename != "notes.txt" || input.SizeBytes != 5 || input.SHA256 != testAttachmentSHA256 || input.ContentMD5 != "XUFAKrxLKna5cZ2REBfFkg==" {
				t.Fatalf("unexpected upload input: owner=%q conversation=%q input=%#v", ownerUserID, conversationID, input)
			}
			if input.IdempotencyKey != "attachment-1" {
				t.Fatalf("idempotency key = %q", input.IdempotencyKey)
			}
			return &ConversationAttachmentUpload{
				Attachment: domain.Attachment{ID: "att-1", Status: domain.AttachmentStatusPending},
				Upload:     &PresignedObjectURL{URL: "https://objects.example/upload", Method: http.MethodPut, ExpiresAt: time.Now().Add(time.Minute)},
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conv-1/attachments", bytes.NewBufferString(`{"filename":"notes.txt","content_type":"text/plain","size_bytes":5,"sha256":"`+testAttachmentSHA256+`","content_md5":"XUFAKrxLKna5cZ2REBfFkg=="}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "attachment-1")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || !bytes.Contains(rec.Body.Bytes(), []byte(`"method":"PUT"`)) {
		t.Fatalf("unexpected response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCompleteConversationAttachmentUpload(t *testing.T) {
	checksum := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	srv := newTestServer(UseCases{
		Auth: attachmentTestAuth(),
		Attachments: AttachmentUseCases{CompleteConversationAttachmentUpload: func(_ context.Context, ownerUserID, conversationID, attachmentID string, _ CompleteConversationAttachmentUploadInput) (*domain.Attachment, error) {
			if ownerUserID != "user-1" || conversationID != "conv-1" || attachmentID != "att-1" {
				t.Fatalf("unexpected complete input")
			}
			return &domain.Attachment{ID: attachmentID, Status: domain.AttachmentStatusReady, SHA256: checksum}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conv-1/attachments/att-1/complete", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"ready"`)) {
		t.Fatalf("unexpected response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetConversationAttachmentReturnsPresignedURL(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: attachmentTestAuth(),
		Attachments: AttachmentUseCases{GetConversationAttachmentDownload: func(context.Context, string, string, string, bool) (*ConversationAttachmentDownload, error) {
			return &ConversationAttachmentDownload{
				Attachment: domain.Attachment{ID: "att-1", Status: domain.AttachmentStatusReady},
				Download:   PresignedObjectURL{URL: "https://objects.example/download", Method: http.MethodGet, ExpiresAt: time.Now().Add(time.Minute)},
			}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/conv-1/attachments/att-1", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`https://objects.example/download`)) {
		t.Fatalf("unexpected response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
