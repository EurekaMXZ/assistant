package workflow

import (
	"context"
	"time"
)

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

type ContextCheckpointStore interface {
	PutImmutableBytes(ctx context.Context, key string, data []byte, contentType string) error
	GetBytes(ctx context.Context, key string) ([]byte, error)
	ContextCheckpointKey(conversationID string, version int64) string
}

type ImmutableRunArtifactStore interface {
	PutImmutableBytes(ctx context.Context, key string, data []byte, contentType string) error
	GetBytes(ctx context.Context, key string) ([]byte, error)
	ImmutableRunArtifactKey(conversationID, turnID string, stepIndex int, runID string, artifact string) string
}

type RunArtifactMetadata struct {
	Name             string `json:"name"`
	ObjectKey        string `json:"object_key"`
	ContentType      string `json:"content_type"`
	UncompressedSize int64  `json:"uncompressed_size"`
	CompressedSize   int64  `json:"compressed_size"`
	SHA256           string `json:"sha256"`
	SchemaVersion    int    `json:"schema_version"`
}

type RunArtifactObject struct {
	Key          string
	LastModified time.Time
}

type RunArtifactObjectStore interface {
	ListRunArtifactObjects(ctx context.Context, prefix string) ([]RunArtifactObject, error)
	DeleteObject(ctx context.Context, key string) error
}
