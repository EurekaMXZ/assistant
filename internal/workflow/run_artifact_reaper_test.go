package workflow

import (
	"context"
	"testing"
	"time"
)

type artifactReferenceStub struct {
	keys []string
}

func (s artifactReferenceStub) ListReferencedRunArtifactKeys(context.Context) ([]string, error) {
	return s.keys, nil
}

type artifactObjectStub struct {
	objects []RunArtifactObject
	deleted []string
}

func (s *artifactObjectStub) ListRunArtifactObjects(context.Context, string) ([]RunArtifactObject, error) {
	return s.objects, nil
}

func (s *artifactObjectStub) DeleteObject(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func TestRunArtifactReaperDeletesOnlyOldUnreferencedObjects(t *testing.T) {
	now := time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC)
	objects := &artifactObjectStub{objects: []RunArtifactObject{
		{Key: "conversations/c1/turns/t1/runs/000001-r1/request.json.zst", LastModified: now.Add(-48 * time.Hour)},
		{Key: "conversations/c1/turns/t1/runs/000001-r1/response.json.zst", LastModified: now.Add(-48 * time.Hour)},
		{Key: "conversations/c1/turns/t1/runs/000002-r2/request.json.zst", LastModified: now.Add(-time.Hour)},
	}}
	reaper := NewRunArtifactReaper(RunArtifactReaperSettings{SafetyInterval: 24 * time.Hour, BatchSize: 10}, artifactReferenceStub{
		keys: []string{"conversations/c1/turns/t1/runs/000001-r1/response.json.zst"},
	}, objects, nil)
	reaper.now = func() time.Time { return now }

	if err := reaper.Reap(context.Background()); err != nil {
		t.Fatalf("reap artifacts: %v", err)
	}
	if len(objects.deleted) != 1 || objects.deleted[0] != "conversations/c1/turns/t1/runs/000001-r1/request.json.zst" {
		t.Fatalf("deleted objects = %#v", objects.deleted)
	}
}
