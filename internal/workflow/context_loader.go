package workflow

import (
	"bytes"
	"context"
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
	completeEvents  CompleteEventStore
	blobs           ContextAnchorStore
	cache           cache.ContextSnapshotCache
	sharedCache     cache.SharedContextSnapshotCache
	modelContexts   TurnArtifactStore
	attachments     AttachmentStore
	attachmentBlobs AttachmentBlobStore
}

func (l *ContextLoader) EnsureHotContext(ctx context.Context, conversationID string) (*cache.ContextSnapshot, *domain.ContextHead, error) {
	head, err := l.store.GetContextHead(ctx, conversationID)
	if err != nil {
		return nil, nil, err
	}

	if cached, ok := l.getLocalSnapshot(conversationID, head.Version); ok {
		if contextSnapshotMatchesHead(cached, head) && cached.ModelInputReady {
			return cached, head, nil
		}
		if cached.AnchorGeneration == head.AnchorGeneration &&
			cached.RawTailStartSeq == head.RawTailStartSeq &&
			cached.LastSeq == head.LastSeq &&
			cached.ActiveTokens == head.ActiveContextTokens &&
			cacheCoversTail(cached, head) {
			if err := l.loadConversationModelInput(ctx, conversationID, cached); err != nil {
				return nil, nil, err
			}
			l.putLocalSnapshot(conversationID, head.Version, cached)
			if l.sharedCache != nil {
				_ = l.sharedCache.PutContextSnapshot(ctx, cached)
			}
			return cached, head, nil
		}
	}
	if l.sharedCache != nil {
		cached, found, sharedErr := l.sharedCache.GetContextSnapshot(ctx, conversationID, head.Version)
		if sharedErr == nil && found && contextSnapshotMatchesHead(cached, head) {
			l.putLocalSnapshot(conversationID, head.Version, cached)
			return cached, head, nil
		}
	}
	if checkpointSnapshot, found, checkpointErr := l.loadCheckpointSnapshot(ctx, conversationID, head); checkpointErr != nil {
		return nil, nil, checkpointErr
	} else if found {
		l.putLocalSnapshot(conversationID, head.Version, checkpointSnapshot)
		if l.sharedCache != nil {
			_ = l.sharedCache.PutContextSnapshot(ctx, checkpointSnapshot)
		}
		return checkpointSnapshot, head, nil
	}
	if eventSnapshot, found, eventErr := l.loadEventSnapshot(ctx, conversationID, head); eventErr != nil {
		return nil, nil, eventErr
	} else if found {
		l.putLocalSnapshot(conversationID, head.Version, eventSnapshot)
		if l.sharedCache != nil {
			_ = l.sharedCache.PutContextSnapshot(ctx, eventSnapshot)
		}
		return eventSnapshot, head, nil
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
		ConversationID:           conversationID,
		Version:                  head.Version,
		SchemaVersion:            head.ContextSchemaVersion,
		CoveredEventSeq:          head.LastContextEventSeq,
		LatestCheckpointKey:      head.LatestCheckpointKey,
		LatestCheckpointChecksum: head.LatestCheckpointChecksum,
		LatestSuccessfulRunID:    head.LatestSuccessfulRunID,
		CreatedAt:                time.Now().UTC(),
		Anchor:                   anchor,
		AnchorGeneration:         head.AnchorGeneration,
		CoveredUntilSeq:          head.CoveredUntilSeq,
		RawTailStartSeq:          head.RawTailStartSeq,
		LastSeq:                  head.LastSeq,
		ActiveTokens:             head.ActiveContextTokens,
		TailCacheStartSeq:        tailCacheStartSeq(head, tail),
		TailCacheEndSeq:          tailCacheEndSeq(head, tail),
		Tail:                     tail,
		UpdatedAt:                time.Now(),
	}
	if err := l.loadConversationModelInput(ctx, conversationID, entry); err != nil {
		return nil, nil, err
	}
	l.putLocalSnapshot(conversationID, head.Version, entry)
	if l.sharedCache != nil {
		_ = l.sharedCache.PutContextSnapshot(ctx, entry)
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
		if message.ContextExcluded {
			continue
		}
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
	if hot.ModelInputReady {
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
		if message.ContextExcluded {
			continue
		}
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
	hot.ModelInputReady = true
	return nil
}

func (l *ContextLoader) getLocalSnapshot(conversationID string, version int64) (*cache.ContextSnapshot, bool) {
	if l == nil || l.cache == nil {
		return nil, false
	}
	if versioned, ok := l.cache.(cache.VersionedContextSnapshotCache); ok {
		return versioned.GetVersion(conversationID, version)
	}
	return l.cache.Get(conversationID)
}

func (l *ContextLoader) putLocalSnapshot(conversationID string, version int64, snapshot *cache.ContextSnapshot) {
	if l == nil || l.cache == nil || snapshot == nil {
		return
	}
	if versioned, ok := l.cache.(cache.VersionedContextSnapshotCache); ok {
		versioned.PutVersion(conversationID, version, snapshot)
		return
	}
	l.cache.Put(conversationID, snapshot)
}

func contextSnapshotMatchesHead(snapshot *cache.ContextSnapshot, head *domain.ContextHead) bool {
	if snapshot == nil || head == nil {
		return false
	}
	return snapshot.ConversationID == head.ConversationID &&
		snapshot.Version == head.Version &&
		snapshot.CoveredEventSeq == head.LastContextEventSeq &&
		snapshot.LatestCheckpointKey == head.LatestCheckpointKey &&
		snapshot.LatestCheckpointChecksum == head.LatestCheckpointChecksum &&
		snapshot.LatestSuccessfulRunID == head.LatestSuccessfulRunID &&
		(snapshot.SchemaVersion == head.ContextSchemaVersion || head.ContextSchemaVersion == 0)
}

func (l *ContextLoader) loadEventSnapshot(ctx context.Context, conversationID string, head *domain.ContextHead) (*cache.ContextSnapshot, bool, error) {
	if l == nil || head == nil || l.completeEvents == nil || head.AnchorKey != "" || head.LastContextEventSeq <= 0 {
		return nil, false, nil
	}
	events, err := l.completeEvents.ListContextEvents(ctx, conversationID, 0, head.LastContextEventSeq)
	if err != nil {
		return nil, false, err
	}
	if len(events) == 0 {
		return nil, false, nil
	}
	items, err := l.reduceContextEvents(ctx, conversationID, nil, events)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	return &cache.ContextSnapshot{
		ConversationID:           conversationID,
		Version:                  head.Version,
		SchemaVersion:            head.ContextSchemaVersion,
		CoveredEventSeq:          head.LastContextEventSeq,
		LatestCheckpointKey:      head.LatestCheckpointKey,
		LatestCheckpointChecksum: head.LatestCheckpointChecksum,
		LatestSuccessfulRunID:    head.LatestSuccessfulRunID,
		CreatedAt:                now,
		AnchorGeneration:         head.AnchorGeneration,
		CoveredUntilSeq:          head.CoveredUntilSeq,
		RawTailStartSeq:          head.RawTailStartSeq,
		LastSeq:                  head.LastSeq,
		ActiveTokens:             head.ActiveContextTokens,
		TailCacheStartSeq:        head.RawTailStartSeq,
		TailCacheEndSeq:          head.LastSeq,
		ModelInput:               items,
		ModelInputReady:          true,
		UpdatedAt:                now,
	}, true, nil
}

func (l *ContextLoader) modelInputItemsForMessage(ctx context.Context, conversationID string, message domain.Message) ([]llm.ModelItem, error) {
	if message.ContextExcluded {
		return nil, nil
	}
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

	content := make([]any, 0, len(attachments)+2)
	if text := strings.TrimSpace(message.ContentText); text != "" {
		content = append(content, map[string]string{
			"type": "input_text",
			"text": text,
		})
	}

	for _, attachment := range attachments {
		part, _, err := l.attachmentContentPart(ctx, attachment)
		if err != nil {
			return nil, false, err
		}
		if len(part) > 0 {
			content = append(content, part)
		}
	}

	content = append(content, map[string]string{
		"type": "input_text",
		"text": formatSandboxAttachmentNote(attachments),
	})
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

func (l *ContextLoader) attachmentContentPart(_ context.Context, attachment domain.Attachment) (map[string]any, string, error) {
	if attachment.Category != domain.AttachmentCategoryImage {
		return nil, attachment.Filename, nil
	}
	if !isSupportedModelImageType(attachment.ContentType) {
		return nil, attachment.Filename, nil
	}
	return map[string]any{
		"type": "input_image",
		"image_ref": modelImageReference{
			AttachmentID: attachment.ID,
			ObjectKey:    attachment.ObjectKey,
			ContentType:  attachment.ContentType,
			SizeBytes:    attachment.SizeBytes,
			SHA256:       attachment.SHA256,
		},
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

func formatSandboxAttachmentNote(attachments []domain.Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("User attachments available for on-demand sandbox import. Create a sandbox first, then call sandbox.import_attachment only for files needed by the task:\n")
	for _, attachment := range attachments {
		fmt.Fprintf(&builder, "- attachment_id=%s filename=%q content_type=%s size_bytes=%d\n", attachment.ID, attachment.Filename, attachment.ContentType, attachment.SizeBytes)
	}
	return strings.TrimSpace(builder.String())
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
	data, err := getCompressedArtifact(ctx, l.modelContexts, key)
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
