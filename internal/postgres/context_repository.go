package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *WorkflowContextRepository) GetContextHead(ctx context.Context, conversationID string) (*domain.ContextHead, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			conversation_id::text,
			anchor_generation,
			COALESCE(anchor_key, ''),
			covered_until_seq,
			raw_tail_start_seq,
			last_seq,
			active_context_tokens,
			updated_at
		FROM context_heads
		WHERE conversation_id = $1::uuid
	`, conversationID)

	head, err := scanContextHead(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get context head: %w", err)
	}

	return head, nil
}

func (r *WorkflowContextRepository) HasActiveRetry(ctx context.Context, conversationID string) (bool, error) {
	var active bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM turns
			WHERE conversation_id = $1::uuid
				AND retry_of_turn_id IS NOT NULL
				AND status IN ($2, $3, $4)
		)
	`, conversationID, domain.TurnStatusAccepted, domain.TurnStatusContextReady, domain.TurnStatusProcessing).Scan(&active); err != nil {
		return false, fmt.Errorf("check active retry: %w", err)
	}
	return active, nil
}

func (r *WorkflowContextRepository) ListRawTailMessages(ctx context.Context, conversationID string, fromSeq int64, toSeq int64) ([]domain.Message, error) {
	if toSeq > 0 && fromSeq > toSeq {
		return nil, nil
	}

	query := `
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
		WHERE conversation_id = $1::uuid
			AND seq >= $2
	`

	args := []any{conversationID, fromSeq}
	if toSeq > 0 {
		query += " AND seq <= $3"
		args = append(args, toSeq)
	}
	query += " ORDER BY seq ASC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list raw tail: %w", err)
	}
	defer rows.Close()

	var messages []domain.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, *message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate raw tail: %w", err)
	}

	return messages, nil
}

func (r *WorkflowContextRepository) CompleteCompaction(ctx context.Context, conversationID string, anchor domain.AnchorObject, expectedLastSeq int64, activeContextTokens int) (*domain.ContextHead, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	head, err := queryContextHeadForUpdate(ctx, tx, conversationID)
	if err != nil {
		return nil, err
	}
	var activeRetry bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM turns
			WHERE conversation_id = $1::uuid
				AND retry_of_turn_id IS NOT NULL
				AND status IN ($2, $3, $4)
		)
	`, conversationID, domain.TurnStatusAccepted, domain.TurnStatusContextReady, domain.TurnStatusProcessing).Scan(&activeRetry); err != nil {
		return nil, fmt.Errorf("check active retry before compaction: %w", err)
	}
	if activeRetry {
		return nil, domain.ErrConflict
	}

	if head.LastSeq != expectedLastSeq || anchor.CoveredUntilSeq > expectedLastSeq {
		return nil, domain.ErrConflict
	}

	row := tx.QueryRow(ctx, `
		UPDATE context_heads
		SET
			anchor_generation = $2,
			anchor_key = $3,
			covered_until_seq = $4,
			raw_tail_start_seq = $5,
			active_context_tokens = $6
		WHERE conversation_id = $1::uuid
		RETURNING
			conversation_id::text,
			anchor_generation,
			COALESCE(anchor_key, ''),
			covered_until_seq,
			raw_tail_start_seq,
			last_seq,
			active_context_tokens,
			updated_at
	`, conversationID, anchor.Generation, anchor.ObjectKey, anchor.CoveredUntilSeq, anchor.CoveredUntilSeq+1, max(0, activeContextTokens))

	head, err = scanContextHead(row)
	if err != nil {
		return nil, fmt.Errorf("update context head after compaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit compaction: %w", err)
	}

	return head, nil
}
