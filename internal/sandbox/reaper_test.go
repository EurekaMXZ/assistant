package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type reaperTestStore struct {
	mu        sync.Mutex
	now       time.Time
	sandboxes map[string]*domain.ConversationSandbox
}

func (s *reaperTestStore) GetActiveConversationSandbox(ctx context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	item, err := s.GetUsableConversationSandbox(ctx, conversationID)
	if err != nil || item.Status != domain.SandboxStatusActive {
		return nil, domain.ErrNotFound
	}
	return item, nil
}

func (s *reaperTestStore) GetUsableConversationSandbox(_ context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.sandboxes[conversationID]
	if item == nil || (item.Status != domain.SandboxStatusActive && item.Status != domain.SandboxStatusStopped) {
		return nil, domain.ErrNotFound
	}
	clone := *item
	return &clone, nil
}

func (s *reaperTestStore) GetLatestConversationSandbox(_ context.Context, conversationID string) (*domain.ConversationSandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.sandboxes[conversationID]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	clone := *item
	return &clone, nil
}

func (s *reaperTestStore) CreateConversationSandbox(context.Context, string, string, string, json.RawMessage) (*domain.ConversationSandbox, error) {
	return nil, nil
}

func (s *reaperTestStore) StopConversationSandbox(_ context.Context, id string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	return s.transition(id, domain.SandboxStatusActive, domain.SandboxStatusStopped, metadata)
}

func (s *reaperTestStore) ResumeConversationSandbox(_ context.Context, id string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	return s.transition(id, domain.SandboxStatusStopped, domain.SandboxStatusActive, metadata)
}

func (s *reaperTestStore) TouchConversationSandbox(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil {
		return domain.ErrConflict
	}
	item.LastActivityAt = s.now
	return nil
}

func (s *reaperTestStore) AcquireConversationSandboxExecution(_ context.Context, id string, token string, leaseDuration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || item.Status != domain.SandboxStatusActive {
		return domain.ErrConflict
	}
	leaseUntil := s.now.Add(leaseDuration)
	item.ExecutionToken = token
	item.ExecutionLeaseUntil = &leaseUntil
	return nil
}

func (s *reaperTestStore) CompleteConversationSandboxExecution(_ context.Context, id string, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || item.ExecutionToken != token {
		return domain.ErrConflict
	}
	item.ExecutionToken = ""
	item.ExecutionLeaseUntil = nil
	item.LastActivityAt = s.now
	return nil
}

func (s *reaperTestStore) RenewConversationSandboxExecution(_ context.Context, id string, token string, leaseDuration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || item.ExecutionToken != token {
		return domain.ErrConflict
	}
	leaseUntil := s.now.Add(leaseDuration)
	item.ExecutionLeaseUntil = &leaseUntil
	return nil
}

func (s *reaperTestStore) ListIdleConversationSandboxes(_ context.Context, before time.Time, limit int) ([]*domain.ConversationSandbox, error) {
	return s.list(domain.SandboxStatusActive, before, limit), nil
}

func (s *reaperTestStore) ListStoppedConversationSandboxes(_ context.Context, before time.Time, limit int) ([]*domain.ConversationSandbox, error) {
	return s.list(domain.SandboxStatusStopped, before, limit), nil
}

func (s *reaperTestStore) ListReleasingConversationSandboxes(_ context.Context, limit int) ([]*domain.ConversationSandbox, error) {
	return s.list(domain.SandboxStatusReleasing, time.Time{}, limit), nil
}

func (s *reaperTestStore) BeginConversationSandboxRelease(_ context.Context, id string) (*domain.ConversationSandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || (item.Status != domain.SandboxStatusActive && item.Status != domain.SandboxStatusStopped) {
		return nil, domain.ErrConflict
	}
	item.ReleasePreviousStatus = item.Status
	item.Status = domain.SandboxStatusReleasing
	clone := *item
	return &clone, nil
}

func (s *reaperTestStore) ClaimConversationSandboxRelease(_ context.Context, id string, token string, leaseDuration time.Duration) (*domain.ConversationSandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || item.Status != domain.SandboxStatusReleasing || (item.ReleaseLeaseUntil != nil && item.ReleaseLeaseUntil.After(s.now)) {
		return nil, domain.ErrConflict
	}
	leaseUntil := s.now.Add(leaseDuration)
	item.ReleaseToken = token
	item.ReleaseLeaseUntil = &leaseUntil
	clone := *item
	return &clone, nil
}

