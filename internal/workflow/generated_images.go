package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/google/uuid"
)

type generatedImageObjectDeleter interface {
	DeleteObject(ctx context.Context, key string) error
}

type persistedGeneratedImage struct {
	Draft      domain.AssistantMessageDraft
	Attachment domain.Attachment
	Reference  modelImageReference
	Width      int
	Height     int
}

func (r *TurnRunner) generatedImageDraftsForTurn(ctx context.Context, turn *domain.Turn) ([]domain.AssistantMessageDraft, error) {
	if r == nil || r.generatedImageAssets == nil || turn == nil {
		return nil, nil
	}
	assets, err := r.generatedImageAssets.ListGeneratedImageAssetsByTurn(ctx, turn.ID)
	if err != nil {
		return nil, err
	}
	drafts := make([]domain.AssistantMessageDraft, 0)
	for _, asset := range assets {
		if asset.Kind != domain.GeneratedImageKindFinal || strings.TrimSpace(asset.AttachmentID) == "" {
			continue
		}
		format := strings.TrimPrefix(asset.ContentType, "image/")
		if format == "jpeg" {
			format = "jpg"
		}
		metadata, err := json.Marshal(map[string]any{
			"display_kind":   "assistant_image",
			"source":         "image_generation",
			"response_id":    asset.ResponseID,
			"model_item_id":  asset.ItemID,
			"attachment_ids": []string{asset.AttachmentID},
			"attachments": []map[string]any{{
				"id":           asset.AttachmentID,
				"filename":     fmt.Sprintf("generated-%s.%s", safeObjectKeyPart(asset.ItemID), format),
				"content_type": asset.ContentType,
				"category":     domain.AttachmentCategoryImage,
				"size_bytes":   asset.SizeBytes,
				"width":        asset.Width,
				"height":       asset.Height,
			}},
		})
		if err != nil {
			return nil, fmt.Errorf("marshal generated image message metadata: %w", err)
		}
		drafts = append(drafts, domain.AssistantMessageDraft{Metadata: metadata})
	}
	return drafts, nil
}

func billableImageGenerationCount(result *llm.ModelResult) int {
	if result == nil {
		return 0
	}
	count := 0
	for _, item := range result.OutputItems {
		if item.Type == llm.ModelItemImageGenerationCall && (strings.TrimSpace(item.Result) != "" || strings.Contains(string(item.Raw), `"result_ref"`)) {
			count++
		}
	}
	return count
}

