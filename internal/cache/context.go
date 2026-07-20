package cache

import (
	"encoding/json"
	"fmt"
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
	latest     map[string]int64
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
		latest:     make(map[string]int64, maxEntries),
	}
}

func (s *Store) Get(conversationID string) (*ContextSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	version, ok := s.latest[conversationID]
	if !ok {
		return nil, false
	}
	entry, ok := s.entries[contextVersionKey(conversationID, version)]
	if !ok {
		return nil, false
	}

	return cloneContext(entry), true
}

func (s *Store) Put(conversationID string, entry *ContextSnapshot) {
	version := int64(0)
	if entry != nil {
		version = entry.Version
	}
	s.PutVersion(conversationID, version, entry)
}

func (s *Store) GetVersion(conversationID string, version int64) (*ContextSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[contextVersionKey(conversationID, version)]
	if !ok {
		return nil, false
	}
	return cloneContext(entry), true
}

func (s *Store) PutVersion(conversationID string, version int64, entry *ContextSnapshot) {
	if entry == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := contextVersionKey(conversationID, version)
	if _, exists := s.entries[key]; !exists {
		s.order = append(s.order, key)
	}
	stored := cloneContext(entry)
	stored.ConversationID = conversationID
	stored.Version = version
	s.entries[key] = stored
	s.latest[conversationID] = version

	for len(s.order) > s.maxEntries {
		s.evictOldestLocked()
	}
}

func (s *Store) evictOldestLocked() {
	if len(s.order) == 0 {
		return
	}
	evict := s.order[0]
	s.order = s.order[1:]
	delete(s.entries, evict)
	for conversationID, version := range s.latest {
		if contextVersionKey(conversationID, version) == evict {
			delete(s.latest, conversationID)
		}
	}
}

func contextVersionKey(conversationID string, version int64) string {
	return fmt.Sprintf("%s:%d", conversationID, version)
}

func (s *Store) ReplaceWithCompacted(conversationID string, anchor *ContextAnchor, head domain.ContextHead, tail []domain.Message) {
	s.Put(conversationID, &ContextSnapshot{
		ConversationID:        conversationID,
		Version:               head.Version,
		SchemaVersion:         head.ContextSchemaVersion,
		CoveredEventSeq:       head.LastContextEventSeq,
		LatestCheckpointKey:   head.LatestCheckpointKey,
		LatestSuccessfulRunID: head.LatestSuccessfulRunID,
		Anchor:                anchor,
		AnchorGeneration:      head.AnchorGeneration,
		CoveredUntilSeq:       head.CoveredUntilSeq,
		RawTailStartSeq:       head.RawTailStartSeq,
		LastSeq:               head.LastSeq,
		ActiveTokens:          head.ActiveContextTokens,
		TailCacheStartSeq:     tailCacheStart(head, tail),
		TailCacheEndSeq:       tailCacheEnd(head, tail),
		Tail:                  append([]domain.Message(nil), tail...),
		UpdatedAt:             time.Now(),
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

	version, ok := s.latest[conversationID]
	if !ok {
		return
	}
	entry, ok := s.entries[contextVersionKey(conversationID, version)]
	if !ok {
		return
	}
	entry = cloneContext(entry)
	entry.ConversationID = conversationID
	entry.Version = head.Version
	entry.SchemaVersion = head.ContextSchemaVersion
	entry.CoveredEventSeq = head.LastContextEventSeq
	entry.LatestCheckpointKey = head.LatestCheckpointKey
	entry.LatestSuccessfulRunID = head.LatestSuccessfulRunID
	entry.RawTailStartSeq = head.RawTailStartSeq
	entry.LastSeq = head.LastSeq
	entry.ActiveTokens = head.ActiveContextTokens
	entry.CoveredUntilSeq = head.CoveredUntilSeq
	entry.AnchorGeneration = head.AnchorGeneration
	entry.UpdatedAt = time.Now()
	entry.ModelInput = nil
	entry.ModelInputReady = false
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
	key := contextVersionKey(conversationID, head.Version)
	if _, exists := s.entries[key]; !exists {
		s.order = append(s.order, key)
	}
	s.entries[key] = entry
	s.latest[conversationID] = head.Version
	for len(s.order) > s.maxEntries {
		s.evictOldestLocked()
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
