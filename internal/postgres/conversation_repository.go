package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConversationRepository struct {
	pool *pgxpool.Pool
}

func NewConversationRepository(pool *pgxpool.Pool) *ConversationRepository {
	return &ConversationRepository{pool: pool}
}

func (r *ConversationRepository) CreateConversation(ctx context.Context, ownerUserID string, title string, metadata json.RawMessage) (*domain.Conversation, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if strings.TrimSpace(ownerUserID) == "" {
		return nil, domain.NewValidationError("owner user id is required")
	}

	trimmedTitle := strings.TrimSpace(title)
	var titleValue any
	if trimmedTitle != "" {
		titleValue = trimmedTitle
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO conversations (owner_user_id, title, metadata)
		VALUES ($1::uuid, $2, $3::jsonb)
		RETURNING id::text, owner_user_id::text, title, status, metadata, created_at, updated_at, archived_at, deleted_at
	`, ownerUserID, titleValue, normalizedJSON(metadata))

	conversation, err := scanConversation(row)
	if err != nil {
		return nil, fmt.Errorf("insert conversation: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO context_heads (conversation_id)
		VALUES ($1::uuid)
	`, conversation.ID); err != nil {
		return nil, fmt.Errorf("insert context head: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit conversation: %w", err)
	}

	return conversation, nil
}

func (r *ConversationRepository) GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(owner_user_id::text, ''), title, status, metadata, created_at, updated_at, archived_at, deleted_at
		FROM conversations
		WHERE id = $1::uuid AND deleted_at IS NULL
	`, conversationID)

	conversation, err := scanConversation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	return conversation, nil
}

func (r *ConversationRepository) ListConversationsByOwner(ctx context.Context, ownerUserID string, limit int) ([]domain.Conversation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, owner_user_id::text, title, status, metadata, created_at, updated_at, archived_at, deleted_at
		FROM conversations
		WHERE owner_user_id = $1::uuid
		  AND deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM conversation_initial_turns initial_turn
			WHERE initial_turn.conversation_id = conversations.id
			  AND initial_turn.turn_id IS NULL
		  )
		ORDER BY created_at DESC
		LIMIT $2
	`, ownerUserID, clampLimit(limit, 50, 200))
	if err != nil {
		return nil, fmt.Errorf("list conversations by owner: %w", err)
	}
	defer rows.Close()

	conversations := make([]domain.Conversation, 0)
	for rows.Next() {
		conversation, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, *conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations by owner: %w", err)
	}

	return conversations, nil
}

func (r *ConversationRepository) GetConversationByOwner(ctx context.Context, conversationID string, ownerUserID string) (*domain.Conversation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, owner_user_id::text, title, status, metadata, created_at, updated_at, archived_at, deleted_at
		FROM conversations
		WHERE id = $1::uuid
		  AND owner_user_id = $2::uuid
		  AND deleted_at IS NULL
	`, conversationID, ownerUserID)

	conversation, err := scanConversation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation by owner: %w", err)
	}

	return conversation, nil
}

type UpdateConversationParams struct {
	ConversationID string
	Title          *string
	Archived       *bool
}

func (r *ConversationRepository) UpdateConversation(ctx context.Context, params UpdateConversationParams) (*domain.Conversation, error) {
	setClauses := make([]string, 0, 2)
	args := []any{params.ConversationID}

	if params.Title != nil {
		title := strings.TrimSpace(*params.Title)
		if title == "" {
			setClauses = append(setClauses, "title = NULL")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("title = $%d", len(args)+1))
			args = append(args, title)
		}
	}
	if params.Archived != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", len(args)+1))
		if *params.Archived {
			args = append(args, "archived")
			setClauses = append(setClauses, "archived_at = now()")
		} else {
			args = append(args, "active")
			setClauses = append(setClauses, "archived_at = NULL")
		}
	}
	if len(setClauses) == 0 {
		return r.GetConversation(ctx, params.ConversationID)
	}

	query := `
		UPDATE conversations
		SET ` + strings.Join(setClauses, ", ") + `
		WHERE id = $1::uuid
		RETURNING id::text, COALESCE(owner_user_id::text, ''), title, status, metadata, created_at, updated_at, archived_at, deleted_at
	`
	row := r.pool.QueryRow(ctx, query, args...)
	conversation, err := scanConversation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update conversation: %w", err)
	}

	return conversation, nil
}

func (r *ConversationRepository) UpdateConversationTitle(ctx context.Context, conversationID string, title string) (*domain.Conversation, error) {
	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		return nil, errors.New("title is required")
	}

	row := r.pool.QueryRow(ctx, `
		UPDATE conversations
		SET title = $2
		WHERE id = $1::uuid
		RETURNING id::text, COALESCE(owner_user_id::text, ''), title, status, metadata, created_at, updated_at, archived_at, deleted_at
	`, conversationID, trimmedTitle)

	conversation, err := scanConversation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update conversation title: %w", err)
	}

	return conversation, nil
}

func (r *ConversationRepository) DeleteConversation(ctx context.Context, conversationID string, ownerUserID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin delete conversation: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE conversations
		SET status = 'deleted', deleted_at = now()
		WHERE id = $1::uuid AND owner_user_id = $2::uuid AND deleted_at IS NULL
	`, conversationID, ownerUserID)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE attachments
		SET status = 'deleting', updated_at = now()
		WHERE conversation_id = $1::uuid AND status IN ('pending', 'ready')
	`, conversationID); err != nil {
		return fmt.Errorf("mark conversation attachments for deletion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete conversation: %w", err)
	}
	return nil
}
