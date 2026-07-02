package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func (r *TurnStreamEventRepository) AppendTurnStreamEvent(ctx context.Context, conversationID string, turnID string, eventType string, payload json.RawMessage) error {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO turn_stream_events (
			turn_id,
			conversation_id,
			event_type,
			payload
		)
		VALUES ($1::uuid, $2::uuid, $3, $4::jsonb)
		RETURNING
			id::text,
			turn_id::text,
			conversation_id::text,
			event_index,
			event_type,
			payload,
			created_at
	`, turnID, conversationID, eventType, normalizedJSON(payload))

	if _, err := scanTurnStreamEvent(row); err != nil {
		return fmt.Errorf("insert turn stream event: %w", err)
	}
	return nil
}

func (r *TurnStreamEventRepository) ListTurnStreamEventsByTurn(ctx context.Context, turnID string) ([]domain.TurnStreamEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			turn_id::text,
			conversation_id::text,
			event_index,
			event_type,
			payload,
			created_at
		FROM turn_stream_events
		WHERE turn_id = $1::uuid
		ORDER BY event_index ASC
	`, turnID)
	if err != nil {
		return nil, fmt.Errorf("list turn stream events: %w", err)
	}
	defer rows.Close()

	var events []domain.TurnStreamEvent
	for rows.Next() {
		event, scanErr := scanTurnStreamEvent(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan turn stream event: %w", scanErr)
		}
		events = append(events, *event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate turn stream events: %w", err)
	}

	return events, nil
}
