package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ActionTokenRepository struct {
	pool *pgxpool.Pool
}

func NewActionTokenRepository(pool *pgxpool.Pool) *ActionTokenRepository {
	return &ActionTokenRepository{pool: pool}
}

func (r *ActionTokenRepository) CreateActionToken(ctx context.Context, params assistantauth.CreateActionTokenParams) (string, error) {
	var tokenID string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE account_action_tokens
			SET used_at = now()
			WHERE user_id = $1::uuid AND purpose = $2 AND used_at IS NULL
		`, params.UserID, params.Purpose); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO account_action_tokens (user_id, purpose, token_hash, expires_at)
			VALUES ($1::uuid, $2, $3, $4)
			RETURNING id::text
		`, params.UserID, params.Purpose, params.TokenHash, params.ExpiresAt).Scan(&tokenID)
	})
	if err != nil {
		return "", fmt.Errorf("create account action token: %w", err)
	}
	return tokenID, nil
}

func (r *ActionTokenRepository) MarkActionTokenSent(ctx context.Context, tokenID string, sentAt time.Time) error {
	command, err := r.pool.Exec(ctx, `
		UPDATE account_action_tokens SET sent_at = $2
		WHERE id = $1::uuid AND used_at IS NULL
	`, tokenID, sentAt)
	if err != nil {
		return fmt.Errorf("mark account action token sent: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ActionTokenRepository) LastActionTokenSentAt(ctx context.Context, userID string, purpose string) (*time.Time, error) {
	var sentAt time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT sent_at
		FROM account_action_tokens
		WHERE user_id = $1::uuid AND purpose = $2 AND sent_at IS NOT NULL
		ORDER BY sent_at DESC
		LIMIT 1
	`, userID, purpose).Scan(&sentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get last account action token sent time: %w", err)
	}
	return &sentAt, nil
}

func (r *ActionTokenRepository) VerifyEmailWithToken(ctx context.Context, tokenHash []byte, now time.Time) (*domain.User, error) {
	var user *domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		userID, err := consumeActionToken(ctx, tx, tokenHash, assistantauth.ActionPurposeEmailVerification, now)
		if err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE users SET email_verified_at = COALESCE(email_verified_at, $2)
			WHERE id = $1::uuid
			RETURNING id::text, email, username, password_hash, role, status, last_login_at,
				email_verified_at, auth_version, storage_quota_bytes, storage_used_bytes, deleted_at, created_at, updated_at
		`, userID, now)
		user, err = scanUser(row)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("verify email with account action token: %w", err)
	}
	return user, nil
}

func (r *ActionTokenRepository) ResetPasswordWithToken(ctx context.Context, tokenHash []byte, passwordHash string, now time.Time) (*domain.User, error) {
	var user *domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		userID, err := consumeActionToken(ctx, tx, tokenHash, assistantauth.ActionPurposePasswordReset, now)
		if err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE users SET password_hash = $2, auth_version = auth_version + 1
			WHERE id = $1::uuid
			RETURNING id::text, email, username, password_hash, role, status, last_login_at,
				email_verified_at, auth_version, storage_quota_bytes, storage_used_bytes, deleted_at, created_at, updated_at
		`, userID, passwordHash)
		user, err = scanUser(row)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("reset password with account action token: %w", err)
	}
	return user, nil
}

func consumeActionToken(ctx context.Context, tx pgx.Tx, tokenHash []byte, purpose string, now time.Time) (string, error) {
	var userID string
	err := tx.QueryRow(ctx, `
		UPDATE account_action_tokens
		SET used_at = $3
		WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > $3
		RETURNING user_id::text
	`, tokenHash, purpose, now).Scan(&userID)
	return userID, err
}
