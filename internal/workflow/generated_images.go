package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/google/uuid"
)

type generatedImageObjectDeleter interface {
	DeleteObject(ctx context.Context, key string) error
}

func (r *TurnRunner) generatedImageDrafts(ctx context.Context, turn *domain.Turn, result *llm.ModelResult) ([]domain.AssistantMessageDraft, error) {
	if turn == nil || result == nil || len(result.OutputItems) == 0 {
		return nil, nil
	}

	var imageItems []llm.ModelItem
	for _, item := range result.OutputItems {
		if item.Type == llm.ModelItemImageGenerationCall && strings.TrimSpace(item.Result) != "" {
			imageItems = append(imageItems, item)
		}
	}
	if len(imageItems) == 0 {
		return nil, nil
	}
	if r == nil || r.blobs == nil || r.generatedAttachments == nil || r.conversations == nil {
		return nil, fmt.Errorf("generated image persistence is not configured")
	}

	conversation, err := r.conversations.GetConversation(ctx, turn.ConversationID)
	if err != nil {
		return nil, err
	}
	ownerUserID := strings.TrimSpace(conversation.OwnerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("conversation owner is required for generated image attachments")
	}

	drafts := make([]domain.AssistantMessageDraft, 0, len(imageItems))
	for index, item := range imageItems {
		draft, err := r.generatedImageDraft(ctx, turn, result.ResponseID, ownerUserID, item, index)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	return drafts, nil
}

func billableImageGenerationCount(result *llm.ModelResult) int {
	if result == nil {
		return 0
	}
	count := 0
	for _, item := range result.OutputItems {
		if item.Type == llm.ModelItemImageGenerationCall && strings.TrimSpace(item.Result) != "" {
			count++
		}
	}
	return count
}

func (r *TurnRunner) generatedImageDraft(ctx context.Context, turn *domain.Turn, responseID string, ownerUserID string, item llm.ModelItem, outputIndex int) (domain.AssistantMessageDraft, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(item.Result))
	if err != nil {
		return domain.AssistantMessageDraft{}, fmt.Errorf("decode generated image %s: %w", generatedImageItemID(item, outputIndex), err)
	}
	if len(data) == 0 {
		return domain.AssistantMessageDraft{}, fmt.Errorf("generated image %s is empty", generatedImageItemID(item, outputIndex))
	}

	format, contentType := detectGeneratedImageFormat(data)
	objectKey := generatedImageObjectKey(turn.ConversationID, turn.ID, generatedImageItemID(item, outputIndex), format)
	if err := r.blobs.PutBytes(ctx, objectKey, data, contentType); err != nil {
		return domain.AssistantMessageDraft{}, fmt.Errorf("store generated image %s: %w", generatedImageItemID(item, outputIndex), err)
	}

	metadata, err := json.Marshal(map[string]any{
		"source":         "image_generation",
		"response_id":    strings.TrimSpace(responseID),
		"turn_id":        turn.ID,
		"output_item_id": strings.TrimSpace(item.ID),
		"output_index":   outputIndex,
		"revised_prompt": strings.TrimSpace(item.RevisedPrompt),
	})
	if err != nil {
		return domain.AssistantMessageDraft{}, fmt.Errorf("marshal generated image metadata: %w", err)
	}

	attachmentID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(objectKey)).String()
	attachment, err := r.generatedAttachments.UpsertAttachment(ctx, assistantattachment.CreateAttachmentParams{
		ID:               attachmentID,
		ConversationID:   turn.ConversationID,
		UploadedByUserID: ownerUserID,
		Filename:         fmt.Sprintf("generated-%s.%s", generatedImageItemID(item, outputIndex), format),
		ContentType:      contentType,
		Category:         domain.AttachmentCategoryImage,
		SizeBytes:        int64(len(data)),
		SHA256:           generatedImageSHA256(data),
		ObjectKey:        objectKey,
		Metadata:         metadata,
	})
	if err != nil {
		if deleter, ok := r.blobs.(generatedImageObjectDeleter); ok {
			_ = deleter.DeleteObject(ctx, objectKey)
		}
		return domain.AssistantMessageDraft{}, fmt.Errorf("record generated image attachment: %w", err)
	}

	draftMetadata, err := json.Marshal(map[string]any{
		"display_kind":   "assistant_image",
		"source":         "image_generation",
		"response_id":    strings.TrimSpace(responseID),
		"model_item_id":  strings.TrimSpace(item.ID),
		"output_index":   outputIndex,
		"revised_prompt": strings.TrimSpace(item.RevisedPrompt),
		"attachment_ids": []string{attachment.ID},
		"attachments":    []map[string]any{attachmentSummary(*attachment)},
	})
	if err != nil {
		return domain.AssistantMessageDraft{}, fmt.Errorf("marshal generated image message metadata: %w", err)
	}

	return domain.AssistantMessageDraft{ContentText: "已生成图片", Metadata: draftMetadata}, nil
}

func generatedImageSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func generatedImageObjectKey(conversationID string, turnID string, itemID string, format string) string {
	return fmt.Sprintf("generated-images/%s/%s/%s.%s", strings.TrimSpace(conversationID), strings.TrimSpace(turnID), safeObjectKeyPart(itemID), format)
}

func generatedImageItemID(item llm.ModelItem, index int) string {
	if id := strings.TrimSpace(item.ID); id != "" {
		return id
	}
	return fmt.Sprintf("image-%d", index)
}

func safeObjectKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "image"
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func detectGeneratedImageFormat(data []byte) (string, string) {
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return "png", "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "jpg", "image/jpeg"
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "webp", "image/webp"
	}
	return "png", "image/png"
}

func attachmentSummary(attachment domain.Attachment) map[string]any {
	return map[string]any{
		"id":           attachment.ID,
		"filename":     attachment.Filename,
		"content_type": attachment.ContentType,
		"category":     attachment.Category,
		"size_bytes":   attachment.SizeBytes,
	}
}
