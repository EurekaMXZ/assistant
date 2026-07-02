package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/billing"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/pagination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BillingAccountRepository struct {
	pool *pgxpool.Pool
}

type ManualBillingTransactionParams struct {
	UserID         string
	ActorUserID    string
	ActorRole      string
	Currency       string
	Kind           string
	AmountNanos    int64
	Reason         string
	Reference      string
	IdempotencyKey string
	RequestID      string
}

type BillingListParams struct {
	UserID string
	Kind   string
	Status string
	Limit  int
	Cursor string
}

func NewBillingAccountRepository(pool *pgxpool.Pool) *BillingAccountRepository {
	return &BillingAccountRepository{pool: pool}
}

const billingAccountColumns = `
	id::text, user_id::text, currency, status, balance_nanos, version, created_at, updated_at`

const billingTransactionColumns = `
	id::text, account_id::text, user_id::text, currency, account_sequence, kind, direction,
	amount_nanos, balance_after_nanos, COALESCE(actor_user_id::text, ''), reason, reference, created_at`

const billingUsageColumns = `
	id::text, request_key, COALESCE(owner_user_id::text, ''), COALESCE(conversation_id::text, ''),
	COALESCE(turn_id::text, ''), COALESCE(turn_run_id::text, ''), workflow, attempt, provider,
	COALESCE(model_id::text, ''), COALESCE(model_revision, 0), COALESCE(model_price_id::text, ''),
	upstream_model, provider_response_id, status, COALESCE(currency, ''), amount_nanos,
	input_tokens, cache_read_input_tokens, cache_creation_input_tokens, output_tokens, reasoning_output_tokens, total_tokens,
	pricing_snapshot, usage, COALESCE(billing_transaction_id::text, ''), error_code, created_at`

func (r *BillingAccountRepository) GetOrCreateAccount(ctx context.Context, userID string, currency string) (*domain.BillingAccount, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO billing_accounts (user_id, currency)
		VALUES ($1::uuid, $2)
		ON CONFLICT (user_id, currency) DO NOTHING
	`, userID, currency); err != nil {
		return nil, fmt.Errorf("ensure billing account: %w", err)
	}
	row := r.pool.QueryRow(ctx, `SELECT `+billingAccountColumns+` FROM billing_accounts WHERE user_id = $1::uuid AND currency = $2`, userID, currency)
	account, err := scanBillingAccount(row)
	if err != nil {
		return nil, fmt.Errorf("get or create billing account: %w", err)
	}
	return account, nil
}

func (r *BillingAccountRepository) UpdateAccount(ctx context.Context, userID string, currency string, status *string) (*domain.BillingAccount, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE billing_accounts
		SET status = COALESCE($3, status), version = version + 1
		WHERE user_id = $1::uuid AND currency = $2
		RETURNING `+billingAccountColumns,
		userID, strings.ToUpper(strings.TrimSpace(currency)), status)
	item, err := scanBillingAccount(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update billing account: %w", err)
	}
	return item, nil
}

