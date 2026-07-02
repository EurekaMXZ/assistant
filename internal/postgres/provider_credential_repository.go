package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/pagination"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProviderCredentialRepository struct {
	pool *pgxpool.Pool
}

type CreateProviderCredentialParams struct {
	ID              string
	Provider        string
	Name            string
	BaseURL         string
	EncryptedAPIKey []byte
	Nonce           []byte
	KeyVersion      int
	KeyHint         string
	ActorUserID     string
}

type UpdateProviderCredentialParams struct {
	ID          string
	Name        *string
	BaseURL     *string
	Status      *string
	ActorUserID string
}

func NewProviderCredentialRepository(pool *pgxpool.Pool) *ProviderCredentialRepository {
	return &ProviderCredentialRepository{pool: pool}
}

const providerCredentialColumns = `
	id::text, provider, name, base_url, key_hint, status,
	last_validated_at, COALESCE(last_validation_error, ''),
	created_by_user_id::text, updated_by_user_id::text, created_at, updated_at`

func (r *ProviderCredentialRepository) Create(ctx context.Context, params CreateProviderCredentialParams) (*domain.ProviderCredential, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = uuid.NewString()
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO provider_credentials (
			id, provider, name, base_url, encrypted_api_key, nonce, key_version, key_hint,
			created_by_user_id, updated_by_user_id
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::uuid, $9::uuid)
		RETURNING `+providerCredentialColumns,
		id, strings.TrimSpace(params.Provider), strings.TrimSpace(params.Name), strings.TrimSpace(params.BaseURL),
		params.EncryptedAPIKey, params.Nonce, params.KeyVersion, params.KeyHint, params.ActorUserID)
	credential, err := scanProviderCredential(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewConflictError("provider credential name already exists")
		}
		return nil, fmt.Errorf("create provider credential: %w", err)
	}
	return credential, nil
}

func (r *ProviderCredentialRepository) Get(ctx context.Context, id string) (*domain.ProviderCredential, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+providerCredentialColumns+` FROM provider_credentials WHERE id = $1::uuid`, id)
	credential, err := scanProviderCredential(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get provider credential: %w", err)
	}
	return credential, nil
}

func (r *ProviderCredentialRepository) GetStored(ctx context.Context, id string) (*domain.StoredProviderCredential, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+providerCredentialColumns+`, encrypted_api_key, nonce, key_version
		FROM provider_credentials
		WHERE id = $1::uuid
	`, id)
	credential, err := scanStoredProviderCredential(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get stored provider credential: %w", err)
	}
	return credential, nil
}

func (r *ProviderCredentialRepository) List(ctx context.Context, limit int, cursor string) ([]domain.ProviderCredential, string, error) {
	limit = clampLimit(limit, 50, 200)
	decoded, err := pagination.Decode(cursor)
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	query := `SELECT ` + providerCredentialColumns + ` FROM provider_credentials`
	args := []any{}
	if decoded != nil {
		query += ` WHERE (created_at, id) < ($1, $2::uuid)`
		args = append(args, decoded.CreatedAt, decoded.ID)
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list provider credentials: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ProviderCredential, 0, limit+1)
	for rows.Next() {
		item, err := scanProviderCredential(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = pagination.Encode(last.CreatedAt, last.ID)
	}
	return items, next, nil
}

func (r *ProviderCredentialRepository) Update(ctx context.Context, params UpdateProviderCredentialParams) (*domain.ProviderCredential, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE provider_credentials
		SET name = COALESCE($2, name), base_url = COALESCE($3, base_url),
			status = COALESCE($4, status), updated_by_user_id = $5::uuid
		WHERE id = $1::uuid AND status <> 'revoked'
		RETURNING `+providerCredentialColumns,
		params.ID, params.Name, params.BaseURL, params.Status, params.ActorUserID)
	credential, err := scanProviderCredential(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		if isUniqueViolation(err) {
			return nil, domain.NewConflictError("provider credential name already exists")
		}
		return nil, fmt.Errorf("update provider credential: %w", err)
	}
	return credential, nil
}

func (r *ProviderCredentialRepository) Rotate(ctx context.Context, id string, actorUserID string, encrypted []byte, nonce []byte, keyVersion int, keyHint string) (*domain.ProviderCredential, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE provider_credentials
		SET encrypted_api_key = $2, nonce = $3, key_version = $4, key_hint = $5,
			status = 'enabled', last_validated_at = NULL, last_validation_error = NULL,
			updated_by_user_id = $6::uuid
		WHERE id = $1::uuid AND status <> 'revoked'
		RETURNING `+providerCredentialColumns,
		id, encrypted, nonce, keyVersion, keyHint, actorUserID)
	credential, err := scanProviderCredential(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("rotate provider credential: %w", err)
	}
	return credential, nil
}

func (r *ProviderCredentialRepository) RecordValidation(ctx context.Context, id string, validationError string) (*domain.ProviderCredential, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE provider_credentials
		SET last_validated_at = now(), last_validation_error = NULLIF($2, '')
		WHERE id = $1::uuid AND status <> 'revoked'
		RETURNING `+providerCredentialColumns, id, validationError)
	credential, err := scanProviderCredential(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("record provider credential validation: %w", err)
	}
	return credential, nil
}

func (r *ProviderCredentialRepository) Revoke(ctx context.Context, id string, actorUserID string) (*domain.ProviderCredential, error) {
	var references int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM models WHERE credential_id = $1::uuid`, id).Scan(&references); err != nil {
		return nil, fmt.Errorf("check provider credential references: %w", err)
	}
	if references > 0 {
		return nil, domain.NewConflictError("provider credential is referenced by one or more models")
	}
	status := domain.CredentialStatusRevoked
	return r.Update(ctx, UpdateProviderCredentialParams{ID: id, Status: &status, ActorUserID: actorUserID})
}

func scanProviderCredential(row scanRow) (*domain.ProviderCredential, error) {
	var item domain.ProviderCredential
	err := row.Scan(&item.ID, &item.Provider, &item.Name, &item.BaseURL, &item.MaskedKey, &item.Status,
		&item.LastValidatedAt, &item.LastValidationError, &item.CreatedByUserID, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt)
	return &item, err
}

func scanStoredProviderCredential(row scanRow) (*domain.StoredProviderCredential, error) {
	var item domain.StoredProviderCredential
	err := row.Scan(&item.ID, &item.Provider, &item.Name, &item.BaseURL, &item.MaskedKey, &item.Status,
		&item.LastValidatedAt, &item.LastValidationError, &item.CreatedByUserID, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt, &item.EncryptedAPIKey, &item.Nonce, &item.KeyVersion)
	return &item, err
}
