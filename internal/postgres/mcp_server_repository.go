package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/mcpconfig"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const userMCPServerColumns = `
	id::text, owner_user_id::text, name, slug, endpoint_url, enabled, revision,
	encrypted_parameters, parameters_nonce, encrypted_headers, headers_nonce,
	last_validation_status, COALESCE(last_validation_error, ''), last_validated_at,
	created_at, updated_at`

const userMCPToolColumns = `
	t.server_id::text, t.name, t.description, t.input_schema, t.enabled, t.created_at, t.updated_at`

type MCPServerRepository struct {
	pool *pgxpool.Pool
}

var _ mcpconfig.Repository = (*MCPServerRepository)(nil)
var _ mcpconfig.RuntimeRepository = (*MCPServerRepository)(nil)

func NewMCPServerRepository(pool *pgxpool.Pool) *MCPServerRepository {
	return &MCPServerRepository{pool: pool}
}

func (r *MCPServerRepository) Create(ctx context.Context, server domain.UserMCPServer) (*domain.UserMCPServer, error) {
	stored, err := scanUserMCPServer(r.pool.QueryRow(ctx, `
		INSERT INTO user_mcp_servers (
			id, owner_user_id, name, slug, endpoint_url, enabled,
			encrypted_parameters, parameters_nonce, encrypted_headers, headers_nonce
		) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+userMCPServerColumns,
		server.ID, server.OwnerUserID, server.Name, server.Slug, server.EndpointURL, server.Enabled,
		server.EncryptedParameters, server.ParametersNonce, server.EncryptedHeaders, server.HeadersNonce))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewConflictError("MCP server slug already exists")
		}
		return nil, fmt.Errorf("create user MCP server: %w", err)
	}
	return stored, nil
}

func (r *MCPServerRepository) List(ctx context.Context, ownerUserID string) ([]domain.UserMCPServer, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+userMCPServerColumns+`
		FROM user_mcp_servers
		WHERE owner_user_id = $1::uuid
		ORDER BY updated_at DESC, id DESC
	`, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("list user MCP servers: %w", err)
	}
	defer rows.Close()
	servers := make([]domain.UserMCPServer, 0)
	for rows.Next() {
		server, err := scanUserMCPServer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user MCP server: %w", err)
		}
		servers = append(servers, *server)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list user MCP servers: %w", err)
	}
	return servers, nil
}

