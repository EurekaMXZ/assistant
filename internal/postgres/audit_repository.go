package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/pagination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditRepository struct {
	pool *pgxpool.Pool
}

type RecordAuditParams struct {
	ActorUserID      string
	ActorRole        string
	SubjectUserID    string
	Action           string
	ResourceType     string
	ResourceID       string
	Outcome          string
	RequestID        string
	ClientIP         string
	UserAgent        string
	Reason           string
	VisibleToSubject bool
	RequiredRole     string
	Metadata         json.RawMessage
}

type AuditListParams struct {
	ViewerUserID  string
	ViewerRole    string
	ActorUserID   string
	SubjectUserID string
	Action        string
	ResourceType  string
	Outcome       string
	Limit         int
	Cursor        string
}

func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

const auditColumns = `
	id::text, COALESCE(actor_user_id::text, ''), actor_role, COALESCE(subject_user_id::text, ''),
	action, resource_type, resource_id, outcome, request_id, COALESCE(client_ip::text, ''),
	user_agent, reason, visible_to_subject, required_role, metadata, created_at`

func (r *AuditRepository) Record(ctx context.Context, params RecordAuditParams) (*domain.AuditEvent, error) {
	var ip any
	if parsed := net.ParseIP(strings.TrimSpace(params.ClientIP)); parsed != nil {
		ip = parsed.String()
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO audit_events (
			actor_user_id, actor_role, subject_user_id, action, resource_type, resource_id,
			outcome, request_id, client_ip, user_agent, reason, visible_to_subject, required_role, metadata
		) VALUES (NULLIF($1, '')::uuid, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8,
			$9::inet, $10, $11, $12, $13, $14::jsonb)
		RETURNING `+auditColumns,
		params.ActorUserID, params.ActorRole, params.SubjectUserID, params.Action, params.ResourceType,
		params.ResourceID, params.Outcome, params.RequestID, ip, params.UserAgent, params.Reason,
		params.VisibleToSubject, normalizeRequiredRole(params.RequiredRole), normalizedJSON(params.Metadata))
	item, err := scanAuditEvent(row)
	if err != nil {
		return nil, fmt.Errorf("record audit event: %w", err)
	}
	return item, nil
}

func (r *AuditRepository) Get(ctx context.Context, id string, viewerUserID string, viewerRole string) (*domain.AuditEvent, error) {
	query := `SELECT ` + auditColumns + ` FROM audit_events WHERE id = $1::uuid`
	args := []any{id}
	switch viewerRole {
	case domain.UserRoleSystem:
	case domain.UserRoleAdmin:
		query += ` AND required_role <> 'system'`
	default:
		query += ` AND (actor_user_id = $2::uuid OR (subject_user_id = $2::uuid AND visible_to_subject))`
		query += ` AND required_role <> 'system'`
		args = append(args, viewerUserID)
	}
	item, err := scanAuditEvent(r.pool.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if viewerRole == domain.UserRoleUser && item.ActorUserID != viewerUserID {
		item.ActorUserID = ""
		if item.ActorRole != "" {
			item.ActorRole = "administrator"
		}
	}
	return item, nil
}

func (r *AuditRepository) List(ctx context.Context, params AuditListParams) ([]domain.AuditEvent, string, error) {
	limit := clampLimit(params.Limit, 50, 200)
	decoded, err := pagination.Decode(params.Cursor)
	if err != nil {
		return nil, "", domain.NewValidationError("invalid cursor")
	}
	conditions := []string{}
	args := []any{}
	switch params.ViewerRole {
	case domain.UserRoleSystem:
	case domain.UserRoleAdmin:
		conditions = append(conditions, "required_role <> 'system'")
	default:
		args = append(args, params.ViewerUserID)
		conditions = append(conditions, fmt.Sprintf("(actor_user_id = $%d::uuid OR (subject_user_id = $%d::uuid AND visible_to_subject))", len(args), len(args)))
		conditions = append(conditions, "required_role <> 'system'")
	}
	if params.ViewerRole == domain.UserRoleAdmin || params.ViewerRole == domain.UserRoleSystem {
		if params.ActorUserID != "" {
			args = append(args, params.ActorUserID)
			conditions = append(conditions, fmt.Sprintf("actor_user_id = $%d::uuid", len(args)))
		}
		if params.SubjectUserID != "" {
			args = append(args, params.SubjectUserID)
			conditions = append(conditions, fmt.Sprintf("subject_user_id = $%d::uuid", len(args)))
		}
	}
	for column, value := range map[string]string{"action": params.Action, "resource_type": params.ResourceType, "outcome": params.Outcome} {
		if value == "" {
			continue
		}
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if decoded != nil {
		args = append(args, decoded.CreatedAt, decoded.ID)
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	query := `SELECT ` + auditColumns + ` FROM audit_events`
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
	items := make([]domain.AuditEvent, 0, limit+1)
	for rows.Next() {
		item, err := scanAuditEvent(rows)
		if err != nil {
			return nil, "", err
		}
		if params.ViewerRole == domain.UserRoleUser && item.ActorUserID != params.ViewerUserID {
			item.ActorUserID = ""
			if item.ActorRole != "" {
				item.ActorRole = "administrator"
			}
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

func scanAuditEvent(row scanRow) (*domain.AuditEvent, error) {
	var item domain.AuditEvent
	err := row.Scan(&item.ID, &item.ActorUserID, &item.ActorRole, &item.SubjectUserID, &item.Action,
		&item.ResourceType, &item.ResourceID, &item.Outcome, &item.RequestID, &item.ClientIP,
		&item.UserAgent, &item.Reason, &item.VisibleToSubject, &item.RequiredRole, &item.Metadata, &item.CreatedAt)
	return &item, err
}

func normalizeRequiredRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case domain.UserRoleAdmin:
		return domain.UserRoleAdmin
	case domain.UserRoleSystem:
		return domain.UserRoleSystem
	default:
		return domain.UserRoleUser
	}
}
