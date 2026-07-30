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
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestToolExecutionRecoveryIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	conversationID, turnID := insertToolExecutionRecoveryFixture(t, pool)
	runs := NewTurnRunRepository(pool)
	runID, err := runs.StartTurnRun(t.Context(), turnID, "openai.responses", "gpt-test", "request", "state")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	run, _, err := runs.ClaimTurnRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("claim run: %v", err)
	}

	plan := &workflow.ToolExecutionPlan{Version: 1, Groups: []workflow.ToolExecutionGroup{{
		Index: 0,
		Calls: []workflow.ToolExecutionPlanCall{
			{Call: tool.ToolCall{Type: llm.ModelItemFunctionCall, CallID: "call-1", Name: "lookup"}, StableOperationID: runID + ":call-1", ArgumentsBlobKey: "arguments-1"},
			{Call: tool.ToolCall{Type: llm.ModelItemFunctionCall, CallID: "call-2", Name: "lookup"}, StableOperationID: runID + ":call-2", ArgumentsBlobKey: "arguments-2"},
		},
	}}}
	calls := NewToolCallRepository(pool)
	records, err := calls.ScheduleToolExecutionPlan(t.Context(), workflow.ToolExecutionScheduleInput{
		Run: run, Scope: tool.ToolScope{ConversationID: conversationID, TurnID: turnID}, Plan: plan,
	})
	if err != nil {
		t.Fatalf("schedule tool calls: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("scheduled records = %#v", records)
	}

	first, firstLease, err := calls.ClaimQueuedToolCall(t.Context(), records[0].ID, time.Minute)
	if err != nil {
		t.Fatalf("claim first tool call: %v", err)
	}
	_, secondLease, err := calls.ClaimQueuedToolCall(t.Context(), records[1].ID, time.Minute)
	if err != nil {
		t.Fatalf("claim second tool call: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		UPDATE tool_calls SET leased_at = now() - interval '2 minutes' WHERE id = $1::uuid
	`, first.ID); err != nil {
		t.Fatalf("expire first tool lease: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		UPDATE outbox_events SET published_at = now() WHERE event_type = $1 AND tool_call_id = $2::uuid
	`, workflow.EventToolCallRequested, first.ID); err != nil {
		t.Fatalf("mark first event published: %v", err)
	}

	requeued, err := NewStaleTurnRepository(pool).RequeueStaleToolCalls(t.Context(), time.Minute)
	if err != nil || requeued != 1 {
		t.Fatalf("requeue stale calls = %d, err=%v", requeued, err)
	}
	if _, err := calls.CompleteQueuedToolCall(t.Context(), firstLease, "stale-output"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale lease completed call: %v", err)
	}
	first, replacementLease, err := calls.ClaimQueuedToolCall(t.Context(), first.ID, time.Minute)
	if err != nil {
		t.Fatalf("claim recovered tool call: %v", err)
	}
	if first.ExecutionAttempt != 2 {
		t.Fatalf("recovered execution attempt = %d, want 2", first.ExecutionAttempt)
	}
	var republishedAt *time.Time
	if err := pool.QueryRow(t.Context(), `
		SELECT published_at FROM outbox_events WHERE event_type = $1 AND tool_call_id = $2::uuid
	`, workflow.EventToolCallRequested, first.ID).Scan(&republishedAt); err != nil {
		t.Fatalf("load requeued outbox event: %v", err)
	}
	if republishedAt != nil {
		t.Fatalf("requeued tool event remains published at %s", republishedAt)
	}

	errs := make(chan error, 2)
	go func() {
		_, err := calls.CompleteQueuedToolCall(t.Context(), replacementLease, "output-1")
		errs <- err
	}()
	go func() {
		_, err := calls.CompleteQueuedToolCall(t.Context(), secondLease, "output-2")
		errs <- err
	}()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("complete recovered group: %v", err)
		}
	}
	if _, _, err := calls.ClaimQueuedToolCall(t.Context(), first.ID, time.Minute); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("completed tool call was claimed again: %v", err)
	}
	var completionEvents int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM outbox_events
		WHERE event_type = $1 AND turn_run_id = $2::uuid AND execution_group = 0
	`, workflow.EventToolGroupCompleted, runID).Scan(&completionEvents); err != nil {
		t.Fatalf("count group completion events: %v", err)
	}
	if completionEvents != 1 {
		t.Fatalf("group completion events = %d, want 1", completionEvents)
	}
}

func insertToolExecutionRecoveryFixture(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	conversationID, turnID := uuid.NewString(), uuid.NewString()
	ownerUserID := insertIntegrationUser(t, pool, domain.UserRoleUser)
	actorUserID := insertIntegrationUser(t, pool, domain.UserRoleAdmin)
	credentialID, modelID, priceID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	pricing := json.RawMessage(`{"currency":"USD","input_per_million_nanos":1000000000,"cache_read_input_per_million_nanos":100000000,"cache_creation_input_per_million_nanos":1200000000,"output_per_million_nanos":8000000000}`)
	modelSnapshot, err := json.Marshal(domain.ModelExecutionSnapshot{
		ModelID: modelID, ModelRevision: 1, Provider: domain.ProviderOpenAI, CredentialID: credentialID,
		BaseURL: "https://api.example.com/v1", UpstreamModel: "gpt-test", ContextWindowTokens: 128000,
		MaxOutputTokens: 4096, SupportsTools: true, SupportsParallelTools: true,
		ModelPriceID: priceID, Currency: "USD", PricingSnapshot: pricing,
	})
	if err != nil {
		t.Fatalf("marshal model snapshot: %v", err)
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO provider_credentials (id, provider, name, base_url, encrypted_api_key, nonce, created_by_user_id, updated_by_user_id) VALUES ($1::uuid, 'openai', $2, 'https://api.example.com/v1', decode('01', 'hex'), decode('000000000000000000000000', 'hex'), $3::uuid, $3::uuid)`, []any{credentialID, "recovery-" + credentialID, actorUserID}},
		{`INSERT INTO models (id, provider, credential_id, slug, upstream_model, display_name, context_window_tokens, max_output_tokens, created_by_user_id, updated_by_user_id) VALUES ($1::uuid, 'openai', $2::uuid, $3, 'gpt-test', 'GPT Test', 128000, 4096, $4::uuid, $4::uuid)`, []any{modelID, credentialID, "recovery-" + modelID, actorUserID}},
		{`INSERT INTO model_price_versions (id, model_id, version, currency, input_per_million_nanos, cache_read_input_per_million_nanos, cache_creation_input_per_million_nanos, output_per_million_nanos, status, effective_from, pricing_snapshot, created_by_user_id, published_by_user_id, published_at) VALUES ($1::uuid, $2::uuid, 1, 'USD', 1000000000, 100000000, 1200000000, 8000000000, 'published', now(), $3::jsonb, $4::uuid, $4::uuid, now())`, []any{priceID, modelID, pricing, actorUserID}},
		{`INSERT INTO billing_accounts (user_id, currency, balance_nanos) VALUES ($1::uuid, 'USD', 1000000000000)`, []any{ownerUserID}},
		{`INSERT INTO conversations (id, owner_user_id) VALUES ($1::uuid, $2::uuid)`, []any{conversationID, ownerUserID}},
		{`INSERT INTO turns (id, conversation_id, seq, status, model_id, model_revision, model_price_id, model_snapshot) VALUES ($1::uuid, $2::uuid, 1, $3, $4::uuid, 1, $5::uuid, $6::jsonb)`, []any{turnID, conversationID, domain.TurnStatusContextReady, modelID, priceID, modelSnapshot}},
	} {
		if _, err := pool.Exec(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatalf("insert tool recovery fixture: %v", err)
		}
	}
	return conversationID, turnID
}
