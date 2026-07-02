package workflow

import (
	"context"
)

type StaleTurnRequeuer struct {
	settings WorkflowSettings
	store    StaleTurnRepository
}

func (r *StaleTurnRequeuer) Requeue(ctx context.Context) (int, error) {
	turns, err := r.store.RequeueStaleTurns(ctx, r.settings.WorkerLeaseTimeout)
	if err != nil {
		return 0, err
	}
	runs, err := r.store.RequeueStaleTurnRuns(ctx, r.settings.WorkerLeaseTimeout)
	if err != nil {
		return turns, err
	}
	return turns + runs, nil
}
