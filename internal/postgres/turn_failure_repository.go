package postgres

import (
	"context"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *WorkflowTurnRepository) FinalizeTurnFailure(ctx context.Context, turnID string, requestKey string, streamKey string, code string, message string, compactTriggerTokens int) error {
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
			stream_blob_key = CASE WHEN $4 = '' THEN stream_blob_key ELSE $4 END,
			error_code = $5,
			error_message = $6,
			metadata = $7::jsonb,
			failed_at = now(),
			completed_at = NULL
		WHERE id = $1::uuid
	`, turnID, domain.TurnStatusFailed, requestKey, streamKey, code, message, metadata); err != nil {
		return fmt.Errorf("update turn failure: %w", err)
	}
	if turn.RetryOfTurnID != "" {
		head, err := queryContextHeadForUpdate(ctx, tx, turn.ConversationID)
		if err != nil {
			return err
		}
		if err := enqueueCompactionRequest(ctx, tx, turn, shouldRequestCompaction(head, compactTriggerTokens)); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit turn failure: %w", err)
	}

	return nil
}
