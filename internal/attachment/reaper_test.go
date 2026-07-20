package attachment

import (
	"context"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type cleanupRepositoryStub struct {
	item    domain.Attachment
	deleted bool
}

func (s *cleanupRepositoryStub) ListExpiredAttachmentUploads(context.Context, time.Time, int) ([]domain.Attachment, error) {
	return []domain.Attachment{s.item}, nil
}

func (s *cleanupRepositoryStub) ClaimExpiredAttachmentUpload(context.Context, string, time.Time) (*domain.Attachment, error) {
	claimed := s.item
	claimed.Status = domain.AttachmentStatusDeleting
	return &claimed, nil
}

func (s *cleanupRepositoryStub) DeleteClaimedAttachmentUpload(context.Context, string) error {
	s.deleted = true
	return nil
}

type objectDeleterStub struct{ key string }

func (s *objectDeleterStub) DeleteObject(_ context.Context, key string) error {
	s.key = key
	return nil
}

func TestReaperDeletesClaimedExpiredUpload(t *testing.T) {
	repo := &cleanupRepositoryStub{item: domain.Attachment{ID: "attachment-1", Status: domain.AttachmentStatusPending, ObjectKey: "attachments/pending"}}
	objects := &objectDeleterStub{}
	reaper := NewReaper(CleanupSettings{PendingTTL: time.Hour, BatchSize: 10}, repo, objects, nil)
	reaper.now = func() time.Time { return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC) }

	if err := reaper.Reap(t.Context()); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if objects.key != "attachments/pending" || !repo.deleted {
		t.Fatalf("cleanup did not finish: key=%q deleted=%v", objects.key, repo.deleted)
	}
}