func (r *BillingAccountRepository) ListAccounts(ctx context.Context, limit int, cursor string) ([]domain.BillingAccount, string, error) {
	limit = clampLimit(limit, 50, 200)
	decoded, err := pagination.Decode(cursor)
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	query := `SELECT ` + billingAccountColumns + ` FROM billing_accounts`
	args := []any{}
	if decoded != nil {
		query += ` WHERE (created_at, id) < ($1, $2::uuid)`
		args = append(args, decoded.CreatedAt, decoded.ID)
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list billing accounts: %w", err)
	}
	defer rows.Close()
	items := make([]domain.BillingAccount, 0, limit+1)
	for rows.Next() {
		item, err := scanBillingAccount(rows)
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

func (r *BillingAccountRepository) ApplyManualTransaction(ctx context.Context, params ManualBillingTransactionParams) (*domain.BillingTransaction, error) {
	if params.Kind != domain.BillingTransactionManualTopup && params.Kind != domain.BillingTransactionManualRefund {
		return nil, domain.NewValidationError("unsupported manual billing transaction")
	}
	if params.AmountNanos <= 0 || strings.TrimSpace(params.Reason) == "" || strings.TrimSpace(params.IdempotencyKey) == "" {
		return nil, domain.NewValidationError("amount, reason, and Idempotency-Key are required")
	}
	currency := strings.ToUpper(strings.TrimSpace(params.Currency))
	hash := manualTransactionHash(params, currency)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin billing transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, params.ActorUserID+":"+params.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("lock billing idempotency key: %w", err)
	}

	existingRow := tx.QueryRow(ctx, `
		SELECT `+billingTransactionColumns+`, request_hash
		FROM billing_transactions
		WHERE actor_user_id = $1::uuid AND idempotency_key = $2
	`, params.ActorUserID, params.IdempotencyKey)
	existing, existingHash, err := scanBillingTransactionWithHash(existingRow)
	if err == nil {
		if string(existingHash) != string(hash[:]) {
			return nil, domain.NewConflictError("Idempotency-Key was already used with different input")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load idempotent billing transaction: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_accounts (user_id, currency)
		VALUES ($1::uuid, $2)
		ON CONFLICT (user_id, currency) DO NOTHING
	`, params.UserID, currency); err != nil {
		return nil, fmt.Errorf("ensure billing account: %w", err)
	}
	var accountID string
	var balance, version int64
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, balance_nanos, version, status
		FROM billing_accounts
		WHERE user_id = $1::uuid AND currency = $2
		FOR UPDATE
	`, params.UserID, currency).Scan(&accountID, &balance, &version, &status); err != nil {
		return nil, fmt.Errorf("lock billing account: %w", err)
	}
	if status != "active" {
		return nil, domain.NewConflictError("billing account is not active")
	}
	direction := "credit"
	newBalance := balance + params.AmountNanos
	if params.Kind == domain.BillingTransactionManualRefund {
		direction = "debit"
		if balance < params.AmountNanos {
			return nil, domain.NewPaymentRequiredError("insufficient account balance for refund")
		}
		newBalance = balance - params.AmountNanos
	}
	if _, err := tx.Exec(ctx, `
		UPDATE billing_accounts SET balance_nanos = $2, version = version + 1 WHERE id = $1::uuid
	`, accountID, newBalance); err != nil {
		return nil, fmt.Errorf("update billing account balance: %w", err)
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO billing_transactions (
			account_id, user_id, currency, account_sequence, kind, direction, amount_nanos,
			balance_after_nanos, actor_user_id, reason, reference, idempotency_key, request_hash
		) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9::uuid, $10, $11, $12, $13)
		RETURNING `+billingTransactionColumns,
		accountID, params.UserID, currency, version+1, params.Kind, direction, params.AmountNanos,
		newBalance, params.ActorUserID, strings.TrimSpace(params.Reason), strings.TrimSpace(params.Reference),
		params.IdempotencyKey, hash[:])
	transaction, err := scanBillingTransaction(row)
	if err != nil {
		return nil, fmt.Errorf("insert billing transaction: %w", err)
	}
	metadata, _ := json.Marshal(map[string]any{"transaction_kind": params.Kind, "amount_nanos": params.AmountNanos, "currency": currency})
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_user_id, actor_role, subject_user_id, action, resource_type, resource_id,
			outcome, request_id, reason, visible_to_subject, required_role, metadata
		) VALUES ($1::uuid, $2, $3::uuid, $4, 'billing_transaction', $5, 'succeeded', $6, $7, true, 'admin', $8::jsonb)
	`, params.ActorUserID, params.ActorRole, params.UserID, "billing."+params.Kind, transaction.ID, params.RequestID, params.Reason, metadata); err != nil {
		return nil, fmt.Errorf("audit billing transaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit billing transaction: %w", err)
	}
	return transaction, nil
}

func (r *BillingAccountRepository) GetTransaction(ctx context.Context, id string, userID string) (*domain.BillingTransaction, error) {
	query := `SELECT ` + billingTransactionColumns + ` FROM billing_transactions WHERE id = $1::uuid`
	args := []any{id}
	if userID != "" {
		query += ` AND user_id = $2::uuid`
		args = append(args, userID)
	}
	item, err := scanBillingTransaction(r.pool.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

func (r *BillingAccountRepository) ListTransactions(ctx context.Context, params BillingListParams) ([]domain.BillingTransaction, string, error) {
	limit := clampLimit(params.Limit, 50, 200)
	decoded, err := pagination.Decode(params.Cursor)
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	conditions := []string{}
	args := []any{}
	if params.UserID != "" {
		args = append(args, params.UserID)
		conditions = append(conditions, fmt.Sprintf("user_id = $%d::uuid", len(args)))
	}
	if params.Kind != "" {
		args = append(args, params.Kind)
		conditions = append(conditions, fmt.Sprintf("kind = $%d", len(args)))
	}
	if decoded != nil {
		args = append(args, decoded.CreatedAt, decoded.ID)
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	query := `SELECT ` + billingTransactionColumns + ` FROM billing_transactions`
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]domain.BillingTransaction, 0, limit+1)
	for rows.Next() {
		item, err := scanBillingTransaction(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, *item)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = pagination.Encode(last.CreatedAt, last.ID)
	}
	return items, next, rows.Err()
}

func (r *BillingAccountRepository) GetUsageEvent(ctx context.Context, id string, userID string) (*domain.BillingUsageEvent, error) {
	query := `SELECT ` + billingUsageColumns + ` FROM billing_usage_events WHERE id = $1::uuid`
	args := []any{id}
	if userID != "" {
		query += ` AND owner_user_id = $2::uuid`
		args = append(args, userID)
	}
	item, err := scanBillingUsageEvent(r.pool.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

func (r *BillingAccountRepository) RecordCompactionUsage(ctx context.Context, conversationID string, turnID string, requestKey string, execution domain.ModelExecutionSnapshot, result *llm.ModelResult, requestError string) error {
	requestKey = strings.TrimSpace(requestKey)
	if requestKey == "" {
		return domain.NewValidationError("compaction billing request key is required")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, requestKey); err != nil {
		return fmt.Errorf("lock compaction billing request key: %w", err)
	}
	var existing bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM billing_usage_events WHERE request_key = $1)`, requestKey).Scan(&existing); err != nil {
		return fmt.Errorf("load compaction billing request key: %w", err)
	}
	if existing {
		return tx.Commit(ctx)
	}
	var ownerUserID string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(owner_user_id::text, '') FROM conversations WHERE id = $1::uuid`, conversationID).Scan(&ownerUserID); err != nil {
		return fmt.Errorf("load compaction billing owner: %w", err)
	}
	usage := llm.ModelUsage{}
	responseID := ""
	if result != nil {
		usage = result.Usage
		responseID = result.ResponseID
	}
	charge, err := billing.QuoteSnapshot(execution.PricingSnapshot, usage)
	if err != nil {
		return err
	}
	status := "completed"
	if requestError != "" {
		status = "failed"
	}
	amount := charge.AmountNanos
	currency := charge.Currency
	billingTransactionID := ""
	if requestError == "" {
		billingTransactionID, err = captureUsageCharge(ctx, tx, ownerUserID, charge)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO billing_usage_events (
			request_key, owner_user_id, conversation_id, turn_id, workflow, provider,
			model_id, model_revision, model_price_id, upstream_model, provider_response_id,
			status, currency, amount_nanos, input_tokens, cache_read_input_tokens, cache_creation_input_tokens, output_tokens,
			reasoning_output_tokens, total_tokens, pricing_snapshot, usage, billing_transaction_id,
			error_code
		) VALUES ($1, NULLIF($2, '')::uuid, $3::uuid, NULLIF($4, '')::uuid, 'compaction', $5,
			NULLIF($6, '')::uuid, NULLIF($7, 0), NULLIF($8, '')::uuid, $9, $10, $11,
			NULLIF($12, ''), $13, $14, $15, $16, $17, $18, $19, $20::jsonb, $21::jsonb,
			NULLIF($22, '')::uuid, $23)
		ON CONFLICT (request_key) DO NOTHING
	`, requestKey, ownerUserID, conversationID, turnID, execution.Provider, execution.ModelID,
		execution.ModelRevision, execution.ModelPriceID, execution.UpstreamModel, responseID, status,
		currency, amount, usage.InputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens, usage.OutputTokens,
		usage.ReasoningOutputTokens, usage.TotalTokens, normalizedJSON(execution.PricingSnapshot),
		normalizedJSON(usage.Raw), billingTransactionID, boolErrorCode(requestError)); err != nil {
		return fmt.Errorf("insert compaction billing usage: %w", err)
	}
	return tx.Commit(ctx)
}

func captureUsageCharge(ctx context.Context, tx pgx.Tx, userID string, charge *billing.Charge) (string, error) {
	if charge == nil {
		return "", nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_accounts (user_id, currency) VALUES ($1::uuid, $2)
		ON CONFLICT (user_id, currency) DO NOTHING
	`, userID, charge.Currency); err != nil {
		return "", fmt.Errorf("ensure usage billing account: %w", err)
	}
	var accountID string
	var balance, version int64
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, balance_nanos, version, status
		FROM billing_accounts WHERE user_id = $1::uuid AND currency = $2 FOR UPDATE
	`, userID, charge.Currency).Scan(&accountID, &balance, &version, &status); err != nil {
		return "", fmt.Errorf("lock usage billing account: %w", err)
	}
	if status != "active" {
		return "", domain.NewPaymentRequiredError("billing account is not active")
	}
	if charge.AmountNanos == 0 {
		return "", nil
	}
	if charge.AmountNanos > balance {
		return "", domain.NewPaymentRequiredError("billing account balance is insufficient")
	}
	debitAmount := charge.AmountNanos
	newBalance := balance - debitAmount
	if _, err := tx.Exec(ctx, `UPDATE billing_accounts SET balance_nanos = $2, version = version + 1 WHERE id = $1::uuid`, accountID, newBalance); err != nil {
		return "", fmt.Errorf("debit usage billing account: %w", err)
	}
	var transactionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO billing_transactions (
			account_id, user_id, currency, account_sequence, kind, direction,
			amount_nanos, balance_after_nanos, reason
		) VALUES ($1::uuid, $2::uuid, $3, $4, 'model_usage_charge', 'debit', $5, $6, 'Model usage charge')
		RETURNING id::text
	`, accountID, userID, charge.Currency, version+1, debitAmount, newBalance).Scan(&transactionID); err != nil {
		return "", fmt.Errorf("insert usage billing transaction: %w", err)
	}
	return transactionID, nil
}

func boolErrorCode(message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	return "upstream_request_failed"
}

func (r *BillingAccountRepository) ListUsageEvents(ctx context.Context, params BillingListParams) ([]domain.BillingUsageEvent, string, error) {
	limit := clampLimit(params.Limit, 50, 200)
	decoded, err := pagination.Decode(params.Cursor)
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	conditions := []string{}
	args := []any{}
	if params.UserID != "" {
		args = append(args, params.UserID)
		conditions = append(conditions, fmt.Sprintf("owner_user_id = $%d::uuid", len(args)))
	}
	if params.Status != "" {
		args = append(args, params.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if decoded != nil {
		args = append(args, decoded.CreatedAt, decoded.ID)
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	query := `SELECT ` + billingUsageColumns + ` FROM billing_usage_events`
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]domain.BillingUsageEvent, 0, limit+1)
	for rows.Next() {
		item, err := scanBillingUsageEvent(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, *item)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = pagination.Encode(last.CreatedAt, last.ID)
	}
	return items, next, rows.Err()
}

func manualTransactionHash(params ManualBillingTransactionParams, currency string) [32]byte {
	payload := strings.Join([]string{params.UserID, params.Kind, currency, fmt.Sprint(params.AmountNanos), strings.TrimSpace(params.Reason), strings.TrimSpace(params.Reference)}, "\x00")
	return sha256.Sum256([]byte(payload))
}

func scanBillingAccount(row scanRow) (*domain.BillingAccount, error) {
	var item domain.BillingAccount
	err := row.Scan(&item.ID, &item.UserID, &item.Currency, &item.Status, &item.BalanceNanos,
		&item.Version, &item.CreatedAt, &item.UpdatedAt)
	item.Balance = billing.FormatAmount(item.BalanceNanos)
	return &item, err
}

func scanBillingTransaction(row scanRow) (*domain.BillingTransaction, error) {
	var item domain.BillingTransaction
	err := row.Scan(&item.ID, &item.AccountID, &item.UserID, &item.Currency, &item.AccountSequence,
		&item.Kind, &item.Direction, &item.AmountNanos, &item.BalanceAfterNanos, &item.ActorUserID,
		&item.Reason, &item.Reference, &item.CreatedAt)
	item.Amount = billing.FormatAmount(item.AmountNanos)
	item.BalanceAfter = billing.FormatAmount(item.BalanceAfterNanos)
	return &item, err
}

func scanBillingTransactionWithHash(row scanRow) (*domain.BillingTransaction, []byte, error) {
	var item domain.BillingTransaction
	var requestHash []byte
	err := row.Scan(&item.ID, &item.AccountID, &item.UserID, &item.Currency, &item.AccountSequence,
		&item.Kind, &item.Direction, &item.AmountNanos, &item.BalanceAfterNanos, &item.ActorUserID,
		&item.Reason, &item.Reference, &item.CreatedAt, &requestHash)
	item.Amount = billing.FormatAmount(item.AmountNanos)
	item.BalanceAfter = billing.FormatAmount(item.BalanceAfterNanos)
	return &item, requestHash, err
}

func scanBillingUsageEvent(row scanRow) (*domain.BillingUsageEvent, error) {
	var item domain.BillingUsageEvent
	err := row.Scan(&item.ID, &item.RequestKey, &item.OwnerUserID, &item.ConversationID, &item.TurnID,
		&item.TurnRunID, &item.Workflow, &item.Attempt, &item.Provider, &item.ModelID,
		&item.ModelRevision, &item.ModelPriceID, &item.UpstreamModel, &item.ProviderResponseID,
		&item.Status, &item.Currency, &item.AmountNanos, &item.InputTokens, &item.CacheReadInputTokens,
		&item.CacheCreationInputTokens, &item.OutputTokens, &item.ReasoningOutputTokens, &item.TotalTokens, &item.PricingSnapshot,
		&item.Usage, &item.BillingTransactionID, &item.ErrorCode, &item.CreatedAt)
	return &item, err
}
