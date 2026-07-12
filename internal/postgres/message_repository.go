package postgres

import (
	"context"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MessageRepository struct {
	pool *pgxpool.Pool
}

func NewMessageRepository(pool *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{pool: pool}
}

func (r *MessageRepository) ListMessages(ctx context.Context, conversationID string, limit int) ([]domain.Message, error) {
	query := `
		SELECT * FROM (
		SELECT
			id::text,
			conversation_id::text,
			COALESCE(turn_id::text, ''),
			seq,
			role,
			COALESCE(content_text, ''),
			token_count,
			metadata,
			created_at
		FROM messages
		WHERE conversation_id = $1::uuid
		ORDER BY seq DESC
		LIMIT $2
		) recent_messages
		ORDER BY seq ASC
	`

	rows, err := r.pool.Query(ctx, query, conversationID, clampLimit(limit, 100, 1000))
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	messages := make([]domain.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, *message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	return messages, nil
}

func (r *MessageRepository) ListAssistantMessagesByTurn(ctx context.Context, turnID string) ([]domain.Message, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			conversation_id::text,
			COALESCE(turn_id::text, ''),
			seq,
			role,
			COALESCE(content_text, ''),
			token_count,
			metadata,
			created_at
		FROM messages
		WHERE turn_id = $1::uuid
			AND role = $2
		ORDER BY seq ASC
	`, turnID, domain.RoleAssistant)
	if err != nil {
		return nil, fmt.Errorf("list assistant messages: %w", err)
	}
	defer rows.Close()

	messages := make([]domain.Message, 0)
	for rows.Next() {
		message, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		messages = append(messages, *message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate assistant messages: %w", err)
	}
	return messages, nil
}
