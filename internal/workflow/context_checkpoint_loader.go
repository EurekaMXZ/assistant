package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/klauspost/compress/zstd"
)

type immutableContextCheckpoint struct {
	SchemaVersion  int             `json:"schema_version"`
	ConversationID string          `json:"conversation_id"`
	TurnID         string          `json:"turn_id"`
	RunID          string          `json:"run_id"`
	StepIndex      int             `json:"step_index"`
	ModelItems     []llm.ModelItem `json:"model_items"`
}

func (l *ContextLoader) loadCheckpointSnapshot(ctx context.Context, conversationID string, head *domain.ContextHead) (*cache.ContextSnapshot, bool, error) {
	if l == nil || head == nil || head.LatestCheckpointKey == "" || l.modelContexts == nil || l.completeEvents == nil {
		return nil, false, nil
	}
	compressed, err := l.modelContexts.GetBytes(ctx, head.LatestCheckpointKey)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load context checkpoint: %w", err)
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, false, fmt.Errorf("create context checkpoint decoder: %w", err)
	}
	payload, err := decoder.DecodeAll(compressed, nil)
	decoder.Close()
	if err != nil {
		return nil, false, fmt.Errorf("decode context checkpoint: %w", err)
	}
	var checkpoint immutableContextCheckpoint
	if err := json.Unmarshal(payload, &checkpoint); err != nil {
		return nil, false, fmt.Errorf("unmarshal context checkpoint: %w", err)
	}
	if checkpoint.SchemaVersion != immutableRunArtifactSchemaVersion || checkpoint.ConversationID != conversationID {
		return nil, false, fmt.Errorf("context checkpoint identity or schema mismatch")
	}
	items := cloneModelItems(checkpoint.ModelItems)
	events, err := l.completeEvents.ListContextEvents(ctx, conversationID, head.CheckpointCoveredEventSeq, head.LastContextEventSeq)
	if err != nil {
		return nil, false, err
	}
	items, err = l.reduceContextEvents(ctx, conversationID, items, events)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	return &cache.ContextSnapshot{
		ConversationID:        conversationID,
		Version:               head.Version,
		SchemaVersion:         head.ContextSchemaVersion,
		CoveredEventSeq:       head.LastContextEventSeq,
		LatestCheckpointKey:   head.LatestCheckpointKey,
		LatestSuccessfulRunID: head.LatestSuccessfulRunID,
		CreatedAt:             now,
		AnchorGeneration:      head.AnchorGeneration,
		CoveredUntilSeq:       head.CoveredUntilSeq,
		RawTailStartSeq:       head.RawTailStartSeq,
		LastSeq:               head.LastSeq,
		ActiveTokens:          head.ActiveContextTokens,
		TailCacheStartSeq:     head.RawTailStartSeq,
		TailCacheEndSeq:       head.LastSeq,
		ModelInput:            items,
		ModelInputReady:       true,
		UpdatedAt:             now,
	}, true, nil
}

func (l *ContextLoader) reduceContextEvents(ctx context.Context, conversationID string, base []llm.ModelItem, events []domain.ConversationEvent) ([]llm.ModelItem, error) {
	items := cloneModelItems(base)
	for _, event := range events {
		if !event.ContextIncluded {
			continue
		}
		switch event.EventType {
		case "message.completed", domain.ConversationEventUserMessageCreated:
			var payload struct {
				Message domain.Message `json:"message"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode context event %s: %w", event.EventKey, err)
			}
			if payload.Message.Role == domain.RoleAssistant {
				if payload.Message.ContentText != "" {
					items = append(items, llm.ModelItem{Type: llm.ModelItemMessage, Role: domain.RoleAssistant, Content: payload.Message.ContentText})
				}
				continue
			}
			messageItems, err := l.modelInputItemsForMessage(ctx, conversationID, payload.Message)
			if err != nil {
				return nil, err
			}
			items = append(items, messageItems...)
		case domain.ConversationEventOutputTextCompleted:
			var payload struct {
				ModelItem llm.ModelItem `json:"model_item"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode context output event %s: %w", event.EventKey, err)
			}
			items = append(items, payload.ModelItem)
		}
	}
	return items, nil
}
