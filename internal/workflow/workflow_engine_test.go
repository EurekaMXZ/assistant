package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
)

type deletedConversationReader struct {
	err error
}

func (r deletedConversationReader) GetConversation(context.Context, string) (*domain.Conversation, error) {
	return nil, r.err
}

func TestCacheCoversTailRequiresFullTailRange(t *testing.T) {
	head := &domain.ContextHead{
		RawTailStartSeq: 11,
		LastSeq:         13,
	}

	entry := &cache.ContextSnapshot{
		RawTailStartSeq:   11,
		LastSeq:           13,
		TailCacheStartSeq: 12,
		TailCacheEndSeq:   13,
	}

	if cacheCoversTail(entry, head) {
		t.Fatal("expected cache miss when oldest tail message has been evicted from the ring")
	}
}

func TestCacheCoversTailAllowsEmptyTailAfterCompaction(t *testing.T) {
	head := &domain.ContextHead{
		RawTailStartSeq: 19,
		LastSeq:         18,
	}

	entry := &cache.ContextSnapshot{
		RawTailStartSeq:   19,
		LastSeq:           18,
		TailCacheStartSeq: 19,
		TailCacheEndSeq:   18,
	}

	if !cacheCoversTail(entry, head) {
		t.Fatal("expected cache hit when raw tail is empty")
	}
}

func TestIgnoreDeletedConversationAcknowledgesStaleEvent(t *testing.T) {
	engine := &Engine{conversations: deletedConversationReader{err: domain.ErrNotFound}}
	if err := engine.ignoreDeletedConversation(t.Context(), "deleted", domain.ErrNotFound); err != nil {
		t.Fatalf("ignore deleted conversation: %v", err)
	}
}

func TestIgnoreDeletedConversationPreservesMissingArtifactError(t *testing.T) {
	engine := &Engine{conversations: deletedConversationReader{}}
	eventErr := domain.ErrNotFound
	if err := engine.ignoreDeletedConversation(t.Context(), "existing", eventErr); !errors.Is(err, eventErr) {
		t.Fatalf("error = %v, want %v", err, eventErr)
	}
}
