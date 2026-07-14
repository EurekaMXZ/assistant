package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5"
)

func (r *WorkflowTurnRepository) GetTurn(ctx context.Context, turnID string) (*domain.Turn, error) {
	turn, err := scanTurn(r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			seq,
			COALESCE(retry_of_turn_id::text, ''),
			variant_index,
			status,
			COALESCE(request_blob_key, ''),
			COALESCE(response_blob_key, ''),
			COALESCE(stream_blob_key, ''),
			COALESCE(openai_response_id, ''),
			COALESCE(error_code, ''),
			COALESCE(error_message, ''),
			metadata,
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
		FROM turns
		WHERE id = $1::uuid
	`, turnID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workflow turn: %w", err)
	}
	return turn, nil
}

func (r *WorkflowTurnRepository) GetUserMessageByTurn(ctx context.Context, turnID string) (*domain.Message, error) {
	message, err := scanMessage(r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			COALESCE(turn_id::text, ''),
			seq,
			role,
			COALESCE(content_text, ''),
			token_count,
			metadata,
			context_excluded,
			created_at
		FROM messages
		WHERE turn_id = $1::uuid AND role = $2
		ORDER BY seq ASC
		LIMIT 1
	`, turnID, domain.RoleUser))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get workflow turn user message: %w", err)
	}
	return message, nil
}

func (r *WorkflowTurnRepository) MarkTurnContextReady(ctx context.Context, turnID string) (*domain.Turn, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		UPDATE turns
		SET status = $2
		WHERE id = $1::uuid
			AND status = $3
		RETURNING
			id::text,
			conversation_id::text,
			seq,
			COALESCE(retry_of_turn_id::text, ''),
			variant_index,
			status,
			COALESCE(request_blob_key, ''),
			COALESCE(response_blob_key, ''),
			COALESCE(stream_blob_key, ''),
			COALESCE(openai_response_id, ''),
			COALESCE(error_code, ''),
			COALESCE(error_message, ''),
			metadata,
			started_at,
			completed_at,
			failed_at,
			created_at,
			updated_at
	`, turnID, domain.TurnStatusContextReady, domain.TurnStatusAccepted)

	turn, err := scanTurn(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("mark turn context ready: %w", err)
	}

	if err := insertOutboxEvent(ctx, tx, outboxInsert{
		EventType:      workflow.EventTurnContextReady,
		ConversationID: turn.ConversationID,
		TurnID:         turn.ID,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit turn context ready: %w", err)
	}

	return turn, nil
}
