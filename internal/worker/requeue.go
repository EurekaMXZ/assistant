package worker

import (
	"context"
	"time"
)

func (s *Service) requeueLoop(ctx context.Context) {
	leaseTimeout := s.settings.LLMClientLeaseTimeout
	if leaseTimeout <= 0 {
		leaseTimeout = s.settings.WorkerLeaseTimeout
	}
	if leaseTimeout <= 0 {
		leaseTimeout = time.Minute
	}
	ticker := time.NewTicker(leaseTimeout / 2)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		requeued, err := s.engine.RequeueStaleTurns(ctx)
		if err != nil && ctx.Err() == nil {
			s.logger.Printf("requeue stale turns: %v", err)
		}
		if requeued > 0 {
			s.logger.Printf("requeued %d stale turns", requeued)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
