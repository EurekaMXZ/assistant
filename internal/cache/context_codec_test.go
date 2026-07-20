package cache

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

func TestContextSnapshotCodecRoundTrip(t *testing.T) {
	snapshot := &ContextSnapshot{
		ConversationID:        "conv-1",
		Version:               7,
		SchemaVersion:         ContextSnapshotSchemaVersion,
		CoveredEventSeq:       12,
		LatestCheckpointKey:   "checkpoint-1",
		LatestSuccessfulRunID: "run-1",
		CreatedAt:             time.Unix(100, 0).UTC(),
		ModelInputReady:       true,
		ModelInput: []llm.ModelItem{{
			Type: llm.ModelItemMessage,
			Role: "user",
			Raw:  json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_image","image_ref":{"object_key":"image-1"}}]}`),
		}},
	}
	encoded, err := EncodeContextSnapshot(snapshot)
	if err != nil {
		t.Fatalf("encode context snapshot: %v", err)
	}
	decoded, err := DecodeContextSnapshot(encoded)
	if err != nil {
		t.Fatalf("decode context snapshot: %v", err)
	}
	if decoded.ConversationID != snapshot.ConversationID || decoded.Version != snapshot.Version || decoded.Checksum == "" {
		t.Fatalf("decoded snapshot = %#v", decoded)
	}
}

func TestContextSnapshotCodecRejectsInlineImages(t *testing.T) {
	_, err := EncodeContextSnapshot(&ContextSnapshot{
		ConversationID:  "conv-1",
		Version:         1,
		SchemaVersion:   ContextSnapshotSchemaVersion,
		ModelInputReady: true,
		ModelInput: []llm.ModelItem{{
			Type: llm.ModelItemMessage,
			Raw:  json.RawMessage(`{"content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}`),
		}},
	})
	if err == nil {
		t.Fatal("expected inline image rejection")
	}
}

func TestStoreKeepsImmutableContextVersions(t *testing.T) {
	store := New(8, 2)
	store.PutVersion("conv-1", 1, &ContextSnapshot{Version: 1, ActiveTokens: 10})
	store.PutVersion("conv-1", 2, &ContextSnapshot{Version: 2, ActiveTokens: 20})
	first, ok := store.GetVersion("conv-1", 1)
	if !ok || first.ActiveTokens != 10 {
		t.Fatalf("version 1 = %#v, found=%t", first, ok)
	}
	latest, ok := store.Get("conv-1")
	if !ok || latest.Version != 2 || latest.ActiveTokens != 20 {
		t.Fatalf("latest = %#v, found=%t", latest, ok)
	}
}