func (s *reaperTestStore) RenewConversationSandboxReleaseClaim(_ context.Context, id string, token string, leaseDuration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || item.ReleaseToken != token {
		return domain.ErrConflict
	}
	leaseUntil := s.now.Add(leaseDuration)
	item.ReleaseLeaseUntil = &leaseUntil
	return nil
}

func (s *reaperTestStore) CompleteConversationSandboxRelease(_ context.Context, id string, token string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || item.Status != domain.SandboxStatusReleasing || item.ReleaseToken != token {
		return nil, domain.ErrConflict
	}
	item.Status = domain.SandboxStatusDestroyed
	item.RuntimeMetadata = metadata
	item.StoppedAt = nil
	item.ReleasePreviousStatus = ""
	item.ReleaseToken = ""
	item.ReleaseLeaseUntil = nil
	destroyedAt := s.now
	item.DestroyedAt = &destroyedAt
	clone := *item
	return &clone, nil
}

func (s *reaperTestStore) transition(id string, from string, to string, metadata json.RawMessage) (*domain.ConversationSandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.byID(id)
	if item == nil || item.Status != from {
		return nil, domain.ErrConflict
	}
	item.Status = to
	item.RuntimeMetadata = metadata
	if to == domain.SandboxStatusStopped {
		stoppedAt := s.now
		item.StoppedAt = &stoppedAt
	} else {
		item.StoppedAt = nil
		item.LastActivityAt = s.now
	}
	clone := *item
	return &clone, nil
}

func (s *reaperTestStore) list(status string, before time.Time, limit int) []*domain.ConversationSandbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]*domain.ConversationSandbox, 0)
	for _, item := range s.sandboxes {
		if status == domain.SandboxStatusActive && executionLeaseActive(item, s.now) {
			continue
		}
		timestamp := item.LastActivityAt
		if status == domain.SandboxStatusStopped && item.StoppedAt != nil {
			timestamp = *item.StoppedAt
		}
		releaseClaimAvailable := item.ReleaseLeaseUntil == nil || !item.ReleaseLeaseUntil.After(s.now)
		if item.Status == status && (status != domain.SandboxStatusReleasing || releaseClaimAvailable) && (status == domain.SandboxStatusReleasing || timestamp.Before(before)) && len(items) < limit {
			clone := *item
			items = append(items, &clone)
		}
	}
	return items
}

func (s *reaperTestStore) byID(id string) *domain.ConversationSandbox {
	for _, item := range s.sandboxes {
		if item.ID == id {
			return item
		}
	}
	return nil
}

type reaperTestRuntime struct {
	stopCalls    int
	resumeCalls  int
	destroyCalls int
	destroyErr   error
}

func (r *reaperTestRuntime) CreateSandbox(context.Context, string, string) (*domain.SandboxHandle, error) {
	return nil, nil
}
func (r *reaperTestRuntime) StopSandbox(_ context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	r.stopCalls++
	return &handle, nil
}
func (r *reaperTestRuntime) ResumeSandbox(_ context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	r.resumeCalls++
	return &handle, nil
}
func (r *reaperTestRuntime) DestroySandbox(_ context.Context, handle domain.SandboxHandle, _ string) (*domain.SandboxHandle, error) {
	r.destroyCalls++
	if r.destroyErr != nil {
		return nil, r.destroyErr
	}
	return &handle, nil
}
func (r *reaperTestRuntime) ExecSandboxCommand(context.Context, domain.SandboxHandle, domain.SandboxCommandRequest, string) (*domain.SandboxCommandResult, error) {
	return nil, nil
}

type reaperTestLocker struct{ mu sync.Mutex }

func (l *reaperTestLocker) WithConversationLock(ctx context.Context, _ string, fn func(context.Context) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return fn(ctx)
}

