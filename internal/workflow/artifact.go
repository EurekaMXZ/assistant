package workflow

import "context"

type TurnArtifactStore interface {
	PutBytes(ctx context.Context, key string, data []byte, contentType string) error
	GetBytes(ctx context.Context, key string) ([]byte, error)
	TurnRequestKey(conversationID, turnID string) string
	TurnResponseKey(conversationID, turnID string) string
	TurnStreamKey(conversationID, turnID string) string
	TurnModelContextKey(conversationID, turnID string) string
}

type ToolArtifactStore interface {
	PutBytes(ctx context.Context, key string, data []byte, contentType string) error
	GetBytes(ctx context.Context, key string) ([]byte, error)
	TurnRunRequestKey(conversationID, turnID string, stepIndex int) string
	TurnRunStateKey(conversationID, turnID string, stepIndex int) string
	TurnRunResultKey(conversationID, turnID string, stepIndex int) string
	TurnRunResponseKey(conversationID, turnID string, stepIndex int) string
	TurnRunOutputItemsKey(conversationID, turnID string, stepIndex int) string
	ToolCallArgumentsKey(conversationID, turnID, callID string) string
	ToolCallOutputKey(conversationID, turnID, callID string) string
}

type ContextAnchorStore interface {
	PutJSON(ctx context.Context, key string, value any) error
	GetJSON(ctx context.Context, key string, target any) error
	ContextAnchorKey(conversationID string, generation int64) string
}
