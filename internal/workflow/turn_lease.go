package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type turnRunLeaseRenewer interface {
	RenewTurnRunLease(ctx context.Context, lease TurnRunLease) error
}

func startTurnRunLeaseHeartbeat(parent context.Context, renewer turnRunLeaseRenewer, lease TurnRunLease, leaseTimeout time.Duration) (context.Context, func() error) {
	runCtx, cancelRun := context.WithCancelCause(parent)
	if leaseTimeout <= 0 {
		return runCtx, func() error {
			cancelRun(nil)
			return nil
		}
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(runCtx)
	done := make(chan struct{})
	fatal := make(chan error, 1)
	interval := leaseTimeout / 3
	if interval <= 0 {
		interval = leaseTimeout
	}

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		lastRenewed := time.Now()

		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				err := renewer.RenewTurnRunLease(heartbeatCtx, lease)
				if err == nil {
					lastRenewed = time.Now()
					continue
				}
				if heartbeatCtx.Err() != nil {
					return
				}

				if errors.Is(err, domain.ErrConflict) || time.Since(lastRenewed) >= leaseTimeout {
					leaseErr := fmt.Errorf("turn lease lost: %w", err)
					fatal <- leaseErr
					cancelRun(leaseErr)
					return
				}
			}
		}
	}()

	var stopOnce sync.Once
	var stopErr error
	stop := func() error {
		stopOnce.Do(func() {
			stopHeartbeat()
			<-done
			select {
			case stopErr = <-fatal:
			default:
			}
			cancelRun(nil)
		})
		return stopErr
	}

	return runCtx, stop
}
