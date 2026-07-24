package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5"
)

const cancellationTurnColumns = `
	id::text, conversation_id::text, seq, COALESCE(retry_of_turn_id::text, ''), variant_index,
	status, COALESCE(request_blob_key, ''), COALESCE(response_blob_key, ''),
	COALESCE(openai_response_id, ''), COALESCE(error_code, ''), COALESCE(error_message, ''), metadata,
	started_at, completed_at, failed_at, created_at, updated_at`

func lockCancellationRuns(ctx context.Context, tx pgx.Tx, turnID string, statuses ...string) error {
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM turn_runs
		WHERE turn_id = $1::uuid AND status = ANY($2::text[])
		ORDER BY id
		FOR UPDATE
	`, turnID, statuses)
	if err != nil {
		return fmt.Errorf("lock turn runs for cancellation: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan locked cancellation run: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate locked cancellation runs: %w", err)
	}
	return nil
}

func lockAwaitingToolCallsForCancellation(ctx context.Context, tx pgx.Tx, turnID string) ([]domain.ToolCallRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT `+toolCallColumns+`
		FROM tool_calls tc
		WHERE tc.turn_id = $1::uuid AND tc.status = $2
		ORDER BY tc.id
		FOR UPDATE OF tc
	`, turnID, domain.ToolCallStatusAwaitingInput)
	if err != nil {
		return nil, fmt.Errorf("lock awaiting tool calls for cancellation: %w", err)
	}
	defer rows.Close()
	records := make([]domain.ToolCallRecord, 0)
	for rows.Next() {
		record, scanErr := scanToolCall(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan awaiting cancellation tool: %w", scanErr)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate awaiting cancellation tools: %w", err)
	}
	return records, nil
}

func cancelledInteractionPayload(ctx context.Context, tx pgx.Tx, conversationID string, turnID string, call domain.ToolCallRecord) (json.RawMessage, error) {
	var payload json.RawMessage
	err := tx.QueryRow(ctx, `
		SELECT payload
		FROM conversation_events
		WHERE conversation_id = $1::uuid AND turn_id = $2::uuid
		  AND event_type = $3 AND payload->>'tool_call_id' = $4
		ORDER BY event_seq DESC
		LIMIT 1
	`, conversationID, turnID, domain.ConversationEventInteractionAwaiting, call.ID).Scan(&payload)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load awaiting interaction for cancellation: %w", err)
	}
	var interaction tool.AskUserInteraction
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &interaction)
	}
	interaction.ID = "ask-user:" + call.ID
	interaction.ToolCallID = call.ID
	interaction.Status = domain.ToolCallStatusCancelled
	interaction.Answer = &tool.AskUserAnswer{
		Status: "cancelled", OptionID: "cancelled", Label: "已取消", UserReported: false,
	}
	encoded, err := json.Marshal(interaction)
	if err != nil {
		return nil, fmt.Errorf("marshal cancelled interaction: %w", err)
	}
	return encoded, nil
}

