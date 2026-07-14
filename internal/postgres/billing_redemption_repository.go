package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/billing"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/pagination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type IssueRedemptionCodeParams struct {
	ActorUserID string
	ActorRole   string
	Currency    string
	AmountNanos int64
	Quantity    int
	ExpiresAt   *time.Time
	RequestID   string
}

type RedemptionCodeListParams struct {
	Limit  int
	Cursor string
}

type DisableRedemptionCodeParams struct {
	CodeID      string
	ActorUserID string
	ActorRole   string
	RequestID   string
}

const billingRedemptionCodeColumns = `
	c.id::text, c.code_hint, c.currency, c.amount_nanos,
	CASE
		WHEN t.id IS NOT NULL THEN 'redeemed'
		WHEN d.redemption_code_id IS NOT NULL THEN 'disabled'
		WHEN c.expires_at IS NOT NULL AND c.expires_at <= now() THEN 'expired'
		ELSE 'active'
	END,
	c.created_by_user_id::text, COALESCE(t.user_id::text, ''), COALESCE(t.id::text, ''),
	COALESCE(d.disabled_by_user_id::text, ''), c.expires_at, t.created_at, d.created_at, c.created_at`

func (r *BillingAccountRepository) IssueRedemptionCode(ctx context.Context, params IssueRedemptionCodeParams) (*domain.BillingRedemptionCodeIssue, error) {
	params.Quantity = 1
	items, err := r.IssueRedemptionCodes(ctx, params)
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (r *BillingAccountRepository) IssueRedemptionCodes(ctx context.Context, params IssueRedemptionCodeParams) ([]domain.BillingRedemptionCodeIssue, error) {
	if params.AmountNanos <= 0 {
		return nil, domain.NewValidationError("redemption amount must be greater than zero")
	}
	if params.Quantity < 1 || params.Quantity > 100 {
		return nil, domain.NewValidationError("redemption code quantity must be between 1 and 100")
	}
	currency := strings.ToUpper(strings.TrimSpace(params.Currency))

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin redemption code issue: %w", err)
	}
	defer tx.Rollback(ctx)

	var validExpiry bool
	if err := tx.QueryRow(ctx, `
		SELECT $1::timestamptz IS NULL OR $1::timestamptz > clock_timestamp()
	`, params.ExpiresAt).Scan(&validExpiry); err != nil {
		return nil, fmt.Errorf("validate redemption code expiry: %w", err)
	}
	if !validExpiry {
		return nil, domain.NewValidationError("redemption code expiry must be in the future")
	}

	issues := make([]domain.BillingRedemptionCodeIssue, 0, params.Quantity)
	codeIDs := make([]string, 0, params.Quantity)
	for range params.Quantity {
		code, hash, hint, err := billing.GenerateRedemptionCode()
		if err != nil {
			return nil, fmt.Errorf("generate redemption code: %w", err)
		}
		var item domain.BillingRedemptionCode
		if err := tx.QueryRow(ctx, `
			INSERT INTO billing_redemption_codes (
				code_hash, code_hint, currency, amount_nanos, created_by_user_id, expires_at
			) VALUES ($1, $2, $3, $4, $5::uuid, $6::timestamptz)
			RETURNING id::text, code_hint, currency, amount_nanos, created_by_user_id::text, expires_at, created_at
		`, hash[:], hint, currency, params.AmountNanos, params.ActorUserID, params.ExpiresAt).Scan(
			&item.ID, &item.CodeHint, &item.Currency, &item.AmountNanos, &item.CreatedByUserID,
			&item.ExpiresAt, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("insert redemption code: %w", err)
		}
		item.Amount = billing.FormatAmount(item.AmountNanos)
		item.Status = domain.BillingRedemptionCodeActive
		issues = append(issues, domain.BillingRedemptionCodeIssue{RedemptionCode: item, Code: code})
		codeIDs = append(codeIDs, item.ID)
	}

	metadata, _ := json.Marshal(map[string]any{
		"amount_nanos": params.AmountNanos,
		"currency":     currency,
		"expires_at":   params.ExpiresAt,
		"quantity":     params.Quantity,
		"code_ids":     codeIDs,
	})
	action := "billing.redemption_code.issue"
	resourceType := "billing_redemption_code"
	if params.Quantity > 1 {
		action = "billing.redemption_code.issue_batch"
		resourceType = "billing_redemption_code_batch"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_user_id, actor_role, action, resource_type, resource_id, outcome,
			request_id, visible_to_subject, required_role, metadata
		) VALUES ($1::uuid, $2, $3, $4, $5, 'succeeded', $6, false, 'admin', $7::jsonb)
	`, params.ActorUserID, params.ActorRole, action, resourceType, codeIDs[0], params.RequestID, metadata); err != nil {
		return nil, fmt.Errorf("audit redemption code issue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit redemption code issue: %w", err)
	}
	return issues, nil
}

func (r *BillingAccountRepository) ListRedemptionCodes(ctx context.Context, params RedemptionCodeListParams) ([]domain.BillingRedemptionCode, string, error) {
	limit := clampLimit(params.Limit, 50, 200)
	decoded, err := pagination.Decode(params.Cursor)
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	query := `
		SELECT ` + billingRedemptionCodeColumns + `
		FROM billing_redemption_codes c
		LEFT JOIN billing_transactions t ON t.redemption_code_id = c.id
		LEFT JOIN billing_redemption_code_disables d ON d.redemption_code_id = c.id`
	args := []any{}
	if decoded != nil {
		query += ` WHERE (c.created_at, c.id) < ($1, $2::uuid)`
		args = append(args, decoded.CreatedAt, decoded.ID)
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY c.created_at DESC, c.id DESC LIMIT $%d`, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list redemption codes: %w", err)
	}
	defer rows.Close()

	items := make([]domain.BillingRedemptionCode, 0, limit+1)
	for rows.Next() {
		item, err := scanBillingRedemptionCode(rows)
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

func (r *BillingAccountRepository) DisableRedemptionCode(ctx context.Context, params DisableRedemptionCodeParams) (*domain.BillingRedemptionCode, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin redemption code disable: %w", err)
	}
	defer tx.Rollback(ctx)

	var expired, redeemed, disabled bool
	if err := tx.QueryRow(ctx, `
		SELECT
			c.expires_at IS NOT NULL AND c.expires_at <= clock_timestamp(),
			EXISTS (SELECT 1 FROM billing_transactions t WHERE t.redemption_code_id = c.id),
			EXISTS (SELECT 1 FROM billing_redemption_code_disables d WHERE d.redemption_code_id = c.id)
		FROM billing_redemption_codes c
		WHERE c.id = $1::uuid
		FOR UPDATE
	`, params.CodeID).Scan(&expired, &redeemed, &disabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock redemption code for disable: %w", err)
	}
	if redeemed {
		return nil, domain.NewConflictError("redeemed redemption code cannot be disabled")
	}
	if expired && !disabled {
		return nil, domain.NewConflictError("expired redemption code cannot be disabled")
	}
	if !disabled {
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_redemption_code_disables (redemption_code_id, disabled_by_user_id)
			VALUES ($1::uuid, $2::uuid)
		`, params.CodeID, params.ActorUserID); err != nil {
			return nil, fmt.Errorf("insert redemption code disable: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (
				actor_user_id, actor_role, action, resource_type, resource_id, outcome,
				request_id, visible_to_subject, required_role, metadata
			) VALUES ($1::uuid, $2, 'billing.redemption_code.disable', 'billing_redemption_code',
				$3, 'succeeded', $4, false, 'admin', '{}'::jsonb)
		`, params.ActorUserID, params.ActorRole, params.CodeID, params.RequestID); err != nil {
			return nil, fmt.Errorf("audit redemption code disable: %w", err)
		}
	}

	item, err := scanBillingRedemptionCode(tx.QueryRow(ctx, `
		SELECT `+billingRedemptionCodeColumns+`
		FROM billing_redemption_codes c
		LEFT JOIN billing_transactions t ON t.redemption_code_id = c.id
		LEFT JOIN billing_redemption_code_disables d ON d.redemption_code_id = c.id
		WHERE c.id = $1::uuid
	`, params.CodeID))
	if err != nil {
		return nil, fmt.Errorf("load disabled redemption code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit redemption code disable: %w", err)
	}
	return item, nil
}

