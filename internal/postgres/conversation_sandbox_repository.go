package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const conversationSandboxColumns = `
	id::text, conversation_id::text, provider, runtime_id, status, runtime_metadata,
	last_activity_at, created_at, updated_at, stopped_at, destroyed_at,
	execution_token::text, execution_lease_until, release_previous_status,
	release_token::text, release_lease_until
`

type ConversationSandboxRepository struct {
	pool *pgxpool.Pool
}

type conversationSandboxDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func NewConversationSandboxRepository(pool *pgxpool.Pool) *ConversationSandboxRepository {
	return &ConversationSandboxRepository{pool: pool}
}

func (r *ConversationSandboxRepository) db(ctx context.Context) conversationSandboxDB {
	if conn := lockedConnection(ctx); conn != nil {
		return conn
	}
	return r.pool
}

func (r *ConversationSandboxRepository) ListNonDestroyedSandboxProviders(ctx context.Context) ([]string, error) {
	rows, err := r.db(ctx).Query(ctx, `
		SELECT DISTINCT provider
		FROM sandboxes
		WHERE status <> $1
		ORDER BY provider
	`, domain.SandboxStatusDestroyed)
	if err != nil {
		return nil, fmt.Errorf("list non-destroyed sandbox providers: %w", err)
	}
	defer rows.Close()
	providers := make([]string, 0, 2)
	for rows.Next() {
		var provider string
		if err := rows.Scan(&provider); err != nil {
			return nil, fmt.Errorf("scan non-destroyed sandbox provider: %w", err)
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate non-destroyed sandbox providers: %w", err)
	}
	return providers, nil
}

func (r *ConversationSandboxRepository) GetActiveConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	row := r.db(ctx).QueryRow(ctx, `SELECT `+conversationSandboxColumns+`
		FROM sandboxes
		WHERE conversation_id = $1::uuid AND status = $2 AND destroyed_at IS NULL
		ORDER BY created_at DESC LIMIT 1
	`, conversationID, domain.SandboxStatusActive)
	return scanConversationSandboxResult(row, "get active conversation sandbox")
}

func (r *ConversationSandboxRepository) GetUsableConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	row := r.db(ctx).QueryRow(ctx, `SELECT `+conversationSandboxColumns+`
		FROM sandboxes
		WHERE conversation_id = $1::uuid AND status IN ($2, $3) AND destroyed_at IS NULL
		ORDER BY created_at DESC LIMIT 1
	`, conversationID, domain.SandboxStatusActive, domain.SandboxStatusStopped)
	return scanConversationSandboxResult(row, "get usable conversation sandbox")
}

func (r *ConversationSandboxRepository) GetLatestConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	row := r.db(ctx).QueryRow(ctx, `SELECT `+conversationSandboxColumns+`
		FROM sandboxes WHERE conversation_id = $1::uuid ORDER BY created_at DESC LIMIT 1
	`, conversationID)
	return scanConversationSandboxResult(row, "get latest conversation sandbox")
}