func TestReaperStopsIdleAndReleasesExpiredSandbox(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	stoppedAt := now.Add(-25 * time.Hour)
	store := &reaperTestStore{now: now, sandboxes: map[string]*domain.ConversationSandbox{
		"conversation-idle": {
			ID: "sandbox-idle", ConversationID: "conversation-idle", Provider: ProviderFirecracker, RuntimeID: "vm-idle",
			Status: domain.SandboxStatusActive, LastActivityAt: now.Add(-16 * time.Minute),
		},
		"conversation-expired": {
			ID: "sandbox-expired", ConversationID: "conversation-expired", Provider: ProviderFirecracker, RuntimeID: "vm-expired",
			Status: domain.SandboxStatusStopped, LastActivityAt: now.Add(-26 * time.Hour), StoppedAt: &stoppedAt,
		},
	}}
	runtime := &reaperTestRuntime{}
	reaper := NewReaper(LifecycleSettings{
		IdleStopAfter: 15 * time.Minute, StoppedRetention: 24 * time.Hour, ReaperBatchSize: 20,
	}, store, runtime, &reaperTestLocker{}, nil)
	reaper.now = func() time.Time { return now }

	if err := reaper.Reap(context.Background()); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if store.sandboxes["conversation-idle"].Status != domain.SandboxStatusStopped || runtime.stopCalls != 1 {
		t.Fatalf("idle sandbox was not stopped: sandbox=%#v calls=%d", store.sandboxes["conversation-idle"], runtime.stopCalls)
	}
	if store.sandboxes["conversation-expired"].Status != domain.SandboxStatusDestroyed || runtime.destroyCalls != 1 {
		t.Fatalf("expired sandbox was not released: sandbox=%#v calls=%d", store.sandboxes["conversation-expired"], runtime.destroyCalls)
	}
}

func TestReaperDoesNotStopSandboxWithActiveExecutionLease(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	store := &reaperTestStore{now: now, sandboxes: map[string]*domain.ConversationSandbox{
		"conversation-busy": {
			ID: "sandbox-busy", ConversationID: "conversation-busy", Provider: ProviderFirecracker, RuntimeID: "vm-busy",
			Status: domain.SandboxStatusActive, LastActivityAt: now.Add(-time.Hour), ExecutionToken: "execution-token", ExecutionLeaseUntil: &leaseUntil,
		},
	}}
	runtime := &reaperTestRuntime{}
	reaper := NewReaper(LifecycleSettings{IdleStopAfter: 15 * time.Minute, ReaperBatchSize: 20}, store, runtime, &reaperTestLocker{}, nil)
	reaper.now = func() time.Time { return now }

	if err := reaper.Reap(context.Background()); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if store.sandboxes["conversation-busy"].Status != domain.SandboxStatusActive || runtime.stopCalls != 0 {
		t.Fatalf("busy sandbox was stopped: sandbox=%#v calls=%d", store.sandboxes["conversation-busy"], runtime.stopCalls)
	}
}

func TestReaperRetriesPendingRelease(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	stoppedAt := now.Add(-25 * time.Hour)
	store := &reaperTestStore{now: now, sandboxes: map[string]*domain.ConversationSandbox{
		"conversation-releasing": {
			ID: "sandbox-releasing", ConversationID: "conversation-releasing", Provider: ProviderFirecracker, RuntimeID: "vm-releasing",
			Status: domain.SandboxStatusReleasing, LastActivityAt: stoppedAt, StoppedAt: &stoppedAt, ReleasePreviousStatus: domain.SandboxStatusStopped,
		},
	}}
	runtime := &reaperTestRuntime{destroyErr: errors.New("provider unavailable")}
	reaper := NewReaper(LifecycleSettings{StoppedRetention: 24 * time.Hour, ReaperBatchSize: 20}, store, runtime, &reaperTestLocker{}, nil)
	reaper.now = func() time.Time { return now }

	if err := reaper.Reap(context.Background()); err == nil {
		t.Fatal("expected provider release failure")
	}
	if store.sandboxes["conversation-releasing"].Status != domain.SandboxStatusReleasing {
		t.Fatalf("failed release did not remain pending: %#v", store.sandboxes["conversation-releasing"])
	}
	runtime.destroyErr = nil
	now = now.Add(tool.SandboxReleaseLeaseDuration + time.Minute)
	store.now = now
	if err := reaper.Reap(context.Background()); err != nil {
		t.Fatalf("retry release: %v", err)
	}
	if store.sandboxes["conversation-releasing"].Status != domain.SandboxStatusDestroyed || runtime.destroyCalls != 2 {
		t.Fatalf("pending release was not completed: sandbox=%#v calls=%d", store.sandboxes["conversation-releasing"], runtime.destroyCalls)
	}
}
