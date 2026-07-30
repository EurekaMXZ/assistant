package workflow

import (
	"context"
	"testing"
	"time"
)

type staleTurnStoreStub struct {
	turnLease      time.Duration
	runLease       time.Duration
	executionLease time.Duration
}

func (s *staleTurnStoreStub) RequeueStaleTurns(_ context.Context, leaseTimeout time.Duration) (int, error) {
	s.turnLease = leaseTimeout
	return 1, nil
}

func (s *staleTurnStoreStub) RequeueStaleTurnRuns(_ context.Context, leaseTimeout time.Duration) (int, error) {
	s.runLease = leaseTimeout
	return 2, nil
}

func (s *staleTurnStoreStub) RequeueStaleToolCalls(_ context.Context, leaseTimeout time.Duration) (int, error) {
	s.executionLease = leaseTimeout
	return 3, nil
}

func TestStaleTurnRequeuerUsesIndependentLeases(t *testing.T) {
	store := &staleTurnStoreStub{}
	requeuer := &StaleTurnRequeuer{
		settings: WorkflowSettings{WorkerLeaseTimeout: time.Minute, LLMClientLeaseTimeout: 2 * time.Minute, ExecutionLeaseTimeout: 15 * time.Second},
		store:    store,
	}
	requeued, err := requeuer.Requeue(t.Context())
	if err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if requeued != 6 || store.turnLease != 2*time.Minute || store.runLease != 2*time.Minute || store.executionLease != 15*time.Second {
		t.Fatalf("requeue leases: count=%d turn=%s run=%s execution=%s", requeued, store.turnLease, store.runLease, store.executionLease)
	}
}
