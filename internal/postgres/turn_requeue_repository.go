package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/workflow"
	"github.com/jackc/pgx/v5"
)

type staleTurnSnapshot struct {
	Status   string
	Metadata json.RawMessage
}

type staleTurnTransition struct {
	Status               string
	Metadata             json.RawMessage
	PublishAcceptedEvent bool
}

func (r *StaleTurnRepository) RequeueStaleTurns(ctx context.Context, leaseTimeout time.Duration) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id::text, conversation_id::text, status, metadata
		FROM turns
		WHERE status = $1
			AND updated_at < now() - ($2 * interval '1 second')
		ORDER BY updated_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 100
	`, domain.TurnStatusContextReady, int(leaseTimeout.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("select stale turns: %w", err)
	}
	defer rows.Close()

	type staleTurn struct {
		ID             string
		ConversationID string
		Status         string
		Metadata       json.RawMessage
	}
	var stale []staleTurn
	for rows.Next() {
		var item staleTurn
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Status, &item.Metadata); err != nil {
			return 0, fmt.Errorf("scan stale turn: %w", err)
		}
		stale = append(stale, item)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate stale turns: %w", err)
	}

	for _, item := range stale {
		transition, err := planStaleTurnTransition(staleTurnSnapshot{Status: item.Status, Metadata: item.Metadata})
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE turns
			SET status = $2, error_code = NULL, error_message = NULL, metadata = $3::jsonb
			WHERE id = $1::uuid
		`, item.ID, transition.Status, transition.Metadata); err != nil {
			return 0, fmt.Errorf("requeue turn: %w", err)
		}
		if transition.PublishAcceptedEvent {
			if err := insertOutboxEvent(ctx, tx, outboxInsert{
				EventType:      workflow.EventTurnAccepted,
				ConversationID: item.ConversationID, TurnID: item.ID,
			}); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit requeue: %w", err)
	}
	return len(stale), nil
}

func (r *StaleTurnRepository) RequeueStaleTurnRuns(ctx context.Context, leaseTimeout time.Duration) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT tr.id::text, tr.turn_id::text, t.conversation_id::text, tr.step_index
		FROM turn_runs tr
		JOIN turns t ON t.id = tr.turn_id
		WHERE tr.status = $1
			AND tr.lease_token IS NOT NULL
			AND tr.heartbeat_at < now() - ($2 * interval '1 second')
			AND t.status = $3
		ORDER BY tr.heartbeat_at ASC
		FOR UPDATE OF tr SKIP LOCKED
		LIMIT 100
	`, domain.TurnRunStatusRunning, int(leaseTimeout.Seconds()), domain.TurnStatusProcessing)
	if err != nil {
		return 0, fmt.Errorf("select stale turn runs: %w", err)
	}
	defer rows.Close()

	type staleRun struct {
		ID             string
		TurnID         string
		ConversationID string
		StepIndex      int
	}
	var stale []staleRun
	for rows.Next() {
		var item staleRun
		if err := rows.Scan(&item.ID, &item.TurnID, &item.ConversationID, &item.StepIndex); err != nil {
			return 0, fmt.Errorf("scan stale turn run: %w", err)
		}
		stale = append(stale, item)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate stale turn runs: %w", err)
	}

	for _, item := range stale {
		if _, err := tx.Exec(ctx, `
			UPDATE turn_runs
			SET status = $2, lease_token = NULL, heartbeat_at = NULL,
				error_message = NULL, completed_at = NULL, failed_at = NULL
			WHERE id = $1::uuid
		`, item.ID, domain.TurnRunStatusQueued); err != nil {
			return 0, fmt.Errorf("requeue stale turn run: %w", err)
		}
		if err := insertTurnRunRequestedEvent(ctx, tx, item.ConversationID, item.TurnID, item.ID, item.StepIndex); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit stale turn runs: %w", err)
	}
	return len(stale), nil
}

func planStaleTurnTransition(turn staleTurnSnapshot) (staleTurnTransition, error) {
	if turn.Status != domain.TurnStatusContextReady {
		return staleTurnTransition{}, domain.ErrConflict
	}
	return staleTurnTransition{
		Status: domain.TurnStatusAccepted, Metadata: normalizedJSON(turn.Metadata), PublishAcceptedEvent: true,
	}, nil
}
