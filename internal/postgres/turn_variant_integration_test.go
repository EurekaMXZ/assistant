package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetryTurnVariantLifecycleIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	userID := uuid.NewString()
	credentialID := uuid.NewString()
	modelID := uuid.NewString()
	priceID := uuid.NewString()
	conversationID := uuid.NewString()
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM conversations WHERE id = $1::uuid`, conversationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_price_versions WHERE id = $1::uuid`, priceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE id = $1::uuid`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM provider_credentials WHERE id = $1::uuid`, credentialID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, userID)
	}()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, username, password_hash, role, email_verified_at)
		VALUES ($1::uuid, $2, $3, 'integration-hash', 'user', now())
	`, userID, userID+"@example.com", "retry-user-"+userID); err != nil {
		t.Fatalf("seed retry user: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO provider_credentials (
			id, provider, name, base_url, encrypted_api_key, nonce, created_by_user_id, updated_by_user_id
		) VALUES ($1::uuid, 'openai', $2, 'https://example.com', decode('00', 'hex'), decode('000000000000000000000000', 'hex'), $3::uuid, $3::uuid)
	`, credentialID, "retry-credential-"+credentialID, userID); err != nil {
		t.Fatalf("seed retry credential: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO models (
			id, provider, credential_id, slug, upstream_model, display_name,
			context_window_tokens, max_output_tokens, created_by_user_id, updated_by_user_id
		) VALUES ($1::uuid, 'openai', $2::uuid, $3, 'gpt-test', 'GPT Test', 1000, 100, $4::uuid, $4::uuid)
	`, modelID, credentialID, "retry-model-"+modelID, userID); err != nil {
		t.Fatalf("seed retry model: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO model_price_versions (
			id, model_id, version, currency, status, effective_from, pricing_snapshot,
			created_by_user_id, published_by_user_id, published_at
		) VALUES ($1::uuid, $2::uuid, 1, 'USD', 'published', now(), '{}', $3::uuid, $3::uuid, now())
	`, priceID, modelID, userID); err != nil {
		t.Fatalf("seed retry model price: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO conversations (id, owner_user_id, title) VALUES ($1::uuid, $2::uuid, 'Retry integration')
	`, conversationID, userID); err != nil {
		t.Fatalf("seed retry conversation: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO context_heads (conversation_id) VALUES ($1::uuid)`, conversationID); err != nil {
		t.Fatalf("seed retry context head: %v", err)
	}

	snapshot := domain.ModelExecutionSnapshot{
		ModelID: modelID, ModelRevision: 1, ModelPriceID: priceID,
		ContextWindowTokens: 1000, MaxOutputTokens: 100,
		PricingSnapshot: json.RawMessage(`{"currency":"USD"}`),
	}
	turns := NewTurnRepository(pool)
	root, err := turns.CreateUserTurn(t.Context(), CreateUserTurnParams{
		ConversationID: conversationID, Content: "question", Metadata: json.RawMessage(`{"model_id":"model"}`), ModelSnapshot: snapshot,
	})
	if err != nil {
		t.Fatalf("create root turn: %v", err)
	}
	userTokens := domain.EstimateTokens("question")
	originalTokens := domain.EstimateTokens("original answer")
	if _, err := pool.Exec(t.Context(), `
		UPDATE turns SET status = $2, completed_at = now() WHERE id = $1::uuid
	`, root.Turn.ID, domain.TurnStatusCompleted); err != nil {
		t.Fatalf("mark root completed: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO messages (conversation_id, turn_id, seq, role, content_text, token_count)
		VALUES ($1::uuid, $2::uuid, 2, $3, 'original answer', $4)
	`, conversationID, root.Turn.ID, domain.RoleAssistant, originalTokens); err != nil {
		t.Fatalf("insert root answer: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		UPDATE context_heads SET last_seq = 2, active_context_tokens = $2 WHERE conversation_id = $1::uuid
	`, conversationID, userTokens+originalTokens); err != nil {
		t.Fatalf("advance root context: %v", err)
	}

	type retryResult struct {
		turn *domain.EnqueuedRetryTurn
		err  error
	}
	start := make(chan struct{})
	results := make(chan retryResult, 2)
	for range 2 {
		go func() {
			<-start
			turn, err := turns.CreateRetryTurn(t.Context(), root.Turn.ID, CreateUserTurnParams{
				ConversationID: conversationID, Content: "question", Metadata: root.Message.Metadata, ModelSnapshot: snapshot,
			})
			results <- retryResult{turn: turn, err: err}
		}()
	}
	close(start)
	var retry *domain.EnqueuedRetryTurn
	conflicts := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			retry = result.turn
		} else if errors.Is(result.err, domain.ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent retry error: %v", result.err)
		}
	}
	if retry == nil || conflicts != 1 {
		t.Fatalf("concurrent retry results: retry=%#v conflicts=%d", retry, conflicts)
	}
	if retry.Turn.RetryOfTurnID != root.Turn.ID || retry.Turn.VariantIndex != 2 {
		t.Fatalf("retry turn = %#v", retry.Turn)
	}

	workflowTurns := NewWorkflowTurnRepository(pool)
	if _, err := pool.Exec(t.Context(), `UPDATE turns SET status = $2 WHERE id = $1::uuid`, retry.Turn.ID, domain.TurnStatusContextReady); err != nil {
		t.Fatalf("mark first retry context ready: %v", err)
	}
	if err := workflowTurns.FinalizeTurnFailure(t.Context(), retry.Turn.ID, "", "", "retry_failed", "retry failed", 0); err != nil {
		t.Fatalf("finalize first retry failure: %v", err)
	}

	successfulRetry, err := turns.CreateRetryTurn(t.Context(), root.Turn.ID, CreateUserTurnParams{
		ConversationID: conversationID, Content: "edited question", Metadata: root.Message.Metadata, ModelSnapshot: snapshot,
	})
	if err != nil {
		t.Fatalf("create successful retry turn: %v", err)
	}
	if successfulRetry.Turn.VariantIndex != 3 {
		t.Fatalf("successful retry variant index = %d, want 3", successfulRetry.Turn.VariantIndex)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE turns SET status = $2 WHERE id = $1::uuid`, successfulRetry.Turn.ID, domain.TurnStatusProcessing); err != nil {
		t.Fatalf("mark retry processing: %v", err)
	}
	editedTokens := domain.EstimateTokens("edited question")
	retryTokens := domain.EstimateTokens("retry answer")
	completed, _, head, triggerCompact, err := workflowTurns.FinalizeTurnSuccess(
		t.Context(), successfulRetry.Turn.ID, []domain.AssistantMessageDraft{{ContentText: "retry answer"}},
		domain.TurnRunSummary{Model: "gpt-test"}, 0,
	)
	if err != nil {
		t.Fatalf("finalize retry success: %v", err)
	}
	if completed.RetryOfTurnID != root.Turn.ID || !triggerCompact || head.ActiveContextTokens != editedTokens+retryTokens {
		t.Fatalf("retry completion: turn=%#v head=%#v compact=%t", completed, head, triggerCompact)
	}

	rows, err := pool.Query(t.Context(), `
		SELECT content_text, context_excluded
		FROM messages
		WHERE conversation_id = $1::uuid AND role = $2
		ORDER BY seq
	`, conversationID, domain.RoleAssistant)
	if err != nil {
		t.Fatalf("list assistant variants: %v", err)
	}
	defer rows.Close()
	var variants []struct {
		content  string
		excluded bool
	}
	for rows.Next() {
		var variant struct {
			content  string
			excluded bool
		}
		if err := rows.Scan(&variant.content, &variant.excluded); err != nil {
			t.Fatalf("scan assistant variant: %v", err)
		}
		variants = append(variants, variant)
	}
	if len(variants) != 2 || !variants[0].excluded || variants[1].excluded {
		t.Fatalf("assistant variant context flags = %#v", variants)
	}
	var activeUserContent string
	if err := pool.QueryRow(t.Context(), `
		SELECT content_text FROM messages
		WHERE conversation_id = $1::uuid AND role = $2 AND context_excluded = false
	`, conversationID, domain.RoleUser).Scan(&activeUserContent); err != nil {
		t.Fatalf("load active edited prompt: %v", err)
	}
	if activeUserContent != "edited question" {
		t.Fatalf("active user prompt = %q, want edited question", activeUserContent)
	}

	failedRetry, err := turns.CreateRetryTurn(t.Context(), successfulRetry.Turn.ID, CreateUserTurnParams{Content: "edited question", Metadata: root.Message.Metadata, ModelSnapshot: snapshot})
	if err != nil {
		t.Fatalf("create second retry: %v", err)
	}
	var selectedSourceTurnID string
	if err := pool.QueryRow(t.Context(), `SELECT metadata ->> 'variant_source_turn_id' FROM turns WHERE id = $1::uuid`, failedRetry.Turn.ID).Scan(&selectedSourceTurnID); err != nil {
		t.Fatalf("load selected variant source: %v", err)
	}
	if selectedSourceTurnID != successfulRetry.Turn.ID {
		t.Fatalf("selected variant source = %q, want %q", selectedSourceTurnID, successfulRetry.Turn.ID)
	}
	contexts := NewWorkflowContextRepository(pool)
	_, err = contexts.CompleteCompaction(t.Context(), conversationID, domain.AnchorObject{CoveredUntilSeq: 1}, head.LastSeq)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("compaction during retry error = %v, want conflict", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE turns SET status = $2 WHERE id = $1::uuid`, failedRetry.Turn.ID, domain.TurnStatusContextReady); err != nil {
		t.Fatalf("mark second retry context ready: %v", err)
	}
	if err := workflowTurns.FinalizeTurnFailure(t.Context(), failedRetry.Turn.ID, "", "", "retry_failed", "retry failed", 0); err != nil {
		t.Fatalf("finalize retry failure: %v", err)
	}

	var compactionRequests int
	if err := pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE conversation_id = $1::uuid AND event_type = $2
	`, conversationID, workflow.EventContextCompactionRequest).Scan(&compactionRequests); err != nil {
		t.Fatalf("count compaction requests: %v", err)
	}
	if compactionRequests != 3 {
		t.Fatalf("compaction request count = %d, want 3", compactionRequests)
	}

	next, err := turns.CreateUserTurn(t.Context(), CreateUserTurnParams{
		ConversationID: conversationID, Content: "follow up", Metadata: json.RawMessage(`{}`), ModelSnapshot: snapshot,
	})
	if err != nil {
		t.Fatalf("create turn after failed retry: %v", err)
	}
	if next.Turn.Seq <= failedRetry.Turn.Seq || next.Message.Seq != next.Turn.Seq {
		t.Fatalf("turn sequence did not skip failed retry: failed=%d next=%#v", failedRetry.Turn.Seq, next)
	}
}
