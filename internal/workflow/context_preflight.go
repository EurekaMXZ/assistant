package workflow

import (
	"context"
	"fmt"
)

const maxToolContinuationOutputSafeTokens = 4_096

type ContextPreflight struct {
	settings  WorkflowSettings
	compactor RequestContextCompactor
	locker    ConversationLocker
}

type RequestContextCompactor interface {
	CanonicalInputLimit(ctx context.Context, state *ScheduledRunState) (int, error)
	Compact(ctx context.Context, state *ScheduledRunState) (*ScheduledRunState, bool, error)
}

func (p *ContextPreflight) Prepare(ctx context.Context, state *ScheduledRunState) (*ScheduledRunState, error) {
	if p == nil || state == nil || state.Request.ContextWindowTokens <= 0 || p.compactor == nil {
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
		target, err := p.compactionTarget(lockCtx, state)
		if err != nil {
			return err
		}
		used := estimateSafeModelContextTokens(state.Request.Instructions, state.Request.Input, state.Request.Tools, p.settings.TokenEstimateMultiplier)
		if target <= 0 || used < target {
			prepared = state
			return p.validateAdmission(state)
		}

		candidate, compacted, err := p.compactor.Compact(lockCtx, state)
		if err != nil {
			return err
		}
		if !compacted {
			return fmt.Errorf("context requires compaction but no replacement context was produced")
		}
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

func (p *ContextPreflight) compactionTarget(ctx context.Context, state *ScheduledRunState) (int, error) {
	mainLimit := modelRequestInputLimit(state.Request.ContextWindowTokens, state.Request.MaxOutputTokens)
	if mainLimit <= 0 {
		return 0, nil
	}
	trigger := compactTriggerTokenLimit(p.settings.CompactTriggerTokens, state.Request.ContextWindowTokens)
	if trigger <= 0 || mainLimit < trigger {
		trigger = mainLimit
	}
	compactionLimit, err := p.compactor.CanonicalInputLimit(ctx, state)
	if err != nil {
		return 0, err
	}
	if len(state.Request.Tools) > 0 {
		reserve := state.Request.MaxOutputTokens + maxToolContinuationOutputSafeTokens
		if compactionLimit <= reserve {
			return 0, fmt.Errorf("compaction model cannot accommodate a bounded tool continuation")
		}
		compactionLimit = max(0, compactionLimit-reserve)
	}
	if compactionLimit > 0 && compactionLimit < trigger {
		trigger = compactionLimit
	}
	return trigger, nil
}

func (p *ContextPreflight) validateAdmission(state *ScheduledRunState) error {
	if state == nil {
		return fmt.Errorf("scheduled run state is required")
	}
	limit := modelRequestInputLimit(state.Request.ContextWindowTokens, state.Request.MaxOutputTokens)
	used := estimateSafeModelContextTokens(state.Request.Instructions, state.Request.Input, state.Request.Tools, p.settings.TokenEstimateMultiplier)
	if limit > 0 && used > limit {
		return fmt.Errorf("model request safe input estimate %d exceeds context limit %d", used, limit)
	}
	return nil
}

var _ ScheduledRunPreflight = (*ContextPreflight)(nil)
