package workflow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

type RunArtifactReaperSettings struct {
	SafetyInterval time.Duration
	Interval       time.Duration
	BatchSize      int
}

type RunArtifactReaper struct {
	settings   RunArtifactReaperSettings
	references RunArtifactReferenceStore
	objects    RunArtifactObjectStore
	logger     *log.Logger
	now        func() time.Time
}

func NewRunArtifactReaper(settings RunArtifactReaperSettings, references RunArtifactReferenceStore, objects RunArtifactObjectStore, logger *log.Logger) *RunArtifactReaper {
	return &RunArtifactReaper{settings: settings, references: references, objects: objects, logger: logger, now: time.Now}
}

func (r *RunArtifactReaper) Run(ctx context.Context) error {
	interval := r.settings.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if err := r.Reap(ctx); err != nil && r.logger != nil {
		r.logger.Printf("reap orphan run artifacts: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.Reap(ctx); err != nil && r.logger != nil {
				r.logger.Printf("reap orphan run artifacts: %v", err)
			}
		}
	}
}

func (r *RunArtifactReaper) Reap(ctx context.Context) error {
	if r == nil || r.references == nil || r.objects == nil {
		return errors.New("run artifact reaper is not configured")
	}
	safetyInterval := r.settings.SafetyInterval
	if safetyInterval <= 0 {
		safetyInterval = 24 * time.Hour
	}
	batchSize := r.settings.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	keys, err := r.references.ListReferencedRunArtifactKeys(ctx)
	if err != nil {
		return err
	}
	referenced := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		referenced[key] = struct{}{}
	}
	objects, err := r.objects.ListRunArtifactObjects(ctx, "conversations/")
	if err != nil {
		return err
	}
	cutoff := r.now().UTC().Add(-safetyInterval)
	deleted := 0
	for _, object := range objects {
		if object.Key == "" || !strings.Contains(object.Key, "/turns/") || object.LastModified.IsZero() || object.LastModified.After(cutoff) {
			continue
		}
		if _, ok := referenced[object.Key]; ok {
			continue
		}
		if err := r.objects.DeleteObject(ctx, object.Key); err != nil {
			return fmt.Errorf("delete orphan run artifact %s: %w", object.Key, err)
		}
		deleted++
		if deleted >= batchSize {
			break
		}
	}
	return nil
}
