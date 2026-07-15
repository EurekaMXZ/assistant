package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConversationShareRepository struct {
	pool *pgxpool.Pool
}

type CreateConversationShareParams struct {
	ConversationID  string
	CreatedByUserID string
	IdempotencyKey  string
}

func NewConversationShareRepository(pool *pgxpool.Pool) *ConversationShareRepository {
	return &ConversationShareRepository{pool: pool}
}

func (r *ConversationShareRepository) CreateConversationShare(ctx context.Context, params CreateConversationShareParams) (*domain.ConversationShare, bool, error) {
	params.ConversationID = strings.TrimSpace(params.ConversationID)
	params.CreatedByUserID = strings.TrimSpace(params.CreatedByUserID)
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if params.ConversationID == "" || params.CreatedByUserID == "" {
		return nil, false, domain.NewValidationError("conversation id and owner user id are required")
	}
	if uuid.Validate(params.ConversationID) != nil {
		return nil, false, domain.ErrNotFound
	}
	if uuid.Validate(params.CreatedByUserID) != nil {
		return nil, false, domain.NewValidationError("owner user id must be a valid UUID")
	}
	if params.IdempotencyKey == "" || len(params.IdempotencyKey) > 128 {
		return nil, false, domain.NewValidationError("Idempotency-Key is required and must be at most 128 bytes")
	}

	share, err := scanConversationShare(r.pool.QueryRow(ctx, `
		INSERT INTO conversation_shares (
			id,
			conversation_id,
			created_by_user_id,
			idempotency_key,
			title,
			last_message_seq
		)
		SELECT
			$1::uuid,
			conversation.id,
			$3::uuid,
			$4,
			COALESCE(conversation.title, ''),
			COALESCE((
				SELECT MAX(message.seq)
				FROM messages message
				WHERE message.conversation_id = conversation.id
			), 0)
		FROM conversations conversation
		WHERE conversation.id = $2::uuid
		  AND conversation.owner_user_id = $3::uuid
		ON CONFLICT (conversation_id, created_by_user_id, idempotency_key) DO NOTHING
		RETURNING
			id::text,
			conversation_id::text,
			created_by_user_id::text,
			title,
			last_message_seq,
			created_at
	`, uuid.NewString(), params.ConversationID, params.CreatedByUserID, params.IdempotencyKey))
	if err == nil {
		return share, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if isUniqueViolation(err) {
			return nil, false, domain.ErrConflict
		}
		return nil, false, fmt.Errorf("insert conversation share: %w", err)
	}

	share, err = scanConversationShare(r.pool.QueryRow(ctx, `
		SELECT
			share.id::text,
			share.conversation_id::text,
			share.created_by_user_id::text,
			share.title,
			share.last_message_seq,
			share.created_at
		FROM conversation_shares share
		JOIN conversations conversation ON conversation.id = share.conversation_id
		WHERE share.conversation_id = $1::uuid
		  AND share.created_by_user_id = $2::uuid
		  AND share.idempotency_key = $3
		  AND conversation.owner_user_id = $2::uuid
	`, params.ConversationID, params.CreatedByUserID, params.IdempotencyKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, domain.ErrNotFound
		}
		return nil, false, fmt.Errorf("get replayed conversation share: %w", err)
	}
	return share, true, nil
}

func (r *ConversationShareRepository) GetConversationShare(ctx context.Context, shareID string) (*domain.ConversationShareSnapshot, error) {
	shareID = strings.TrimSpace(shareID)
	if uuid.Validate(shareID) != nil {
		return nil, domain.ErrNotFound
	}

	var (
		snapshot       domain.ConversationShareSnapshot
		conversationID string
	)
	if err := r.pool.QueryRow(ctx, `
		SELECT
			share.id::text,
			share.conversation_id::text,
			share.title,
			share.last_message_seq,
			share.created_at
		FROM conversation_shares share
		WHERE share.id = $1::uuid
	`, shareID).Scan(
		&snapshot.ID,
		&conversationID,
		&snapshot.Title,
		&snapshot.LastMessageSeq,
		&snapshot.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get conversation share: %w", err)
	}

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
			context_excluded,
			created_at
		FROM messages
		WHERE conversation_id = $1::uuid
		  AND seq <= $2
		  AND role IN ($3, $4)
		ORDER BY seq ASC
	`, conversationID, snapshot.LastMessageSeq, domain.RoleUser, domain.RoleAssistant)
	if err != nil {
		return nil, fmt.Errorf("list conversation share messages: %w", err)
	}
	defer rows.Close()

	snapshot.Messages = make([]domain.Message, 0)
	for rows.Next() {
		message, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		snapshot.Messages = append(snapshot.Messages, *message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation share messages: %w", err)
	}
	return &snapshot, nil
}

func scanConversationShare(row scanRow) (*domain.ConversationShare, error) {
	var share domain.ConversationShare
	if err := row.Scan(
		&share.ID,
		&share.ConversationID,
		&share.CreatedByUserID,
		&share.Title,
		&share.LastMessageSeq,
		&share.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &share, nil
}
