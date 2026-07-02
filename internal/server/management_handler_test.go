package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	assistantmail "github.com/EurekaMXZ/assistant/internal/mail"
)

func authenticatedUser(role string) func(context.Context, string) (*domain.User, error) {
	return func(context.Context, string) (*domain.User, error) {
		return &domain.User{ID: "user-1", Role: role, Status: domain.UserStatusActive}, nil
	}
}

func TestAdminManagementRoutesRejectRegularUser(t *testing.T) {
	srv := newTestServer(UseCases{Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)}})
	for _, route := range []string{
		"/api/v1/admin/provider-credentials",
		"/api/v1/admin/models",
		"/api/v1/admin/billing/accounts",
		"/api/v1/admin/audit-events",
	} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s status = %d, want %d", route, rec.Code, http.StatusForbidden)
		}
	}
}

func TestCreateMessagePassesSelectedModelAndReasoningEffort(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Conversations: ConversationUseCases{SendMessage: func(_ context.Context, ownerUserID string, conversationID string, input SendMessageInput) (*domain.EnqueuedTurn, error) {
			if ownerUserID != "user-1" || conversationID != "conversation-1" || input.ModelID != "model-1" || input.ReasoningEffort != "high" {
				t.Fatalf("unexpected message input: owner=%q conversation=%q model=%q reasoning=%q", ownerUserID, conversationID, input.ModelID, input.ReasoningEffort)
			}
			return &domain.EnqueuedTurn{ConversationID: conversationID}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/conversation-1/messages", strings.NewReader(`{"content":"hello","model_id":"model-1","reasoning_effort":"high"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateModelPassesSupportedReasoningEfforts(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleSystem)},
		Models: ModelUseCases{CreateModel: func(_ context.Context, actor *domain.User, input CreateModelInput) (*domain.Model, error) {
			if actor.ID != "user-1" || len(input.SupportedReasoningEfforts) != 2 || input.SupportedReasoningEfforts[0] != "low" || input.SupportedReasoningEfforts[1] != "xhigh" {
				t.Fatalf("unexpected create model input: actor=%q input=%#v", actor.ID, input)
			}
			return &domain.Model{ID: "model-1", SupportedReasoningEfforts: input.SupportedReasoningEfforts}, nil
		}},
	})
	body := `{"provider":"openai","credential_id":"credential-1","slug":"gpt-test","upstream_model":"gpt-test","display_name":"GPT Test","supported_reasoning_efforts":["low","xhigh"],"context_window_tokens":128000,"max_output_tokens":4096}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"supported_reasoning_efforts":["low","xhigh"]`) {
		t.Fatalf("unexpected model response: %s", body)
	}
}

func TestCreateModelPricePassesCacheRates(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleSystem)},
		Models: ModelUseCases{CreateModelPrice: func(_ context.Context, _ *domain.User, input CreateModelPriceInput) (*domain.ModelPriceVersion, error) {
			if input.ModelID != "model-1" || input.InputPerMillionNanos != 1 || input.OutputPerMillionNanos != 2 || input.CacheReadInputPerMillionNanos != 3 || input.CacheCreationInputPerMillionNanos != 4 {
				t.Fatalf("unexpected model price input: %#v", input)
			}
			return &domain.ModelPriceVersion{ID: "price-1", ModelID: input.ModelID}, nil
		}},
	})
	body := `{"currency":"USD","input_per_million_nanos":1,"output_per_million_nanos":2,"cache_read_input_per_million_nanos":3,"cache_creation_input_per_million_nanos":4}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models/model-1/prices", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminCannotAccessSystemConfigurationRoutes(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleAdmin)},
		Mail: MailUseCases{GetMailSettings: func(context.Context, *domain.User) (*assistantmail.Settings, error) {
			t.Fatal("system-only mail use case was called for admin")
			return nil, nil
		}},
	})
	for _, route := range []string{
		"/api/v1/admin/provider-credentials",
		"/api/v1/admin/models",
		"/api/v1/admin/model-settings",
		"/api/v1/admin/mail-settings",
	} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s status = %d, want %d", route, rec.Code, http.StatusForbidden)
		}
	}
}

func TestManualTopupPassesIdempotencyAndRequestIDs(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleAdmin)},
		Billing: BillingUseCases{ApplyManualTopup: func(_ context.Context, actor *domain.User, input ManualBillingInput) (*domain.BillingTransaction, error) {
			if actor.ID != "user-1" || input.UserID != "target-1" || input.IdempotencyKey != "topup-1" || input.RequestID != "request-1" {
				t.Fatalf("unexpected topup input: actor=%q input=%#v", actor.ID, input)
			}
			return &domain.BillingTransaction{ID: "transaction-1"}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/accounts/target-1/topups", strings.NewReader(`{"amount":"10.00","currency":"USD","reason":"support adjustment"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "topup-1")
	req.Header.Set("X-Request-ID", "request-1")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Request-ID") != "request-1" {
		t.Fatalf("status = %d, request id = %q, body=%s", rec.Code, rec.Header().Get("X-Request-ID"), rec.Body.String())
	}
}

func TestOwnBillingTransactionsAreScopedToAuthenticatedUser(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Billing: BillingUseCases{ListBillingTransactions: func(_ context.Context, input BillingListInput) (*PageResult[domain.BillingTransaction], error) {
			if input.UserID != "user-1" {
				t.Fatalf("billing query user id = %q", input.UserID)
			}
			return &PageResult[domain.BillingTransaction]{}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/transactions?user_id=other-user", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}