func (r *ConversationSandboxRepository) CreateConversationSandbox(ctx context.Context, conversationID string, provider string, runtimeID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	conn, releaseQuota, err := r.acquireSandboxQuotaSlot(ctx, `
		SELECT u.id::text, u.sandbox_quota
		FROM conversations c
		JOIN users u ON u.id = c.owner_user_id
		WHERE c.id = $1::uuid
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer releaseQuota()

	row := conn.QueryRow(ctx, `
		INSERT INTO sandboxes (conversation_id, provider, runtime_id, status, runtime_metadata)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb)
		RETURNING `+conversationSandboxColumns, conversationID, provider, runtimeID, domain.SandboxStatusActive, normalizedJSON(metadata))
	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("create conversation sandbox: %w", err)
	}
	return sandbox, nil
}

func (r *ConversationSandboxRepository) StopConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	row := r.db(ctx).QueryRow(ctx, `
		UPDATE sandboxes
		SET status = $2, runtime_metadata = $3::jsonb, stopped_at = now(),
			execution_token = NULL, execution_lease_until = NULL
		WHERE id = $1::uuid AND status = $4 AND destroyed_at IS NULL
		  AND (execution_lease_until IS NULL OR execution_lease_until < now())
		RETURNING `+conversationSandboxColumns, sandboxID, domain.SandboxStatusStopped, normalizedJSON(metadata), domain.SandboxStatusActive)
	return scanConversationSandboxMutation(row, "stop conversation sandbox")
}

func (r *ConversationSandboxRepository) ResumeConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	conn, releaseQuota, err := r.acquireSandboxQuotaSlot(ctx, `
		SELECT u.id::text, u.sandbox_quota
		FROM sandboxes s
		JOIN conversations c ON c.id = s.conversation_id
		JOIN users u ON u.id = c.owner_user_id
		WHERE s.id = $1::uuid AND s.destroyed_at IS NULL
	`, sandboxID)
	if err != nil {
		return nil, err
	}
	defer releaseQuota()

	row := conn.QueryRow(ctx, `
		UPDATE sandboxes
		SET status = $2, runtime_metadata = $3::jsonb, stopped_at = NULL, last_activity_at = now()
		WHERE id = $1::uuid AND status = $4 AND destroyed_at IS NULL
		RETURNING `+conversationSandboxColumns, sandboxID, domain.SandboxStatusActive, normalizedJSON(metadata), domain.SandboxStatusStopped)
	return scanConversationSandboxMutation(row, "resume conversation sandbox")
}

func (r *ConversationSandboxRepository) acquireSandboxQuotaSlot(ctx context.Context, ownerQuery string, identifier string) (*pgxpool.Conn, func(), error) {
	if r == nil || r.pool == nil {
		return nil, nil, errors.New("conversation sandbox repository is not configured")
	}
	conn := lockedConnection(ctx)
	releaseConnection := false
	if conn == nil {
		var err error
		conn, err = r.pool.Acquire(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("acquire sandbox quota connection: %w", err)
		}
		releaseConnection = true
	}
	release := func() {
		if releaseConnection {
			conn.Release()
		}
	}

	var ownerUserID string
	var quota int
	if err := conn.QueryRow(ctx, ownerQuery, identifier).Scan(&ownerUserID, &quota); err != nil {
		release()
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, domain.ErrNotFound
		}
		return nil, nil, fmt.Errorf("load sandbox quota owner: %w", err)
	}
	lockKey := "sandbox-quota:" + ownerUserID
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", lockKey); err != nil {
		release()
		return nil, nil, fmt.Errorf("acquire sandbox quota lock: %w", err)
	}
	cleanup := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", lockKey)
		release()
	}
	if err := conn.QueryRow(ctx, `SELECT sandbox_quota FROM users WHERE id = $1::uuid`, ownerUserID).Scan(&quota); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("reload user sandbox quota: %w", err)
	}

	var running int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM sandboxes s
		JOIN conversations c ON c.id = s.conversation_id
		WHERE c.owner_user_id = $1::uuid
		  AND s.destroyed_at IS NULL
		  AND (
			s.status = $2
			OR (s.status = $3 AND s.release_previous_status = $2)
		  )
	`, ownerUserID, domain.SandboxStatusActive, domain.SandboxStatusReleasing).Scan(&running); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("count running user sandboxes: %w", err)
	}
	if running >= quota {
		cleanup()
		return nil, nil, domain.NewConflictError(fmt.Sprintf("sandbox quota exceeded: %d running, limit %d", running, quota))
	}
	return conn, cleanup, nil
}

func (r *ConversationSandboxRepository) TouchConversationSandbox(ctx context.Context, sandboxID string) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE sandboxes SET last_activity_at = now()
		WHERE id = $1::uuid AND status = $2 AND destroyed_at IS NULL
	`, sandboxID, domain.SandboxStatusActive)
	return expectSandboxMutation("touch conversation sandbox", tag, err)
}

func (r *ConversationSandboxRepository) AcquireConversationSandboxExecution(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE sandboxes
		SET execution_token = $2::uuid,
			execution_lease_until = now() + ($3::bigint * interval '1 microsecond'),
			last_activity_at = now()
		WHERE id = $1::uuid AND status = $4 AND destroyed_at IS NULL
		  AND (execution_lease_until IS NULL OR execution_lease_until < now())
	`, sandboxID, token, leaseDuration.Microseconds(), domain.SandboxStatusActive)
	return expectSandboxMutation("acquire conversation sandbox execution", tag, err)
}

func (r *ConversationSandboxRepository) RenewConversationSandboxExecution(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE sandboxes
		SET execution_lease_until = now() + ($3::bigint * interval '1 microsecond')
		WHERE id = $1::uuid AND execution_token = $2::uuid AND status = $4
		  AND destroyed_at IS NULL AND execution_lease_until >= now()
	`, sandboxID, token, leaseDuration.Microseconds(), domain.SandboxStatusActive)
	return expectSandboxMutation("renew conversation sandbox execution", tag, err)
}

func (r *ConversationSandboxRepository) CompleteConversationSandboxExecution(ctx context.Context, sandboxID string, token string) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE sandboxes
		SET execution_token = NULL, execution_lease_until = NULL, last_activity_at = now()
		WHERE id = $1::uuid AND execution_token = $2::uuid AND status = $3
	`, sandboxID, token, domain.SandboxStatusActive)
	return expectSandboxMutation("complete conversation sandbox execution", tag, err)
}

func (r *ConversationSandboxRepository) ListIdleConversationSandboxes(ctx context.Context, inactiveBefore time.Time, limit int) ([]*domain.ConversationSandbox, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+conversationSandboxColumns+`
		FROM sandboxes
		WHERE status = $1 AND destroyed_at IS NULL AND last_activity_at < $2
		  AND (execution_lease_until IS NULL OR execution_lease_until < now())
		ORDER BY last_activity_at, id LIMIT $3
	`, domain.SandboxStatusActive, inactiveBefore, normalizeSandboxLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list idle conversation sandboxes: %w", err)
	}
	return scanConversationSandboxes(rows, "idle")
}

