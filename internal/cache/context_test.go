package cache

import (
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestAppendTailMessageTracksCachedRangeAfterOverflow(t *testing.T) {
	store := New(8, 2)
	head := domain.ContextHead{
		ConversationID:      "conv-1",
		AnchorGeneration:    1,
		CoveredUntilSeq:     10,
		RawTailStartSeq:     11,
		LastSeq:             12,
		ActiveContextTokens: 200,
	}

	store.Put(head.ConversationID, &ContextSnapshot{
		AnchorGeneration:  1,
		CoveredUntilSeq:   10,
		RawTailStartSeq:   11,
		LastSeq:           12,
		ActiveTokens:      200,
		TailCacheStartSeq: 11,
		TailCacheEndSeq:   12,
		Tail: []domain.Message{
			{Seq: 11, ContentText: "u1"},
			{Seq: 12, ContentText: "a1"},
		},
	})

	head.LastSeq = 13
	head.ActiveContextTokens = 240
	store.AppendTailMessage(head.ConversationID, head, domain.Message{Seq: 13, ContentText: "u2"})

	cached, ok := store.Get(head.ConversationID)
	if !ok {
		t.Fatal("expected cached context")
	}

	if got, want := len(cached.Tail), 2; got != want {
		t.Fatalf("tail len = %d, want %d", got, want)
	}
	if got, want := cached.Tail[0].Seq, int64(12); got != want {
		t.Fatalf("first cached tail seq = %d, want %d", got, want)
	}
	if got, want := cached.TailCacheStartSeq, int64(12); got != want {
		t.Fatalf("tail cache start = %d, want %d", got, want)
	}
	if got, want := cached.TailCacheEndSeq, int64(13); got != want {
		t.Fatalf("tail cache end = %d, want %d", got, want)
	}
}
