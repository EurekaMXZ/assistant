package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/billing"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const turnRunColumns = `
	tr.id::text,
	tr.turn_id::text,
	tr.step_index,
	tr.provider,
	tr.model,
	tr.status,
	tr.request_blob_key,
	COALESCE(tr.response_blob_key, ''),
	COALESCE(tr.output_items_blob_key, ''),
	COALESCE(tr.tool_results_blob_key, ''),
	COALESCE(tr.presentation_events_blob_key, ''),
	COALESCE(tr.checkpoint_blob_key, ''),
	COALESCE(tr.request_checksum, ''),
	COALESCE(tr.response_checksum, ''),
	COALESCE(tr.request_size_bytes, 0),
	COALESCE(tr.response_size_bytes, 0),
	tr.request_schema_version,
	tr.response_schema_version,
	COALESCE(tr.response_id, ''),
	tr.input_tokens,
	tr.cache_read_input_tokens,
	tr.cache_creation_input_tokens,
	tr.output_tokens,
	tr.reasoning_output_tokens,
	tr.total_tokens,
	COALESCE(tr.billing_currency, ''),
	tr.billing_amount_nanos,
	COALESCE(tr.error_message, ''),
	tr.started_at,
	tr.completed_at,
	tr.failed_at,
	tr.created_at,
	tr.updated_at,
	tr.attempt,
	tr.state_blob_key,
	COALESCE(tr.result_blob_key, ''),
	tr.heartbeat_at,
	tr.cancelled_at`

func (r *TurnRunRepository) StartTurnRun(ctx context.Context, turnID string, provider string, model string, requestBlobKey string, stateBlobKey string) (string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var conversationID, status string
	if err := tx.QueryRow(ctx, `
		SELECT conversation_id::text, status
		FROM turns
		WHERE id = $1::uuid
		FOR UPDATE
	`, turnID).Scan(&conversationID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return "", fmt.Errorf("lock turn for first run: %w", err)
	}
	if status != domain.TurnStatusContextReady {
		return "", domain.ErrConflict
	}

	var runID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO turn_runs (
			turn_id, step_index, provider, model, status,
			request_blob_key, state_blob_key
		)
		VALUES ($1::uuid, 1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, turnID, provider, model, domain.TurnRunStatusQueued, requestBlobKey, stateBlobKey).Scan(&runID); err != nil {
		return "", fmt.Errorf("insert first turn run: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE turns
		SET
			status = $2,
			started_at = COALESCE(started_at, now()),
			error_code = NULL,
			error_message = NULL,
			completed_at = NULL,
			failed_at = NULL
		WHERE id = $1::uuid
	`, turnID, domain.TurnStatusProcessing); err != nil {
		return "", fmt.Errorf("mark turn processing: %w", err)
	}

	if err := insertTurnRunRequestedEvent(ctx, tx, conversationID, turnID, runID, 1); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit first turn run: %w", err)
	}
	return runID, nil
}