func (r *TurnRunner) externalizeGeneratedImages(ctx context.Context, turn *domain.Turn, runID string, outcome *ScheduledRunOutcome) error {
	if outcome == nil || outcome.Model == nil {
		return nil
	}
	hasImages := false
	for _, item := range outcome.Model.OutputItems {
		if item.Type == llm.ModelItemImageGenerationCall && strings.TrimSpace(item.Result) != "" {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return nil
	}
	if r == nil || r.conversations == nil {
		return fmt.Errorf("generated image persistence is not configured")
	}
	conversation, err := r.conversations.GetConversation(ctx, turn.ConversationID)
	if err != nil {
		return err
	}
	ownerUserID := strings.TrimSpace(conversation.OwnerUserID)
	if ownerUserID == "" {
		return fmt.Errorf("conversation owner is required for generated image attachments")
	}
	for index, item := range outcome.Model.OutputItems {
		if item.Type != llm.ModelItemImageGenerationCall || strings.TrimSpace(item.Result) == "" {
			continue
		}
		persisted, err := r.persistGeneratedImage(ctx, turn, runID, outcome.Model.ResponseID, ownerUserID, item, index)
		if err != nil {
			return err
		}
		outcome.GeneratedImageDrafts = append(outcome.GeneratedImageDrafts, persisted.Draft)
		referenced, err := generatedImageReferenceItem(item, persisted.Reference)
		if err != nil {
			return err
		}
		outcome.Model.OutputItems[index] = referenced
		outcome.Model.RawResponse = replaceGeneratedImageResult(outcome.Model.RawResponse, item.ID, persisted.Reference)
		replaceGeneratedImageItem(outcome.ContextItems, item.ID, referenced)
		if outcome.NextState != nil {
			replaceGeneratedImageItem(outcome.NextState.Request.Input, item.ID, referenced)
		}
		if strings.TrimSpace(runID) != "" {
			asset, err := r.upsertGeneratedImageAsset(ctx, domain.UpsertGeneratedImageAssetParams{
				ID:             generatedImageAssetID(persisted.Reference.ObjectKey, domain.GeneratedImageKindFinal, 0),
				ConversationID: turn.ConversationID,
				TurnID:         turn.ID,
				TurnRunID:      runID,
				ResponseID:     outcome.Model.ResponseID,
				ItemID:         generatedImageItemID(item, index),
				Kind:           domain.GeneratedImageKindFinal,
				Revision:       0,
				ObjectKey:      persisted.Reference.ObjectKey,
				ContentType:    persisted.Reference.ContentType,
				SizeBytes:      persisted.Reference.SizeBytes,
				SHA256:         persisted.Reference.SHA256,
				Width:          persisted.Width,
				Height:         persisted.Height,
				AttachmentID:   persisted.Attachment.ID,
			})
			if err != nil {
				return err
			}
			r.publishGeneratedImageAsset(ctx, turn, runID, outcome.Model.ResponseID, item, index, "completed", asset)
		}
	}
	return nil
}

func replaceGeneratedImageResult(raw json.RawMessage, itemID string, ref modelImageReference) json.RawMessage {
	if len(raw) == 0 || strings.TrimSpace(itemID) == "" {
		return raw
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return raw
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(envelope["response"], &response) != nil {
		return raw
	}
	var output []json.RawMessage
	if json.Unmarshal(response["output"], &output) != nil {
		return raw
	}
	changed := false
	for index, value := range output {
		var outputItem map[string]json.RawMessage
		if json.Unmarshal(value, &outputItem) != nil {
			continue
		}
		var id, itemType string
		if json.Unmarshal(outputItem["id"], &id) != nil || id != itemID ||
			json.Unmarshal(outputItem["type"], &itemType) != nil || itemType != llm.ModelItemImageGenerationCall {
			continue
		}
		delete(outputItem, "result")
		outputItem["result_ref"], _ = json.Marshal(ref)
		output[index], _ = json.Marshal(outputItem)
		changed = true
	}
	if !changed {
		return raw
	}
	response["output"], _ = json.Marshal(output)
	envelope["response"], _ = json.Marshal(response)
	updated, err := json.Marshal(envelope)
	if err != nil {
		return raw
	}
	return updated
}

func generatedImageReferenceItem(item llm.ModelItem, ref modelImageReference) (llm.ModelItem, error) {
	raw, err := json.Marshal(map[string]any{
		"id": item.ID, "type": item.Type, "status": item.Status,
		"revised_prompt": item.RevisedPrompt, "result_ref": ref,
	})
	if err != nil {
		return item, fmt.Errorf("marshal generated image reference: %w", err)
	}
	item.Result = ""
	item.Raw = raw
	return item, nil
}

func replaceGeneratedImageItem(items []llm.ModelItem, itemID string, replacement llm.ModelItem) {
	for index := range items {
		if items[index].Type == llm.ModelItemImageGenerationCall && items[index].ID == itemID {
			items[index] = replacement
		}
	}
}

func (r *TurnRunner) persistGeneratedImage(ctx context.Context, turn *domain.Turn, runID string, responseID string, ownerUserID string, item llm.ModelItem, outputIndex int) (*persistedGeneratedImage, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("generated image run id is required")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(item.Result))
	if err != nil {
		return nil, fmt.Errorf("decode generated image %s: %w", generatedImageItemID(item, outputIndex), err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("generated image %s is empty", generatedImageItemID(item, outputIndex))
	}

	format, contentType := detectGeneratedImageFormat(data)
	width, height, err := generatedImageDimensions(data)
	if err != nil {
		return nil, fmt.Errorf("decode generated image dimensions %s: %w", generatedImageItemID(item, outputIndex), err)
	}
	objectKey := generatedImageObjectKey(turn.ConversationID, turn.ID, runID, generatedImageItemID(item, outputIndex), format)
	if err := r.blobs.PutBytes(ctx, objectKey, data, contentType); err != nil {
		return nil, fmt.Errorf("store generated image %s: %w", generatedImageItemID(item, outputIndex), err)
	}

	metadata, err := json.Marshal(map[string]any{
		"source":         "image_generation",
		"response_id":    strings.TrimSpace(responseID),
		"turn_id":        turn.ID,
		"output_item_id": strings.TrimSpace(item.ID),
		"output_index":   outputIndex,
		"revised_prompt": strings.TrimSpace(item.RevisedPrompt),
		"width":          width,
		"height":         height,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal generated image metadata: %w", err)
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
		return nil, fmt.Errorf("record generated image attachment: %w", err)
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
		return nil, fmt.Errorf("marshal generated image message metadata: %w", err)
	}

	return &persistedGeneratedImage{
		Draft:      domain.AssistantMessageDraft{Metadata: draftMetadata},
		Attachment: *attachment,
		Reference: modelImageReference{
			AttachmentID: attachment.ID,
			ObjectKey:    objectKey,
			ContentType:  contentType,
			SizeBytes:    int64(len(data)),
			SHA256:       generatedImageSHA256(data),
		},
		Width:  width,
		Height: height,
	}, nil
}

func (r *TurnRunner) persistGeneratedImagePartial(ctx context.Context, turn *domain.Turn, runID string, event llm.ModelEvent) error {
	if event.Image == nil || strings.TrimSpace(event.Image.Base64) == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(event.Image.Base64))
	if err != nil {
		return fmt.Errorf("decode partial generated image: %w", err)
	}
	if len(data) == 0 || int64(len(data)) > maxProviderImageBytes {
		return fmt.Errorf("partial generated image size %d is invalid", len(data))
	}
	format, contentType := detectGeneratedImageFormat(data)
	width, height, err := generatedImageDimensions(data)
	if err != nil {
		return fmt.Errorf("decode partial generated image dimensions: %w", err)
	}
	revision := event.Image.PartialIndex
	if revision < 0 || revision > 3 {
		return fmt.Errorf("partial generated image revision %d is invalid", revision)
	}
	itemID := generatedImageItemID(llm.ModelItem{ID: event.ItemID}, event.OutputIndex)
	objectKey := generatedImagePreviewObjectKey(turn.ConversationID, turn.ID, runID, itemID, revision, format)
	if immutable, ok := r.blobs.(interface {
		PutImmutableBytes(context.Context, string, []byte, string) error
	}); ok {
		err = immutable.PutImmutableBytes(ctx, objectKey, data, contentType)
	} else {
		err = r.blobs.PutBytes(ctx, objectKey, data, contentType)
	}
	if err != nil {
		return fmt.Errorf("store partial generated image: %w", err)
	}
	ttl := r.settings.ImagePreviewTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	expiresAt := time.Now().UTC().Add(ttl)
	asset, err := r.upsertGeneratedImageAsset(ctx, domain.UpsertGeneratedImageAssetParams{
		ID:             generatedImageAssetID(objectKey, domain.GeneratedImageKindPartial, revision),
		ConversationID: turn.ConversationID,
		TurnID:         turn.ID,
		TurnRunID:      runID,
		ResponseID:     event.ResponseID,
		ItemID:         itemID,
		Kind:           domain.GeneratedImageKindPartial,
		Revision:       revision,
		ObjectKey:      objectKey,
		ContentType:    contentType,
		SizeBytes:      int64(len(data)),
		SHA256:         generatedImageSHA256(data),
		Width:          width,
		Height:         height,
		ExpiresAt:      &expiresAt,
	})
	if err != nil {
		return err
	}
	r.publishGeneratedImageAsset(ctx, turn, runID, event.ResponseID, llm.ModelItem{ID: itemID}, event.OutputIndex, "generating", asset)
	return nil
}

func (r *TurnRunner) upsertGeneratedImageAsset(ctx context.Context, params domain.UpsertGeneratedImageAssetParams) (*domain.GeneratedImageAsset, error) {
	if r == nil || r.generatedImageAssets == nil {
		return nil, fmt.Errorf("generated image asset persistence is not configured")
	}
	return r.generatedImageAssets.UpsertGeneratedImageAsset(ctx, params)
}

func (r *TurnRunner) publishGeneratedImageAsset(ctx context.Context, turn *domain.Turn, runID string, responseID string, item llm.ModelItem, outputIndex int, status string, asset *domain.GeneratedImageAsset) {
	if r == nil || r.streamHub == nil || turn == nil || asset == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"response_id":    strings.TrimSpace(responseID),
		"item_id":        asset.ItemID,
		"output_index":   outputIndex,
		"status":         status,
		"revised_prompt": strings.TrimSpace(item.RevisedPrompt),
		"asset":          generatedImageAssetSummary(*asset),
	})
	if err != nil {
		return
	}
	eventType := stream.EventImagePreview
	if asset.Kind == domain.GeneratedImageKindFinal {
		eventType = stream.EventImageCompleted
	}
	if err := r.streamHub.Publish(ctx, stream.Event{
		Type:           eventType,
		ConversationID: turn.ConversationID,
		TurnID:         turn.ID,
		RunID:          runID,
		ResponseID:     responseID,
		ItemID:         asset.ItemID,
		OutputIndex:    outputIndex,
		Payload:        string(payload),
	}); err != nil && r.logger != nil {
		r.logger.Printf("publish generated image asset %s for turn %s: %v", asset.ID, turn.ID, err)
	}
}

