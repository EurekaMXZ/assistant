package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *WorkflowTurnRepository) FinalizeTurnFailure(ctx context.Context, turnID string, requestKey string, code string, message string, compactTriggerTokens int) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	turn, err := lockTurnForFailure(ctx, tx, turnID)
	if err != nil {
		return err
	}

	metadata := normalizedJSON(turn.Metadata)

	if _, err := tx.Exec(ctx, `
		UPDATE turns
		SET
			status = $2,
			request_blob_key = CASE WHEN $3 = '' THEN request_blob_key ELSE $3 END,
			error_code = $4,
			error_message = $5,
			metadata = $6::jsonb,
			failed_at = now(),
			completed_at = NULL
		WHERE id = $1::uuid
	`, turnID, domain.TurnStatusFailed, requestKey, code, message, metadata); err != nil {
		return fmt.Errorf("update turn failure: %w", err)
	}
	head, err := queryContextHeadForUpdate(ctx, tx, turn.ConversationID)
	if err != nil {
		return err
	}
	failurePayload, err := json.Marshal(map[string]any{
		"turn_id":    turn.ID,
		"status":     domain.TurnStatusFailed,
		"error_code": code,
		"error":      message,
	})
	if err != nil {
		return fmt.Errorf("marshal failure complete event: %w", err)
	}
	if err := insertCompleteEvent(ctx, tx, head, domain.ConversationEventInput{
		ConversationID:  turn.ConversationID,
		TurnID:          turn.ID,
		EventKey:        "turn:" + turn.ID + ":failed",
		SchemaVersion:   1,
		EventType:       "turn.failed",
		Payload:         failurePayload,
		ContextIncluded: false,
	}); err != nil {
		return err
	}
	if turn.RetryOfTurnID != "" {
		if err := enqueueCompactionRequest(ctx, tx, turn, true); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit turn failure: %w", err)
	}

	return nil
}