func (r *TurnRunRepository) SetTurnRunArtifactMetadata(ctx context.Context, runID string, requestKey string, responseKey string, outputItemsKey string, toolResultsKey string, presentationEventsKey string, checkpointKey string, requestChecksum string, responseChecksum string, requestSizeBytes int64, responseSizeBytes int64, requestSchemaVersion int, responseSchemaVersion int) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin turn run artifact metadata: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE turn_runs
		SET
			request_blob_key = COALESCE(NULLIF($2, ''), request_blob_key),
			response_blob_key = COALESCE(NULLIF($3, ''), response_blob_key),
			output_items_blob_key = COALESCE(NULLIF($4, ''), output_items_blob_key),
			tool_results_blob_key = COALESCE(NULLIF($5, ''), tool_results_blob_key),
			presentation_events_blob_key = COALESCE(NULLIF($6, ''), presentation_events_blob_key),
			checkpoint_blob_key = COALESCE(NULLIF($7, ''), checkpoint_blob_key),
			request_checksum = COALESCE(NULLIF($8, ''), request_checksum),
			response_checksum = COALESCE(NULLIF($9, ''), response_checksum),
			request_size_bytes = CASE WHEN $10 > 0 THEN $10 ELSE request_size_bytes END,
			response_size_bytes = CASE WHEN $11 > 0 THEN $11 ELSE response_size_bytes END,
			request_schema_version = CASE WHEN $12 > 0 THEN $12 ELSE request_schema_version END,
			response_schema_version = CASE WHEN $13 > 0 THEN $13 ELSE response_schema_version END
		WHERE id = $1::uuid
	`, runID, requestKey, responseKey, outputItemsKey, toolResultsKey, presentationEventsKey, checkpointKey,
		requestChecksum, responseChecksum, requestSizeBytes, responseSizeBytes, requestSchemaVersion, responseSchemaVersion); err != nil {
		return fmt.Errorf("update turn run artifact metadata: %w", err)
	}
	if requestKey != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE context_heads ch
			SET latest_request_run_id = $1::uuid
			FROM turns t
			JOIN turn_runs tr ON tr.turn_id = t.id
			WHERE tr.id = $1::uuid AND ch.conversation_id = t.conversation_id
		`, runID); err != nil {
			return fmt.Errorf("update latest request run: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit turn run artifact metadata: %w", err)
	}
	return nil
}

func (r *TurnRunRepository) ListReferencedRunArtifactKeys(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT object_key
		FROM (
			SELECT request_blob_key AS object_key FROM turn_runs
			UNION ALL SELECT response_blob_key FROM turn_runs
			UNION ALL SELECT output_items_blob_key FROM turn_runs
			UNION ALL SELECT tool_results_blob_key FROM turn_runs
			UNION ALL SELECT presentation_events_blob_key FROM turn_runs
			UNION ALL SELECT checkpoint_blob_key FROM turn_runs
			UNION ALL SELECT state_blob_key FROM turn_runs
			UNION ALL SELECT result_blob_key FROM turn_runs
			UNION ALL SELECT request_blob_key FROM turns
			UNION ALL SELECT response_blob_key FROM turns
			UNION ALL SELECT stream_blob_key FROM turns
			UNION ALL SELECT anchor_key FROM context_heads
			UNION ALL SELECT latest_checkpoint_key FROM context_heads
			UNION ALL SELECT arguments_blob_key FROM tool_calls
			UNION ALL SELECT output_blob_key FROM tool_calls
		) artifact_refs
		WHERE NULLIF(btrim(object_key), '') IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("list referenced run artifact keys: %w", err)
	}
	defer rows.Close()

	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan referenced run artifact key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate referenced run artifact keys: %w", err)
	}
	return keys, nil
}

func (r *TurnRunRepository) ScheduleNextTurnRun(ctx context.Context, turnID string, previousRunID string, stepIndex int, provider string, model string, requestBlobKey string, stateBlobKey string) (string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var conversationID, turnStatus, previousStatus string
	if err := tx.QueryRow(ctx, `
		SELECT t.conversation_id::text, t.status, tr.status
		FROM turns t
		JOIN turn_runs tr ON tr.id = $2::uuid AND tr.turn_id = t.id
		WHERE t.id = $1::uuid
		FOR UPDATE OF t, tr
	`, turnID, previousRunID).Scan(&conversationID, &turnStatus, &previousStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return "", fmt.Errorf("lock turn for next run: %w", err)
	}
	if turnStatus != domain.TurnStatusProcessing || previousStatus != domain.TurnRunStatusCompleted {
		return "", domain.ErrConflict
	}

	var runID string
	err = tx.QueryRow(ctx, `
		INSERT INTO turn_runs (
			turn_id, step_index, provider, model, status,
			request_blob_key, state_blob_key
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (turn_id, step_index, attempt) DO NOTHING
		RETURNING id::text
	`, turnID, stepIndex, provider, model, domain.TurnRunStatusQueued, requestBlobKey, stateBlobKey).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `
			SELECT id::text
			FROM turn_runs
			WHERE turn_id = $1::uuid AND step_index = $2 AND attempt = 0
		`, turnID, stepIndex).Scan(&runID); err != nil {
			return "", fmt.Errorf("load existing next turn run: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return "", fmt.Errorf("commit existing next turn run: %w", err)
		}
		return runID, nil
	}
	if err != nil {
		return "", fmt.Errorf("insert next turn run: %w", err)
	}

	if err := insertTurnRunRequestedEvent(ctx, tx, conversationID, turnID, runID, stepIndex); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit next turn run: %w", err)
	}
	return runID, nil
}

