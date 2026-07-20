package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/pagination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ModelRepository struct {
	pool *pgxpool.Pool
}

type CreateModelParams struct {
	Provider                  string
	CredentialID              string
	Slug                      string
	UpstreamModel             string
	DisplayName               string
	Description               string
	InputModalities           []string
	OutputModalities          []string
	SupportsTools             bool
	SupportsParallelTools     bool
	SupportedReasoningEfforts []string
	ContextWindowTokens       int
	MaxOutputTokens           int
	DefaultParameters         json.RawMessage
	ActorUserID               string
}

type UpdateModelParams struct {
	ID                        string
	CredentialID              *string
	DisplayName               *string
	Description               *string
	InputModalities           []string
	OutputModalities          []string
	SupportsTools             *bool
	SupportsParallelTools     *bool
	SupportedReasoningEfforts []string
	ContextWindowTokens       *int
	MaxOutputTokens           *int
	DefaultParameters         json.RawMessage
	Status                    *string
	ActorUserID               string
}

type CreateModelPriceParams struct {
	ModelID                           string
	Currency                          string
	InputPerMillionNanos              int64
	CacheReadInputPerMillionNanos     int64
	CacheCreationInputPerMillionNanos int64
	OutputPerMillionNanos             int64
	ImageInputPerMillionNanos         *int64
	ImageOutputPerImageNanos          *int64
	ActorUserID                       string
}

func NewModelRepository(pool *pgxpool.Pool) *ModelRepository {
	return &ModelRepository{pool: pool}
}

const modelColumns = `
	id::text, provider, credential_id::text, slug, upstream_model, display_name, description,
	input_modalities, output_modalities, supports_tools, supports_parallel_tools, supported_reasoning_efforts,
	context_window_tokens, max_output_tokens, default_parameters, status, revision,
	created_by_user_id::text, updated_by_user_id::text, created_at, updated_at, deleted_at`

const modelPriceColumns = `
	id::text, model_id::text, version, currency,
	input_per_million_nanos, cache_read_input_per_million_nanos, cache_creation_input_per_million_nanos,
	output_per_million_nanos, image_input_per_million_nanos, image_output_per_image_nanos,
	status, effective_from, pricing_snapshot, created_by_user_id::text,
	COALESCE(published_by_user_id::text, ''), published_at, archived_at, created_at`

func (r *ModelRepository) Create(ctx context.Context, params CreateModelParams) (*domain.Model, error) {
	efforts := params.SupportedReasoningEfforts
	if efforts == nil {
		efforts = []string{}
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO models (
			provider, credential_id, slug, upstream_model, display_name, description,
			input_modalities, output_modalities, supports_tools, supports_parallel_tools,
			supported_reasoning_efforts, context_window_tokens, max_output_tokens, default_parameters,
			created_by_user_id, updated_by_user_id
		) VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11::text[], $12, $13, $14::jsonb, $15::uuid, $15::uuid)
		RETURNING `+modelColumns,
		strings.TrimSpace(params.Provider), params.CredentialID, strings.ToLower(strings.TrimSpace(params.Slug)),
		strings.TrimSpace(params.UpstreamModel), strings.TrimSpace(params.DisplayName), strings.TrimSpace(params.Description),
		normalizeModalities(params.InputModalities, "text"), normalizeModalities(params.OutputModalities, "text"),
		params.SupportsTools, params.SupportsParallelTools, efforts,
		params.ContextWindowTokens, params.MaxOutputTokens, normalizedJSON(params.DefaultParameters), params.ActorUserID)
	model, err := scanModel(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewConflictError("model already exists")
		}
		return nil, fmt.Errorf("create model: %w", err)
	}
	return model, nil
}

func (r *ModelRepository) Get(ctx context.Context, id string) (*domain.Model, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+modelColumns+` FROM models WHERE id = $1::uuid AND deleted_at IS NULL`, id)
	model, err := scanModel(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get model: %w", err)
	}
	return model, nil
}

func (r *ModelRepository) List(ctx context.Context, enabledOnly bool, limit int, cursor string) ([]domain.Model, string, error) {
	limit = clampLimit(limit, 50, 200)
	decoded, err := pagination.Decode(cursor)
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	conditions := []string{}
	args := []any{}
	conditions = append(conditions, "deleted_at IS NULL")
	if enabledOnly {
		args = append(args, domain.ModelStatusEnabled)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if decoded != nil {
		args = append(args, decoded.CreatedAt, decoded.ID)
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	query := `SELECT ` + modelColumns + ` FROM models`
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Model, 0, limit+1)
	for rows.Next() {
		item, err := scanModel(rows)
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

func (r *ModelRepository) Update(ctx context.Context, params UpdateModelParams) (*domain.Model, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE models
		SET credential_id = COALESCE($2::uuid, credential_id),
			display_name = COALESCE($3, display_name), description = COALESCE($4, description),
			input_modalities = COALESCE($5, input_modalities), output_modalities = COALESCE($6, output_modalities),
			supports_tools = COALESCE($7, supports_tools),
			supports_parallel_tools = COALESCE($8, supports_parallel_tools),
			supported_reasoning_efforts = COALESCE($9::text[], supported_reasoning_efforts),
			context_window_tokens = COALESCE($10, context_window_tokens),
			max_output_tokens = COALESCE($11, max_output_tokens),
			default_parameters = COALESCE($12::jsonb, default_parameters),
			status = COALESCE($13, status), revision = revision + 1, updated_by_user_id = $14::uuid
		WHERE id = $1::uuid
		RETURNING `+modelColumns,
		params.ID, params.CredentialID, params.DisplayName, params.Description,
		nullableStringSlice(params.InputModalities), nullableStringSlice(params.OutputModalities),
		params.SupportsTools, params.SupportsParallelTools, nullableStringSlice(params.SupportedReasoningEfforts),
		params.ContextWindowTokens, params.MaxOutputTokens, nullableJSON(params.DefaultParameters), params.Status, params.ActorUserID)
	model, err := scanModel(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update model: %w", err)
	}
	return model, nil
}