func (r *MCPServerRepository) Get(ctx context.Context, ownerUserID string, serverID string) (*domain.UserMCPServer, error) {
	server, err := scanUserMCPServer(r.pool.QueryRow(ctx, `
		SELECT `+userMCPServerColumns+`
		FROM user_mcp_servers
		WHERE id = $1::uuid AND owner_user_id = $2::uuid
	`, serverID, ownerUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user MCP server: %w", err)
	}
	return server, nil
}

func (r *MCPServerRepository) Update(ctx context.Context, ownerUserID string, server domain.UserMCPServer, expectedRevision int64, enabledTools *[]string) (*domain.UserMCPServer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin user MCP server update: %w", err)
	}
	defer tx.Rollback(ctx)

	stored, err := scanUserMCPServer(tx.QueryRow(ctx, `
		UPDATE user_mcp_servers SET
			name = $3, slug = $4, endpoint_url = $5, enabled = $6,
			encrypted_parameters = $7, parameters_nonce = $8,
			encrypted_headers = $9, headers_nonce = $10,
			last_validation_status = $11,
			last_validation_error = NULLIF($12, ''), last_validated_at = $13,
			revision = revision + 1
		WHERE id = $1::uuid AND owner_user_id = $2::uuid AND revision = $14
		RETURNING `+userMCPServerColumns,
		server.ID, ownerUserID, server.Name, server.Slug, server.EndpointURL, server.Enabled,
		server.EncryptedParameters, server.ParametersNonce, server.EncryptedHeaders, server.HeadersNonce,
		server.LastValidationStatus, server.LastValidationError, server.LastValidatedAt, expectedRevision))
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if getErr := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM user_mcp_servers
				WHERE id = $1::uuid AND owner_user_id = $2::uuid
			)
		`, server.ID, ownerUserID).Scan(&exists); getErr != nil {
			return nil, fmt.Errorf("check user MCP server revision: %w", getErr)
		}
		if !exists {
			return nil, domain.ErrNotFound
		}
		return nil, domain.NewConflictError("MCP server was modified; reload and retry")
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewConflictError("MCP server slug already exists")
		}
		return nil, fmt.Errorf("update user MCP server: %w", err)
	}

	if enabledTools != nil {
		var matching int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			FROM user_mcp_tools t
			JOIN user_mcp_servers s ON s.id = t.server_id
			WHERE t.server_id = $1::uuid AND s.owner_user_id = $2::uuid
				AND t.name = ANY($3::text[])
		`, server.ID, ownerUserID, *enabledTools).Scan(&matching); err != nil {
			return nil, fmt.Errorf("validate enabled MCP tools: %w", err)
		}
		if matching != len(*enabledTools) {
			return nil, domain.NewValidationError("enabled_tools contains an unknown tool")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE user_mcp_tools t
			SET enabled = t.name = ANY($3::text[])
			FROM user_mcp_servers s
			WHERE s.id = t.server_id AND t.server_id = $1::uuid AND s.owner_user_id = $2::uuid
		`, server.ID, ownerUserID, *enabledTools); err != nil {
			return nil, fmt.Errorf("update enabled MCP tools: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit user MCP server update: %w", err)
	}
	return stored, nil
}

func (r *MCPServerRepository) Delete(ctx context.Context, ownerUserID string, serverID string) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM user_mcp_servers
		WHERE id = $1::uuid AND owner_user_id = $2::uuid
	`, serverID, ownerUserID)
	if err != nil {
		return fmt.Errorf("delete user MCP server: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *MCPServerRepository) RecordValidation(ctx context.Context, ownerUserID string, serverID string, status string, validationError string, tools []domain.UserMCPTool) (*domain.UserMCPServer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin MCP validation update: %w", err)
	}
	defer tx.Rollback(ctx)

	stored, err := scanUserMCPServer(tx.QueryRow(ctx, `
		UPDATE user_mcp_servers SET
			last_validation_status = $3, last_validation_error = NULLIF($4, ''),
			last_validated_at = now(), revision = revision + 1
		WHERE id = $1::uuid AND owner_user_id = $2::uuid
		RETURNING `+userMCPServerColumns,
		serverID, ownerUserID, status, validationError))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("record MCP validation: %w", err)
	}

	if tools != nil {
		names := make([]string, 0, len(tools))
		for _, tool := range tools {
			result, err := tx.Exec(ctx, `
				INSERT INTO user_mcp_tools (server_id, name, description, input_schema)
				SELECT s.id, $3, $4, $5::jsonb
				FROM user_mcp_servers s
				WHERE s.id = $1::uuid AND s.owner_user_id = $2::uuid
				ON CONFLICT (server_id, name) DO UPDATE SET
					description = EXCLUDED.description,
					input_schema = EXCLUDED.input_schema
			`, serverID, ownerUserID, tool.Name, tool.Description, string(tool.InputSchema))
			if err != nil {
				return nil, fmt.Errorf("sync MCP tool: %w", err)
			}
			if result.RowsAffected() == 0 {
				return nil, domain.ErrNotFound
			}
			names = append(names, tool.Name)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM user_mcp_tools t
			USING user_mcp_servers s
			WHERE s.id = t.server_id AND t.server_id = $1::uuid
				AND s.owner_user_id = $2::uuid AND NOT (t.name = ANY($3::text[]))
		`, serverID, ownerUserID, names); err != nil {
			return nil, fmt.Errorf("delete stale MCP tools: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit MCP validation update: %w", err)
	}
	return stored, nil
}

func (r *MCPServerRepository) ListTools(ctx context.Context, ownerUserID string, serverID string) ([]domain.UserMCPTool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+userMCPToolColumns+`
		FROM user_mcp_tools t
		JOIN user_mcp_servers s ON s.id = t.server_id
		WHERE t.server_id = $1::uuid AND s.owner_user_id = $2::uuid
		ORDER BY t.name
	`, serverID, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("list user MCP tools: %w", err)
	}
	defer rows.Close()
	tools := make([]domain.UserMCPTool, 0)
	for rows.Next() {
		tool, err := scanUserMCPTool(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user MCP tool: %w", err)
		}
		tools = append(tools, *tool)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list user MCP tools: %w", err)
	}
	return tools, nil
}

func (r *MCPServerRepository) ListEnabledRuntimeTools(ctx context.Context, ownerUserID string) ([]mcpconfig.RuntimeTool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id::text, s.name, s.slug, t.name, t.description, t.input_schema
		FROM user_mcp_servers s
		JOIN user_mcp_tools t ON t.server_id = s.id
		WHERE s.owner_user_id = $1::uuid AND s.enabled AND t.enabled
		ORDER BY s.id, t.name
	`, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("list enabled runtime MCP tools: %w", err)
	}
	defer rows.Close()
	tools := make([]mcpconfig.RuntimeTool, 0)
	for rows.Next() {
		var runtimeTool mcpconfig.RuntimeTool
		if err := rows.Scan(
			&runtimeTool.ServerID, &runtimeTool.ServerName, &runtimeTool.ServerSlug,
			&runtimeTool.ToolName, &runtimeTool.Description, &runtimeTool.InputSchema,
		); err != nil {
			return nil, fmt.Errorf("scan enabled runtime MCP tool: %w", err)
		}
		tools = append(tools, runtimeTool)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list enabled runtime MCP tools: %w", err)
	}
	return tools, nil
}

