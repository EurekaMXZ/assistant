package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5"
)

func (r *TurnRepository) RequestTurnCancellation(ctx context.Context, turnID string) (*domain.Turn, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin turn cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	turn, err := scanTurn(tx.QueryRow(ctx, `
		UPDATE turns
		SET status = $2, cancel_requested_at = now()
		WHERE id = $1::uuid AND status IN ($3, $4, $5)
		RETURNING id::text, conversation_id::text, seq, COALESCE(retry_of_turn_id::text, ''), variant_index,
			status, COALESCE(request_blob_key, ''), COALESCE(response_blob_key, ''), COALESCE(stream_blob_key, ''),
			COALESCE(openai_response_id, ''), COALESCE(error_code, ''), COALESCE(error_message, ''), metadata,
			started_at, completed_at, failed_at, created_at, updated_at
	`, turnID, domain.TurnStatusCancelRequested, domain.TurnStatusAccepted, domain.TurnStatusContextReady, domain.TurnStatusProcessing))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("request turn cancellation: %w", err)
	}
	var requestedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT cancel_requested_at FROM turns WHERE id = $1::uuid`, turnID).Scan(&requestedAt); err != nil {
		return nil, fmt.Errorf("load turn cancellation timestamp: %w", err)
	}
	turn.CancelRequestedAt = &requestedAt
	if _, err := tx.Exec(ctx, `
		UPDATE turn_runs
		SET status = $2
		WHERE turn_id = $1::uuid AND status IN ($3, $4)
	`, turnID, domain.TurnRunStatusCancelRequested, domain.TurnRunStatusQueued, domain.TurnRunStatusRunning); err != nil {
		return nil, fmt.Errorf("request active run cancellation: %w", err)
	}
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
	if err := tx.QueryRow(ctx, `
		SELECT id::text, COALESCE(checkpoint_blob_key, '')
		FROM turn_runs
		WHERE turn_id = $1::uuid AND status = $2
		ORDER BY step_index DESC, attempt DESC LIMIT 1
	`, turnID, domain.TurnRunStatusCompleted).Scan(&successfulRunID, &checkpointKey); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load latest successful run during cancellation: %w", err)
	}
	if successfulRunID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE context_heads
			SET latest_successful_run_id = $2::uuid,
				latest_checkpoint_key = COALESCE(NULLIF($3, ''), latest_checkpoint_key)
			WHERE conversation_id = $1::uuid
		`, conversationID, successfulRunID, checkpointKey); err != nil {
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
