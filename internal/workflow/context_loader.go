package workflow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type ContextLoader struct {
	store           WorkflowContextRepository
	blobs           ContextAnchorStore
	cache           cache.ContextSnapshotCache
	modelContexts   TurnArtifactStore
	attachments     AttachmentStore
	attachmentBlobs AttachmentBlobStore
}

func (l *ContextLoader) EnsureHotContext(ctx context.Context, conversationID string) (*cache.ContextSnapshot, *domain.ContextHead, error) {
	head, err := l.store.GetContextHead(ctx, conversationID)
	if err != nil {
		return nil, nil, err
	}

	if cached, ok := l.cache.Get(conversationID); ok {
		if cached.AnchorGeneration == head.AnchorGeneration &&
			cached.RawTailStartSeq == head.RawTailStartSeq &&
			cached.LastSeq == head.LastSeq &&
			cached.ActiveTokens == head.ActiveContextTokens &&
			cacheCoversTail(cached, head) {
			if err := l.loadConversationModelInput(ctx, conversationID, cached); err != nil {
				return nil, nil, err
			}
			return cached, head, nil
		}
	}

	var anchor *cache.ContextAnchor
	if head.AnchorKey != "" {
		var object domain.AnchorObject
		if err := l.blobs.GetJSON(ctx, head.AnchorKey, &object); err != nil {
			return nil, nil, err
		}
		anchor = &cache.ContextAnchor{
			ConversationID:  object.ConversationID,
			Generation:      object.Generation,
			CoveredFromSeq:  object.CoveredFromSeq,
			CoveredUntilSeq: object.CoveredUntilSeq,
			Role:            object.Role,
			Content:         object.Content,
			TokenCount:      object.TokenCount,
		}
	}

	var tail []domain.Message
	if head.RawTailStartSeq <= head.LastSeq {
		tail, err = l.store.ListRawTailMessages(ctx, conversationID, head.RawTailStartSeq, head.LastSeq)
		if err != nil {
			return nil, nil, err
		}
	}

	entry := &cache.ContextSnapshot{
		Anchor:            anchor,
		AnchorGeneration:  head.AnchorGeneration,
		CoveredUntilSeq:   head.CoveredUntilSeq,
		RawTailStartSeq:   head.RawTailStartSeq,
		LastSeq:           head.LastSeq,
		ActiveTokens:      head.ActiveContextTokens,
		TailCacheStartSeq: tailCacheStartSeq(head, tail),
		TailCacheEndSeq:   tailCacheEndSeq(head, tail),
		Tail:              tail,
		UpdatedAt:         time.Now(),
	}
	l.cache.Put(conversationID, entry)
	if err := l.loadConversationModelInput(ctx, conversationID, entry); err != nil {
		return nil, nil, err
	}

	return entry, head, nil
}

func buildConversationHistoryInput(hot *cache.ContextSnapshot) []llm.ModelItem {
	if hot != nil && hot.ModelInput != nil {
		return cloneModelItems(hot.ModelInput)
	}
	return buildTextConversationHistoryInput(hot)
}

func buildTextConversationHistoryInput(hot *cache.ContextSnapshot) []llm.ModelItem {
	var input []llm.ModelItem
	if hot == nil {
		return input
	}

	if hot.Anchor != nil && strings.TrimSpace(hot.Anchor.Content) != "" {
		input = append(input, llm.ModelItem{
			Type:    llm.ModelItemMessage,
			Role:    hot.Anchor.Role,
			Content: hot.Anchor.Content,
		})
	}

	for _, message := range hot.Tail {
		if strings.TrimSpace(message.ContentText) == "" {
			continue
		}
		input = append(input, llm.ModelItem{
			Type:    llm.ModelItemMessage,
			Role:    message.Role,
			Content: message.ContentText,
		})
	}

	return input
}

