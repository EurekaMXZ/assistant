package tool

import (
	"context"
	"encoding/json"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type ConversationSandboxReader interface {
	GetActiveConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error)
}

type ConversationSandboxStore interface {
	ConversationSandboxReader
	GetLatestConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error)
	CreateConversationSandbox(ctx context.Context, conversationID string, provider string, runtimeID string, metadata json.RawMessage) (*domain.ConversationSandbox, error)
	DestroyConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error)
	RestoreConversationSandbox(ctx context.Context, sandboxID string, metadata json.RawMessage) (*domain.ConversationSandbox, error)
}

type SandboxManager interface {
	CreateSandbox(ctx context.Context, conversationID string, requestKey string) (*domain.SandboxHandle, error)
	DestroySandbox(ctx context.Context, handle domain.SandboxHandle, requestKey string) (*domain.SandboxHandle, error)
	ExecSandboxCommand(ctx context.Context, handle domain.SandboxHandle, request domain.SandboxCommandRequest, requestKey string) (*domain.SandboxCommandResult, error)
}