func (r *MCPServerRepository) GetEnabledRuntimeTool(ctx context.Context, ownerUserID string, serverID string, toolName string) (*mcpconfig.RuntimeTool, error) {
	var runtimeTool mcpconfig.RuntimeTool
	err := r.pool.QueryRow(ctx, `
		SELECT s.id::text, s.name, s.slug, s.endpoint_url,
			s.encrypted_parameters, s.parameters_nonce, s.encrypted_headers, s.headers_nonce,
			t.name, t.description, t.input_schema
		FROM user_mcp_servers s
		JOIN user_mcp_tools t ON t.server_id = s.id
		WHERE s.id = $1::uuid AND s.owner_user_id = $2::uuid
			AND t.name = $3 AND s.enabled AND t.enabled
	`, serverID, ownerUserID, toolName).Scan(
		&runtimeTool.ServerID, &runtimeTool.ServerName, &runtimeTool.ServerSlug, &runtimeTool.EndpointURL,
		&runtimeTool.EncryptedParameters, &runtimeTool.ParametersNonce,
		&runtimeTool.EncryptedHeaders, &runtimeTool.HeadersNonce,
		&runtimeTool.ToolName, &runtimeTool.Description, &runtimeTool.InputSchema,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get enabled runtime MCP tool: %w", err)
	}
	return &runtimeTool, nil
}

func scanUserMCPServer(row scanRow) (*domain.UserMCPServer, error) {
	var server domain.UserMCPServer
	err := row.Scan(
		&server.ID, &server.OwnerUserID, &server.Name, &server.Slug, &server.EndpointURL,
		&server.Enabled, &server.Revision, &server.EncryptedParameters, &server.ParametersNonce,
		&server.EncryptedHeaders, &server.HeadersNonce, &server.LastValidationStatus,
		&server.LastValidationError, &server.LastValidatedAt, &server.CreatedAt, &server.UpdatedAt,
	)
	return &server, err
}

func scanUserMCPTool(row scanRow) (*domain.UserMCPTool, error) {
	var tool domain.UserMCPTool
	err := row.Scan(
		&tool.ServerID, &tool.Name, &tool.Description, &tool.InputSchema,
		&tool.Enabled, &tool.CreatedAt, &tool.UpdatedAt,
	)
	return &tool, err
}
