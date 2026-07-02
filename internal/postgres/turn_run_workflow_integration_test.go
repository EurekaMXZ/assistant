package postgres

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTurnRunWorkflowLifecycleIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	conversationID := uuid.NewString()
	turnID := uuid.NewString()
	ownerUserID := insertIntegrationUser(t, pool, domain.UserRoleUser)
	actorUserID := insertIntegrationUser(t, pool, domain.UserRoleAdmin)
	credentialID := uuid.NewString()
	modelID := uuid.NewString()
	priceID := uuid.NewString()
	pricing := json.RawMessage(`{"currency":"USD","input_per_million_nanos":1000000000,"cache_read_input_per_million_nanos":100000000,"cache_creation_input_per_million_nanos":1200000000,"output_per_million_nanos":8000000000}`)
	modelSnapshot, err := json.Marshal(domain.ModelExecutionSnapshot{
		ModelID: modelID, ModelRevision: 1, Provider: domain.ProviderOpenAI, CredentialID: credentialID,
		BaseURL: "https://api.example.com/v1", UpstreamModel: "gpt-test", ContextWindowTokens: 128000,
		MaxOutputTokens: 4096, SupportsTools: true, SupportsParallelTools: true,
		SupportedReasoningEfforts: []string{}, DefaultParameters: json.RawMessage(`{}`),
		ModelPriceID: priceID, Currency: "USD", PricingSnapshot: pricing,
	})
	if err != nil {
		t.Fatalf("marshal model snapshot: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO provider_credentials (
			id, provider, name, base_url, encrypted_api_key, nonce, created_by_user_id, updated_by_user_id
		) VALUES ($1::uuid, 'openai', $2, 'https://api.example.com/v1', decode('01', 'hex'), decode('000000000000000000000000', 'hex'), $3::uuid, $3::uuid)
	`, credentialID, "integration-"+credentialID, actorUserID); err != nil {
		t.Fatalf("insert credential fixture: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO models (
			id, provider, credential_id, slug, upstream_model, display_name, context_window_tokens,
			max_output_tokens, created_by_user_id, updated_by_user_id
		) VALUES ($1::uuid, 'openai', $2::uuid, $3, 'gpt-test', 'GPT Test', 128000, 4096, $4::uuid, $4::uuid)
	`, modelID, credentialID, "integration-"+modelID, actorUserID); err != nil {
		t.Fatalf("insert model fixture: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO model_price_versions (
			id, model_id, version, currency, input_per_million_nanos, cache_read_input_per_million_nanos,
			cache_creation_input_per_million_nanos, output_per_million_nanos, status, effective_from,
			pricing_snapshot, created_by_user_id, published_by_user_id, published_at
		) VALUES ($1::uuid, $2::uuid, 1, 'USD', 1000000000, 100000000, 1200000000, 8000000000,
			'published', now(), $3::jsonb, $4::uuid, $4::uuid, now())
	`, priceID, modelID, pricing, actorUserID); err != nil {
		t.Fatalf("insert price fixture: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO billing_accounts (user_id, currency, balance_nanos)
		VALUES ($1::uuid, 'USD', 1000000000000)
	`, ownerUserID); err != nil {
		t.Fatalf("insert billing fixture: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO conversations (id, owner_user_id) VALUES ($1::uuid, $2::uuid)`, conversationID, ownerUserID); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO turns (id, conversation_id, seq, status, model_id, model_revision, model_price_id, model_snapshot)
		VALUES ($1::uuid, $2::uuid, 1, $3, $4::uuid, 1, $5::uuid, $6::jsonb)
	`, turnID, conversationID, domain.TurnStatusContextReady, modelID, priceID, modelSnapshot); err != nil {
		t.Fatalf("insert turn: %v", err)
	}

	runs := NewTurnRunRepository(pool)
	runID, err := runs.StartTurnRun(t.Context(), turnID, "openai.responses", "gpt-test", "request-1", "state-1")
	if err != nil {
		t.Fatalf("start turn run: %v", err)
	}
	run, err := runs.GetTurnRun(t.Context(), runID)
	if err != nil || run.Status != domain.TurnRunStatusQueued {
		t.Fatalf("queued run = %#v, err=%v", run, err)
	}

	run, firstLease, err := runs.ClaimTurnRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("claim first run: %v", err)
	}
	if run.Attempt != 1 || run.Status != domain.TurnRunStatusRunning || firstLease.Token == "" {
		t.Fatalf("claimed run = %#v, lease=%#v", run, firstLease)
	}
	if err := runs.RenewTurnRunLease(t.Context(), firstLease); err != nil {
		t.Fatalf("renew first run: %v", err)
	}
	calls := NewToolCallRepository(pool)
	call := tool.ToolCall{CallID: "call-1", Type: llm.ModelItemFunctionCall, Name: "lookup"}
	firstCall, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt, call, "arguments-1")
	if err != nil || !acquired {
		t.Fatalf("create tool call: %v", err)
	}
	duplicateCall, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt, call, "arguments-1")
	if err != nil || acquired || duplicateCall.ID != firstCall.ID {
		t.Fatalf("idempotent tool call = %#v, err=%v", duplicateCall, err)
	}
	if _, err := calls.CompleteToolCall(t.Context(), firstCall.ID, "output-1"); err != nil {
		t.Fatalf("complete tool call: %v", err)
	}
	replayedCall, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt+1, call, "arguments-1")
	if err != nil || acquired || replayedCall.Status != domain.ToolCallStatusCompleted {
		t.Fatalf("replayed tool call = %#v, err=%v", replayedCall, err)
	}
	ambiguousCall := tool.ToolCall{CallID: "call-ambiguous", Type: llm.ModelItemFunctionCall, Name: "side-effect"}
	if _, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt, ambiguousCall, "arguments-ambiguous"); err != nil || !acquired {
		t.Fatalf("acquire ambiguous fixture: acquired=%t err=%v", acquired, err)
	}
	ambiguous, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt+1, ambiguousCall, "arguments-ambiguous")
	if err != nil || acquired || ambiguous.Status != domain.ToolCallStatusAmbiguous {
		t.Fatalf("recover ambiguous call = %#v acquired=%t err=%v", ambiguous, acquired, err)
	}
	completed, err := runs.CompleteScheduledTurnRun(t.Context(), firstLease, "resp-1", "response-1", "result-1", llm.ModelUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5})
	if err != nil {
		t.Fatalf("complete first run: %v", err)
	}
	if completed.Status != domain.TurnRunStatusCompleted || completed.ResultBlobKey != "result-1" {
		t.Fatalf("completed run = %#v", completed)
	}

	nextRunID, err := runs.ScheduleNextTurnRun(t.Context(), turnID, runID, 2, "openai.responses", "gpt-test", "request-2", "state-2")
	if err != nil {
		t.Fatalf("schedule next run: %v", err)
	}
	_, staleLease, err := runs.ClaimTurnRun(t.Context(), nextRunID)
	if err != nil {
		t.Fatalf("claim next run: %v", err)
	}
	if err := runs.CheckpointScheduledTurnRun(t.Context(), staleLease, "resp-2", "response-2", "result-2"); err != nil {
		t.Fatalf("checkpoint next run: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE turn_runs SET heartbeat_at = now() - interval '10 minutes' WHERE id = $1::uuid`, nextRunID); err != nil {
		t.Fatalf("age heartbeat: %v", err)
	}
	requeued, err := NewStaleTurnRepository(pool).RequeueStaleTurnRuns(t.Context(), time.Minute)
	if err != nil || requeued != 1 {
		t.Fatalf("requeue stale run count=%d err=%v", requeued, err)
	}
	replacement, replacementLease, err := runs.ClaimTurnRun(t.Context(), nextRunID)
	if err != nil {
		t.Fatalf("claim replacement attempt: %v", err)
	}
	if replacementLease.Token == staleLease.Token {
		t.Fatal("replacement attempt reused stale lease token")
	}
	if replacement.ResultBlobKey != "result-2" || replacement.ResponseBlobKey != "response-2" {
		t.Fatalf("replacement lost checkpoint: %#v", replacement)
	}
	if _, err := runs.CompleteScheduledTurnRun(t.Context(), staleLease, "stale", "", "", llm.ModelUsage{}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale completion error = %v, want conflict", err)
	}
	failed, err := runs.FailScheduledTurnRun(t.Context(), replacementLease, "", "", "", "upstream failed",
		"request-2", "stream-2", domain.TurnErrorUpstreamRequestFailed, domain.TurnPublicErrorUpstreamRequestFailed)
	if err != nil || failed.Status != domain.TurnRunStatusFailed {
		t.Fatalf("fail replacement run = %#v, err=%v", failed, err)
	}
	var parentStatus, parentCode string
	if err := pool.QueryRow(t.Context(), `SELECT status, error_code FROM turns WHERE id = $1::uuid`, turnID).Scan(&parentStatus, &parentCode); err != nil {
		t.Fatalf("load failed parent turn: %v", err)
	}
	if parentStatus != domain.TurnStatusFailed || parentCode != domain.TurnErrorUpstreamRequestFailed {
		t.Fatalf("failed run diverged from parent turn: status=%q code=%q", parentStatus, parentCode)
	}

	insufficientConversationID := uuid.NewString()
	insufficientTurnID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `INSERT INTO conversations (id, owner_user_id) VALUES ($1::uuid, $2::uuid)`, insufficientConversationID, ownerUserID); err != nil {
		t.Fatalf("insert insufficient conversation: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO turns (id, conversation_id, seq, status, model_id, model_revision, model_price_id, model_snapshot)
		VALUES ($1::uuid, $2::uuid, 1, $3, $4::uuid, 1, $5::uuid, $6::jsonb)
	`, insufficientTurnID, insufficientConversationID, domain.TurnStatusContextReady, modelID, priceID, modelSnapshot); err != nil {
		t.Fatalf("insert insufficient turn: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE billing_accounts SET balance_nanos = 0, status = 'active' WHERE user_id = $1::uuid AND currency = 'USD'`, ownerUserID); err != nil {
		t.Fatalf("empty billing account: %v", err)
	}
	insufficientRunID, err := runs.StartTurnRun(t.Context(), insufficientTurnID, "openai.responses", "gpt-test", "request-insufficient", "state-insufficient")
	if err != nil {
		t.Fatalf("start insufficient run: %v", err)
	}
	_, insufficientLease, err := runs.ClaimTurnRun(t.Context(), insufficientRunID)
	if err != nil {
		t.Fatalf("claim insufficient run: %v", err)
	}
	settled, err := runs.CompleteScheduledTurnRun(t.Context(), insufficientLease, "resp-insufficient", "response-insufficient", "result-insufficient", llm.ModelUsage{InputTokens: 1000, TotalTokens: 1000})
	if err != nil || settled.Status != domain.TurnRunStatusFailed {
		t.Fatalf("insufficient settlement = %#v, err=%v", settled, err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT status, error_code FROM turns WHERE id = $1::uuid`, insufficientTurnID).Scan(&parentStatus, &parentCode); err != nil {
		t.Fatalf("load insufficient parent turn: %v", err)
	}
	if parentStatus != domain.TurnStatusFailed || parentCode != domain.TurnErrorBillingSettlementFailed {
		t.Fatalf("insufficient settlement left stale turn: status=%q code=%q", parentStatus, parentCode)
	}
	var usageStatus string
	var transactionID *string
	if err := pool.QueryRow(t.Context(), `SELECT status, billing_transaction_id::text FROM billing_usage_events WHERE turn_run_id = $1::uuid`, insufficientRunID).Scan(&usageStatus, &transactionID); err != nil {
		t.Fatalf("load insufficient usage event: %v", err)
	}
	if usageStatus != "failed" || transactionID != nil {
		t.Fatalf("insufficient usage status=%q transaction=%v", usageStatus, transactionID)
	}
}
