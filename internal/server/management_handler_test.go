package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		"/api/v1/admin/billing/redemption-codes",
		"/api/v1/admin/billing/tool-prices",
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
	if body := rec.Body.String(); !strings.Contains(body, `"data":[]`) {
		t.Fatalf("empty page encoded as null: %s", body)
	}
}

func TestIssueRedemptionCodesPassesAmountQuantityExpiryAndRequestID(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleAdmin)},
		Billing: BillingUseCases{IssueRedemptionCodes: func(_ context.Context, actor *domain.User, input IssueRedemptionCodeInput) ([]domain.BillingRedemptionCodeIssue, error) {
			if actor.ID != "user-1" || input.Amount != "10.00" || input.Quantity != 3 || input.ExpiresAt == nil || !input.ExpiresAt.Equal(expiresAt) || input.RequestID != "request-issue" {
				t.Fatalf("unexpected redemption issue input: actor=%q input=%#v", actor.ID, input)
			}
			return []domain.BillingRedemptionCodeIssue{
				{RedemptionCode: domain.BillingRedemptionCode{ID: "code-1", Amount: "10.00"}, Code: "0123456789abcdef0123456789abcdef0123456789abcdef"},
				{RedemptionCode: domain.BillingRedemptionCode{ID: "code-2", Amount: "10.00"}, Code: "1123456789abcdef0123456789abcdef0123456789abcdef"},
				{RedemptionCode: domain.BillingRedemptionCode{ID: "code-3", Amount: "10.00"}, Code: "2123456789abcdef0123456789abcdef0123456789abcdef"},
			}, nil
		}},
	})
	body := `{"amount":"10.00","quantity":3,"expires_at":"` + expiresAt.Format(time.RFC3339) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/redemption-codes", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "request-issue")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Header().Get("Cache-Control") != "no-store" || strings.Count(rec.Body.String(), `"redemption_code"`) != 3 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRedeemBillingCodeUsesAuthenticatedUser(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleUser)},
		Billing: BillingUseCases{RedeemBillingCode: func(_ context.Context, actor *domain.User, input RedeemBillingCodeInput) (*domain.BillingRedemptionResult, error) {
			if actor.ID != "user-1" || actor.Role != domain.UserRoleUser || input.Code != "0123456789abcdef0123456789abcdef0123456789abcdef" || input.RequestID != "request-redeem" {
				t.Fatalf("unexpected redemption input: actor=%#v input=%#v", actor, input)
			}
			return &domain.BillingRedemptionResult{
				Account:     domain.BillingAccount{ID: "account-1", Balance: "10.00"},
				Transaction: domain.BillingTransaction{ID: "transaction-1", Kind: domain.BillingTransactionRedemptionCredit},
			}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/redemptions", strings.NewReader(`{"code":"0123456789abcdef0123456789abcdef0123456789abcdef","user_id":"other-user"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "request-redeem")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"kind":"redemption_credit"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDisableRedemptionCodeUsesAdminAndRequestID(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleAdmin)},
		Billing: BillingUseCases{DisableRedemptionCode: func(_ context.Context, actor *domain.User, codeID string, requestID string) (*domain.BillingRedemptionCode, error) {
			if actor.ID != "user-1" || codeID != "code-1" || requestID != "request-disable" {
				t.Fatalf("unexpected disable input: actor=%#v code=%q request=%q", actor, codeID, requestID)
			}
			return &domain.BillingRedemptionCode{ID: codeID, Status: domain.BillingRedemptionCodeDisabled}, nil
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/redemption-codes/code-1/disable", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Request-ID", "request-disable")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"disabled"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateBillingToolPricesPassesCompletePlanAndRequestID(t *testing.T) {
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: authenticatedUser(domain.UserRoleAdmin)},
		Billing: BillingUseCases{UpdateBillingToolPrices: func(_ context.Context, actor *domain.User, input UpdateBillingToolPricesInput) ([]domain.BillingToolPrice, error) {
			if actor.ID != "user-1" || input.RequestID != "request-tool-prices" || len(input.Prices) != 4 {
				t.Fatalf("unexpected tool price input: actor=%#v input=%#v", actor, input)
			}
			if input.Prices[0].ToolKey != domain.BillingToolSandboxCreate || !input.Prices[0].Enabled ||
				input.Prices[0].PricePerCallNanos != 250_000_000 || input.Prices[0].Version != 1 {
				t.Fatalf("unexpected sandbox price: %#v", input.Prices[0])
			}
			return []domain.BillingToolPrice{{
				ToolKey: domain.BillingToolSandboxCreate, Currency: "USD", PricePerCallNanos: 250_000_000,
				PricePerCall: "0.25", Enabled: true, Version: 2,
			}}, nil
		}},
	})
	body := `{"tool_prices":[{"tool_key":"sandbox.create","price_per_call_nanos":250000000,"enabled":true,"version":1},{"tool_key":"image_generation","price_per_call_nanos":100000000,"enabled":true,"version":1},{"tool_key":"tavily.search","price_per_call_nanos":10000000,"enabled":true,"version":1},{"tool_key":"tavily.extract","price_per_call_nanos":20000000,"enabled":false,"version":1}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/billing/tool-prices", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "request-tool-prices")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tool_key":"sandbox.create"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
