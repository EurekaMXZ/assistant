package postgres

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/EurekaMXZ/assistant/internal/workflow"
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
	billingRepository := NewBillingAccountRepository(pool)
	currentToolPrices, err := billingRepository.ListToolPrices(t.Context(), "USD")
	if err != nil {
		t.Fatalf("load current tool prices: %v", err)
	}
	toolPriceVersions := make(map[string]int64, len(currentToolPrices))
	for _, price := range currentToolPrices {
		toolPriceVersions[price.ToolKey] = price.Version
	}
	if _, err := billingRepository.UpdateToolPrices(t.Context(), UpdateBillingToolPricesParams{
		Currency: "USD", ActorUserID: actorUserID, ActorRole: domain.UserRoleAdmin,
		Prices: []BillingToolPriceUpdate{
			{ToolKey: domain.BillingToolSandboxCreate, PricePerCallNanos: 400_000_000, Enabled: true, ExpectedVersion: toolPriceVersions[domain.BillingToolSandboxCreate]},
			{ToolKey: domain.BillingToolImageGeneration, PricePerCallNanos: 300_000_000, Enabled: true, ExpectedVersion: toolPriceVersions[domain.BillingToolImageGeneration]},
			{ToolKey: domain.BillingToolTavilySearch, PricePerCallNanos: 10_000_000, Enabled: false, ExpectedVersion: toolPriceVersions[domain.BillingToolTavilySearch]},
			{ToolKey: domain.BillingToolTavilyExtract, PricePerCallNanos: 20_000_000, Enabled: false, ExpectedVersion: toolPriceVersions[domain.BillingToolTavilyExtract]},
		},
	}); err != nil {
		t.Fatalf("configure tool prices: %v", err)
	}
	calls := NewToolCallRepository(pool)
	call := tool.ToolCall{CallID: "call-1", Type: llm.ModelItemFunctionCall, Namespace: "sandbox", Name: "create"}
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
	incompleteCall := tool.ToolCall{CallID: "call-incomplete", Type: llm.ModelItemFunctionCall, Name: "side-effect"}
	if _, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt, incompleteCall, "arguments-incomplete"); err != nil || !acquired {
		t.Fatalf("acquire incomplete fixture: acquired=%t err=%v", acquired, err)
	}
	failedCall, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt+1, incompleteCall, "arguments-incomplete")
	if err != nil || acquired || failedCall.Status != domain.ToolCallStatusFailed {
		t.Fatalf("recover incomplete call = %#v acquired=%t err=%v", failedCall, acquired, err)
	}
	askCall, acquired, err := calls.AcquireToolCall(t.Context(), turnID, runID, run.Attempt, tool.ToolCall{
		CallID: "call-ask", Type: llm.ModelItemFunctionCall, Name: tool.AskUser,
	}, "arguments-ask")
	if err != nil || !acquired {
		t.Fatalf("acquire ask_user call: call=%#v acquired=%t err=%v", askCall, acquired, err)
	}
	if err := runs.CheckpointScheduledTurnRun(t.Context(), firstLease, "resp-1", "response-1", "result-1"); err != nil {
		t.Fatalf("checkpoint ask_user run: %v", err)
	}
	usage := llm.ModelUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
	awaitingPayload := json.RawMessage(`{"id":"ask-user:` + askCall.ID + `","tool_call_id":"` + askCall.ID + `","prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}],"status":"awaiting_input"}`)
	if _, err := runs.AwaitScheduledTurnRunInput(t.Context(), workflow.AwaitScheduledTurnRunInput{
		Lease: firstLease, ToolCallID: uuid.NewString(), Interaction: awaitingPayload,
		Usage: usage, ImageGenerationCount: 1, CompactTriggerTokens: 100_000,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("invalid pause error = %v, want not found", err)
	}
	var failedPauseRunStatus, failedPauseTurnStatus, failedPauseToolStatus string
	if err := pool.QueryRow(t.Context(), `
		SELECT tr.status, t.status, tc.status
		FROM turn_runs tr
		JOIN turns t ON t.id = tr.turn_id
		JOIN tool_calls tc ON tc.turn_run_id = tr.id AND tc.id = $2::uuid
		WHERE tr.id = $1::uuid
	`, runID, askCall.ID).Scan(&failedPauseRunStatus, &failedPauseTurnStatus, &failedPauseToolStatus); err != nil {
		t.Fatalf("load failed pause states: %v", err)
	}
	if failedPauseRunStatus != domain.TurnRunStatusRunning || failedPauseTurnStatus != domain.TurnStatusProcessing || failedPauseToolStatus != domain.ToolCallStatusRunning {
		t.Fatalf("failed pause exposed answerable state: run=%q turn=%q tool=%q", failedPauseRunStatus, failedPauseTurnStatus, failedPauseToolStatus)
	}
	awaited, err := runs.AwaitScheduledTurnRunInput(t.Context(), workflow.AwaitScheduledTurnRunInput{
		Lease: firstLease, ToolCallID: askCall.ID, Interaction: awaitingPayload,
		Usage: usage, ImageGenerationCount: 1, CompactTriggerTokens: 100_000,
	})
	if err != nil {
		t.Fatalf("pause ask_user run: %v", err)
	}
	if awaited.Status != domain.TurnRunStatusAwaitingInput || awaited.BillingSettledAt == nil {
		t.Fatalf("awaited run = %#v", awaited)
	}
	var pausedTurnStatus, pausedToolStatus string
	if err := pool.QueryRow(t.Context(), `
		SELECT t.status, tc.status
		FROM turns t JOIN tool_calls tc ON tc.turn_id = t.id
		WHERE t.id = $1::uuid AND tc.id = $2::uuid
	`, turnID, askCall.ID).Scan(&pausedTurnStatus, &pausedToolStatus); err != nil {
		t.Fatalf("load paused states: %v", err)
	}
	if pausedTurnStatus != domain.TurnStatusAwaitingInput || pausedToolStatus != domain.ToolCallStatusAwaitingInput {
		t.Fatalf("paused states: turn=%q tool=%q", pausedTurnStatus, pausedToolStatus)
	}
	fingerprint := strings.Repeat("a", 64)
	claim, err := calls.ClaimAwaitingInputAnswer(t.Context(), ownerUserID, turnID, askCall.ID, "answer-1", fingerprint, "yes", "answer-output-1")
	if err != nil || claim.Finalized || claim.ToolCall == nil || !claim.ToolCall.AnswerOutputPending {
		t.Fatalf("claim ask_user answer = %#v err=%v", claim, err)
	}
	if _, err := calls.ClaimAwaitingInputAnswer(t.Context(), ownerUserID, turnID, askCall.ID, "answer-2", strings.Repeat("b", 64), "cancel", "answer-output-2"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting answer claim error = %v", err)
	}
	if _, err := NewTurnRepository(pool).RequestTurnCancellation(t.Context(), turnID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("cancellation over declared answer error = %v, want conflict", err)
	}
	completedInteraction := json.RawMessage(`{"id":"ask-user:` + askCall.ID + `","tool_call_id":"` + askCall.ID + `","prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}],"answer":{"status":"answered","option_id":"yes","label":"Yes","user_reported":true},"status":"completed"}`)
	answered, replayed, err := calls.FinalizeAwaitingInputAnswer(t.Context(), ownerUserID, turnID, askCall.ID, "answer-1", fingerprint, "yes", "answer-output-1", completedInteraction)
	if err != nil || replayed || answered.Status != domain.ToolCallStatusCompleted {
		t.Fatalf("finalize ask_user answer = %#v replayed=%t err=%v", answered, replayed, err)
	}
	var completedInteractionCount, resumeOutboxCount int
	var completedInteractionPayload string
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*), COALESCE(max(payload::text), '')
		FROM conversation_events
		WHERE turn_id = $1::uuid AND event_type = $2
	`, turnID, domain.ConversationEventInteractionCompleted).Scan(&completedInteractionCount, &completedInteractionPayload); err != nil {
		t.Fatalf("load completed interaction event: %v", err)
	}
	if completedInteractionCount != 1 || (!strings.Contains(completedInteractionPayload, `"option_id": "yes"`) && !strings.Contains(completedInteractionPayload, `"option_id":"yes"`)) {
		t.Fatalf("completed interaction event count=%d payload=%s", completedInteractionCount, completedInteractionPayload)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox_events WHERE turn_run_id = $1::uuid AND event_type = $2`, runID, workflow.EventTurnRunRequested).Scan(&resumeOutboxCount); err != nil {
		t.Fatalf("count resumed run outbox: %v", err)
	}
	if _, replayed, err := calls.FinalizeAwaitingInputAnswer(t.Context(), ownerUserID, turnID, askCall.ID, "answer-1", fingerprint, "yes", "answer-output-1", completedInteraction); err != nil || !replayed {
		t.Fatalf("replay finalized ask_user answer: replayed=%t err=%v", replayed, err)
	}
	var replayedInteractionCount, replayedOutboxCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM conversation_events WHERE turn_id = $1::uuid AND event_type = $2`, turnID, domain.ConversationEventInteractionCompleted).Scan(&replayedInteractionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox_events WHERE turn_run_id = $1::uuid AND event_type = $2`, runID, workflow.EventTurnRunRequested).Scan(&replayedOutboxCount); err != nil {
		t.Fatal(err)
	}
	if replayedInteractionCount != completedInteractionCount || replayedOutboxCount != resumeOutboxCount {
		t.Fatalf("idempotent finalize duplicated durable work: events %d->%d outbox %d->%d", completedInteractionCount, replayedInteractionCount, resumeOutboxCount, replayedOutboxCount)
	}
	resumed, resumedLease, err := runs.ClaimTurnRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("claim resumed ask_user run: %v", err)
	}
	if !resumed.StartedAt.Equal(run.StartedAt) {
		t.Fatalf("resume changed started_at: first=%s resumed=%s", run.StartedAt, resumed.StartedAt)
	}
	if _, err := NewConversationEventRepository(pool).AppendCompleteEvent(t.Context(), domain.ConversationEventInput{
		ConversationID: conversationID, TurnID: turnID, TurnRunID: runID,
		EventKey: "run:" + runID + ":completed", EventType: domain.ConversationEventRunCompleted,
	}); err != nil {
		t.Fatalf("persist first run completion event: %v", err)
	}
	completed, err := runs.CompleteScheduledTurnRun(t.Context(), resumedLease, "resp-1", "response-1", "result-1", usage, 1, 100_000)
	if err != nil {
		t.Fatalf("complete first run: %v", err)
	}
	if completed.Status != domain.TurnRunStatusCompleted || completed.ResultBlobKey != "result-1" {
		t.Fatalf("completed run = %#v", completed)
	}
	var totalAmount, toolAmount int64
	var billingTransactionID string
	var toolUsage, toolPricing []byte
	if err := pool.QueryRow(t.Context(), `
		SELECT amount_nanos, tool_amount_nanos, tool_usage, tool_pricing_snapshot,
			COALESCE(billing_transaction_id::text, '')
		FROM billing_usage_events WHERE turn_run_id = $1::uuid
	`, runID).Scan(&totalAmount, &toolAmount, &toolUsage, &toolPricing, &billingTransactionID); err != nil {
		t.Fatalf("load tool-rated usage event: %v", err)
	}
	if toolAmount != 700_000_000 || totalAmount != 700_026_000 {
		t.Fatalf("rated amounts: total=%d tool=%d", totalAmount, toolAmount)
	}
	if !strings.Contains(string(toolUsage), `"sandbox.create": 1`) && !strings.Contains(string(toolUsage), `"sandbox.create":1`) {
		t.Fatalf("tool usage missing sandbox call: %s", toolUsage)
	}
	if !strings.Contains(string(toolPricing), `"image_generation"`) {
		t.Fatalf("tool pricing missing image generation: %s", toolPricing)
	}
	var balanceAfter, transactionAmount int64
	if err := pool.QueryRow(t.Context(), `SELECT balance_nanos FROM billing_accounts WHERE user_id = $1::uuid AND currency = 'USD'`, ownerUserID).Scan(&balanceAfter); err != nil {
		t.Fatalf("load balance after tool charge: %v", err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT amount_nanos FROM billing_transactions WHERE id = $1::uuid`, billingTransactionID).Scan(&transactionAmount); err != nil {
		t.Fatalf("load tool-rated billing transaction: %v", err)
	}
	if balanceAfter != 1_000_000_000_000-totalAmount || transactionAmount != totalAmount {
		t.Fatalf("tool debit mismatch: balance=%d transaction=%d total=%d", balanceAfter, transactionAmount, totalAmount)
	}
	if _, err := runs.CompleteScheduledTurnRun(t.Context(), resumedLease, "resp-duplicate", "", "", llm.ModelUsage{}, 0, 100_000); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate completion error = %v, want conflict", err)
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
	var replacementRunID string
	if err := pool.QueryRow(t.Context(), `
		SELECT id::text FROM turn_runs WHERE turn_id = $1::uuid AND step_index = 2 AND attempt = 2
	`, turnID).Scan(&replacementRunID); err != nil {
		t.Fatalf("load replacement run: %v", err)
	}
	replacement, replacementLease, err := runs.ClaimTurnRun(t.Context(), replacementRunID)
	if err != nil {
		t.Fatalf("claim replacement attempt: %v", err)
	}
	if replacementLease.Token == staleLease.Token {
		t.Fatal("replacement attempt reused stale lease token")
	}
	if replacement.ID == nextRunID || replacement.Attempt != 2 || replacement.ResultBlobKey != "result-2" || replacement.ResponseBlobKey != "response-2" {
		t.Fatalf("replacement lost checkpoint: %#v", replacement)
	}
	if _, err := runs.CompleteScheduledTurnRun(t.Context(), staleLease, "stale", "", "", llm.ModelUsage{}, 0, 100_000); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale completion error = %v, want conflict", err)
	}
	failed, err := runs.FailScheduledTurnRun(t.Context(), replacementLease, "", "", "", "upstream failed",
		"request-2", "stream-2", domain.TurnErrorUpstreamRequestFailed, domain.TurnPublicErrorUpstreamRequestFailed, 100_000)
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

	cancelConversationID := uuid.NewString()
	cancelTurnID := uuid.NewString()
	if _, err := pool.Exec(t.Context(), `INSERT INTO conversations (id, owner_user_id) VALUES ($1::uuid, $2::uuid)`, cancelConversationID, ownerUserID); err != nil {
		t.Fatalf("insert cancellation conversation: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO turns (id, conversation_id, seq, status, model_id, model_revision, model_price_id, model_snapshot)
		VALUES ($1::uuid, $2::uuid, 1, $3, $4::uuid, 1, $5::uuid, $6::jsonb)
	`, cancelTurnID, cancelConversationID, domain.TurnStatusContextReady, modelID, priceID, modelSnapshot); err != nil {
		t.Fatalf("insert cancellation turn: %v", err)
	}
	cancelRunID, err := runs.StartTurnRun(t.Context(), cancelTurnID, "openai.responses", "gpt-test", "request-cancel", "state-cancel")
	if err != nil {
		t.Fatalf("start cancellation run: %v", err)
	}
	cancelRun, cancelLease, err := runs.ClaimTurnRun(t.Context(), cancelRunID)
	if err != nil {
		t.Fatalf("claim cancellation run: %v", err)
	}
	cancelCall, acquired, err := calls.AcquireToolCall(t.Context(), cancelTurnID, cancelRunID, cancelRun.Attempt, tool.ToolCall{
		CallID: "call-cancel", Type: llm.ModelItemFunctionCall, Name: tool.AskUser,
	}, "arguments-cancel")
	if err != nil || !acquired {
		t.Fatalf("acquire cancellation ask_user: call=%#v acquired=%t err=%v", cancelCall, acquired, err)
	}
	if err := runs.CheckpointScheduledTurnRun(t.Context(), cancelLease, "resp-cancel", "response-cancel", "result-cancel"); err != nil {
		t.Fatalf("checkpoint cancellation run: %v", err)
	}
	cancelAwaitingPayload := json.RawMessage(`{"id":"ask-user:` + cancelCall.ID + `","tool_call_id":"` + cancelCall.ID + `","prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}],"status":"awaiting_input"}`)
	if _, err := runs.AwaitScheduledTurnRunInput(t.Context(), workflow.AwaitScheduledTurnRunInput{
		Lease: cancelLease, ToolCallID: cancelCall.ID, Interaction: cancelAwaitingPayload,
		Usage: llm.ModelUsage{InputTokens: 4, OutputTokens: 6, TotalTokens: 10}, CompactTriggerTokens: 100_000,
	}); err != nil {
		t.Fatalf("pause cancellation run: %v", err)
	}
	cancelledTurn, err := NewTurnRepository(pool).RequestTurnCancellation(t.Context(), cancelTurnID)
	if err != nil || cancelledTurn.Status != domain.TurnStatusCancelRequested {
		t.Fatalf("request waiting turn cancellation = %#v err=%v", cancelledTurn, err)
	}
	if err := runs.FinalizeTurnCancellation(t.Context(), cancelConversationID, cancelTurnID); err != nil {
		t.Fatalf("finalize waiting turn cancellation: %v", err)
	}
	var cancelledToolStatus, cancelledInteractionPayload, cancellationBillingTransactionID string
	if err := pool.QueryRow(t.Context(), `SELECT status FROM tool_calls WHERE id = $1::uuid`, cancelCall.ID).Scan(&cancelledToolStatus); err != nil {
		t.Fatalf("load cancelled tool status: %v", err)
	}
	if err := pool.QueryRow(t.Context(), `
		SELECT payload::text
		FROM conversation_events
		WHERE turn_id = $1::uuid AND event_type = $2
	`, cancelTurnID, domain.ConversationEventInteractionCancelled).Scan(&cancelledInteractionPayload); err != nil {
		t.Fatalf("load cancelled interaction event: %v", err)
	}
	if err := pool.QueryRow(t.Context(), `
		SELECT COALESCE(billing_transaction_id::text, '')
		FROM billing_usage_events
		WHERE turn_run_id = $1::uuid
	`, cancelRunID).Scan(&cancellationBillingTransactionID); err != nil {
		t.Fatalf("load cancelled run billing: %v", err)
	}
	if cancelledToolStatus != domain.ToolCallStatusCancelled || !strings.Contains(cancelledInteractionPayload, `"option_id": "cancelled"`) || cancellationBillingTransactionID == "" {
		t.Fatalf("cancelled interaction status=%q payload=%s billing_transaction=%q", cancelledToolStatus, cancelledInteractionPayload, cancellationBillingTransactionID)
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
	insufficientCall, acquired, err := calls.AcquireToolCall(t.Context(), insufficientTurnID, insufficientRunID, 1,
		tool.ToolCall{CallID: "call-insufficient", Type: llm.ModelItemFunctionCall, Namespace: "sandbox", Name: "create"}, "arguments-insufficient")
	if err != nil || !acquired {
		t.Fatalf("acquire insufficient tool call: call=%#v acquired=%t err=%v", insufficientCall, acquired, err)
	}
	if _, err := calls.CompleteToolCall(t.Context(), insufficientCall.ID, "output-insufficient"); err != nil {
		t.Fatalf("complete insufficient tool call: %v", err)
	}
	if _, err := NewConversationEventRepository(pool).AppendCompleteEvent(t.Context(), domain.ConversationEventInput{
		ConversationID: insufficientConversationID, TurnID: insufficientTurnID, TurnRunID: insufficientRunID,
		EventKey: "run:" + insufficientRunID + ":completed", EventType: domain.ConversationEventRunCompleted,
	}); err != nil {
		t.Fatalf("persist insufficient run completion event: %v", err)
	}
	settled, err := runs.CompleteScheduledTurnRun(t.Context(), insufficientLease, "resp-insufficient", "response-insufficient", "result-insufficient", llm.ModelUsage{InputTokens: 1000, TotalTokens: 1000}, 0, 100_000)
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
	var failedToolAmount int64
	if err := pool.QueryRow(t.Context(), `SELECT status, billing_transaction_id::text, tool_amount_nanos FROM billing_usage_events WHERE turn_run_id = $1::uuid`, insufficientRunID).Scan(&usageStatus, &transactionID, &failedToolAmount); err != nil {
		t.Fatalf("load insufficient usage event: %v", err)
	}
	if usageStatus != "failed" || transactionID != nil || failedToolAmount != 400_000_000 {
		t.Fatalf("insufficient usage status=%q transaction=%v tool=%d", usageStatus, transactionID, failedToolAmount)
	}
}
