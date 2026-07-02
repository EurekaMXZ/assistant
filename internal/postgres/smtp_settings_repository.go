package postgres

import (
	"context"
	"database/sql"
	"fmt"

	assistantmail "github.com/EurekaMXZ/assistant/internal/mail"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SMTPSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewSMTPSettingsRepository(pool *pgxpool.Pool) *SMTPSettingsRepository {
	return &SMTPSettingsRepository{pool: pool}
}

const smtpSettingsColumns = `
	enabled, host, port, security, username, encrypted_password, password_nonce,
	key_version, from_email, from_name, updated_by_user_id::text, updated_at`

func (r *SMTPSettingsRepository) GetSMTPSettings(ctx context.Context) (*assistantmail.StoredSettings, error) {
	settings, err := scanSMTPSettings(r.pool.QueryRow(ctx, `SELECT `+smtpSettingsColumns+` FROM smtp_settings WHERE singleton`))
	if err != nil {
		return nil, fmt.Errorf("get SMTP settings: %w", err)
	}
	return settings, nil
}

func (r *SMTPSettingsRepository) UpdateSMTPSettings(ctx context.Context, params assistantmail.UpdateSettingsParams) (*assistantmail.StoredSettings, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE smtp_settings
		SET enabled = $1, host = $2, port = $3, security = $4, username = $5,
			encrypted_password = $6, password_nonce = $7, key_version = $8,
			from_email = $9, from_name = $10, updated_by_user_id = $11::uuid
		WHERE singleton
		RETURNING `+smtpSettingsColumns,
		params.Enabled, params.Host, params.Port, params.Security, params.Username,
		params.EncryptedPassword, params.PasswordNonce, params.KeyVersion,
		params.FromEmail, params.FromName, params.UpdatedByUserID)
	settings, err := scanSMTPSettings(row)
	if err != nil {
		return nil, fmt.Errorf("update SMTP settings: %w", err)
	}
	return settings, nil
}

func scanSMTPSettings(row scanRow) (*assistantmail.StoredSettings, error) {
	var (
		settings  assistantmail.StoredSettings
		updatedBy sql.NullString
	)
	if err := row.Scan(
		&settings.Enabled,
		&settings.Host,
		&settings.Port,
		&settings.Security,
		&settings.Username,
		&settings.EncryptedPassword,
		&settings.PasswordNonce,
		&settings.KeyVersion,
		&settings.FromEmail,
		&settings.FromName,
		&updatedBy,
		&settings.UpdatedAt,
	); err != nil {
		return nil, err
	}
	settings.PasswordConfigured = len(settings.EncryptedPassword) > 0
	if updatedBy.Valid {
		settings.UpdatedByUserID = updatedBy.String
	}
	return &settings, nil
}
