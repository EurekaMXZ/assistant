package workflow

import (
	"encoding/json"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

func TestAssistantAttachmentDraftsFromItems(t *testing.T) {
	reference := tool.AssistantAttachmentToolOutput{AssistantAttachment: &tool.AssistantAttachmentReference{
		ConversationID: "conversation-1",
		TurnID:         "turn-1",
		Source:         "text_export",
		Attachment: tool.AssistantAttachmentResult{
			ID: "11111111-1111-1111-1111-111111111111", Filename: "result.md",
			ContentType: "text/plain", Category: "text", SizeBytes: 12,
		},
	}}
	payload, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	items := []llm.ModelItem{
		{Type: llm.ModelItemFunctionCall, Name: "conversation_export_text", CallID: "call-1"},
		{Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: string(payload)},
		{Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: string(payload)},
	}
	drafts, err := assistantAttachmentDraftsFromItems(items, "conversation-1", "turn-1")
	if err != nil {
		t.Fatalf("build attachment drafts: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("attachment draft count = %d, want 1", len(drafts))
	}
	var metadata map[string]any
	if err := json.Unmarshal(drafts[0].Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["display_kind"] != "assistant_attachment" || metadata["source"] != "text_export" {
		t.Fatalf("unexpected attachment metadata: %#v", metadata)
	}
}

func TestAssistantAttachmentDraftsIgnoreUnmatchedOutput(t *testing.T) {
	payload, _ := json.Marshal(tool.AssistantAttachmentToolOutput{AssistantAttachment: &tool.AssistantAttachmentReference{
		ConversationID: "conversation-1", TurnID: "turn-1", Source: "text_export",
		Attachment: tool.AssistantAttachmentResult{ID: "11111111-1111-1111-1111-111111111111"},
	}})
	drafts, err := assistantAttachmentDraftsFromItems([]llm.ModelItem{{
		Type: llm.ModelItemFunctionCallOutput, CallID: "remote-call", Output: string(payload),
	}}, "conversation-1", "turn-1")
	if err != nil {
		t.Fatalf("ignore unmatched output: %v", err)
	}
	if len(drafts) != 0 {
		t.Fatalf("unmatched output created %d drafts", len(drafts))
	}
}

func TestAssistantAttachmentDraftsRejectWrongScope(t *testing.T) {
	payload, _ := json.Marshal(tool.AssistantAttachmentToolOutput{AssistantAttachment: &tool.AssistantAttachmentReference{
		ConversationID: "other-conversation", TurnID: "turn-1", Source: "text_export",
		Attachment: tool.AssistantAttachmentResult{ID: "11111111-1111-1111-1111-111111111111"},
	}})
	_, err := assistantAttachmentDraftsFromItems([]llm.ModelItem{
		{Type: llm.ModelItemFunctionCall, Namespace: "conversation", Name: "export_text", CallID: "call-1"},
		{Type: llm.ModelItemFunctionCallOutput, CallID: "call-1", Output: string(payload)},
	}, "conversation-1", "turn-1")
	if err == nil {
		t.Fatal("expected wrong-scope output to fail")
	}
}
