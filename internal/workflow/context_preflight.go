package workflow

import (
	"context"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

type ContextPreflight struct {
	settings  WorkflowSettings
	loader    *ContextLoader
	compactor RequestContextCompactor
	locker    ConversationLocker
}

type RequestContextCompactor interface {
	CompactForRequest(ctx context.Context, event WorkflowEvent) (bool, error)
}

func (p *ContextPreflight) Prepare(ctx context.Context, state *ScheduledRunState) (*ScheduledRunState, error) {
	if p == nil || state == nil || state.Request.ContextWindowTokens <= 0 || p.loader == nil || p.compactor == nil {
		return state, nil
	}
	conversationID := state.Scope.ConversationID
	if conversationID == "" {
		return state, nil
	}

	var prepared *ScheduledRunState
	prepare := func(lockCtx context.Context) error {
		if state.InitialInputCount < 0 || state.InitialInputCount > len(state.Request.Input) {
			return fmt.Errorf("invalid scheduled run initial input count")
		}
		used := estimateSafeModelContextTokens(state.Request.Instructions, state.Request.Input, state.Request.Tools)
		threshold := compactTriggerTokenLimit(p.settings.CompactTriggerTokens, state.Request.ContextWindowTokens)
		limit := modelRequestInputLimit(state.Request.ContextWindowTokens, state.Request.MaxOutputTokens)
		needsCompaction := threshold > 0 && used >= threshold
		if limit > 0 && used > limit {
			needsCompaction = true
		}
		if !needsCompaction {
			prepared = state
			return nil
		}

		compacted, err := p.compactor.CompactForRequest(lockCtx, WorkflowEvent{
			EventType:      EventTurnAccepted,
			ConversationID: conversationID,
			TurnID:         state.Scope.TurnID,
		})
		if err != nil {
			return err
		}
		if !compacted {
			prepared = state
			return p.validateAdmission(state)
		}

		hot, _, err := p.loader.EnsureHotContext(lockCtx, conversationID)
		if err != nil {
			return err
		}
		if hot == nil {
			return fmt.Errorf("compaction returned no conversation context")
		}
		candidate, err := cloneScheduledRunState(state)
		if err != nil {
			return err
		}
		base := buildTurnModelInput(hot)
		base = insertAccountPersonalizationContext(base, accountPersonalizationContext(state.Request.Input))
		suffix := cloneModelItems(state.Request.Input[state.InitialInputCount:])
		candidate.Request.Input = append(base, suffix...)
		candidate.InitialInputCount = len(base)
		if err := p.validateAdmission(candidate); err != nil {
			return err
		}
		prepared = candidate
		return nil
	}

	if p.locker != nil {
		if err := p.locker.WithConversationLock(ctx, conversationID, prepare); err != nil {
			return nil, err
		}
	} else if err := prepare(ctx); err != nil {
		return nil, err
	}
	return prepared, nil
}

func accountPersonalizationContext(input []llm.ModelItem) *llm.ModelItem {
	for index := len(input) - 1; index >= 0; index-- {
		if input[index].Type != llm.ModelItemMessage || input[index].Role != domain.RoleUser {
			continue
		}
		if index == 0 || !isAccountPersonalizationContext(input[index-1]) {
			return nil
		}
		personalization := cloneModelItems(input[index-1 : index])
		return &personalization[0]
	}
	return nil
}

func (p *ContextPreflight) validateAdmission(state *ScheduledRunState) error {
	if state == nil {
		return fmt.Errorf("scheduled run state is required")
	}
	limit := modelRequestInputLimit(state.Request.ContextWindowTokens, state.Request.MaxOutputTokens)
	used := estimateSafeModelContextTokens(state.Request.Instructions, state.Request.Input, state.Request.Tools)
	if limit > 0 && used > limit {
		return fmt.Errorf("model request safe input estimate %d exceeds context limit %d", used, limit)
	}
	return nil
}

var _ ScheduledRunPreflight = (*ContextPreflight)(nil)