func (r *ModelRepository) Delete(ctx context.Context, modelID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin delete model: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE models
		SET status = 'disabled', deleted_at = COALESCE(deleted_at, now()), revision = revision + 1
		WHERE id = $1::uuid AND deleted_at IS NULL
	`, modelID)
	if err != nil {
		return fmt.Errorf("delete model: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE model_settings
		SET default_chat_model_id = CASE WHEN default_chat_model_id = $1::uuid THEN NULL ELSE default_chat_model_id END,
			compaction_model_id = CASE WHEN compaction_model_id = $1::uuid THEN NULL ELSE compaction_model_id END,
			updated_at = now()
		WHERE singleton = true
	`, modelID); err != nil {
		return fmt.Errorf("clear deleted model settings: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete model: %w", err)
	}
	return nil
}

func (r *ModelRepository) CreatePrice(ctx context.Context, params CreateModelPriceParams) (*domain.ModelPriceVersion, error) {
	snapshot, err := json.Marshal(map[string]any{
		"currency":                               params.Currency,
		"input_per_million_nanos":                params.InputPerMillionNanos,
		"cache_read_input_per_million_nanos":     params.CacheReadInputPerMillionNanos,
		"cache_creation_input_per_million_nanos": params.CacheCreationInputPerMillionNanos,
		"output_per_million_nanos":               params.OutputPerMillionNanos,
		"image_input_per_million_nanos":          params.ImageInputPerMillionNanos,
		"image_output_per_image_nanos":           params.ImageOutputPerImageNanos,
	})
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO model_price_versions (
			model_id, version, currency, input_per_million_nanos, cache_read_input_per_million_nanos,
			cache_creation_input_per_million_nanos, output_per_million_nanos,
			image_input_per_million_nanos, image_output_per_image_nanos, pricing_snapshot, created_by_user_id
		) SELECT $1::uuid, COALESCE(max(version), 0) + 1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::uuid
		FROM model_price_versions WHERE model_id = $1::uuid
		RETURNING `+modelPriceColumns,
		params.ModelID, strings.ToUpper(strings.TrimSpace(params.Currency)), params.InputPerMillionNanos,
		params.CacheReadInputPerMillionNanos, params.CacheCreationInputPerMillionNanos, params.OutputPerMillionNanos,
		params.ImageInputPerMillionNanos, params.ImageOutputPerImageNanos, snapshot, params.ActorUserID)
	price, err := scanModelPrice(row)
	if err != nil {
		return nil, fmt.Errorf("create model price: %w", err)
	}
	return price, nil
}

func (r *ModelRepository) GetPrice(ctx context.Context, modelID string, priceID string) (*domain.ModelPriceVersion, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+modelPriceColumns+` FROM model_price_versions WHERE id = $1::uuid AND model_id = $2::uuid`, priceID, modelID)
	price, err := scanModelPrice(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get model price: %w", err)
	}
	return price, nil
}

func (r *ModelRepository) ListPrices(ctx context.Context, modelID string, limit int, cursor string) ([]domain.ModelPriceVersion, string, error) {
	limit = clampLimit(limit, 50, 200)
	decoded, err := pagination.Decode(strings.TrimSpace(cursor))
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	args := []any{modelID}
	query := `SELECT ` + modelPriceColumns + ` FROM model_price_versions WHERE model_id = $1::uuid`
	if decoded != nil {
		args = append(args, decoded.CreatedAt, decoded.ID)
		query += fmt.Sprintf(` AND (created_at, id) < ($%d, $%d::uuid)`, len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list model prices: %w", err)
	}
	defer rows.Close()
	items := make([]domain.ModelPriceVersion, 0, limit+1)
	for rows.Next() {
		item, err := scanModelPrice(rows)
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

func (r *ModelRepository) SetPriceStatus(ctx context.Context, modelID string, priceID string, status string, actorUserID string, effectiveFrom *time.Time) (*domain.ModelPriceVersion, error) {
	if status != domain.ModelPriceStatusPublished && status != domain.ModelPriceStatusArchived {
		return nil, domain.NewValidationError("unsupported model price status")
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE model_price_versions
		SET status = $3,
			effective_from = CASE WHEN $3 = 'published' THEN COALESCE($5, now()) ELSE effective_from END,
			published_by_user_id = CASE WHEN $3 = 'published' THEN $4::uuid ELSE published_by_user_id END,
			published_at = CASE WHEN $3 = 'published' THEN now() ELSE published_at END,
			archived_at = CASE WHEN $3 = 'archived' THEN now() ELSE NULL END
		WHERE id = $1::uuid AND model_id = $2::uuid
			AND (($3 = 'published' AND status = 'draft') OR ($3 = 'archived' AND status = 'published'))
		RETURNING `+modelPriceColumns, priceID, modelID, status, actorUserID, effectiveFrom)
	price, err := scanModelPrice(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("set model price status: %w", err)
	}
	return price, nil
}

