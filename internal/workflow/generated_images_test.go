package workflow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type stubGeneratedImageAssetStore struct {
	assets []domain.GeneratedImageAsset
}

func (s *stubGeneratedImageAssetStore) UpsertGeneratedImageAsset(context.Context, domain.UpsertGeneratedImageAssetParams) (*domain.GeneratedImageAsset, error) {
	return nil, nil
}

func (s *stubGeneratedImageAssetStore) ListGeneratedImageAssetsByTurn(context.Context, string) ([]domain.GeneratedImageAsset, error) {
	return s.assets, nil
}

func TestBillableImageGenerationCount(t *testing.T) {
	result := &llm.ModelResult{OutputItems: []llm.ModelItem{
		{Type: llm.ModelItemImageGenerationCall, Result: "image-a"},
		{Type: llm.ModelItemImageGenerationCall, Result: "  "},
		{Type: llm.ModelItemMessage, Result: "not-an-image"},
		{Type: llm.ModelItemImageGenerationCall, Result: "image-b"},
	}}
	if got := billableImageGenerationCount(result); got != 2 {
		t.Fatalf("billable image count = %d, want 2", got)
	}
}

func TestGeneratedImageDraftsForTurnPreserveImageDimensions(t *testing.T) {
	runner := &TurnRunner{generatedImageAssets: &stubGeneratedImageAssetStore{assets: []domain.GeneratedImageAsset{
		{
			AttachmentID: "attachment-1",
			ContentType:  "image/png",
			Height:       1024,
			ItemID:       "image-1",
			Kind:         domain.GeneratedImageKindFinal,
			ResponseID:   "response-1",
			SizeBytes:    2048,
			Width:        1536,
		},
	}}}

	drafts, err := runner.generatedImageDraftsForTurn(context.Background(), &domain.Turn{ID: "turn-1"})
	if err != nil {
		t.Fatalf("generated image drafts for turn: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("draft count = %d, want 1", len(drafts))
	}
	var metadata struct {
		Attachments []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(drafts[0].Metadata, &metadata); err != nil {
		t.Fatalf("decode draft metadata: %v", err)
	}
	if len(metadata.Attachments) != 1 || metadata.Attachments[0].Width != 1536 || metadata.Attachments[0].Height != 1024 {
		t.Fatalf("unexpected draft dimensions: %#v", metadata.Attachments)
	}
}
