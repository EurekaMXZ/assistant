package workflow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type GeneratedImageAssetCleanupStore interface {
	ListExpiredGeneratedImageAssets(ctx context.Context, expiredBefore time.Time, limit int) ([]domain.GeneratedImageAsset, error)
	ClaimExpiredGeneratedImageAsset(ctx context.Context, assetID string, expiredBefore time.Time) (*domain.GeneratedImageAsset, error)
	DeleteClaimedGeneratedImageAsset(ctx context.Context, assetID string) error
}

type GeneratedImageReaperSettings struct {
	Interval  time.Duration
	BatchSize int
}

type GeneratedImageReaper struct {
	settings GeneratedImageReaperSettings
	store    GeneratedImageAssetCleanupStore
	objects  generatedImageObjectDeleter
	logger   *log.Logger
	now      func() time.Time
}

func NewGeneratedImageReaper(settings GeneratedImageReaperSettings, store GeneratedImageAssetCleanupStore, objects generatedImageObjectDeleter, logger *log.Logger) *GeneratedImageReaper {
	return &GeneratedImageReaper{settings: settings, store: store, objects: objects, logger: logger, now: time.Now}
}

func (r *GeneratedImageReaper) Run(ctx context.Context) error {
	interval := r.settings.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if err := r.Reap(ctx); err != nil && r.logger != nil {
		r.logger.Printf("reap generated image previews: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.Reap(ctx); err != nil && r.logger != nil {
				r.logger.Printf("reap generated image previews: %v", err)
			}
		}
	}
}

func (r *GeneratedImageReaper) Reap(ctx context.Context) error {
	if r == nil || r.store == nil || r.objects == nil {
		return errors.New("generated image reaper is not configured")
	}
	limit := r.settings.BatchSize
	if limit <= 0 {
		limit = 100
	}
	cutoff := r.now().UTC()
	assets, err := r.store.ListExpiredGeneratedImageAssets(ctx, cutoff, limit)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		claimed, err := r.store.ClaimExpiredGeneratedImageAsset(ctx, asset.ID, cutoff)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := r.objects.DeleteObject(ctx, claimed.ObjectKey); err != nil {
			return fmt.Errorf("delete generated image preview %s: %w", claimed.ID, err)
		}
		if err := r.store.DeleteClaimedGeneratedImageAsset(ctx, claimed.ID); err != nil {
			return err
		}
	}
	return nil
}