func cancelAwaitingToolCalls(ctx context.Context, tx pgx.Tx, conversationID string, turnID string, calls []domain.ToolCallRecord) error {
	if len(calls) == 0 {
		return nil
	}
	head, err := queryContextHeadForUpdate(ctx, tx, conversationID)
	if err != nil {
		return err
	}
	for _, call := range calls {
		payload, err := cancelledInteractionPayload(ctx, tx, conversationID, turnID, call)
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `
			UPDATE tool_calls
			SET status = $2, answer_output_pending = false, error_message = NULL,
				completed_at = NULL, failed_at = NULL, cancelled_at = now()
			WHERE id = $1::uuid AND status = $3
		`, call.ID, domain.ToolCallStatusCancelled, domain.ToolCallStatusAwaitingInput)
		if err != nil {
			return fmt.Errorf("cancel awaiting tool call: %w", err)
		}
		if result.RowsAffected() != 1 {
			return domain.ErrConflict
		}
		if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
			ConversationID:  conversationID,
			TurnID:          turnID,
			TurnRunID:       call.TurnRunID,
			EventKey:        "run:" + call.TurnRunID + ":" + domain.ConversationEventInteractionCancelled + ":" + call.ID + ":cancelled",
			SchemaVersion:   1,
			EventType:       domain.ConversationEventInteractionCancelled,
			Payload:         payload,
			ContextIncluded: false,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *TurnRepository) RequestTurnCancellation(ctx context.Context, turnID string) (*domain.Turn, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin turn cancellation: %w", err)
	}
	defer tx.Rollback(ctx)

	turn, err := scanTurn(tx.QueryRow(ctx, `
		SELECT `+cancellationTurnColumns+`
		FROM turns
		WHERE id = $1::uuid
		FOR UPDATE
	`, turnID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock turn for cancellation: %w", err)
	}
	switch turn.Status {
	case domain.TurnStatusAccepted, domain.TurnStatusContextReady, domain.TurnStatusProcessing, domain.TurnStatusAwaitingInput:
	default:
		return nil, domain.ErrConflict
	}
	var durableCompletion bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM turn_runs tr
			JOIN conversation_events ce ON ce.turn_run_id = tr.id AND ce.event_type = $2
			WHERE tr.turn_id = $1::uuid AND tr.status = $3
		)
	`, turnID, domain.ConversationEventRunCompleted, domain.TurnRunStatusRunning).Scan(&durableCompletion); err != nil {
		return nil, fmt.Errorf("check durable completion before cancellation: %w", err)
	}
	if durableCompletion {
		return nil, domain.ErrConflict
	}
	if err := lockCancellationRuns(ctx, tx, turnID, domain.TurnRunStatusQueued, domain.TurnRunStatusRunning, domain.TurnRunStatusAwaitingInput); err != nil {
		return nil, err
	}
	calls, err := lockAwaitingToolCallsForCancellation(ctx, tx, turnID)
	if err != nil {
		return nil, err
	}
	for _, call := range calls {
		if call.AnswerOutputPending {
			return nil, domain.ErrConflict
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE turn_runs
		SET status = $2
		WHERE turn_id = $1::uuid AND status IN ($3, $4, $5)
	`, turnID, domain.TurnRunStatusCancelRequested, domain.TurnRunStatusQueued, domain.TurnRunStatusRunning, domain.TurnRunStatusAwaitingInput); err != nil {
		return nil, fmt.Errorf("request active run cancellation: %w", err)
	}
	if err := cancelAwaitingToolCalls(ctx, tx, turn.ConversationID, turnID, calls); err != nil {
		return nil, err
	}
	turn, err = scanTurn(tx.QueryRow(ctx, `
		UPDATE turns
		SET status = $2, cancel_requested_at = now()
		WHERE id = $1::uuid
		RETURNING `+cancellationTurnColumns,
		turnID, domain.TurnStatusCancelRequested))
	if err != nil {
		return nil, fmt.Errorf("request turn cancellation: %w", err)
	}
	var requestedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT cancel_requested_at FROM turns WHERE id = $1::uuid`, turnID).Scan(&requestedAt); err != nil {
		return nil, fmt.Errorf("load turn cancellation timestamp: %w", err)
	}
	turn.CancelRequestedAt = &requestedAt
	if err := insertOutboxEvent(ctx, tx, outboxInsert{EventType: workflow.EventTurnCancellationRequested, ConversationID: turn.ConversationID, TurnID: turn.ID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit turn cancellation request: %w", err)
	}
	return turn, nil
}

func (r *TurnRunRepository) FinalizeTurnCancellation(ctx context.Context, conversationID string, turnID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin finalize turn cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM turns WHERE id = $1::uuid AND conversation_id = $2::uuid FOR UPDATE`, turnID, conversationID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}
	if status == domain.TurnStatusCancelled {
		return tx.Commit(ctx)
	}
	if status != domain.TurnStatusCancelRequested {
		return domain.ErrConflict
	}
	if err := lockCancellationRuns(ctx, tx, turnID, domain.TurnRunStatusCancelRequested); err != nil {
		return err
	}
	calls, err := lockAwaitingToolCallsForCancellation(ctx, tx, turnID)
	if err != nil {
		return err
	}
	var cancelledRunID string
	if err := tx.QueryRow(ctx, `
		UPDATE turn_runs
		SET status = $2, cancelled_at = now(), lease_token = NULL, heartbeat_at = NULL
		WHERE id = (
			SELECT id FROM turn_runs WHERE turn_id = $1::uuid AND status = $3 ORDER BY step_index DESC, attempt DESC LIMIT 1
		)
		RETURNING id::text
	`, turnID, domain.TurnRunStatusCancelled, domain.TurnRunStatusCancelRequested).Scan(&cancelledRunID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("cancel active turn run: %w", err)
	}
	if err := cancelAwaitingToolCalls(ctx, tx, conversationID, turnID, calls); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE turns
		SET status = $2, cancelled_at = now(), completed_at = NULL, failed_at = NULL
		WHERE id = $1::uuid AND status = $3
	`, turnID, domain.TurnStatusCancelled, domain.TurnStatusCancelRequested); err != nil {
		return fmt.Errorf("finalize turn cancellation: %w", err)
	}
	head, err := queryContextHeadForUpdate(ctx, tx, conversationID)
	if err != nil {
		return err
	}
	var successfulRunID, checkpointKey string
	var artifactMetadata json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT id::text, COALESCE(checkpoint_blob_key, ''), artifact_metadata
		FROM turn_runs
		WHERE turn_id = $1::uuid AND status = $2
		ORDER BY step_index DESC, attempt DESC LIMIT 1
	`, turnID, domain.TurnRunStatusCompleted).Scan(&successfulRunID, &checkpointKey, &artifactMetadata); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load latest successful run during cancellation: %w", err)
	}
	if successfulRunID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE context_heads
			SET latest_successful_run_id = $2::uuid,
				latest_checkpoint_key = COALESCE(NULLIF($3, ''), latest_checkpoint_key),
				latest_checkpoint_checksum = CASE WHEN $3 <> '' THEN NULLIF($4, '') ELSE latest_checkpoint_checksum END
			WHERE conversation_id = $1::uuid
		`, conversationID, successfulRunID, checkpointKey, turnRunArtifactChecksum(artifactMetadata, "context-checkpoint.json.zst")); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]any{"turn_id": turnID, "run_id": cancelledRunID, "status": domain.TurnStatusCancelled})
	if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
		ConversationID: conversationID, TurnID: turnID, TurnRunID: cancelledRunID,
		EventKey: "turn:" + turnID + ":cancelled", SchemaVersion: 1,
		EventType: domain.ConversationEventTurnCancelled, Payload: payload, ContextIncluded: false,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit turn cancellation: %w", err)
	}
	return nil
}
