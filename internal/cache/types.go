package cache

import (
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
	Anchor            *ContextAnchor
	AnchorGeneration  int64
	CoveredUntilSeq   int64
	RawTailStartSeq   int64
	LastSeq           int64
	ActiveTokens      int
	TailCacheStartSeq int64
	TailCacheEndSeq   int64
	Tail              []domain.Message
	ModelInput        []llm.ModelItem
	UpdatedAt         time.Time
}

type ContextSnapshotCache interface {
	Get(conversationID string) (*ContextSnapshot, bool)
	Put(conversationID string, entry *ContextSnapshot)
}

type ContextCompactionCache interface {
	ReplaceWithCompacted(conversationID string, anchor *ContextAnchor, head domain.ContextHead, tail []domain.Message)
}

type ContextTailAppender interface {
	AppendTailMessage(conversationID string, head domain.ContextHead, message domain.Message)
}