func (r *ConversationSandboxRepository) ListStoppedConversationSandboxes(ctx context.Context, stoppedBefore time.Time, limit int) ([]*domain.ConversationSandbox, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+conversationSandboxColumns+`
		FROM sandboxes
		WHERE status = $1 AND destroyed_at IS NULL AND stopped_at < $2
		ORDER BY stopped_at, id LIMIT $3
	`, domain.SandboxStatusStopped, stoppedBefore, normalizeSandboxLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list stopped conversation sandboxes: %w", err)
	}
	return scanConversationSandboxes(rows, "stopped")
}

func (r *ConversationSandboxRepository) ListReleasingConversationSandboxes(ctx context.Context, limit int) ([]*domain.ConversationSandbox, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+conversationSandboxColumns+`
		FROM sandboxes
		WHERE status = $1 AND destroyed_at IS NULL
		  AND (release_lease_until IS NULL OR release_lease_until < now())
		ORDER BY updated_at, id LIMIT $2
	`, domain.SandboxStatusReleasing, normalizeSandboxLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list releasing conversation sandboxes: %w", err)
	}
	return scanConversationSandboxes(rows, "releasing")
}

func (r *ConversationSandboxRepository) BeginConversationSandboxRelease(ctx context.Context, sandboxID string) (*domain.ConversationSandbox, error) {
	row := r.db(ctx).QueryRow(ctx, `
		UPDATE sandboxes
		SET release_previous_status = status, status = $2,
			execution_token = NULL, execution_lease_until = NULL,
			release_token = NULL, release_lease_until = NULL
		WHERE id = $1::uuid AND status IN ($3, $4) AND destroyed_at IS NULL
		  AND (execution_lease_until IS NULL OR execution_lease_until < now())
		RETURNING `+conversationSandboxColumns, sandboxID, domain.SandboxStatusReleasing, domain.SandboxStatusActive, domain.SandboxStatusStopped)
	return scanConversationSandboxMutation(row, "begin conversation sandbox release")
}

func (r *ConversationSandboxRepository) ClaimConversationSandboxRelease(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) (*domain.ConversationSandbox, error) {
	row := r.db(ctx).QueryRow(ctx, `
		UPDATE sandboxes
		SET release_token = $2::uuid,
			release_lease_until = now() + ($3::bigint * interval '1 microsecond')
		WHERE id = $1::uuid AND status = $4 AND destroyed_at IS NULL
		  AND (release_lease_until IS NULL OR release_lease_until < now())
		RETURNING `+conversationSandboxColumns, sandboxID, token, leaseDuration.Microseconds(), domain.SandboxStatusReleasing)
	return scanConversationSandboxMutation(row, "claim conversation sandbox release")
}

func (r *ConversationSandboxRepository) RenewConversationSandboxReleaseClaim(ctx context.Context, sandboxID string, token string, leaseDuration time.Duration) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE sandboxes
		SET release_lease_until = now() + ($4::bigint * interval '1 microsecond')
		WHERE id = $1::uuid AND status = $3 AND release_token = $2::uuid
	`, sandboxID, token, domain.SandboxStatusReleasing, leaseDuration.Microseconds())
	return expectSandboxMutation("renew conversation sandbox release claim", tag, err)
}

func (r *ConversationSandboxRepository) CompleteConversationSandboxRelease(ctx context.Context, sandboxID string, token string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	row := r.db(ctx).QueryRow(ctx, `
		UPDATE sandboxes
		SET status = $2, runtime_metadata = $4::jsonb, stopped_at = NULL, destroyed_at = now(),
			release_previous_status = NULL, release_token = NULL, release_lease_until = NULL
		WHERE id = $1::uuid AND status = $5 AND release_token = $3::uuid AND destroyed_at IS NULL
		RETURNING `+conversationSandboxColumns, sandboxID, domain.SandboxStatusDestroyed, token, normalizedJSON(metadata), domain.SandboxStatusReleasing)
	return scanConversationSandboxMutation(row, "complete conversation sandbox release")
}

func expectSandboxMutation(operation string, tag pgconn.CommandTag, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

func normalizeSandboxLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

func scanConversationSandboxResult(row scanRow, operation string) (*domain.ConversationSandbox, error) {
	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return sandbox, nil
}

func scanConversationSandboxMutation(row scanRow, operation string) (*domain.ConversationSandbox, error) {
	sandbox, err := scanConversationSandbox(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return sandbox, nil
}

func scanConversationSandboxes(rows pgx.Rows, label string) ([]*domain.ConversationSandbox, error) {
	defer rows.Close()
	items := make([]*domain.ConversationSandbox, 0)
	for rows.Next() {
		item, err := scanConversationSandbox(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s conversation sandbox: %w", label, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s conversation sandboxes: %w", label, err)
	}
	return items, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