func insertTurnRunRequestedEvent(ctx context.Context, tx pgx.Tx, conversationID string, turnID string, runID string, stepIndex int) error {
	return insertOutboxEvent(ctx, tx, outboxInsert{
		EventType:      workflow.EventTurnRunRequested,
		ConversationID: conversationID,
		TurnID:         turnID,
		TurnRunID:      runID,
	})
}

func (r *TurnRunRepository) GetTurnRun(ctx context.Context, runID string) (*domain.TurnRun, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+turnRunColumns+` FROM turn_runs tr WHERE tr.id = $1::uuid`, runID)
	run, err := scanTurnRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get turn run: %w", err)
	}
	return run, nil
}

func (r *TurnRunRepository) ClaimTurnRun(ctx context.Context, runID string) (*domain.TurnRun, workflow.TurnRunLease, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, workflow.TurnRunLease{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	token := uuid.NewString()
	row := tx.QueryRow(ctx, `
		UPDATE turn_runs tr
		SET
			status = $2,
			attempt = attempt + 1,
			lease_token = $3::uuid,
			heartbeat_at = now(),
			started_at = now(),
			error_message = NULL,
			completed_at = NULL,
			failed_at = NULL
		FROM turns t
		WHERE tr.id = $1::uuid
			AND tr.turn_id = t.id
			AND tr.status = $4
			AND t.status = $5
		RETURNING `+turnRunColumns, runID, domain.TurnRunStatusRunning, token, domain.TurnRunStatusQueued, domain.TurnStatusProcessing)
	run, err := scanTurnRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workflow.TurnRunLease{}, domain.ErrConflict
		}
		return nil, workflow.TurnRunLease{}, fmt.Errorf("claim turn run: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, workflow.TurnRunLease{}, fmt.Errorf("commit turn run claim: %w", err)
	}
	return run, workflow.TurnRunLease{TurnID: run.TurnID, RunID: run.ID, Token: token}, nil
}

