package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/gin-gonic/gin"
)

func newTestServer(useCases UseCases) *http.Server {
	return newTestServerWithStream(useCases, nil)
}

func newTestServerWithStream(useCases UseCases, streamHub turnStreamSubscriber) *http.Server {
	gin.SetMode(gin.TestMode)
	useCases = completeTestUseCases(useCases)
	return New(Settings{
		Address:      ":0",
		WebOrigin:    "https://example.com",
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		IdleTimeout:  time.Second,
	}, useCases, streamHub, context.Background())
}

func completeTestUseCases(useCases UseCases) UseCases {
	fillTestUseCases(reflect.ValueOf(&useCases).Elem())
	return useCases
}

func fillTestUseCases(value reflect.Value) {
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		switch field.Kind() {
		case reflect.Struct:
			fillTestUseCases(field)
		case reflect.Func:
			if field.IsNil() {
				field.Set(reflect.MakeFunc(field.Type(), func(_ []reflect.Value) []reflect.Value {
					outputs := make([]reflect.Value, field.Type().NumOut())
					for output := range outputs {
						outputs[output] = reflect.Zero(field.Type().Out(output))
					}
					return outputs
				}))
			}
		}
	}
}

func TestProtectedRouteRequiresBearerToken(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleCreateMessageRejectsBlankContent(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Conversations: ConversationUseCases{SendMessage: func(context.Context, string, string, SendMessageInput) (*domain.EnqueuedTurn, error) {
			t.Fatal("unexpected SendMessage call")
			return nil, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conv-1/messages", strings.NewReader(`{"content":"   "}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), `"content is required"`) {
		t.Fatalf("expected validation error, got %q", rec.Body.String())
	}
}

func TestHandleCreateMessageAllowsAttachmentOnly(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Conversations: ConversationUseCases{SendMessage: func(_ context.Context, ownerUserID string, conversationID string, input SendMessageInput) (*domain.EnqueuedTurn, error) {
			if ownerUserID != "user-1" || conversationID != "conv-1" {
				t.Fatalf("unexpected owner/conversation: %q %q", ownerUserID, conversationID)
			}
			if len(input.AttachmentIDs) != 1 || input.AttachmentIDs[0] != "att-1" {
				t.Fatalf("unexpected attachment ids: %#v", input.AttachmentIDs)
			}
			return &domain.EnqueuedTurn{
				ConversationID: conversationID,
				Message:        domain.Message{ID: "msg-1", ConversationID: conversationID},
				Turn:           domain.Turn{ID: "turn-1", ConversationID: conversationID},
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conv-1/messages", strings.NewReader(`{"attachment_ids":["att-1"]}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestHandleLoginReturnsSession(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{Login: func(context.Context, assistantauth.LoginInput) (*assistantauth.Session, error) {
			return &assistantauth.Session{
				AccessToken: "token-1",
				TokenType:   "Bearer",
				ExpiresAt:   now,
				User: &domain.User{
					ID:       "user-1",
					Email:    "user@example.com",
					Username: "user",
					Role:     domain.UserRoleUser,
					Status:   domain.UserStatusActive,
				},
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"user@example.com","password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"access_token":"token-1"`) {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestHandleRegisterReturnsVerificationContractWithoutSession(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{Register: func(context.Context, assistantauth.RegisterInput) (*assistantauth.RegistrationResult, error) {
			return &assistantauth.RegistrationResult{VerificationRequired: true, EmailSent: false}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":"new@example.com","username":"new","password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"verification_required":true`) || strings.Contains(body, `"session"`) {
		t.Fatalf("unexpected registration body: %q", body)
	}
}

func TestHandleForgotPasswordReturnsGenericResponse(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{ForgotPassword: func(context.Context, assistantauth.ForgotPasswordInput) error { return nil }},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", strings.NewReader(`{"email":"unknown@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "if the account exists") {
		t.Fatalf("unexpected forgot-password response: %d %q", rec.Code, rec.Body.String())
	}
}
