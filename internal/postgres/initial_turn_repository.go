package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PrepareInitialConversationParams struct {
	OwnerUserID        string
	IdempotencyKey     string
	Title              string
	Metadata           json.RawMessage
	PrepareFingerprint string
}

type CommitInitialTurnParams struct {
	OwnerUserID       string
	IdempotencyKey    string
	CommitFingerprint string
	Turn              CreateUserTurnParams
}

type PreparedInitialConversation struct {
	Conversation domain.Conversation
	Replayed     bool
}

type CommittedInitialTurn struct {
	Conversation domain.Conversation
	EnqueuedTurn domain.EnqueuedTurn
	Replayed     bool
}

type InitialTurnRepository struct {
	pool *pgxpool.Pool
}

func NewInitialTurnRepository(pool *pgxpool.Pool) *InitialTurnRepository {
	return &InitialTurnRepository{pool: pool}
}

func (r *InitialTurnRepository) Prepare(ctx context.Context, params PrepareInitialConversationParams) (*PreparedInitialConversation, error) {
	ownerUserID := strings.TrimSpace(params.OwnerUserID)
	idempotencyKey := strings.TrimSpace(params.IdempotencyKey)
	if ownerUserID == "" || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, domain.NewValidationError("owner user id and a valid idempotency key are required")
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin initial conversation tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockInitialTurnRequest(ctx, tx, ownerUserID, idempotencyKey); err != nil {
		return nil, err
	}

	row := tx.QueryRow(ctx, `
		SELECT c.id::text, c.owner_user_id::text, c.title, c.status, c.metadata, c.created_at, c.updated_at, c.archived_at, c.deleted_at,
		       initial_turn.prepare_fingerprint
		FROM conversation_initial_turns initial_turn
		JOIN conversations c ON c.id = initial_turn.conversation_id
		WHERE initial_turn.owner_user_id = $1::uuid AND initial_turn.idempotency_key = $2
	`, ownerUserID, idempotencyKey)
	conversation, fingerprint, err := scanPreparedConversation(row)
	if err == nil {
		if fingerprint != params.PrepareFingerprint {
			return nil, domain.NewConflictError("Idempotency-Key was already used with different prepare input")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit initial conversation replay: %w", err)
		}
		return &PreparedInitialConversation{Conversation: *conversation, Replayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lookup initial conversation: %w", err)
	}

	trimmedTitle := strings.TrimSpace(params.Title)
	var title any
	if trimmedTitle != "" {
		title = trimmedTitle
	}
	conversation, err = scanConversation(tx.QueryRow(ctx, `
		INSERT INTO conversations (owner_user_id, title, metadata)
		VALUES ($1::uuid, $2, $3::jsonb)
		RETURNING id::text, owner_user_id::text, title, status, metadata, created_at, updated_at, archived_at, deleted_at
	`, ownerUserID, title, normalizedJSON(params.Metadata)))
	if err != nil {
		return nil, fmt.Errorf("insert initial conversation: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO context_heads (conversation_id) VALUES ($1::uuid)`, conversation.ID); err != nil {
		return nil, fmt.Errorf("insert initial conversation context head: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO conversation_initial_turns (owner_user_id, idempotency_key, conversation_id, prepare_fingerprint)
		VALUES ($1::uuid, $2, $3::uuid, $4)
	`, ownerUserID, idempotencyKey, conversation.ID, params.PrepareFingerprint); err != nil {
		return nil, fmt.Errorf("insert initial turn request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit initial conversation: %w", err)
	}
	return &PreparedInitialConversation{Conversation: *conversation}, nil
}

func (r *InitialTurnRepository) Replay(ctx context.Context, ownerUserID string, idempotencyKey string, conversationID string, commitFingerprint string) (*CommittedInitialTurn, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("begin initial turn replay tx: %w", err)
	}
	defer tx.Rollback(ctx)
	var (
		storedConversationID string
		turnID               sql.NullString
		fingerprint          sql.NullString
	)
	err = tx.QueryRow(ctx, `
		SELECT conversation_id::text, turn_id::text, commit_fingerprint
		FROM conversation_initial_turns
		WHERE owner_user_id = $1::uuid AND idempotency_key = $2
	`, strings.TrimSpace(ownerUserID), strings.TrimSpace(idempotencyKey)).Scan(&storedConversationID, &turnID, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, domain.ErrNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("lookup initial turn replay: %w", err)
	}
	if storedConversationID != strings.TrimSpace(conversationID) {
		return nil, false, domain.NewConflictError("conversation_id does not match the prepared Idempotency-Key")
	}
	if !turnID.Valid {
		return nil, false, nil
	}
	if !fingerprint.Valid || fingerprint.String != commitFingerprint {
		return nil, false, domain.NewConflictError("Idempotency-Key was already committed with different input")
	}
	conversation, err := getConversationTx(ctx, tx, storedConversationID)
	if err != nil {
		return nil, false, err
	}
	enqueued, err := getEnqueuedTurnTx(ctx, tx, storedConversationID, turnID.String)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit initial turn replay lookup: %w", err)
	}
	return &CommittedInitialTurn{Conversation: *conversation, EnqueuedTurn: *enqueued, Replayed: true}, true, nil
}

func (r *InitialTurnRepository) Commit(ctx context.Context, params CommitInitialTurnParams) (*CommittedInitialTurn, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin initial turn tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockInitialTurnRequest(ctx, tx, params.OwnerUserID, params.IdempotencyKey); err != nil {
		return nil, err
	}

	var (
		conversationID string
		turnID         sql.NullString
		fingerprint    sql.NullString
	)
	err = tx.QueryRow(ctx, `
		SELECT conversation_id::text, turn_id::text, commit_fingerprint
		FROM conversation_initial_turns
		WHERE owner_user_id = $1::uuid AND idempotency_key = $2
		FOR UPDATE
	`, strings.TrimSpace(params.OwnerUserID), strings.TrimSpace(params.IdempotencyKey)).Scan(&conversationID, &turnID, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock initial turn request: %w", err)
	}
	if conversationID != strings.TrimSpace(params.Turn.ConversationID) {
		return nil, domain.NewConflictError("conversation_id does not match the prepared Idempotency-Key")
	}

	conversation, err := getConversationTx(ctx, tx, conversationID)
	if err != nil {
		return nil, err
	}
	if turnID.Valid {
		if !fingerprint.Valid || fingerprint.String != params.CommitFingerprint {
			return nil, domain.NewConflictError("Idempotency-Key was already committed with different input")
		}
		enqueued, err := getEnqueuedTurnTx(ctx, tx, conversationID, turnID.String)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit initial turn replay: %w", err)
		}
		return &CommittedInitialTurn{Conversation: *conversation, EnqueuedTurn: *enqueued, Replayed: true}, nil
	}

	enqueued, err := createUserTurn(ctx, tx, params.Turn)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE conversation_initial_turns
		SET turn_id = $3::uuid, commit_fingerprint = $4
		WHERE owner_user_id = $1::uuid AND idempotency_key = $2 AND turn_id IS NULL
	`, params.OwnerUserID, params.IdempotencyKey, enqueued.Turn.ID, params.CommitFingerprint); err != nil {
		return nil, fmt.Errorf("complete initial turn request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit initial turn: %w", err)
	}
	return &CommittedInitialTurn{Conversation: *conversation, EnqueuedTurn: *enqueued}, nil
}

func lockInitialTurnRequest(ctx context.Context, tx pgx.Tx, ownerUserID string, idempotencyKey string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, strings.TrimSpace(ownerUserID)+":"+strings.TrimSpace(idempotencyKey)); err != nil {
		return fmt.Errorf("lock initial turn idempotency key: %w", err)
	}
	return nil
}

func scanPreparedConversation(row pgx.Row) (*domain.Conversation, string, error) {
	var (
		conversation domain.Conversation
		title        sql.NullString
		metadata     []byte
		archivedAt   sql.NullTime
		deletedAt    sql.NullTime
		fingerprint  string
	)
	if err := row.Scan(&conversation.ID, &conversation.OwnerUserID, &title, &conversation.Status, &metadata,
		&conversation.CreatedAt, &conversation.UpdatedAt, &archivedAt, &deletedAt, &fingerprint); err != nil {
		return nil, "", err
	}
	if title.Valid {
		conversation.Title = title.String
	}
	if archivedAt.Valid {
		conversation.ArchivedAt = &archivedAt.Time
	}
	if deletedAt.Valid {
		conversation.DeletedAt = &deletedAt.Time
	}
	conversation.Metadata = cloneJSON(metadata)
	return &conversation, fingerprint, nil
}

func getConversationTx(ctx context.Context, tx pgx.Tx, conversationID string) (*domain.Conversation, error) {
	conversation, err := scanConversation(tx.QueryRow(ctx, `
		SELECT id::text, owner_user_id::text, title, status, metadata, created_at, updated_at, archived_at, deleted_at
		FROM conversations WHERE id = $1::uuid
	`, conversationID))
	if err != nil {
		return nil, fmt.Errorf("get initial conversation: %w", err)
	}
	return conversation, nil
}

func getEnqueuedTurnTx(ctx context.Context, tx pgx.Tx, conversationID string, turnID string) (*domain.EnqueuedTurn, error) {
	turn, err := scanTurn(tx.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, seq, COALESCE(retry_of_turn_id::text, ''), variant_index, status, COALESCE(request_blob_key, ''),
		       COALESCE(response_blob_key, ''), COALESCE(stream_blob_key, ''), COALESCE(openai_response_id, ''),
		       COALESCE(error_code, ''), COALESCE(error_message, ''), metadata, started_at, completed_at, failed_at,
		       created_at, updated_at
		FROM turns WHERE id = $1::uuid AND conversation_id = $2::uuid
	`, turnID, conversationID))
	if err != nil {
		return nil, fmt.Errorf("get replayed initial turn: %w", err)
	}
	message, err := scanMessage(tx.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, COALESCE(turn_id::text, ''), seq, role,
			COALESCE(content_text, ''), token_count, metadata, context_excluded, created_at
		FROM messages WHERE turn_id = $1::uuid AND role = $2
	`, turnID, domain.RoleUser))
	if err != nil {
		return nil, fmt.Errorf("get replayed initial message: %w", err)
	}
	return &domain.EnqueuedTurn{ConversationID: conversationID, Message: *message, Turn: *turn}, nil
}