func generatedImageAssetSummary(asset domain.GeneratedImageAsset) map[string]any {
	return map[string]any{
		"id":            asset.ID,
		"kind":          asset.Kind,
		"revision":      asset.Revision,
		"content_type":  asset.ContentType,
		"size_bytes":    asset.SizeBytes,
		"width":         asset.Width,
		"height":        asset.Height,
		"attachment_id": asset.AttachmentID,
	}
}

func generatedImageSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r *TurnRunner) cleanupFailedGeneratedImages(ctx context.Context, conversationID string, turnID string) {
	if r == nil || r.generatedAttachments == nil {
		return
	}

	prefix := generatedImageObjectPrefix(conversationID, turnID)
	objectKeys, err := r.generatedAttachments.DeleteGeneratedImageAttachments(ctx, prefix)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf("delete generated image attachments for failed turn %s: %v", turnID, err)
		}
		return
	}

	deleter, ok := r.blobs.(generatedImageObjectDeleter)
	if !ok {
		return
	}
	keysToDelete := objectKeys
	if lister, ok := r.blobs.(RunArtifactObjectStore); ok {
		objects, err := lister.ListRunArtifactObjects(ctx, prefix)
		if err != nil {
			if r.logger != nil {
				r.logger.Printf("list generated image objects for failed turn %s: %v", turnID, err)
			}
		} else {
			keysToDelete = make([]string, 0, len(objects))
			for _, object := range objects {
				keysToDelete = append(keysToDelete, object.Key)
			}
		}
	}
	for _, objectKey := range keysToDelete {
		if err := deleter.DeleteObject(ctx, objectKey); err != nil && r.logger != nil {
			r.logger.Printf("delete generated image object %s for failed turn %s: %v", objectKey, turnID, err)
		}
	}
}