func (r *TurnRunRepository) RenewTurnRunLease(ctx context.Context, lease workflow.TurnRunLease) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE turn_runs tr
		SET heartbeat_at = now()
		WHERE id = $1::uuid
			AND turn_id = $2::uuid
			AND status = $3
			AND lease_token = $4::uuid
	`, lease.RunID, lease.TurnID, domain.TurnRunStatusRunning, lease.Token)
	if err != nil {
		return fmt.Errorf("renew turn run lease: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (r *TurnRunRepository) CheckpointScheduledTurnRun(ctx context.Context, lease workflow.TurnRunLease, responseID string, responseBlobKey string, resultBlobKey string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE turn_runs
		SET response_id = $4, response_blob_key = $5, result_blob_key = $6
		WHERE id = $1::uuid
			AND turn_id = $2::uuid
			AND lease_token = $3::uuid
			AND status = $7
	`, lease.RunID, lease.TurnID, lease.Token, responseID, responseBlobKey, resultBlobKey, domain.TurnRunStatusRunning)
	if err != nil {
		return fmt.Errorf("checkpoint scheduled turn run: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (r *TurnRunRepository) ListTurnRunsByTurn(ctx context.Context, turnID string) ([]domain.TurnRun, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+turnRunColumns+` FROM turn_runs tr WHERE tr.turn_id = $1::uuid ORDER BY tr.step_index ASC, tr.created_at ASC`, turnID)
	if err != nil {
		return nil, fmt.Errorf("list turn runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.TurnRun
	for rows.Next() {
		run, scanErr := scanTurnRun(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan turn run: %w", scanErr)
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate turn runs: %w", err)
	}
	return runs, nil
}

func (r *TurnRunRepository) CompleteScheduledTurnRun(ctx context.Context, lease workflow.TurnRunLease, responseID string, responseBlobKey string, resultBlobKey string, usage llm.ModelUsage, imageGenerationCount int, compactTriggerTokens int) (*domain.TurnRun, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		UPDATE turn_runs tr
		SET
			status = $4,
			response_id = $5,
			response_blob_key = $6,
			result_blob_key = $7,
			input_tokens = $8,
			cache_read_input_tokens = $9,
			cache_creation_input_tokens = $10,
			output_tokens = $11,
			reasoning_output_tokens = $12,
			total_tokens = $13,
			lease_token = NULL,
			heartbeat_at = NULL,
			error_message = NULL,
			completed_at = now(),
			failed_at = NULL
		WHERE id = $1::uuid
			AND turn_id = $2::uuid
			AND lease_token = $3::uuid
			AND status = $14
		RETURNING `+turnRunColumns,
		lease.RunID, lease.TurnID, lease.Token, domain.TurnRunStatusCompleted,
		responseID, responseBlobKey, resultBlobKey,
		usage.InputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens, usage.OutputTokens,
		usage.ReasoningOutputTokens, usage.TotalTokens,
		domain.TurnRunStatusRunning)
	run, err := scanTurnRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("complete turn run: %w", err)
	}

	var modelID, modelPriceID, ownerUserID, conversationID string
	var modelRevision int64
	var modelSnapshot json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(t.model_id::text, ''), COALESCE(t.model_revision, 0),
			COALESCE(t.model_price_id::text, ''), t.model_snapshot,
			COALESCE(c.owner_user_id::text, ''), t.conversation_id::text
		FROM turns t JOIN conversations c ON c.id = t.conversation_id
		WHERE t.id = $1::uuid
	`, run.TurnID).Scan(&modelID, &modelRevision, &modelPriceID, &modelSnapshot, &ownerUserID, &conversationID); err != nil {
		return nil, fmt.Errorf("load turn billing snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE context_heads
		SET latest_successful_run_id = $2::uuid,
			latest_checkpoint_key = COALESCE(NULLIF($3, ''), latest_checkpoint_key),
			checkpoint_covered_event_seq = CASE WHEN $3 <> '' THEN last_context_event_seq ELSE checkpoint_covered_event_seq END,
			version = CASE WHEN $3 <> '' THEN version + 1 ELSE version END
		WHERE conversation_id = $1::uuid
	`, conversationID, run.ID, run.CheckpointBlobKey); err != nil {
		return nil, fmt.Errorf("advance successful run context head: %w", err)
	}
	var execution domain.ModelExecutionSnapshot
	_ = json.Unmarshal(modelSnapshot, &execution)
	modelCharge, err := billing.QuoteSnapshot(execution.PricingSnapshot, usage)
	if err != nil {
		return nil, fmt.Errorf("rate turn run: %w", err)
	}
	toolCharge, err := loadRunToolCharge(ctx, tx, run.ID, modelCharge.Currency, imageGenerationCount)
	if err != nil {
		return nil, err
	}
	charge, err := billing.AddToolCharge(modelCharge, toolCharge)
	if err != nil {
		return nil, fmt.Errorf("combine turn run charges: %w", err)
	}
	row = tx.QueryRow(ctx, `
		UPDATE turn_runs tr
		SET billing_currency = $2, billing_amount_nanos = $3
		WHERE id = $1::uuid
		RETURNING `+turnRunColumns,
		run.ID, charge.Currency, charge.AmountNanos)
	run, err = scanTurnRun(row)
	if err != nil {
		return nil, fmt.Errorf("update turn run billing: %w", err)
	}
	var billingTransactionID string
	billingTransactionID, err = captureUsageCharge(ctx, tx, ownerUserID, charge)
	if err != nil {
		if errors.Is(err, domain.ErrPaymentRequired) {
			run, err = r.failBillingSettlement(ctx, tx, run, usage, execution, ownerUserID, conversationID, modelID, modelRevision, modelPriceID, charge, toolCharge, compactTriggerTokens)
			if err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit failed turn run settlement: %w", err)
			}
			return run, nil
		}
		return nil, err
	}
	if err := r.insertBillingUsageEvent(ctx, tx, run, usage, execution, ownerUserID, conversationID, modelID, modelRevision, modelPriceID, charge, toolCharge, billingTransactionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit turn run completion: %w", err)
	}
	return run, nil
}

func (r *TurnRunRepository) failBillingSettlement(
	ctx context.Context,
	tx pgx.Tx,
	run *domain.TurnRun,
	usage llm.ModelUsage,
	execution domain.ModelExecutionSnapshot,
	ownerUserID string,
	conversationID string,
	modelID string,
	modelRevision int64,
	modelPriceID string,
	charge *billing.Charge,
	toolCharge *billing.ToolCharge,
	compactTriggerTokens int,
) (*domain.TurnRun, error) {
	row := tx.QueryRow(ctx, `
		UPDATE turn_runs tr
		SET status = $2, billing_currency = $3, billing_amount_nanos = $4,
			error_message = $5, completed_at = NULL, failed_at = now(),
			lease_token = NULL, heartbeat_at = NULL
		WHERE id = $1::uuid
		RETURNING `+turnRunColumns,
		run.ID, domain.TurnRunStatusFailed, charge.Currency, charge.AmountNanos,
		domain.TurnPublicErrorBillingRequired)
	failedRun, err := scanTurnRun(row)
	if err != nil {
		return nil, fmt.Errorf("fail turn run billing settlement: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE turns
		SET status = $2, error_code = $3, error_message = $4,
			completed_at = NULL, failed_at = now()
		WHERE id = $1::uuid
	`, run.TurnID, domain.TurnStatusFailed, domain.TurnErrorBillingSettlementFailed,
		domain.TurnPublicErrorBillingRequired); err != nil {
		return nil, fmt.Errorf("fail turn billing settlement: %w", err)
	}
	if err := enqueueRetryCompactionIfNeeded(ctx, tx, run.TurnID, compactTriggerTokens); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_usage_events (
			request_key, owner_user_id, conversation_id, turn_id, turn_run_id, workflow, attempt,
			provider, model_id, model_revision, model_price_id, upstream_model, provider_response_id,
			status, currency, amount_nanos, input_tokens, cache_read_input_tokens,
			cache_creation_input_tokens, output_tokens, reasoning_output_tokens, total_tokens,
			tool_amount_nanos, tool_usage, tool_pricing_snapshot, pricing_snapshot, usage, error_code
		) VALUES ($1, NULLIF($2, '')::uuid, $3::uuid, $4::uuid, $5::uuid, 'turn', $6,
			$7, NULLIF($8, '')::uuid, NULLIF($9, 0), NULLIF($10, '')::uuid, $11, $12,
			'failed', $13, $14, $15, $16, $17, $18, $19, $20, $21, $22::jsonb, $23::jsonb,
			$24::jsonb, $25::jsonb, $26)
		ON CONFLICT (turn_run_id) WHERE turn_run_id IS NOT NULL DO NOTHING
	`, "turn-run:"+run.ID, ownerUserID, conversationID, run.TurnID, run.ID, run.Attempt,
		run.Provider, modelID, modelRevision, modelPriceID, run.Model, run.ResponseID,
		charge.Currency, charge.AmountNanos, usage.InputTokens, usage.CacheReadInputTokens,
		usage.CacheCreationInputTokens, usage.OutputTokens, usage.ReasoningOutputTokens,
		usage.TotalTokens, toolCharge.AmountNanos, normalizedJSON(toolCharge.UsageJSON),
		normalizedJSON(toolCharge.PricingJSON), normalizedJSON(charge.PricingJSON), normalizedJSON(usage.Raw),
		domain.TurnErrorBillingSettlementFailed); err != nil {
		return nil, fmt.Errorf("insert failed settlement usage event: %w", err)
	}
	return failedRun, nil
}

func (r *TurnRunRepository) insertBillingUsageEvent(
	ctx context.Context,
	tx pgx.Tx,
	run *domain.TurnRun,
	usage llm.ModelUsage,
	execution domain.ModelExecutionSnapshot,
	ownerUserID string,
	conversationID string,
	modelID string,
	modelRevision int64,
	modelPriceID string,
	charge *billing.Charge,
	toolCharge *billing.ToolCharge,
	billingTransactionID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO billing_usage_events (
			request_key, owner_user_id, conversation_id, turn_id, turn_run_id, workflow, attempt,
			provider, model_id, model_revision, model_price_id, upstream_model, provider_response_id,
			status, currency, amount_nanos, input_tokens, cache_read_input_tokens, cache_creation_input_tokens, output_tokens,
			reasoning_output_tokens, total_tokens, tool_amount_nanos, tool_usage, tool_pricing_snapshot,
			pricing_snapshot, usage, billing_transaction_id
		) VALUES ($1, NULLIF($2, '')::uuid, $3::uuid, $4::uuid, $5::uuid, 'turn', $6,
			$7, NULLIF($8, '')::uuid, NULLIF($9, 0), NULLIF($10, '')::uuid, $11, $12,
			'completed', $13, $14, $15, $16, $17, $18, $19, $20, $21, $22::jsonb, $23::jsonb,
			$24::jsonb, $25::jsonb, NULLIF($26, '')::uuid)
		ON CONFLICT (turn_run_id) WHERE turn_run_id IS NOT NULL DO NOTHING
	`, "turn-run:"+run.ID, ownerUserID, conversationID, run.TurnID, run.ID, run.Attempt,
		run.Provider, modelID, modelRevision, modelPriceID, run.Model, run.ResponseID,
		charge.Currency, charge.AmountNanos, usage.InputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens, usage.OutputTokens,
		usage.ReasoningOutputTokens, usage.TotalTokens, toolCharge.AmountNanos, normalizedJSON(toolCharge.UsageJSON),
		normalizedJSON(toolCharge.PricingJSON), normalizedJSON(charge.PricingJSON), normalizedJSON(usage.Raw), billingTransactionID)
	if err != nil {
		return fmt.Errorf("insert billing usage event: %w", err)
	}
	return nil
}

func (r *TurnRunRepository) FailScheduledTurnRun(ctx context.Context, lease workflow.TurnRunLease, responseID string, responseBlobKey string, resultBlobKey string, runMessage string, requestBlobKey string, streamBlobKey string, turnCode string, turnMessage string, compactTriggerTokens int) (*domain.TurnRun, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin turn run failure: %w", err)
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		UPDATE turn_runs tr
		SET
			status = $4,
			response_id = $5,
			response_blob_key = $6,
			result_blob_key = $7,
			error_message = $8,
			lease_token = NULL,
			heartbeat_at = NULL,
			completed_at = NULL,
			failed_at = now()
		WHERE id = $1::uuid
			AND turn_id = $2::uuid
			AND lease_token = $3::uuid
			AND status = $9
		RETURNING `+turnRunColumns,
		lease.RunID, lease.TurnID, lease.Token, domain.TurnRunStatusFailed,
		responseID, responseBlobKey, resultBlobKey, runMessage, domain.TurnRunStatusRunning)
	run, err := scanTurnRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("fail turn run: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE turns
		SET status = $2,
			request_blob_key = CASE WHEN $3 = '' THEN request_blob_key ELSE $3 END,
			stream_blob_key = CASE WHEN $4 = '' THEN stream_blob_key ELSE $4 END,
			error_code = $5, error_message = $6, completed_at = NULL, failed_at = now()
		WHERE id = $1::uuid
	`, run.TurnID, domain.TurnStatusFailed, requestBlobKey, streamBlobKey, turnCode, turnMessage); err != nil {
		return nil, fmt.Errorf("fail parent turn: %w", err)
	}
	var modelID, modelPriceID, ownerUserID, conversationID string
	var modelRevision int64
	var modelSnapshot json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(t.model_id::text, ''), COALESCE(t.model_revision, 0),
			COALESCE(t.model_price_id::text, ''), t.model_snapshot,
			COALESCE(c.owner_user_id::text, ''), t.conversation_id::text
		FROM turns t JOIN conversations c ON c.id = t.conversation_id
		WHERE t.id = $1::uuid
	`, run.TurnID).Scan(&modelID, &modelRevision, &modelPriceID, &modelSnapshot, &ownerUserID, &conversationID); err != nil {
		return nil, fmt.Errorf("load failed turn billing snapshot: %w", err)
	}
	if err := enqueueRetryCompactionIfNeeded(ctx, tx, run.TurnID, compactTriggerTokens); err != nil {
		return nil, err
	}
	var execution domain.ModelExecutionSnapshot
	_ = json.Unmarshal(modelSnapshot, &execution)
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_usage_events (
			request_key, owner_user_id, conversation_id, turn_id, turn_run_id, workflow, attempt,
			provider, model_id, model_revision, model_price_id, upstream_model, provider_response_id,
			status, pricing_snapshot, usage, error_code
		) VALUES ($1, NULLIF($2, '')::uuid, $3::uuid, $4::uuid, $5::uuid, 'turn', $6,
			$7, NULLIF($8, '')::uuid, NULLIF($9, 0), NULLIF($10, '')::uuid, $11, $12,
			'failed', $13::jsonb, '{}'::jsonb, 'turn_run_failed')
		ON CONFLICT (turn_run_id) WHERE turn_run_id IS NOT NULL DO NOTHING
	`, "turn-run:"+run.ID, ownerUserID, conversationID, run.TurnID, run.ID, run.Attempt,
		run.Provider, modelID, modelRevision, modelPriceID, run.Model, responseID,
		normalizedJSON(execution.PricingSnapshot)); err != nil {
		return nil, fmt.Errorf("insert failed turn usage event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit turn run failure: %w", err)
	}
	return run, nil
}

func enqueueRetryCompactionIfNeeded(ctx context.Context, tx pgx.Tx, turnID string, compactTriggerTokens int) error {
	var conversationID, retryOfTurnID string
	if err := tx.QueryRow(ctx, `
		SELECT conversation_id::text, COALESCE(retry_of_turn_id::text, '')
		FROM turns
		WHERE id = $1::uuid
	`, turnID).Scan(&conversationID, &retryOfTurnID); err != nil {
		return fmt.Errorf("load failed retry turn: %w", err)
	}
	if retryOfTurnID == "" {
		return nil
	}
	head, err := queryContextHeadForUpdate(ctx, tx, conversationID)
	if err != nil {
		return err
	}
	return enqueueCompactionRequest(ctx, tx, &domain.Turn{ID: turnID, ConversationID: conversationID}, shouldRequestCompaction(head, compactTriggerTokens))
}
