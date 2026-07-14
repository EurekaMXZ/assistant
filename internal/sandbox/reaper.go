package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/google/uuid"
)

type LifecycleSettings struct {
	IdleStopAfter    time.Duration
	StoppedRetention time.Duration
	ReaperInterval   time.Duration
	ReaperBatchSize  int
	CommandDefault   time.Duration
	CommandMaximum   time.Duration
}

type Reaper struct {
	settings LifecycleSettings
	store    tool.ConversationSandboxStore
	runtime  tool.SandboxManager
	locker   tool.ConversationLocker
	logger   *log.Logger
	now      func() time.Time
}

func NewReaper(settings LifecycleSettings, store tool.ConversationSandboxStore, runtime tool.SandboxManager, locker tool.ConversationLocker, logger *log.Logger) *Reaper {
	if logger == nil {
		logger = log.Default()
	}
	return &Reaper{settings: settings, store: store, runtime: runtime, locker: locker, logger: logger, now: time.Now}
}

func (r *Reaper) Run(ctx context.Context) error {
	if r == nil || r.store == nil || r.runtime == nil {
		return nil
	}
	interval := r.settings.ReaperInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := r.Reap(ctx); err != nil && ctx.Err() == nil {
			r.logger.Printf("sandbox lifecycle reaper: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Reaper) Reap(ctx context.Context) error {
	if r == nil || r.store == nil || r.runtime == nil {
		return nil
	}
	now := r.now().UTC()
	batchSize := r.settings.ReaperBatchSize
	if batchSize <= 0 {
		batchSize = 20
	}
	var result error
	if r.settings.IdleStopAfter > 0 {
		idleBefore := now.Add(-r.settings.IdleStopAfter)
		items, err := r.store.ListIdleConversationSandboxes(ctx, idleBefore, batchSize)
		if err != nil {
			result = errors.Join(result, err)
		} else {
			for _, item := range items {
				if err := r.stopIdle(ctx, item, idleBefore); err != nil {
					result = errors.Join(result, err)
				}
			}
		}
	}
	releasing, err := r.store.ListReleasingConversationSandboxes(ctx, batchSize)
	if err != nil {
		result = errors.Join(result, err)
	} else {
		for _, item := range releasing {
			if err := r.release(ctx, item); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	if r.settings.StoppedRetention > 0 {
		stoppedBefore := now.Add(-r.settings.StoppedRetention)
		items, err := r.store.ListStoppedConversationSandboxes(ctx, stoppedBefore, batchSize)
		if err != nil {
			result = errors.Join(result, err)
		} else {
			for _, item := range items {
				if err := r.releaseStopped(ctx, item, stoppedBefore); err != nil {
					result = errors.Join(result, err)
				}
			}
		}
	}
	return result
}

func (r *Reaper) stopIdle(ctx context.Context, candidate *domain.ConversationSandbox, idleBefore time.Time) error {
	if candidate == nil {
		return nil
	}
	return withLifecycleLock(ctx, r.locker, candidate.ConversationID, func(lockCtx context.Context) error {
		current, err := r.store.GetUsableConversationSandbox(lockCtx, candidate.ConversationID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.Status != domain.SandboxStatusActive || !current.LastActivityAt.Before(idleBefore) || executionLeaseActive(current, r.now().UTC()) {
			return nil
		}
		handle, stopErr := r.runtime.StopSandbox(lockCtx, lifecycleHandle(current), "sandbox:idle-stop:"+current.ID)
		if stopErr != nil {
			retryCtx, cancel := context.WithTimeout(context.WithoutCancel(lockCtx), 5*time.Second)
			defer cancel()
			retryErr := r.store.TouchConversationSandbox(retryCtx, current.ID)
			return errors.Join(fmt.Errorf("stop idle sandbox %s: %w", current.ID, stopErr), retryErr)
		}
		metadata := current.RuntimeMetadata
		if handle != nil {
			metadata = handle.Metadata
		}
		stopped, err := r.store.StopConversationSandbox(lockCtx, current.ID, metadata)
		if err != nil {
			compensateCtx, cancel := context.WithTimeout(context.WithoutCancel(lockCtx), 30*time.Second)
			defer cancel()
			_, resumeErr := r.runtime.ResumeSandbox(compensateCtx, lifecycleHandle(current), "sandbox:idle-stop:"+current.ID+":compensate")
			return errors.Join(err, resumeErr)
		}
		r.logger.Printf("stopped idle sandbox id=%s conversation_id=%s provider=%s", stopped.ID, stopped.ConversationID, stopped.Provider)
		return nil
	})
}

func (r *Reaper) releaseStopped(ctx context.Context, candidate *domain.ConversationSandbox, stoppedBefore time.Time) error {
	if candidate == nil {
		return nil
	}
	var releasing *domain.ConversationSandbox
	err := withLifecycleLock(ctx, r.locker, candidate.ConversationID, func(lockCtx context.Context) error {
		current, err := r.store.GetUsableConversationSandbox(lockCtx, candidate.ConversationID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.Status != domain.SandboxStatusStopped || current.StoppedAt == nil || !current.StoppedAt.Before(stoppedBefore) {
			return nil
		}
		releasing, err = r.store.BeginConversationSandboxRelease(lockCtx, current.ID)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil || releasing == nil {
		return err
	}
	return r.release(ctx, releasing)
}

func (r *Reaper) release(ctx context.Context, candidate *domain.ConversationSandbox) error {
	if candidate == nil {
		return nil
	}
	current, err := r.store.GetLatestConversationSandbox(ctx, candidate.ConversationID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && current.Status == domain.SandboxStatusDestroyed) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Status != domain.SandboxStatusReleasing {
		return nil
	}
	claimToken := uuid.NewString()
	claimed, err := r.store.ClaimConversationSandboxRelease(ctx, current.ID, claimToken, tool.SandboxReleaseLeaseDuration)
	if errors.Is(err, domain.ErrConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	handle, destroyErr := tool.RunSandboxReleaseOperation(ctx, r.store, claimed.ID, claimToken, func(operationCtx context.Context) (*domain.SandboxHandle, error) {
		return r.runtime.DestroySandbox(operationCtx, lifecycleHandle(claimed), "sandbox:retention-release:"+claimed.ID)
	})
	if destroyErr != nil {
		return fmt.Errorf("release stopped sandbox %s: %w", claimed.ID, destroyErr)
	}
	metadata := claimed.RuntimeMetadata
	if handle != nil && len(handle.Metadata) > 0 {
		metadata = handle.Metadata
	}
	completeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	destroyed, err := r.store.CompleteConversationSandboxRelease(completeCtx, claimed.ID, claimToken, metadata)
	if err != nil {
		latest, latestErr := r.store.GetLatestConversationSandbox(completeCtx, claimed.ConversationID)
		if latestErr == nil && latest.Status == domain.SandboxStatusDestroyed {
			return nil
		}
		return errors.Join(err, latestErr)
	}
	r.logger.Printf("released stopped sandbox id=%s conversation_id=%s provider=%s", destroyed.ID, destroyed.ConversationID, destroyed.Provider)
	return nil
}

func executionLeaseActive(sandbox *domain.ConversationSandbox, now time.Time) bool {
	return sandbox != nil && sandbox.ExecutionLeaseUntil != nil && sandbox.ExecutionLeaseUntil.After(now)
}

func withLifecycleLock(ctx context.Context, locker tool.ConversationLocker, conversationID string, fn func(context.Context) error) error {
	if locker == nil {
		return fn(ctx)
	}
	return locker.WithConversationLock(ctx, conversationID, fn)
}

func lifecycleHandle(sandbox *domain.ConversationSandbox) domain.SandboxHandle {
	if sandbox == nil {
		return domain.SandboxHandle{}
	}
	return domain.SandboxHandle{Provider: sandbox.Provider, RuntimeID: sandbox.RuntimeID, Metadata: sandbox.RuntimeMetadata}
}
