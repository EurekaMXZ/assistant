package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

func queryContextHeadForUpdate(ctx context.Context, tx pgx.Tx, conversationID string) (*domain.ContextHead, error) {
	row := tx.QueryRow(ctx, `
		SELECT `+contextHeadColumns+`
		FROM context_heads
		WHERE conversation_id = $1::uuid
		FOR UPDATE
	`, conversationID)

	head, err := scanContextHead(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("query context head: %w", err)
	}
	return head, nil
}

const contextHeadColumns = `
	conversation_id::text,
	version,
	anchor_generation,
	COALESCE(anchor_key, ''),
	covered_until_seq,
	raw_tail_start_seq,
	last_seq,
	active_context_tokens,
	COALESCE(latest_request_run_id::text, ''),
	COALESCE(latest_successful_run_id::text, ''),
	COALESCE(latest_checkpoint_key, ''),
	checkpoint_covered_event_seq,
	last_context_event_seq,
	context_schema_version,
	updated_at`