func (l *ContextLoader) loadConversationModelInput(ctx context.Context, conversationID string, hot *cache.ContextSnapshot) error {
	if hot == nil {
		return nil
	}

	var input []llm.ModelItem
	if hot.Anchor != nil && strings.TrimSpace(hot.Anchor.Content) != "" {
		input = append(input, llm.ModelItem{
			Type:    llm.ModelItemMessage,
			Role:    hot.Anchor.Role,
			Content: hot.Anchor.Content,
		})
	}

	coveredAssistantTurns := map[string]struct{}{}
	missingAssistantContexts := map[string]struct{}{}
	for _, message := range hot.Tail {
		if message.Role == domain.RoleAssistant && strings.TrimSpace(message.TurnID) != "" {
			turnID := strings.TrimSpace(message.TurnID)
			if _, ok := coveredAssistantTurns[turnID]; ok {
				continue
			}
			if _, missing := missingAssistantContexts[turnID]; !missing && l != nil && l.modelContexts != nil {
				items, found, err := l.loadTurnModelContextItems(ctx, conversationID, turnID)
				if err != nil {
					return err
				}
				if found {
					if strings.TrimSpace(message.ContentText) != "" && !hasAssistantMessageItem(items) {
						items = append(items, llm.ModelItem{
							Type:    llm.ModelItemMessage,
							Role:    domain.RoleAssistant,
							Content: message.ContentText,
						})
					}
					input = append(input, items...)
					coveredAssistantTurns[turnID] = struct{}{}
					continue
				}
				missingAssistantContexts[turnID] = struct{}{}
			}
			if strings.TrimSpace(message.ContentText) != "" {
				input = append(input, llm.ModelItem{
					Type:    llm.ModelItemMessage,
					Role:    domain.RoleAssistant,
					Content: message.ContentText,
				})
			}
			continue
		}
		items, err := l.modelInputItemsForMessage(ctx, conversationID, message)
		if err != nil {
			return err
		}
		input = append(input, items...)
	}

	hot.ModelInput = input
	return nil
}

func (l *ContextLoader) modelInputItemsForMessage(ctx context.Context, conversationID string, message domain.Message) ([]llm.ModelItem, error) {
	if message.Role == domain.RoleAssistant && strings.TrimSpace(message.TurnID) != "" && l != nil && l.modelContexts != nil {
		items, found, err := l.loadTurnModelContextItems(ctx, conversationID, message.TurnID)
		if err != nil {
			return nil, err
		}
		if found {
			if strings.TrimSpace(message.ContentText) != "" && !hasAssistantMessageItem(items) {
				items = append(items, llm.ModelItem{
					Type:    llm.ModelItemMessage,
					Role:    domain.RoleAssistant,
					Content: message.ContentText,
				})
			}
			return items, nil
		}
	}

	if message.Role == domain.RoleUser {
		items, handled, err := l.userMessageInputItems(ctx, conversationID, message)
		if err != nil {
			return nil, err
		}
		if handled {
			return items, nil
		}
	}

	if strings.TrimSpace(message.ContentText) == "" {
		return nil, nil
	}
	return []llm.ModelItem{
		{
			Type:    llm.ModelItemMessage,
			Role:    message.Role,
			Content: message.ContentText,
		},
	}, nil
}

func (l *ContextLoader) userMessageInputItems(ctx context.Context, conversationID string, message domain.Message) ([]llm.ModelItem, bool, error) {
	ids := messageAttachmentIDs(message.Metadata)
	if len(ids) == 0 {
		return nil, false, nil
	}
	if l == nil || l.attachments == nil {
		return nil, false, fmt.Errorf("load message attachments: attachment store is not configured")
	}

	attachments, err := l.attachments.ListAttachmentsByIDs(ctx, conversationID, ids)
	if err != nil {
		return nil, false, err
	}

	content := make([]map[string]string, 0, len(attachments)+2)
	if text := strings.TrimSpace(message.ContentText); text != "" {
		content = append(content, map[string]string{
			"type": "input_text",
			"text": text,
		})
	}

	var unavailable []string
	for _, attachment := range attachments {
		part, reason, err := l.attachmentContentPart(ctx, attachment)
		if err != nil {
			return nil, false, err
		}
		if len(part) > 0 {
			content = append(content, part)
			continue
		}
		if reason != "" {
			unavailable = append(unavailable, reason)
		}
	}

	if len(unavailable) > 0 {
		content = append(content, map[string]string{
			"type": "input_text",
			"text": formatUnavailableAttachmentNote(unavailable),
		})
	}
	if len(content) == 0 {
		return nil, true, nil
	}

	raw, err := json.Marshal(map[string]any{
		"type":    llm.ModelItemMessage,
		"role":    message.Role,
		"content": content,
	})
	if err != nil {
		return nil, false, fmt.Errorf("marshal user attachment message: %w", err)
	}

	return []llm.ModelItem{{
		Type: llm.ModelItemMessage,
		Role: message.Role,
		Raw:  raw,
	}}, true, nil
}

