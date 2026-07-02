package worker

import (
	"context"
	"time"
)

func (s *Service) requeueLoop(ctx context.Context) {
	ticker := time.NewTicker(s.settings.WorkerLeaseTimeout / 2)
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