func generatedImageObjectPrefix(conversationID string, turnID string) string {
	return fmt.Sprintf("conversations/%s/turns/%s/generated-images/", strings.TrimSpace(conversationID), strings.TrimSpace(turnID))
}

func generatedImageObjectKey(conversationID string, turnID string, runID string, itemID string, format string) string {
	runID = safeObjectKeyPart(runID)
	return fmt.Sprintf("%s%s/%s.%s", generatedImageObjectPrefix(conversationID, turnID), runID, safeObjectKeyPart(itemID), format)
}

func generatedImagePreviewObjectKey(conversationID string, turnID string, runID string, itemID string, revision int, format string) string {
	return fmt.Sprintf("conversations/%s/turns/%s/generated-image-previews/%s/%s/partial-%d.%s",
		strings.TrimSpace(conversationID), strings.TrimSpace(turnID), strings.TrimSpace(runID), safeObjectKeyPart(itemID), revision, format)
}

func generatedImageAssetID(objectKey string, kind string, revision int) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("%s:%s:%d", objectKey, kind, revision))).String()
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

func generatedImageDimensions(data []byte) (int, int, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	if config.Width <= 0 || config.Height <= 0 {
		return 0, 0, fmt.Errorf("image dimensions are empty")
	}
	return config.Width, config.Height, nil
}

func attachmentSummary(attachment domain.Attachment) map[string]any {
	summary := map[string]any{
		"id":           attachment.ID,
		"filename":     attachment.Filename,
		"content_type": attachment.ContentType,
		"category":     attachment.Category,
		"size_bytes":   attachment.SizeBytes,
	}
	var metadata struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if json.Unmarshal(attachment.Metadata, &metadata) == nil && metadata.Width > 0 && metadata.Height > 0 {
		summary["width"] = metadata.Width
		summary["height"] = metadata.Height
	}
	return summary
}
