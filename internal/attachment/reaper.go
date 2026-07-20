package attachment

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type CleanupSettings struct {
	PendingTTL time.Duration
	Interval   time.Duration
	BatchSize  int
}

type CleanupRepository interface {
	ListExpiredAttachmentUploads(ctx context.Context, createdBefore time.Time, limit int) ([]domain.Attachment, error)
	ClaimExpiredAttachmentUpload(ctx context.Context, attachmentID string, createdBefore time.Time) (*domain.Attachment, error)
	DeleteClaimedAttachmentUpload(ctx context.Context, attachmentID string) error
}

type ObjectDeleter interface {
	DeleteObject(ctx context.Context, key string) error
}

type Reaper struct {
	settings CleanupSettings
	repo     CleanupRepository
	objects  ObjectDeleter
	logger   *log.Logger
	now      func() time.Time
}

func NewReaper(settings CleanupSettings, repo CleanupRepository, objects ObjectDeleter, logger *log.Logger) *Reaper {
	return &Reaper{settings: settings, repo: repo, objects: objects, logger: logger, now: time.Now}
}

func (r *Reaper) Run(ctx context.Context) error {
	interval := r.settings.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if err := r.Reap(ctx); err != nil && r.logger != nil {
		r.logger.Printf("reap expired attachment uploads: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.Reap(ctx); err != nil && r.logger != nil {
				r.logger.Printf("reap expired attachment uploads: %v", err)
			}
		}
	}
}

func (r *Reaper) Reap(ctx context.Context) error {
	if r == nil || r.repo == nil || r.objects == nil {
		return fmt.Errorf("attachment upload reaper is not configured")
	}
	ttl := r.settings.PendingTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	limit := r.settings.BatchSize
	if limit <= 0 {
		limit = 100
	}
	createdBefore := r.now().UTC().Add(-ttl)
	items, err := r.repo.ListExpiredAttachmentUploads(ctx, createdBefore, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		claimed, claimErr := r.repo.ClaimExpiredAttachmentUpload(ctx, item.ID, createdBefore)
		if claimErr != nil {
			if errors.Is(claimErr, domain.ErrNotFound) {
				continue
			}
			return claimErr
		}
		if err := r.objects.DeleteObject(ctx, claimed.ObjectKey); err != nil {
			return err
		}
		if err := r.repo.DeleteClaimedAttachmentUpload(ctx, claimed.ID); err != nil {
			return err
		}
	}
	return nil
}
