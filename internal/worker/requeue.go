package worker

import (
	"context"
	"time"
)

func (s *Service) requeueLoop(ctx context.Context) {
	leaseTimeout := s.requeueLeaseTimeout()
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

func (s *Service) requeueLeaseTimeout() time.Duration {
	timeouts := []time.Duration{
		s.settings.LLMClientLeaseTimeout,
		s.settings.ExecutionLeaseTimeout,
		s.settings.WorkerLeaseTimeout,
	}
	minimum := time.Duration(0)
	for _, timeout := range timeouts {
		if timeout <= 0 || (minimum > 0 && timeout >= minimum) {
			continue
		}
		minimum = timeout
	}
	if minimum <= 0 {
		return time.Minute
	}
	return minimum
}
