package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/google/uuid"
)

func assistantAttachmentDraftsFromItems(items []llm.ModelItem, conversationID string, turnID string) ([]domain.AssistantMessageDraft, error) {
	exportCalls := make(map[string]struct{})
	for _, item := range items {
		if item.Type != llm.ModelItemFunctionCall {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if namespace := strings.TrimSpace(item.Namespace); namespace != "" {
			name = namespace + "." + name
		}
		if name == tool.SandboxExportFileTool || name == tool.ConversationExportText ||
			name == llm.SafeToolName(tool.SandboxExportFileTool) || name == llm.SafeToolName(tool.ConversationExportText) {
			exportCalls[strings.TrimSpace(item.CallID)] = struct{}{}
		}
	}

	seen := make(map[string]struct{})
	drafts := make([]domain.AssistantMessageDraft, 0)
	for _, item := range items {
		if item.Type != llm.ModelItemFunctionCallOutput {
			continue
		}
		callID := strings.TrimSpace(item.CallID)
		if _, ok := exportCalls[callID]; !ok {
			continue
		}
		var output struct {
			OK                  *bool                              `json:"ok,omitempty"`
			AssistantAttachment *tool.AssistantAttachmentReference `json:"assistant_attachment,omitempty"`
		}
		if err := json.Unmarshal([]byte(item.Output), &output); err != nil {
			return nil, fmt.Errorf("decode assistant attachment output for call %s: %w", callID, err)
		}
		if output.OK != nil && !*output.OK {
			continue
		}
		reference := output.AssistantAttachment
		if reference == nil {
			return nil, fmt.Errorf("assistant attachment output for call %s is missing its reference", callID)
		}
		if reference.ConversationID != conversationID || reference.TurnID != turnID {
			return nil, fmt.Errorf("assistant attachment output for call %s has the wrong scope", callID)
		}
		attachment := reference.Attachment
		if _, err := uuid.Parse(strings.TrimSpace(attachment.ID)); err != nil {
			return nil, fmt.Errorf("assistant attachment output for call %s has an invalid attachment id", callID)
		}
		if _, exists := seen[attachment.ID]; exists {
			continue
		}
		seen[attachment.ID] = struct{}{}
		metadata, err := json.Marshal(map[string]any{
			"display_kind":   "assistant_attachment",
			"source":         strings.TrimSpace(reference.Source),
			"tool_call_id":   callID,
			"attachment_ids": []string{attachment.ID},
			"attachments": []map[string]any{{
				"id": attachment.ID, "filename": attachment.Filename,
				"content_type": attachment.ContentType, "category": attachment.Category,
				"size_bytes": attachment.SizeBytes,
			}},
		})
		if err != nil {
			return nil, fmt.Errorf("marshal assistant attachment message metadata: %w", err)
		}
		drafts = append(drafts, domain.AssistantMessageDraft{Metadata: metadata})
	}
	return drafts, nil
}
