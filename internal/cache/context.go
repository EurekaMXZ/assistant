package cache

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

var _ ContextSnapshotCache = (*Store)(nil)
var _ ContextCompactionCache = (*Store)(nil)
var _ ContextTailAppender = (*Store)(nil)

type Store struct {
	mu         sync.RWMutex
	maxEntries int
	tailCap    int
	entries    map[string]*ContextSnapshot
	order      []string
}

func New(maxEntries int, tailCap int) *Store {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	if tailCap <= 0 {
		tailCap = 256
	}

	return &Store{
		maxEntries: maxEntries,
		tailCap:    tailCap,
		entries:    make(map[string]*ContextSnapshot, maxEntries),
	}
}

func (s *Store) Get(conversationID string) (*ContextSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[conversationID]
	if !ok {
		return nil, false
	}

	return cloneContext(entry), true
}

func (s *Store) Put(conversationID string, entry *ContextSnapshot) {
	if entry == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.entries[conversationID]; !exists {
		s.order = append(s.order, conversationID)
	}
	s.entries[conversationID] = cloneContext(entry)

	for len(s.order) > s.maxEntries {
		evict := s.order[0]
		s.order = s.order[1:]
		delete(s.entries, evict)
	}
}

func (s *Store) ReplaceWithCompacted(conversationID string, anchor *ContextAnchor, head domain.ContextHead, tail []domain.Message) {
	s.Put(conversationID, &ContextSnapshot{
		Anchor:            anchor,
		AnchorGeneration:  head.AnchorGeneration,
		CoveredUntilSeq:   head.CoveredUntilSeq,
		RawTailStartSeq:   head.RawTailStartSeq,
		LastSeq:           head.LastSeq,
		ActiveTokens:      head.ActiveContextTokens,
		TailCacheStartSeq: tailCacheStart(head, tail),
		TailCacheEndSeq:   tailCacheEnd(head, tail),
		Tail:              append([]domain.Message(nil), tail...),
		UpdatedAt:         time.Now(),
	})
}

func tailCacheStart(head domain.ContextHead, tail []domain.Message) int64 {
	if len(tail) > 0 {
		return tail[0].Seq
	}
	return head.RawTailStartSeq
}

func tailCacheEnd(head domain.ContextHead, tail []domain.Message) int64 {
	if len(tail) > 0 {
		return tail[len(tail)-1].Seq
	}
	return head.RawTailStartSeq - 1
}

func (s *Store) AppendTailMessage(conversationID string, head domain.ContextHead, message domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[conversationID]
	if !ok {
		return
	}

	entry.RawTailStartSeq = head.RawTailStartSeq
	entry.LastSeq = head.LastSeq
	entry.ActiveTokens = head.ActiveContextTokens
	entry.CoveredUntilSeq = head.CoveredUntilSeq
	entry.AnchorGeneration = head.AnchorGeneration
	entry.UpdatedAt = time.Now()
	entry.Tail = append(entry.Tail, message)
	if len(entry.Tail) > s.tailCap {
		entry.Tail = append([]domain.Message(nil), entry.Tail[len(entry.Tail)-s.tailCap:]...)
	}
	if len(entry.Tail) > 0 {
		entry.TailCacheStartSeq = entry.Tail[0].Seq
		entry.TailCacheEndSeq = entry.Tail[len(entry.Tail)-1].Seq
	} else {
		entry.TailCacheStartSeq = head.RawTailStartSeq
		entry.TailCacheEndSeq = head.RawTailStartSeq - 1
	}
}

func cloneContext(source *ContextSnapshot) *ContextSnapshot {
	if source == nil {
		return nil
	}

	clone := *source
	if source.Anchor != nil {
		anchorCopy := *source.Anchor
		clone.Anchor = &anchorCopy
	}
	if len(source.Tail) > 0 {
		clone.Tail = append([]domain.Message(nil), source.Tail...)
	}
	if len(source.ModelInput) > 0 {
		clone.ModelInput = cloneModelItems(source.ModelInput)
	}
	return &clone
}

func cloneModelItems(items []llm.ModelItem) []llm.ModelItem {
	if len(items) == 0 {
		return nil
	}

	cloned := make([]llm.ModelItem, 0, len(items))
	for _, item := range items {
		item.Arguments = append(json.RawMessage(nil), item.Arguments...)
		item.Raw = append(json.RawMessage(nil), item.Raw...)
		cloned = append(cloned, item)
	}
	return cloned
}
