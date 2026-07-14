package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/pagination"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	usersEmailUniqueConstraint    = "idx_users_email_unique"
	usersUsernameUniqueConstraint = "idx_users_username_unique"
	usersSystemUniqueConstraint   = "idx_users_unique_system_role"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) CreateUser(ctx context.Context, params assistantauth.CreateUserParams) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (
			email,
			username,
			password_hash,
			role,
			status,
			email_verified_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
	`, params.Email, params.Username, params.PasswordHash, params.Role, params.Status, params.EmailVerifiedAt)

	user, err := scanUser(row)
	if err != nil {
		if conflict := classifyUserConflict(err); conflict != nil {
			return nil, conflict
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
		FROM users
		WHERE id = $1::uuid
	`, userID)

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return user, nil
}

func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
		FROM users
		WHERE lower(email) = lower($1)
	`, email)

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	return user, nil
}

func (r *UserRepository) ListUsers(ctx context.Context, params assistantauth.ListUsersParams) ([]domain.User, string, error) {
	limit := clampLimit(params.Limit, 50, 200)
	decoded, err := pagination.Decode(strings.TrimSpace(params.Cursor))
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}

	query := `
		SELECT
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
		FROM users
	`

	conditions := make([]string, 0, 3)
	args := make([]any, 0, 5)

	if len(params.Roles) > 0 {
		conditions = append(conditions, fmt.Sprintf("role = ANY($%d::text[])", len(args)+1))
		args = append(args, params.Roles)
	}
	if userID := strings.TrimSpace(params.ExcludeUserID); userID != "" {
		conditions = append(conditions, fmt.Sprintf("id <> $%d::uuid", len(args)+1))
		args = append(args, userID)
	}
	if decoded != nil {
		args = append(args, decoded.CreatedAt, decoded.ID)
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0, limit+1)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, "", err
		}
		users = append(users, *user)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate users: %w", err)
	}

	next := ""
	if len(users) > limit {
		users = users[:limit]
		last := users[len(users)-1]
		next = pagination.Encode(last.CreatedAt, last.ID)
	}
	return users, next, nil
}

func (r *UserRepository) UpdateUser(ctx context.Context, params assistantauth.UpdateUserParams) (*domain.User, error) {
	setClauses := make([]string, 0, 4)
	args := make([]any, 0, 5)

	args = append(args, params.UserID)

	if params.Email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", len(args)+1))
		args = append(args, *params.Email)
	}
	if params.Username != nil {
		setClauses = append(setClauses, fmt.Sprintf("username = $%d", len(args)+1))
		args = append(args, *params.Username)
	}
	if params.Role != nil {
		setClauses = append(setClauses, fmt.Sprintf("role = $%d", len(args)+1))
		args = append(args, *params.Role)
	}
	if params.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, *params.Status)
	}

	if len(setClauses) == 0 {
		return r.GetUserByID(ctx, params.UserID)
	}

	query := `
		UPDATE users
		SET ` + strings.Join(setClauses, ", ") + `
		WHERE id = $1::uuid`
	if len(params.AllowedCurrentRoles) > 0 {
		args = append(args, params.AllowedCurrentRoles)
		query += fmt.Sprintf(" AND role = ANY($%d::text[])", len(args))
	}
	query += `
		RETURNING
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
	`

	row := r.pool.QueryRow(ctx, query, args...)
	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		if conflict := classifyUserConflict(err); conflict != nil {
			return nil, conflict
		}
		return nil, fmt.Errorf("update user: %w", err)
	}

	return user, nil
}

func (r *UserRepository) UpdateUserPassword(ctx context.Context, userID string, passwordHash string, allowedCurrentRoles []string) (*domain.User, error) {
	query := `
		UPDATE users
		SET password_hash = $2, auth_version = auth_version + 1
		WHERE id = $1::uuid`
	args := []any{userID, passwordHash}
	if len(allowedCurrentRoles) > 0 {
		args = append(args, allowedCurrentRoles)
		query += ` AND role = ANY($3::text[])`
	}
	query += `
		RETURNING
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
	`
	row := r.pool.QueryRow(ctx, query, args...)

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update user password: %w", err)
	}

	return user, nil
}

func (r *UserRepository) TouchUserLogin(ctx context.Context, userID string) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE users
		SET last_login_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
	`, userID)

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("touch user login: %w", err)
	}

	return user, nil
}

func (r *UserRepository) EnsureSystemUser(ctx context.Context, params assistantauth.EnsureSystemUserParams) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (
			email,
			username,
			password_hash,
			role,
			status,
			email_verified_at
		)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (role) WHERE role = 'system'
		DO UPDATE SET
			email = EXCLUDED.email,
			username = EXCLUDED.username,
			password_hash = EXCLUDED.password_hash,
			status = EXCLUDED.status,
			email_verified_at = COALESCE(users.email_verified_at, now()),
			auth_version = users.auth_version + CASE
				WHEN users.password_hash IS DISTINCT FROM EXCLUDED.password_hash THEN 1
				ELSE 0
			END
		RETURNING
			id::text,
			email,
			username,
			password_hash,
			role,
			status,
			last_login_at,
			email_verified_at,
			auth_version,
			created_at,
			updated_at
	`, params.Email, params.Username, params.PasswordHash, domain.UserRoleSystem, domain.UserStatusActive)

	user, err := scanUser(row)
	if err != nil {
		if conflict := classifyUserConflict(err); conflict != nil {
			return nil, conflict
		}
		return nil, fmt.Errorf("ensure system user: %w", err)
	}

	return user, nil
}

func classifyUserConflict(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.UniqueViolation {
		return nil
	}

	switch pgErr.ConstraintName {
	case usersEmailUniqueConstraint:
		return domain.NewConflictError("email already exists")
	case usersUsernameUniqueConstraint:
		return domain.NewConflictError("username already exists")
	case usersSystemUniqueConstraint:
		return domain.NewConflictError("system user already exists")
	default:
		return domain.ErrConflict
	}
}
