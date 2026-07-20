package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestHandleRetryTurnReturnsVariantStream(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{RetryTurn: func(_ context.Context, ownerUserID string, sourceTurnID string) (*domain.EnqueuedRetryTurn, error) {
			if ownerUserID != "user-1" || sourceTurnID != "turn-1" {
				t.Fatalf("unexpected retry input: owner=%q source=%q", ownerUserID, sourceTurnID)
			}
			return &domain.EnqueuedRetryTurn{
				ConversationID: "conversation-1",
				Message:        domain.Message{ID: "message-2", ConversationID: "conversation-1", TurnID: "turn-2", Role: domain.RoleUser},
				Turn: domain.Turn{
					ID: "turn-2", ConversationID: "conversation-1", RetryOfTurnID: "turn-1", VariantIndex: 2,
				},
			}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/turns/turn-1/retries", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted ||
		!strings.Contains(rec.Body.String(), `"retry_of_turn_id":"turn-1"`) ||
		!strings.Contains(rec.Body.String(), `"stream_path":"/api/v1/turns/turn-2/stream"`) ||
		!strings.Contains(rec.Body.String(), `"id":"message-2"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleEditTurnReturnsEditedPromptVariant(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{EditTurn: func(_ context.Context, ownerUserID string, sourceTurnID string, content string) (*domain.EnqueuedRetryTurn, error) {
			if ownerUserID != "user-1" || sourceTurnID != "turn-1" || content != "edited prompt" {
				t.Fatalf("unexpected edit input: owner=%q source=%q content=%q", ownerUserID, sourceTurnID, content)
			}
			return &domain.EnqueuedRetryTurn{
				ConversationID: "conversation-1",
				Message: domain.Message{
					ID: "message-2", ConversationID: "conversation-1", TurnID: "turn-2", Role: domain.RoleUser, ContentText: content,
				},
				Turn: domain.Turn{ID: "turn-2", ConversationID: "conversation-1", RetryOfTurnID: "turn-1", VariantIndex: 2},
			}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/turns/turn-1/edits", strings.NewReader(`{"content":"edited prompt"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted ||
		!strings.Contains(rec.Body.String(), `"content_text":"edited prompt"`) ||
		!strings.Contains(rec.Body.String(), `"stream_path":"/api/v1/turns/turn-2/stream"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListConversationResourcesEncodeEmptyArrays(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{
			ListConversations: func(context.Context, string, int) ([]domain.Conversation, error) {
				return nil, nil
			},
			ListMessages: func(context.Context, string, string, int) ([]domain.Message, error) {
				return nil, nil
			},
			ListConversationEvents: func(context.Context, string, string, int, int64, int64) (*ConversationEventPage, error) {
				return &ConversationEventPage{}, nil
			},
		},
	})
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/api/v1/conversations?limit=200", want: `"conversations":[]`},
		{path: "/api/v1/conversations/conversation-1/messages", want: `"messages":[]`},
		{path: "/api/v1/conversations/conversation-1/events", want: `"events":[]`},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), test.want) {
			t.Fatalf("GET %s status=%d body=%s, want %s", test.path, rec.Code, rec.Body.String(), test.want)
		}
	}
}

func TestListConversationEventsUsesDecimalCursors(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{ListConversationEvents: func(_ context.Context, ownerID string, conversationID string, limit int, before int64, after int64) (*ConversationEventPage, error) {
			if ownerID != "user-1" || conversationID != "conversation-1" || limit != 25 || before != 101 || after != 0 {
				t.Fatalf("unexpected event query: owner=%s conversation=%s limit=%d before=%d after=%d", ownerID, conversationID, limit, before, after)
			}
			return &ConversationEventPage{Items: []domain.ConversationEvent{{ID: "event-1", ConversationID: conversationID, EventSeq: 100, EventKey: "message:1", SchemaVersion: 1, EventType: "message.completed", Payload: []byte(`{}`)}}, NextBefore: "100", HasMoreBefore: true}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/conversation-1/events?limit=25&before=101", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"event_seq":"100"`) || !strings.Contains(rec.Body.String(), `"next_before":"100"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCancelTurnRequestsCancellation(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Turns: TurnUseCases{RequestTurnCancellation: func(_ context.Context, ownerID string, turnID string) (*domain.Turn, error) {
			if ownerID != "user-1" || turnID != "turn-1" {
				t.Fatalf("unexpected cancellation request: owner=%s turn=%s", ownerID, turnID)
			}
			return &domain.Turn{ID: turnID, Status: domain.TurnStatusCancelRequested}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/turns/turn-1/cancel", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"status":"cancel_requested"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateConversationShare(t *testing.T) {
	createdAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{CreateConversationShare: func(_ context.Context, ownerUserID string, conversationID string, idempotencyKey string) (*CreateConversationShareResult, error) {
			if ownerUserID != "user-1" || conversationID != "conversation-1" || idempotencyKey != "share-1" {
				t.Fatalf("unexpected create share input: owner=%q conversation=%q key=%q", ownerUserID, conversationID, idempotencyKey)
			}
			return &CreateConversationShareResult{Share: domain.ConversationShare{
				ID: "share-id", ConversationID: conversationID, CreatedByUserID: ownerUserID,
				Title: "Shared conversation", LastMessageSeq: 4, CreatedAt: createdAt,
			}}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conversation-1/shares", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Idempotency-Key", "share-1")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"id":"share-id"`) || !strings.Contains(rec.Body.String(), `"last_message_seq":4`) || !strings.Contains(rec.Body.String(), `"replayed":false`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetConversationShareIsPublic(t *testing.T) {
	createdAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(UseCases{
		Conversations: ConversationUseCases{GetConversationShare: func(_ context.Context, shareID string) (*domain.ConversationShareSnapshot, error) {
			if shareID != "share-1" {
				t.Fatalf("share ID = %q, want share-1", shareID)
			}
			return &domain.ConversationShareSnapshot{
				ID: "share-1", Title: "Shared conversation", LastMessageSeq: 2, CreatedAt: createdAt,
				Messages: []domain.Message{{ID: "message-1", Seq: 1, Role: domain.RoleUser, ContentText: "hello", Metadata: json.RawMessage(`{}`), CreatedAt: createdAt}},
			}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversation-shares/share-1", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"title":"Shared conversation"`) || !strings.Contains(rec.Body.String(), `"content_text":"hello"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetConversationShareReturnsNotFound(t *testing.T) {
	srv := newTestServer(UseCases{
		Conversations: ConversationUseCases{GetConversationShare: func(context.Context, string) (*domain.ConversationShareSnapshot, error) {
			return nil, domain.ErrNotFound
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversation-shares/missing", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateConversationShareReplaysIdempotently(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{CreateConversationShare: func(context.Context, string, string, string) (*CreateConversationShareResult, error) {
			return &CreateConversationShareResult{Share: domain.ConversationShare{ID: "existing-share"}, Replayed: true}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conversation-1/shares", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Idempotency-Key", "share-1")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"existing-share"`) || !strings.Contains(rec.Body.String(), `"replayed":true`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateConversationShareRequiresIdempotencyKey(t *testing.T) {
	srv := newTestServer(UseCases{Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conversation-1/shares", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Idempotency-Key") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateConversationShareHidesUnownedConversation(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{CreateConversationShare: func(context.Context, string, string, string) (*CreateConversationShareResult, error) {
			return nil, domain.ErrNotFound
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conversation-1/shares", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Idempotency-Key", "share-1")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "resource not found") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