func (l *ContextLoader) attachmentContentPart(ctx context.Context, attachment domain.Attachment) (map[string]string, string, error) {
	if attachment.Category != domain.AttachmentCategoryImage {
		return nil, attachment.Filename, nil
	}
	if !isSupportedModelImageType(attachment.ContentType) {
		return nil, attachment.Filename, nil
	}
	if l == nil || l.attachmentBlobs == nil {
		return nil, "", fmt.Errorf("load image attachment %s: attachment blob store is not configured", attachment.ID)
	}

	data, err := l.attachmentBlobs.GetBytes(ctx, attachment.ObjectKey)
	if err != nil {
		return nil, "", fmt.Errorf("load image attachment %s: %w", attachment.ID, err)
	}
	return map[string]string{
		"type":      "input_image",
		"image_url": "data:" + attachment.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}, "", nil
}

func messageAttachmentIDs(metadata json.RawMessage) []string {
	if len(metadata) == 0 {
		return nil
	}

	var payload struct {
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := json.Unmarshal(metadata, &payload); err != nil {
		return nil
	}

	var ids []string
	for _, id := range payload.AttachmentIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

func formatUnavailableAttachmentNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "User attached files stored for later sandbox analysis and not available for direct model inspection yet: " + strings.Join(names, ", ")
}

func isSupportedModelImageType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func (l *ContextLoader) loadTurnModelContextItems(ctx context.Context, conversationID string, turnID string) ([]llm.ModelItem, bool, error) {
	if l == nil || l.modelContexts == nil {
		return nil, false, nil
	}

	key := l.modelContexts.TurnModelContextKey(conversationID, turnID)
	data, err := l.modelContexts.GetBytes(ctx, key)
	switch {
	case err == nil:
		data = bytes.TrimSpace(data)
		if len(data) == 0 {
			return nil, true, nil
		}
		items, err := unmarshalModelContextItems(data)
		if err != nil {
			return nil, false, fmt.Errorf("turn model context %q is not valid json: %w", key, err)
		}
		return items, true, nil
	case errors.Is(err, domain.ErrNotFound):
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("get turn model context %q: %w", key, err)
	}
}

func buildTurnModelInput(hot *cache.ContextSnapshot) []llm.ModelItem {
	return buildConversationHistoryInput(hot)
}

func cacheCoversTail(entry *cache.ContextSnapshot, head *domain.ContextHead) bool {
	if entry == nil || head == nil {
		return false
	}
	if head.RawTailStartSeq > head.LastSeq {
		return len(entry.Tail) == 0
	}
	return entry.TailCacheStartSeq <= head.RawTailStartSeq && entry.TailCacheEndSeq >= head.LastSeq
}

func tailCacheStartSeq(head *domain.ContextHead, tail []domain.Message) int64 {
	if len(tail) > 0 {
		return tail[0].Seq
	}
	if head == nil {
		return 0
	}
	return head.RawTailStartSeq
}

func tailCacheEndSeq(head *domain.ContextHead, tail []domain.Message) int64 {
	if len(tail) > 0 {
		return tail[len(tail)-1].Seq
	}
	if head == nil {
		return 0
	}
	return head.RawTailStartSeq - 1
}
