package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/llm"
)

type modelImageReference struct {
	AttachmentID string `json:"attachment_id"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

const (
	maxProviderImageBytes      int64 = 20 << 20
	maxProviderTotalImageBytes int64 = 64 << 20
)

func cloneScheduledRunState(state *ScheduledRunState) (*ScheduledRunState, error) {
	if state == nil {
		return nil, nil
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("clone scheduled run state: %w", err)
	}
	var cloned ScheduledRunState
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil, fmt.Errorf("clone scheduled run state: %w", err)
	}
	return &cloned, nil
}

func (l *ContextLoader) hydrateScheduledRunImages(ctx context.Context, state *ScheduledRunState) error {
	if state == nil {
		return nil
	}
	var totalBytes int64
	for index := range state.Request.Input {
		hydrated, hydratedBytes, err := l.hydrateModelItemImages(ctx, state.Request.Input[index])
		if err != nil {
			return err
		}
		totalBytes += hydratedBytes
		if totalBytes > maxProviderTotalImageBytes {
			return fmt.Errorf("provider request images exceed %d bytes", maxProviderTotalImageBytes)
		}
		state.Request.Input[index] = hydrated
	}
	return nil
}

func (l *ContextLoader) hydrateModelItemImages(ctx context.Context, item llm.ModelItem) (llm.ModelItem, int64, error) {
	if len(item.Raw) == 0 {
		return item, 0, nil
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(item.Raw, &message); err != nil {
		return item, 0, nil
	}
	if rawRef, ok := message["result_ref"]; ok {
		var ref modelImageReference
		if err := json.Unmarshal(rawRef, &ref); err != nil {
			return item, 0, fmt.Errorf("decode generated image reference: %w", err)
		}
		data, err := l.loadImageReferenceBytes(ctx, ref)
		if err != nil {
			return item, 0, err
		}
		result := base64.StdEncoding.EncodeToString(data)
		message["result"], _ = json.Marshal(result)
		delete(message, "result_ref")
		item.Raw, err = json.Marshal(message)
		item.Result = result
		return item, int64(len(data)), err
	}
	var content []json.RawMessage
	if err := json.Unmarshal(message["content"], &content); err != nil {
		return item, 0, nil
	}
	changed := false
	var hydratedBytes int64
	for index, rawPart := range content {
		var part struct {
			Type     string               `json:"type"`
			ImageRef *modelImageReference `json:"image_ref"`
		}
		if json.Unmarshal(rawPart, &part) != nil || part.Type != "input_image" || part.ImageRef == nil {
			continue
		}
		data, err := l.loadImageReferenceBytes(ctx, *part.ImageRef)
		if err != nil {
			return item, 0, err
		}
		imageURL := "data:" + part.ImageRef.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data)
		content[index], err = json.Marshal(map[string]string{"type": "input_image", "image_url": imageURL})
		if err != nil {
			return item, 0, fmt.Errorf("marshal hydrated image: %w", err)
		}
		hydratedBytes += int64(len(data))
		changed = true
	}
	if !changed {
		return item, 0, nil
	}
	encodedContent, err := json.Marshal(content)
	if err != nil {
		return item, 0, fmt.Errorf("marshal hydrated image content: %w", err)
	}
	message["content"] = encodedContent
	item.Raw, err = json.Marshal(message)
	if err != nil {
		return item, 0, fmt.Errorf("marshal hydrated model item: %w", err)
	}
	return item, hydratedBytes, nil
}

func (l *ContextLoader) hydrateImageReference(ctx context.Context, ref modelImageReference) (string, error) {
	data, err := l.loadImageReferenceBytes(ctx, ref)
	if err != nil {
		return "", err
	}
	return "data:" + ref.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (l *ContextLoader) hydrateImageReferenceBytes(ctx context.Context, ref modelImageReference) (string, error) {
	data, err := l.loadImageReferenceBytes(ctx, ref)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (l *ContextLoader) loadImageReferenceBytes(ctx context.Context, ref modelImageReference) ([]byte, error) {
	if l == nil || l.attachmentBlobs == nil {
		return nil, fmt.Errorf("load image attachment %s: attachment blob store is not configured", ref.AttachmentID)
	}
	if ref.SizeBytes > maxProviderImageBytes {
		return nil, fmt.Errorf("load image attachment %s: image exceeds %d bytes", ref.AttachmentID, maxProviderImageBytes)
	}
	data, err := l.attachmentBlobs.GetBytes(ctx, ref.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("load image attachment %s: %w", ref.AttachmentID, err)
	}
	if int64(len(data)) > maxProviderImageBytes {
		return nil, fmt.Errorf("load image attachment %s: image exceeds %d bytes", ref.AttachmentID, maxProviderImageBytes)
	}
	if ref.SizeBytes > 0 && int64(len(data)) != ref.SizeBytes {
		return nil, fmt.Errorf("load image attachment %s: size mismatch", ref.AttachmentID)
	}
	if expected := strings.TrimSpace(ref.SHA256); expected != "" {
		digest := sha256.Sum256(data)
		if !strings.EqualFold(expected, hex.EncodeToString(digest[:])) {
			return nil, fmt.Errorf("load image attachment %s: checksum mismatch", ref.AttachmentID)
		}
	}
	return data, nil
}
