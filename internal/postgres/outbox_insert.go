package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type outboxInsert struct {
	EventType      string
	ConversationID string
	TurnID         string
	TurnRunID      string
	ToolCallID     string
	ExecutionGroup int
}

func insertOutboxEvent(ctx context.Context, tx pgx.Tx, record outboxInsert) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (
			event_type,
			conversation_id,
			turn_id,
			turn_run_id,
			tool_call_id,
			execution_group
		)
		VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6)
		ON CONFLICT DO NOTHING
	`, record.EventType, nullableID(record.ConversationID), nullableID(record.TurnID), nullableID(record.TurnRunID), nullableID(record.ToolCallID), record.ExecutionGroup); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return nil
}
