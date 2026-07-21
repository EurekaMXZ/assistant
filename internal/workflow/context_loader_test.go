package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type stubContextLoaderArtifactStore struct {
	data map[string][]byte
}

type stubContextLoaderAttachmentStore struct {
	attachments []domain.Attachment
}

func (s *stubContextLoaderAttachmentStore) ListAttachmentsByIDs(_ context.Context, conversationID string, ids []string) ([]domain.Attachment, error) {
	index := make(map[string]domain.Attachment, len(s.attachments))
	for _, item := range s.attachments {
		if item.ConversationID == conversationID {
			index[item.ID] = item
		}
	}
	items := make([]domain.Attachment, 0, len(ids))
	for _, id := range ids {
		item, ok := index[id]
		if !ok {
			return nil, domain.ErrNotFound
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *stubContextLoaderArtifactStore) PutBytes(context.Context, string, []byte, string) error {
	return nil
}

func (s *stubContextLoaderArtifactStore) GetBytes(_ context.Context, key string) ([]byte, error) {
	data, ok := s.data[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return append([]byte(nil), data...), nil
}

func (s *stubContextLoaderArtifactStore) TurnRequestKey(conversationID, turnID string) string {
	return fmt.Sprintf("request:%s:%s", conversationID, turnID)
}

func (s *stubContextLoaderArtifactStore) TurnResponseKey(conversationID, turnID string) string {
	return fmt.Sprintf("response:%s:%s", conversationID, turnID)
}

func (s *stubContextLoaderArtifactStore) TurnStreamKey(conversationID, turnID string) string {
	return fmt.Sprintf("stream:%s:%s", conversationID, turnID)
}

func (s *stubContextLoaderArtifactStore) TurnModelContextKey(conversationID, turnID string) string {
	return fmt.Sprintf("model-context:%s:%s", conversationID, turnID)
}

func TestBuildTurnModelInputUsesConversationHistory(t *testing.T) {
	input := buildTurnModelInput(&cache.ContextSnapshot{
		Anchor: &cache.ContextAnchor{
			Role:    domain.RoleUser,
			Content: formatConversationCheckpoint("Stable compacted history"),
		},
		Tail: []domain.Message{
			{
				Role:        domain.RoleUser,
				ContentText: "Please keep going",
			},
		},
	})

	if len(input) != 2 {
		t.Fatalf("expected anchor followed by history message, got %#v", input)
	}
	if input[0].Role != domain.RoleUser || !strings.Contains(input[0].Content, "Stable compacted history") {
		t.Fatalf("unexpected stable prefix item: %#v", input[0])
	}
	if input[1].Role != domain.RoleUser {
		t.Fatalf("unexpected history item: %#v", input[0])
	}
}

func TestContextLoaderUsesTurnModelContextForAssistantHistory(t *testing.T) {
	loader := &ContextLoader{
		modelContexts: &stubContextLoaderArtifactStore{
			data: map[string][]byte{
				"model-context:conv-1:turn-1": []byte(`[
					{"type":"reasoning","raw":{"type":"reasoning","encrypted_content":"ciphertext"}},
					{"type":"message","role":"assistant","content":"done"}
				]`),
			},
		},
	}
	hot := &cache.ContextSnapshot{
		Tail: []domain.Message{
			{
				ConversationID: "conv-1",
				TurnID:         "turn-1",
				Role:           domain.RoleAssistant,
				ContentText:    "fallback text",
			},
		},
	}

	if err := loader.loadConversationModelInput(context.Background(), "conv-1", hot); err != nil {
		t.Fatalf("load model input: %v", err)
	}
	input := buildConversationHistoryInput(hot)
	if len(input) != 2 {
		t.Fatalf("expected reasoning + assistant output, got %#v", input)
	}
	if input[0].Type != "reasoning" || !strings.Contains(string(input[0].Raw), "ciphertext") {
		t.Fatalf("expected encrypted reasoning raw item, got %#v", input[0])
	}
	if input[1].Role != domain.RoleAssistant || input[1].Content != "done" {
		t.Fatalf("unexpected assistant item: %#v", input[1])
	}
}

func TestContextLoaderKeepsAllAssistantMessagesWhenTurnContextIsMissing(t *testing.T) {
	loader := &ContextLoader{modelContexts: &stubContextLoaderArtifactStore{data: map[string][]byte{}}}
	hot := &cache.ContextSnapshot{Tail: []domain.Message{
		{ConversationID: "conv-1", TurnID: "turn-1", Role: domain.RoleAssistant, ContentText: "Progress update"},
		{ConversationID: "conv-1", TurnID: "turn-1", Role: domain.RoleAssistant, ContentText: "Final answer"},
	}}

	if err := loader.loadConversationModelInput(context.Background(), "conv-1", hot); err != nil {
		t.Fatalf("load model input: %v", err)
	}
	input := buildConversationHistoryInput(hot)
	if len(input) != 2 {
		t.Fatalf("expected both assistant messages, got %#v", input)
	}
	if input[0].Content != "Progress update" || input[1].Content != "Final answer" {
		t.Fatalf("assistant message order was not preserved: %#v", input)
	}
}

func TestContextLoaderBuildsImageMessageInputWithAttachmentContent(t *testing.T) {
	loader := &ContextLoader{
		attachments: &stubContextLoaderAttachmentStore{
			attachments: []domain.Attachment{
				{
					ID:             "att-img",
					ConversationID: "conv-1",
					Filename:       "screen.png",
					ContentType:    "image/png",
					Category:       domain.AttachmentCategoryImage,
					ObjectKey:      "attachments/conv-1/att-img/screen.png",
				},
				{
					ID:             "att-pdf",
					ConversationID: "conv-1",
					Filename:       "brief.pdf",
					ContentType:    "application/pdf",
					Category:       domain.AttachmentCategoryDocument,
					ObjectKey:      "attachments/conv-1/att-pdf/brief.pdf",
				},
			},
		},
		attachmentBlobs: &stubContextLoaderArtifactStore{
			data: map[string][]byte{
				"attachments/conv-1/att-img/screen.png": []byte("pngdata"),
			},
		},
	}
	hot := &cache.ContextSnapshot{
		Tail: []domain.Message{
			{
				ConversationID: "conv-1",
				Role:           domain.RoleUser,
				ContentText:    "Check this",
				Metadata:       json.RawMessage(`{"attachment_ids":["att-img","att-pdf"]}`),
			},
		},
	}

	if err := loader.loadConversationModelInput(context.Background(), "conv-1", hot); err != nil {
		t.Fatalf("load model input: %v", err)
	}
	input := buildConversationHistoryInput(hot)
	if len(input) != 1 {
		t.Fatalf("expected one user input item, got %#v", input)
	}
	if len(input[0].Raw) == 0 {
		t.Fatalf("expected raw item payload, got %#v", input[0])
	}
	raw := string(input[0].Raw)
	if !strings.Contains(raw, `"type":"input_image"`) || !strings.Contains(raw, `"object_key":"attachments/conv-1/att-img/screen.png"`) {
		t.Fatalf("expected image reference, got %s", raw)
	}
	if strings.Contains(raw, `data:image/png;base64,`) {
		t.Fatalf("persisted model input must not contain image base64, got %s", raw)
	}
	if !strings.Contains(raw, `brief.pdf`) || !strings.Contains(raw, `attachment_id=att-pdf`) || !strings.Contains(raw, `sandbox.import_attachment`) {
		t.Fatalf("expected sandbox attachment manifest, got %s", raw)
	}
	if !strings.Contains(raw, `"text":"Check this"`) {
		t.Fatalf("expected original text in payload, got %s", raw)
	}
	state := &ScheduledRunState{Request: llm.ModelRequest{Input: input}}
	if err := loader.hydrateScheduledRunImages(t.Context(), state); err != nil {
		t.Fatalf("hydrate provider request: %v", err)
	}
	if hydrated := string(state.Request.Input[0].Raw); !strings.Contains(hydrated, `data:image/png;base64,`) || strings.Contains(hydrated, `"image_ref"`) {
		t.Fatalf("expected provider-only image hydration, got %s", hydrated)
	}
}

func TestContextLoaderRejectsCorruptedImageAttachment(t *testing.T) {
	loader := &ContextLoader{attachmentBlobs: &stubContextLoaderArtifactStore{data: map[string][]byte{"image": []byte("bad")}}}
	_, err := loader.hydrateImageReference(t.Context(), modelImageReference{
		AttachmentID: "attachment-1", ContentType: "image/png", SizeBytes: 3,
		SHA256: strings.Repeat("0", 64), ObjectKey: "image",
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
}

func TestContextLoaderRejectsOversizedImageAttachment(t *testing.T) {
	loader := &ContextLoader{attachmentBlobs: &stubContextLoaderArtifactStore{data: map[string][]byte{"image": []byte("data")}}}
	_, err := loader.hydrateImageReference(t.Context(), modelImageReference{
		AttachmentID: "attachment-1", ContentType: "image/png", SizeBytes: maxProviderImageBytes + 1,
		ObjectKey: "image",
	})
	if err == nil || !strings.Contains(err.Error(), "image exceeds") {
		t.Fatalf("error = %v, want image limit", err)
	}
}

func TestContextLoaderBuildsNoCheckpointSnapshotFromCompleteEvents(t *testing.T) {
	messageEvent := func(seq int64, message domain.Message) domain.ConversationEvent {
		payload, err := json.Marshal(map[string]any{"message": message})
		if err != nil {
			t.Fatalf("marshal message event: %v", err)
		}
		return domain.ConversationEvent{
			EventSeq: seq, EventKey: fmt.Sprintf("message-%d", seq), EventType: "message.completed",
			Payload: payload, ContextIncluded: true,
		}
	}
	loader := &ContextLoader{completeEvents: &stubCompleteEventStore{contextEvents: []domain.ConversationEvent{
		messageEvent(1, domain.Message{Role: domain.RoleUser, ContentText: "question"}),
		messageEvent(2, domain.Message{Role: domain.RoleAssistant, ContentText: "answer"}),
	}}}
	head := &domain.ContextHead{
		ConversationID: "conv-1", Version: 3, ContextSchemaVersion: 1,
		LastContextEventSeq: 2, LastSeq: 2,
	}

	snapshot, found, err := loader.loadEventSnapshot(t.Context(), "conv-1", head)
	if err != nil || !found {
		t.Fatalf("load event snapshot: found=%t err=%v", found, err)
	}
	if len(snapshot.ModelInput) != 2 || snapshot.ModelInput[0].Content != "question" || snapshot.ModelInput[1].Content != "answer" {
		t.Fatalf("event snapshot model input = %#v", snapshot.ModelInput)
	}
}

func TestContextLoaderKeepsLegacyAnchorFallbackWithoutCheckpoint(t *testing.T) {
	loader := &ContextLoader{completeEvents: &stubCompleteEventStore{contextEvents: []domain.ConversationEvent{{
		EventSeq: 1, EventType: "message.completed", ContextIncluded: true,
	}}}}
	_, found, err := loader.loadEventSnapshot(t.Context(), "conv-1", &domain.ContextHead{
		AnchorKey: "legacy-anchor", LastContextEventSeq: 1,
	})
	if err != nil || found {
		t.Fatalf("legacy anchor event fallback: found=%t err=%v", found, err)
	}
}

func TestContextLoaderRejectsCheckpointChecksumMismatch(t *testing.T) {
	payload, err := json.Marshal(immutableContextCheckpoint{
		SchemaVersion: immutableRunArtifactSchemaVersion, ConversationID: "conv-1",
		ModelItems: []llm.ModelItem{{Type: llm.ModelItemMessage, Role: domain.RoleUser, Content: "history"}},
	})
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	compressed, _, err := compressImmutableRunPayload(payload)
	if err != nil {
		t.Fatalf("compress checkpoint: %v", err)
	}
	loader := &ContextLoader{
		modelContexts:  &stubContextLoaderArtifactStore{data: map[string][]byte{"checkpoint": compressed}},
		completeEvents: &stubCompleteEventStore{},
	}
	_, _, err = loader.loadCheckpointSnapshot(t.Context(), "conv-1", &domain.ContextHead{
		LatestCheckpointKey: "checkpoint", LatestCheckpointChecksum: strings.Repeat("0", 64),
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("checkpoint error = %v", err)
	}
}