func (r *ModelRepository) GetSettings(ctx context.Context) (*domain.ModelSettings, error) {
	var item domain.ModelSettings
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(default_chat_model_id::text, ''), COALESCE(compaction_model_id::text, ''),
			COALESCE(updated_by_user_id::text, ''), updated_at
		FROM model_settings WHERE singleton = true
	`).Scan(&item.DefaultChatModelID, &item.CompactionModelID, &item.UpdatedByUserID, &item.UpdatedAt)
	return &item, err
}

func (r *ModelRepository) UpdateSettings(ctx context.Context, defaultChatModelID *string, compactionModelID *string, actorUserID string) (*domain.ModelSettings, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE model_settings
		SET default_chat_model_id = COALESCE($1::uuid, default_chat_model_id),
			compaction_model_id = COALESCE($2::uuid, compaction_model_id),
			updated_by_user_id = $3::uuid, updated_at = now()
		WHERE singleton = true
		RETURNING COALESCE(default_chat_model_id::text, ''), COALESCE(compaction_model_id::text, ''),
			COALESCE(updated_by_user_id::text, ''), updated_at
	`, defaultChatModelID, compactionModelID, actorUserID)
	var item domain.ModelSettings
	if err := row.Scan(&item.DefaultChatModelID, &item.CompactionModelID, &item.UpdatedByUserID, &item.UpdatedAt); err != nil {
		return nil, fmt.Errorf("update model settings: %w", err)
	}
	return &item, nil
}

