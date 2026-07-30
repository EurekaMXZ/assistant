package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type toolCallLeaseRenewer interface {
	RenewQueuedToolCallLease(ctx context.Context, lease ToolCallLease) error
}

func startToolCallLeaseHeartbeat(parent context.Context, renewer toolCallLeaseRenewer, lease ToolCallLease, leaseTimeout time.Duration) (context.Context, func() error) {
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
				err := renewer.RenewQueuedToolCallLease(heartbeatCtx, lease)
				if err == nil {
					lastRenewed = time.Now()
					continue
				}
				if heartbeatCtx.Err() != nil {
					return
				}
				if errors.Is(err, domain.ErrConflict) || time.Since(lastRenewed) >= leaseTimeout {
					leaseErr := fmt.Errorf("tool call lease lost: %w", err)
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
