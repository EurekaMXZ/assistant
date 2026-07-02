package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type stubTurnLeaseRenewer struct {
	mu      sync.Mutex
	results []error
	calls   chan struct{}
}

func (s *stubTurnLeaseRenewer) RenewTurnRunLease(context.Context, TurnRunLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.calls <- struct{}{}:
	default:
	}
	if len(s.results) == 0 {
		return nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result
}

func TestTurnLeaseHeartbeatRenewsUntilStopped(t *testing.T) {
	renewer := &stubTurnLeaseRenewer{calls: make(chan struct{}, 1)}
	runCtx, stop := startTurnRunLeaseHeartbeat(context.Background(), renewer, TurnRunLease{TurnID: "turn-1", RunID: "run-1", Token: "lease-1"}, 60*time.Millisecond)

	waitForLeaseCalls(t, renewer.calls, 1)
	select {
	case <-runCtx.Done():
		t.Fatalf("run context canceled before lease stopped: %v", context.Cause(runCtx))
	default:
	}
	if err := stop(); err != nil {
		t.Fatalf("stop heartbeat: %v", err)
	}
}

func TestTurnLeaseHeartbeatToleratesTransientRenewalError(t *testing.T) {
	renewer := &stubTurnLeaseRenewer{
		calls:   make(chan struct{}, 2),
		results: []error{errors.New("database temporarily unavailable"), nil},
	}
	runCtx, stop := startTurnRunLeaseHeartbeat(context.Background(), renewer, TurnRunLease{TurnID: "turn-1", RunID: "run-1", Token: "lease-1"}, 150*time.Millisecond)

	waitForLeaseCalls(t, renewer.calls, 2)
	select {
	case <-runCtx.Done():
		t.Fatalf("transient renewal error canceled run: %v", context.Cause(runCtx))
	default:
	}
	if err := stop(); err != nil {
		t.Fatalf("stop heartbeat: %v", err)
	}
}

func TestTurnLeaseHeartbeatCancelsStaleAttemptOnConflict(t *testing.T) {
	renewer := &stubTurnLeaseRenewer{
		calls:   make(chan struct{}, 1),
		results: []error{domain.ErrConflict},
	}
	runCtx, stop := startTurnRunLeaseHeartbeat(context.Background(), renewer, TurnRunLease{TurnID: "turn-1", RunID: "run-1", Token: "stale-lease"}, 60*time.Millisecond)

	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stale attempt cancellation")
	}
	if !errors.Is(context.Cause(runCtx), domain.ErrConflict) {
		t.Fatalf("run cancellation cause = %v, want conflict", context.Cause(runCtx))
	}
	if err := stop(); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stop heartbeat error = %v, want conflict", err)
	}
}

func TestTurnLeaseHeartbeatCancelsAfterRenewalOutageExceedsLease(t *testing.T) {
	renewErr := errors.New("database unavailable")
	renewer := &stubTurnLeaseRenewer{
		calls: make(chan struct{}, 4),
		results: []error{
			renewErr,
			renewErr,
			renewErr,
		},
	}
	runCtx, stop := startTurnRunLeaseHeartbeat(context.Background(), renewer, TurnRunLease{TurnID: "turn-1", RunID: "run-1", Token: "lease-1"}, 60*time.Millisecond)

	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lease renewal outage cancellation")
	}
	if cause := context.Cause(runCtx); cause == nil || !errors.Is(cause, renewErr) {
		t.Fatalf("run cancellation cause = %v, want renewal error", cause)
	}
	if err := stop(); err == nil {
		t.Fatal("expected heartbeat stop to report lost lease")
	}
}

func waitForLeaseCalls(t *testing.T, calls <-chan struct{}, count int) {
	t.Helper()
	for range count {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for lease renewal %d", count)
		}
	}
}
