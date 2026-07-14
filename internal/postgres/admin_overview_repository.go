package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type AdminOverviewCounts struct {
	Users          int64
	EnabledModels  int64
	Credentials    int64
	ActiveAccounts int64
	AuditEvents    int64
}

type AdminOverviewRepository struct {
	pool queryRower
}

func NewAdminOverviewRepository(pool queryRower) *AdminOverviewRepository {
	return &AdminOverviewRepository{pool: pool}
}

func (r *AdminOverviewRepository) GetCounts(ctx context.Context, includeSystemAudit bool) (*AdminOverviewCounts, error) {
	var counts AdminOverviewCounts
	err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM models WHERE status = 'enabled'),
			(SELECT count(*) FROM provider_credentials WHERE status <> 'revoked'),
			(SELECT count(*) FROM billing_accounts WHERE status = 'active'),
			(SELECT count(*) FROM audit_events WHERE $1 OR required_role <> 'system')
	`, includeSystemAudit).Scan(
		&counts.Users,
		&counts.EnabledModels,
		&counts.Credentials,
		&counts.ActiveAccounts,
		&counts.AuditEvents,
	)
	if err != nil {
		return nil, fmt.Errorf("get admin overview counts: %w", err)
	}
	return &counts, nil
}
