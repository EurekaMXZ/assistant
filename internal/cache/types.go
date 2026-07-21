package cache

import (
	"context"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type ContextAnchor struct {
	ConversationID  string `json:"conversation_id"`
	Generation      int64  `json:"generation"`
	CoveredFromSeq  int64  `json:"covered_from_seq"`
	CoveredUntilSeq int64  `json:"covered_until_seq"`
	Role            string `json:"role"`
	Content         string `json:"content"`
	TokenCount      int    `json:"token_count"`
}

type ContextSnapshot struct {
	ConversationID           string           `json:"conversation_id" msgpack:"conversation_id"`
	Version                  int64            `json:"version" msgpack:"version"`
	SchemaVersion            int              `json:"schema_version" msgpack:"schema_version"`
	CoveredEventSeq          int64            `json:"covered_event_seq" msgpack:"covered_event_seq"`
	LatestCheckpointKey      string           `json:"latest_checkpoint_key,omitempty" msgpack:"latest_checkpoint_key,omitempty"`
	LatestCheckpointChecksum string           `json:"latest_checkpoint_checksum,omitempty" msgpack:"latest_checkpoint_checksum,omitempty"`
	LatestSuccessfulRunID    string           `json:"latest_successful_run_id,omitempty" msgpack:"latest_successful_run_id,omitempty"`
	Checksum                 string           `json:"checksum" msgpack:"checksum"`
	CreatedAt                time.Time        `json:"created_at" msgpack:"created_at"`
	Anchor                   *ContextAnchor   `json:"anchor,omitempty" msgpack:"anchor,omitempty"`
	AnchorGeneration         int64            `json:"anchor_generation" msgpack:"anchor_generation"`
	CoveredUntilSeq          int64            `json:"covered_until_seq" msgpack:"covered_until_seq"`
	RawTailStartSeq          int64            `json:"raw_tail_start_seq" msgpack:"raw_tail_start_seq"`
	LastSeq                  int64            `json:"last_seq" msgpack:"last_seq"`
	ActiveTokens             int              `json:"active_tokens" msgpack:"active_tokens"`
	TailCacheStartSeq        int64            `json:"tail_cache_start_seq" msgpack:"tail_cache_start_seq"`
	TailCacheEndSeq          int64            `json:"tail_cache_end_seq" msgpack:"tail_cache_end_seq"`
	Tail                     []domain.Message `json:"tail,omitempty" msgpack:"tail,omitempty"`
	ModelInput               []llm.ModelItem  `json:"model_input,omitempty" msgpack:"model_input,omitempty"`
	ModelInputReady          bool             `json:"model_input_ready" msgpack:"model_input_ready"`
	UpdatedAt                time.Time        `json:"updated_at" msgpack:"updated_at"`
}

type ContextSnapshotCache interface {
	Get(conversationID string) (*ContextSnapshot, bool)
	Put(conversationID string, entry *ContextSnapshot)
}

type VersionedContextSnapshotCache interface {
	GetVersion(conversationID string, version int64) (*ContextSnapshot, bool)
	PutVersion(conversationID string, version int64, entry *ContextSnapshot)
}

type SharedContextSnapshotCache interface {
	GetContextSnapshot(ctx context.Context, conversationID string, version int64) (*ContextSnapshot, bool, error)
	PutContextSnapshot(ctx context.Context, snapshot *ContextSnapshot) error
}

type ContextCompactionCache interface {
	ReplaceWithCompacted(conversationID string, anchor *ContextAnchor, head domain.ContextHead, tail []domain.Message)
}

type ContextTailAppender interface {
	AppendTailMessage(conversationID string, head domain.ContextHead, message domain.Message)
}
