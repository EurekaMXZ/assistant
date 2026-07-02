package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestHandleInitialTurnCommitPassesIdempotencyAndReturnsStream(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{InitialTurn: func(_ context.Context, ownerUserID string, idempotencyKey string, input InitialTurnInput) (*InitialTurnResult, error) {
			if ownerUserID != "user-1" || idempotencyKey != "initial-1" || input.Action != InitialTurnActionCommit || input.ConversationID != "conversation-1" {
				t.Fatalf("unexpected initial turn input: owner=%q key=%q input=%#v", ownerUserID, idempotencyKey, input)
			}
			return &InitialTurnResult{
				State: "committed", Conversation: domain.Conversation{ID: input.ConversationID},
				Message: &domain.Message{ID: "message-1"}, Turn: &domain.Turn{ID: "turn-1"},
			}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/initial-turns", strings.NewReader(`{"action":"commit","conversation_id":"conversation-1","content":"hello","model_id":"model-1"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "initial-1")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"stream_path":"/api/v1/turns/turn-1/stream"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleInitialTurnRequiresIdempotencyKey(t *testing.T) {
	srv := newTestServer(UseCases{Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/initial-turns", strings.NewReader(`{"action":"prepare"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Idempotency-Key") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
