package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/google/uuid"
)

func (r *WorkflowOutboxRepository) ClaimPendingOutboxEvents(ctx context.Context, leaseTimeout time.Duration, limit int) ([]workflow.OutboxEvent, error) {
	if leaseTimeout <= 0 {
		leaseTimeout = time.Minute
	}
	claimToken := uuid.NewString()
	rows, err := r.pool.Query(ctx, `
		WITH pending AS (
			SELECT id
			FROM outbox_events
			WHERE published_at IS NULL
				AND (claim_token IS NULL OR claimed_at < now() - $1::interval)
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE outbox_events AS event
		SET claim_token = $3::uuid, claimed_at = now()
		FROM pending
		WHERE event.id = pending.id
		RETURNING
			event.id::text,
			event.event_type,
			COALESCE(event.conversation_id::text, ''),
			COALESCE(event.turn_id::text, ''),
			COALESCE(event.turn_run_id::text, ''),
			event.published_at,
			event.claim_token::text,
			event.claimed_at,
			COALESCE(event.error_message, ''),
			event.created_at
	`, leaseTimeout.String(), clampLimit(limit, 100, 1000), claimToken)
	if err != nil {
		return nil, fmt.Errorf("claim pending outbox events: %w", err)
	}
	defer rows.Close()

	var items []workflow.OutboxEvent
	for rows.Next() {
		item, err := scanOutboxEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending outbox events: %w", err)
	}

	return items, nil
}

func (r *WorkflowOutboxRepository) MarkOutboxPublished(ctx context.Context, eventID string, claimToken string) error {
	commandTag, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET
			published_at = now(),
			claim_token = NULL,
			claimed_at = NULL,
			error_message = NULL
		WHERE id = $1::uuid
			AND claim_token = $2::uuid
			AND published_at IS NULL
	`, eventID, claimToken)
	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func (r *WorkflowOutboxRepository) MarkOutboxPublishError(ctx context.Context, eventID string, claimToken string, message string) error {
	commandTag, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET error_message = $3, claim_token = NULL, claimed_at = NULL
		WHERE id = $1::uuid
			AND claim_token = $2::uuid
			AND published_at IS NULL
	`, eventID, claimToken, message)
	if err != nil {
		return fmt.Errorf("mark outbox publish error: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
