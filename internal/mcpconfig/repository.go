package mcpconfig

import (
	"context"
	"encoding/json"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type RuntimeTool struct {
	ServerID            string          `json:"server_id"`
	ServerName          string          `json:"server_name"`
	ServerSlug          string          `json:"server_slug"`
	EndpointURL         string          `json:"-"`
	ToolName            string          `json:"tool_name"`
	Description         string          `json:"description"`
	InputSchema         json.RawMessage `json:"input_schema"`
	EncryptedParameters []byte          `json:"-"`
	ParametersNonce     []byte          `json:"-"`
	EncryptedHeaders    []byte          `json:"-"`
	HeadersNonce        []byte          `json:"-"`
}

type RuntimeRepository interface {
	ListEnabledRuntimeTools(ctx context.Context, ownerUserID string) ([]RuntimeTool, error)
	GetEnabledRuntimeTool(ctx context.Context, ownerUserID string, serverID string, toolName string) (*RuntimeTool, error)
}

type Repository interface {
	Create(ctx context.Context, server domain.UserMCPServer) (*domain.UserMCPServer, error)
	List(ctx context.Context, ownerUserID string) ([]domain.UserMCPServer, error)
	Get(ctx context.Context, ownerUserID string, serverID string) (*domain.UserMCPServer, error)
	Update(ctx context.Context, ownerUserID string, server domain.UserMCPServer, expectedRevision int64, enabledTools *[]string) (*domain.UserMCPServer, error)
	Delete(ctx context.Context, ownerUserID string, serverID string) error
	RecordValidation(ctx context.Context, ownerUserID string, serverID string, status string, validationError string, tools []domain.UserMCPTool) (*domain.UserMCPServer, error)
	ListTools(ctx context.Context, ownerUserID string, serverID string) ([]domain.UserMCPTool, error)
}

type ToolLister interface {
	ListTools(ctx context.Context, endpointURL string, parameters map[string]string, headers map[string]string) ([]domain.UserMCPTool, error)
}
