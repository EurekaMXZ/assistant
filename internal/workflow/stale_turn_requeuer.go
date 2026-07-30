package workflow

import (
	"context"
	"time"
)

type StaleTurnRequeuer struct {
	settings WorkflowSettings
	store    StaleTurnRepository
}

func (r *StaleTurnRequeuer) Requeue(ctx context.Context) (int, error) {
	turns, err := r.store.RequeueStaleTurns(ctx, r.llmLeaseTimeout())
	if err != nil {
		return 0, err
	}
	runs, err := r.store.RequeueStaleTurnRuns(ctx, r.llmLeaseTimeout())
	if err != nil {
		return turns, err
	}
	calls, err := r.store.RequeueStaleToolCalls(ctx, r.executionLeaseTimeout())
	if err != nil {
		return turns + runs, err
	}
	return turns + runs + calls, nil
}

func (r *StaleTurnRequeuer) llmLeaseTimeout() time.Duration {
	if r != nil && r.settings.LLMClientLeaseTimeout > 0 {
		return r.settings.LLMClientLeaseTimeout
	}
	if r != nil && r.settings.WorkerLeaseTimeout > 0 {
		return r.settings.WorkerLeaseTimeout
	}
	return time.Minute
}

func (r *StaleTurnRequeuer) executionLeaseTimeout() time.Duration {
	if r != nil && r.settings.ExecutionLeaseTimeout > 0 {
		return r.settings.ExecutionLeaseTimeout
	}
	return r.llmLeaseTimeout()
}