func (r *BillingAccountRepository) RedeemCode(ctx context.Context, userID string, actorRole string, code string, requestID string) (*domain.BillingRedemptionResult, error) {
	hash, err := billing.HashRedemptionCode(code)
	if err != nil {
		return nil, invalidRedemptionCodeError()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin redemption: %w", err)
	}
	defer tx.Rollback(ctx)

	var codeID, codeHint, currency string
	var amountNanos int64
	if err := tx.QueryRow(ctx, `
		SELECT id::text, code_hint, currency, amount_nanos
		FROM billing_redemption_codes
		WHERE code_hash = $1
		FOR UPDATE
	`, hash[:]).Scan(&codeID, &codeHint, &currency, &amountNanos); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invalidRedemptionCodeError()
		}
		return nil, fmt.Errorf("lock redemption code: %w", err)
	}

	existing, err := scanBillingTransaction(tx.QueryRow(ctx, `
		SELECT `+billingTransactionColumns+`
		FROM billing_transactions
		WHERE redemption_code_id = $1::uuid
	`, codeID))
	if err == nil {
		if existing.UserID != userID {
			return nil, invalidRedemptionCodeError()
		}
		account, err := scanBillingAccount(tx.QueryRow(ctx, `
			SELECT `+billingAccountColumns+`
			FROM billing_accounts WHERE id = $1::uuid
		`, existing.AccountID))
		if err != nil {
			return nil, fmt.Errorf("load redeemed billing account: %w", err)
		}
		metadata, _ := json.Marshal(map[string]any{"billing_transaction_id": existing.ID})
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (
				actor_user_id, actor_role, subject_user_id, action, resource_type, resource_id,
				outcome, request_id, visible_to_subject, required_role, metadata
			) VALUES ($1::uuid, $2, $1::uuid, 'billing.redemption_code.replay',
				'billing_redemption_code', $3, 'succeeded', $4, true, 'user', $5::jsonb)
		`, userID, actorRole, codeID, requestID, metadata); err != nil {
			return nil, fmt.Errorf("audit redemption replay: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit redemption replay: %w", err)
		}
		return &domain.BillingRedemptionResult{Account: *account, Transaction: *existing, Replayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load redemption transaction: %w", err)
	}
	var disabled bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM billing_redemption_code_disables WHERE redemption_code_id = $1::uuid
		)
	`, codeID).Scan(&disabled); err != nil {
		return nil, fmt.Errorf("check redemption code disabled status: %w", err)
	}
	if disabled {
		return nil, invalidRedemptionCodeError()
	}
	var expired bool
	if err := tx.QueryRow(ctx, `
		SELECT expires_at IS NOT NULL AND expires_at <= clock_timestamp()
		FROM billing_redemption_codes WHERE id = $1::uuid
	`, codeID).Scan(&expired); err != nil {
		return nil, fmt.Errorf("check redemption code expiry: %w", err)
	}
	if expired {
		return nil, invalidRedemptionCodeError()
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_accounts (user_id, currency)
		VALUES ($1::uuid, $2)
		ON CONFLICT (user_id, currency) DO NOTHING
	`, userID, currency); err != nil {
		return nil, fmt.Errorf("ensure redemption billing account: %w", err)
	}
	var accountID, accountStatus string
	var balance, version int64
	if err := tx.QueryRow(ctx, `
		SELECT id::text, balance_nanos, version, status
		FROM billing_accounts
		WHERE user_id = $1::uuid AND currency = $2
		FOR UPDATE
	`, userID, currency).Scan(&accountID, &balance, &version, &accountStatus); err != nil {
		return nil, fmt.Errorf("lock redemption billing account: %w", err)
	}
	if accountStatus != "active" {
		return nil, domain.NewConflictError("billing account is not active")
	}
	if amountNanos > math.MaxInt64-balance {
		return nil, domain.NewConflictError("billing account balance limit exceeded")
	}
	newBalance := balance + amountNanos
	account, err := scanBillingAccount(tx.QueryRow(ctx, `
		UPDATE billing_accounts
		SET balance_nanos = $2, version = version + 1
		WHERE id = $1::uuid
		RETURNING `+billingAccountColumns,
		accountID, newBalance))
	if err != nil {
		return nil, fmt.Errorf("update redemption balance: %w", err)
	}
	transaction, err := scanBillingTransaction(tx.QueryRow(ctx, `
		INSERT INTO billing_transactions (
			account_id, user_id, currency, account_sequence, kind, direction, amount_nanos,
			balance_after_nanos, actor_user_id, reason, reference, redemption_code_id
		) VALUES ($1::uuid, $2::uuid, $3, $4, 'redemption_credit', 'credit', $5,
			$6, $2::uuid, 'Redemption code', $7, $8::uuid)
		RETURNING `+billingTransactionColumns,
		accountID, userID, currency, version+1, amountNanos, newBalance, codeHint, codeID))
	if err != nil {
		return nil, fmt.Errorf("insert redemption transaction: %w", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"amount_nanos":           amountNanos,
		"currency":               currency,
		"billing_transaction_id": transaction.ID,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_user_id, actor_role, subject_user_id, action, resource_type, resource_id,
			outcome, request_id, visible_to_subject, required_role, metadata
		) VALUES ($1::uuid, $2, $1::uuid, 'billing.redemption_code.redeem',
			'billing_redemption_code', $3, 'succeeded', $4, true, 'user', $5::jsonb)
	`, userID, actorRole, codeID, requestID, metadata); err != nil {
		return nil, fmt.Errorf("audit redemption: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit redemption: %w", err)
	}
	return &domain.BillingRedemptionResult{Account: *account, Transaction: *transaction}, nil
}

func invalidRedemptionCodeError() error {
	return domain.NewValidationError("redemption code is invalid or unavailable")
}

func scanBillingRedemptionCode(row scanRow) (*domain.BillingRedemptionCode, error) {
	var item domain.BillingRedemptionCode
	var expiresAt, redeemedAt, disabledAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.CodeHint, &item.Currency, &item.AmountNanos, &item.Status,
		&item.CreatedByUserID, &item.RedeemedByUserID, &item.BillingTransactionID,
		&item.DisabledByUserID, &expiresAt, &redeemedAt, &disabledAt, &item.CreatedAt,
	)
	if expiresAt.Valid {
		value := expiresAt.Time
		item.ExpiresAt = &value
	}
	if redeemedAt.Valid {
		value := redeemedAt.Time
		item.RedeemedAt = &value
	}
	if disabledAt.Valid {
		value := disabledAt.Time
		item.DisabledAt = &value
	}
	item.Amount = billing.FormatAmount(item.AmountNanos)
	return &item, err
}