func (r *ModelRepository) ResolveExecution(ctx context.Context, modelID string, compaction bool) (*domain.ModelExecutionSnapshot, error) {
	selector := strings.TrimSpace(modelID)
	if selector == "" {
		column := "default_chat_model_id"
		if compaction {
			column = "compaction_model_id"
		}
		if err := r.pool.QueryRow(ctx, `SELECT COALESCE(`+column+`::text, '') FROM model_settings WHERE singleton = true`).Scan(&selector); err != nil {
			return nil, fmt.Errorf("load default model setting: %w", err)
		}
	}
	if selector == "" {
		return nil, domain.NewValidationError("no enabled default model is configured")
	}
	row := r.pool.QueryRow(ctx, `
		SELECT m.id::text, m.revision, m.provider, m.credential_id::text, c.base_url,
			m.upstream_model, m.context_window_tokens, m.max_output_tokens, m.supports_tools, m.supports_parallel_tools,
			m.supported_reasoning_efforts, m.default_parameters,
			p.id::text, p.currency, p.pricing_snapshot
		FROM models m
		JOIN provider_credentials c ON c.id = m.credential_id AND c.status = 'enabled'
		JOIN LATERAL (
			SELECT id, currency, pricing_snapshot
			FROM model_price_versions
			WHERE model_id = m.id AND status = 'published' AND effective_from <= now()
			ORDER BY effective_from DESC, version DESC LIMIT 1
		) p ON true
		WHERE m.id = $1::uuid AND m.status = 'enabled'
	`, selector)
	var snapshot domain.ModelExecutionSnapshot
	if err := row.Scan(&snapshot.ModelID, &snapshot.ModelRevision, &snapshot.Provider, &snapshot.CredentialID,
		&snapshot.BaseURL, &snapshot.UpstreamModel, &snapshot.ContextWindowTokens, &snapshot.MaxOutputTokens, &snapshot.SupportsTools,
		&snapshot.SupportsParallelTools, &snapshot.SupportedReasoningEfforts, &snapshot.DefaultParameters,
		&snapshot.ModelPriceID, &snapshot.Currency, &snapshot.PricingSnapshot); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewValidationError("selected model is unavailable")
		}
		return nil, fmt.Errorf("resolve model execution: %w", err)
	}
	return &snapshot, nil
}

func (r *ModelRepository) GetTurnExecution(ctx context.Context, turnID string) (*domain.ModelExecutionSnapshot, error) {
	var raw json.RawMessage
	if err := r.pool.QueryRow(ctx, `SELECT model_snapshot FROM turns WHERE id = $1::uuid`, turnID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	var snapshot domain.ModelExecutionSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.UpstreamModel == "" {
		return nil, domain.ErrNotFound
	}
	return &snapshot, nil
}

func scanModel(row scanRow) (*domain.Model, error) {
	var item domain.Model
	err := row.Scan(&item.ID, &item.Provider, &item.CredentialID, &item.Slug, &item.UpstreamModel,
		&item.DisplayName, &item.Description, &item.InputModalities, &item.OutputModalities,
		&item.SupportsTools, &item.SupportsParallelTools, &item.SupportedReasoningEfforts,
		&item.ContextWindowTokens, &item.MaxOutputTokens, &item.DefaultParameters,
		&item.Status, &item.Revision, &item.CreatedByUserID, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt)
	return &item, err
}

func scanModelPrice(row scanRow) (*domain.ModelPriceVersion, error) {
	var item domain.ModelPriceVersion
	err := row.Scan(&item.ID, &item.ModelID, &item.Version, &item.Currency,
		&item.InputPerMillionNanos, &item.CacheReadInputPerMillionNanos, &item.CacheCreationInputPerMillionNanos,
		&item.OutputPerMillionNanos, &item.ImageInputPerMillionNanos, &item.ImageOutputPerImageNanos,
		&item.Status, &item.EffectiveFrom, &item.PricingSnapshot, &item.CreatedByUserID,
		&item.PublishedByUserID, &item.PublishedAt, &item.ArchivedAt, &item.CreatedAt)
	return &item, err
}

func normalizeModalities(values []string, fallback string) []string {
	if len(values) == 0 {
		return []string{fallback}
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func nullableStringSlice(value []string) any {
	if value == nil {
		return nil
	}
	return value
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return normalizedJSON(value)
}
