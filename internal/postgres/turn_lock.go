package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

func lockTurnForCompletion(ctx context.Context, tx pgx.Tx, turnID string) (*domain.Turn, error) {
	return lockTurnInStatuses(ctx, tx, turnID, []string{domain.TurnStatusProcessing})
}

func lockTurnForFailure(ctx context.Context, tx pgx.Tx, turnID string) (*domain.Turn, error) {
	return lockTurnInStatuses(ctx, tx, turnID, []string{domain.TurnStatusContextReady, domain.TurnStatusProcessing})
}

func lockTurnInStatuses(ctx context.Context, tx pgx.Tx, turnID string, statuses []string) (*domain.Turn, error) {
	row := tx.QueryRow(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			seq,
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
			AND status = ANY($2::text[])
		FOR UPDATE
	`, turnID, statuses)

	turn, err := scanTurn(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("lock turn: %w", err)
	}

	return turn, nil
}
