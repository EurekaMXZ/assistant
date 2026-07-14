package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/billing"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/jackc/pgx/v5"
)

type BillingToolPriceUpdate struct {
	ToolKey           string
	PricePerCallNanos int64
	Enabled           bool
	ExpectedVersion   int64
}

type UpdateBillingToolPricesParams struct {
	Currency    string
	Prices      []BillingToolPriceUpdate
	ActorUserID string
	ActorRole   string
	RequestID   string
}

func ensureBillingToolPrices(ctx context.Context, tx pgx.Tx, currency string) error {
	for _, key := range domain.SupportedBillingToolKeys() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_tool_prices (tool_key, currency)
			VALUES ($1, $2)
			ON CONFLICT (tool_key, currency) DO NOTHING
		`, key, currency); err != nil {
			return fmt.Errorf("ensure billing tool price %s: %w", key, err)
		}
	}
	return nil
}

func (r *BillingAccountRepository) ListToolPrices(ctx context.Context, currency string) ([]domain.BillingToolPrice, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tool price list: %w", err)
	}
	defer tx.Rollback(ctx)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if err := ensureBillingToolPrices(ctx, tx, currency); err != nil {
		return nil, err
	}
	items, err := listBillingToolPrices(ctx, tx, currency)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tool price list: %w", err)
	}
	return items, nil
}

func (r *BillingAccountRepository) UpdateToolPrices(ctx context.Context, params UpdateBillingToolPricesParams) ([]domain.BillingToolPrice, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tool price update: %w", err)
	}
	defer tx.Rollback(ctx)
	currency := strings.ToUpper(strings.TrimSpace(params.Currency))
	if err := ensureBillingToolPrices(ctx, tx, currency); err != nil {
		return nil, err
	}
	for _, price := range params.Prices {
		if !domain.IsSupportedBillingToolKey(price.ToolKey) || price.PricePerCallNanos < 0 ||
			price.PricePerCallNanos > domain.BillingToolMaxPriceNanos || price.ExpectedVersion <= 0 ||
			(price.Enabled && price.PricePerCallNanos == 0) {
			return nil, domain.NewValidationError("unsupported tool price")
		}
		result, err := tx.Exec(ctx, `
			UPDATE billing_tool_prices
			SET price_per_call_nanos = $3, enabled = $4, version = version + 1,
				updated_by_user_id = $5::uuid
			WHERE tool_key = $1 AND currency = $2 AND version = $6
		`, price.ToolKey, currency, price.PricePerCallNanos, price.Enabled, params.ActorUserID, price.ExpectedVersion)
		if err != nil {
			return nil, fmt.Errorf("update billing tool price %s: %w", price.ToolKey, err)
		}
		if result.RowsAffected() != 1 {
			return nil, domain.NewConflictError("tool pricing plan changed; reload before saving")
		}
	}
	items, err := listBillingToolPrices(ctx, tx, currency)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]any{"currency": currency, "tool_prices": items})
	if err != nil {
		return nil, fmt.Errorf("marshal tool price audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_user_id, actor_role, action, resource_type, resource_id,
			outcome, request_id, visible_to_subject, required_role, metadata
		) VALUES ($1::uuid, $2, 'billing.tool_prices.update', 'billing_tool_prices', $3,
			'succeeded', $4, false, 'admin', $5::jsonb)
	`, params.ActorUserID, params.ActorRole, currency, params.RequestID, metadata); err != nil {
		return nil, fmt.Errorf("audit tool price update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tool price update: %w", err)
	}
	return items, nil
}

func listBillingToolPrices(ctx context.Context, tx pgx.Tx, currency string) ([]domain.BillingToolPrice, error) {
	rows, err := tx.Query(ctx, `
		SELECT tool_key, currency, price_per_call_nanos, enabled, version,
			COALESCE(updated_by_user_id::text, ''), created_at, updated_at
		FROM billing_tool_prices
		WHERE currency = $1
	`, currency)
	if err != nil {
		return nil, fmt.Errorf("list billing tool prices: %w", err)
	}
	defer rows.Close()
	byKey := make(map[string]domain.BillingToolPrice)
	for rows.Next() {
		var item domain.BillingToolPrice
		if err := rows.Scan(&item.ToolKey, &item.Currency, &item.PricePerCallNanos, &item.Enabled,
			&item.Version, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan billing tool price: %w", err)
		}
		item.PricePerCall = billing.FormatAmount(item.PricePerCallNanos)
		byKey[item.ToolKey] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing tool prices: %w", err)
	}
	items := make([]domain.BillingToolPrice, 0, len(byKey))
	for _, key := range domain.SupportedBillingToolKeys() {
		if item, ok := byKey[key]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func loadRunToolCharge(ctx context.Context, tx pgx.Tx, runID string, currency string, imageCount int) (*billing.ToolCharge, error) {
	if imageCount < 0 {
		return nil, domain.NewValidationError("image generation count cannot be negative")
	}
	if err := ensureBillingToolPrices(ctx, tx, currency); err != nil {
		return nil, err
	}
	rates := make(map[string]billing.ToolRate)
	rows, err := tx.Query(ctx, `
		SELECT tool_key, price_per_call_nanos, enabled, version
		FROM billing_tool_prices
		WHERE currency = $1
	`, currency)
	if err != nil {
		return nil, fmt.Errorf("load billing tool rates: %w", err)
	}
	for rows.Next() {
		var key string
		var rate billing.ToolRate
		if err := rows.Scan(&key, &rate.PricePerCallNanos, &rate.Enabled, &rate.Version); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan billing tool rate: %w", err)
		}
		rates[key] = rate
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate billing tool rates: %w", err)
	}
	rows.Close()

	usage := make(map[string]int)
	if imageCount > 0 {
		usage[domain.BillingToolImageGeneration] = imageCount
	}
	rows, err = tx.Query(ctx, `
		SELECT COALESCE(namespace, ''), tool_name, count(*)::integer
		FROM tool_calls
		WHERE turn_run_id = $1::uuid AND status = $2
		GROUP BY namespace, tool_name
	`, runID, domain.ToolCallStatusCompleted)
	if err != nil {
		return nil, fmt.Errorf("load completed tool calls for billing: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var namespace, name string
		var count int
		if err := rows.Scan(&namespace, &name, &count); err != nil {
			return nil, fmt.Errorf("scan completed tool call usage: %w", err)
		}
		if key := billingToolKey(namespace, name); key != "" {
			usage[key] += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate completed tool call usage: %w", err)
	}
	quote, err := billing.QuoteToolUsage(currency, rates, usage)
	if err != nil {
		return nil, fmt.Errorf("rate tool usage: %w", err)
	}
	return quote, nil
}

func billingToolKey(namespace string, name string) string {
	qualified := strings.TrimSpace(name)
	if value := strings.TrimSpace(namespace); value != "" {
		qualified = value + "." + qualified
	}
	switch qualified {
	case domain.BillingToolSandboxCreate:
		return domain.BillingToolSandboxCreate
	case "internet.search", domain.BillingToolTavilySearch:
		return domain.BillingToolTavilySearch
	case "internet.extract", domain.BillingToolTavilyExtract:
		return domain.BillingToolTavilyExtract
	default:
		return ""
	}
}
