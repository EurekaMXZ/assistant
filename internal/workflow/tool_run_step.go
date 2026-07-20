package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

const scheduledRunStateVersion = 1

type ScheduledRunState struct {
	Version           int              `json:"version"`
	StepIndex         int              `json:"step_index"`
	InitialInputCount int              `json:"initial_input_count"`
	Scope             tool.ToolScope   `json:"scope"`
	Request           llm.ModelRequest `json:"request"`
}

type ScheduledRunOutcome struct {
	Model                *llm.ModelResult               `json:"model"`
	ContextWindowTokens  int                            `json:"context_window_tokens,omitempty"`
	Tools                []llm.ModelTool                `json:"tools,omitempty"`
	ContextItems         []llm.ModelItem                `json:"context_items,omitempty"`
	NextState            *ScheduledRunState             `json:"next_state,omitempty"`
	NextRequest          json.RawMessage                `json:"next_request,omitempty"`
	Postprocessed        bool                           `json:"postprocessed"`
	GeneratedImageDrafts []domain.AssistantMessageDraft `json:"generated_image_drafts,omitempty"`
}

func (o *ToolOrchestrator) PrepareScheduledRun(ctx context.Context, input ToolRunInput, stepIndex int, initialInputCount int) (*ScheduledRunState, json.RawMessage, error) {
	if o == nil || o.model == nil {
		return nil, nil, fmt.Errorf("tool orchestrator requires model client")
	}
	var tools []llm.ModelTool
	var err error
	if !input.DisableTools {
		tools, err = o.listTools(ctx, input.Scope)
	}
	if err != nil {
		return nil, nil, err
	}
	request := llm.ModelRequest{
		Model:               input.Model,
		ContextWindowTokens: input.ContextWindowTokens,
		CatalogModelID:      input.CatalogModelID,
		ModelRevision:       input.ModelRevision,
		ModelPriceID:        input.ModelPriceID,
		PricingSnapshot:     append(json.RawMessage(nil), input.PricingSnapshot...),
		CredentialID:        input.CredentialID,
		ProviderBaseURL:     input.ProviderBaseURL,
		ReasoningEffort:     input.ReasoningEffort,
		ReasoningSummary:    input.ReasoningSummary,
		TextVerbosity:       input.TextVerbosity,
		Instructions:        input.Instructions,
		Input:               truncateModelContextItems(input.Input, o.modelToolOutputTokenLimit()),
		Tools:               tools,
		PromptCacheKey:      input.PromptCacheKey,
		MaxOutputTokens:     input.MaxOutputTokens,
		Metadata:            cloneStringMap(input.Metadata),
		ParallelToolCalls:   input.ParallelToolCalls,
	}
	rawRequest, err := o.model.MarshalRequest(request)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal scheduled model request: %w", err)
	}
	if initialInputCount < 0 || initialInputCount > len(request.Input) {
		initialInputCount = len(request.Input)
	}
	return &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: stepIndex,
		InitialInputCount: initialInputCount, Scope: cloneToolScope(input.Scope), Request: request,
	}, rawRequest, nil
}

func (o *ToolOrchestrator) RequestScheduledRun(ctx context.Context, state *ScheduledRunState, handler llm.ModelEventHandler) (*ScheduledRunOutcome, error) {
	if o == nil || o.model == nil || state == nil {
		return nil, fmt.Errorf("scheduled request requires orchestrator and state")
	}
	outcome := &ScheduledRunOutcome{
		ContextWindowTokens: state.Request.ContextWindowTokens,
		Tools:               cloneModelTools(state.Request.Tools),
	}
	inputTokens := estimateModelContextTokens(state.Request.Instructions, state.Request.Input, state.Request.Tools)
	if inputLimit := modelRequestInputLimit(state.Request.ContextWindowTokens, 0); inputLimit > 0 && inputTokens > inputLimit {
		return outcome, fmt.Errorf("scheduled model input estimate %d exceeds context limit", inputTokens)
	}
	result, err := o.model.StreamResponse(ctx, state.Request, handler)
	outcome.Model = result
	return outcome, err
}

func (o *ToolOrchestrator) PostprocessScheduledRun(ctx context.Context, run *domain.TurnRun, state *ScheduledRunState, outcome *ScheduledRunOutcome) error {
	if o == nil || o.model == nil || run == nil || state == nil {
		return fmt.Errorf("scheduled run postprocessing requires orchestrator, run, and state")
	}
	if outcome == nil {
		return fmt.Errorf("scheduled run postprocessing requires outcome")
	}
	result := outcome.Model
	if result == nil {
		return fmt.Errorf("scheduled model request returned no result")
	}

	o.publishReasoningSummary(ctx, state.Scope, run, state.StepIndex, result)
	functionCalls := normalizeFunctionCallItems(functionCalls(result), state.Request.Tools)
	approvalRequests := approvalRequests(result)
	remoteCalls := remoteToolCalls(result)
	remoteContinuations := remoteContinuationItems(result)
	if err := o.recordObservedRemoteCalls(ctx, state.Scope, run, remoteCalls); err != nil {
		return err
	}
	if len(approvalRequests) > 0 {
		return unsupportedApprovalRequestError(approvalRequests[0])
	}
	if len(functionCalls) == 0 && len(remoteContinuations) == 0 {
		if action := firstUnsupportedAction(result); action != nil {
			return fmt.Errorf("tool action %s is not implemented yet", describeModelItem(*action))
		}
		initialInput := state.Request.Input[:state.InitialInputCount]
		outcome.ContextItems = buildModelContextItems(initialInput, state.Request.Input, result, o.modelToolOutputTokenLimit())
		outcome.Postprocessed = true
		return nil
	}
	if err := o.ensureStepCapabilities(functionCalls); err != nil {
		return err
	}

	nextInput := append([]llm.ModelItem(nil), state.Request.Input...)
	nextInput = append(nextInput, o.replayOutputItems(result.OutputItems)...)
	toolOutputBudget := remainingToolOutputTokens(state.Request, nextInput, result.Usage.TotalTokens)
	nextInput, nextScope, err := o.executeLocalToolCalls(ctx, run, nextInput, state.Scope, modelItemsToToolCalls(functionCalls), toolOutputBudget)
	if err != nil {
		return err
	}
	nextState, nextRequest, err := o.PrepareScheduledRun(ctx, ToolRunInput{
		Scope: nextScope, Model: state.Request.Model, ContextWindowTokens: state.Request.ContextWindowTokens,
		Instructions:   state.Request.Instructions,
		CatalogModelID: state.Request.CatalogModelID, ModelRevision: state.Request.ModelRevision,
		ModelPriceID: state.Request.ModelPriceID, PricingSnapshot: state.Request.PricingSnapshot,
		CredentialID: state.Request.CredentialID, ProviderBaseURL: state.Request.ProviderBaseURL,
		ReasoningEffort: state.Request.ReasoningEffort, ReasoningSummary: state.Request.ReasoningSummary,
		TextVerbosity:  state.Request.TextVerbosity,
		DisableTools:   len(state.Request.Tools) == 0,
		PromptCacheKey: state.Request.PromptCacheKey,
		Input:          nextInput, MaxOutputTokens: state.Request.MaxOutputTokens,
		Metadata: state.Request.Metadata, ParallelToolCalls: state.Request.ParallelToolCalls,
	}, state.StepIndex+1, state.InitialInputCount)
	if err != nil {
		return err
	}
	outcome.NextState = nextState
	outcome.NextRequest = nextRequest
	outcome.Postprocessed = true
	return nil
}

func (o *ToolOrchestrator) PersistScheduledRunState(ctx context.Context, scope tool.ToolScope, state *ScheduledRunState, rawRequest json.RawMessage) (string, string, error) {
	if o == nil || o.artifacts == nil || state == nil {
		return "", "", fmt.Errorf("persist scheduled run state requires artifact store")
	}
	statePayload, err := json.Marshal(state)
	if err != nil {
		return "", "", fmt.Errorf("marshal scheduled run state: %w", err)
	}
	stateKey := o.artifacts.TurnRunStateKey(scope.ConversationID, scope.TurnID, state.StepIndex)
	requestKey := o.artifacts.TurnRunRequestKey(scope.ConversationID, scope.TurnID, state.StepIndex)
	if err := o.artifacts.PutBytes(ctx, stateKey, statePayload, "application/json"); err != nil {
		return "", "", fmt.Errorf("persist scheduled run state: %w", err)
	}
	if err := o.artifacts.PutBytes(ctx, requestKey, rawRequest, "application/json"); err != nil {
		return "", "", fmt.Errorf("persist scheduled run request: %w", err)
	}
	return stateKey, requestKey, nil
}

func (o *ToolOrchestrator) LoadScheduledRunState(ctx context.Context, key string) (*ScheduledRunState, error) {
	if o == nil || o.artifacts == nil {
		return nil, fmt.Errorf("load scheduled run state requires artifact store")
	}
	payload, err := o.artifacts.GetBytes(ctx, key)
	if err != nil {
		return nil, err
	}
	var state ScheduledRunState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, fmt.Errorf("decode scheduled run state: %w", err)
	}
	if state.Version != scheduledRunStateVersion {
		return nil, fmt.Errorf("unsupported scheduled run state version %d", state.Version)
	}
	if state.StepIndex <= 0 || state.InitialInputCount < 0 || state.InitialInputCount > len(state.Request.Input) {
		return nil, fmt.Errorf("invalid scheduled run state")
	}
	state.Request.Input = truncateModelContextItems(state.Request.Input, o.modelToolOutputTokenLimit())
	return &state, nil
}

func (o *ToolOrchestrator) PersistScheduledRunOutcome(ctx context.Context, scope tool.ToolScope, run *domain.TurnRun, outcome *ScheduledRunOutcome) (string, string, error) {
	if o == nil || o.artifacts == nil || run == nil || outcome == nil || outcome.Model == nil {
		return "", "", fmt.Errorf("persist scheduled run outcome requires artifact store and model result")
	}
	if err := o.persistTurnRunOutputItems(ctx, scope, run.StepIndex, outcome.Model); err != nil {
		return "", "", err
	}
	responseKey := ""
	if len(outcome.Model.RawResponse) > 0 {
		responseKey = o.artifacts.TurnRunResponseKey(scope.ConversationID, scope.TurnID, run.StepIndex)
		if err := o.artifacts.PutBytes(ctx, responseKey, outcome.Model.RawResponse, "application/json"); err != nil {
			return "", "", fmt.Errorf("persist scheduled run response: %w", err)
		}
	}
	payload, err := json.Marshal(outcome)
	if err != nil {
		return "", "", fmt.Errorf("marshal scheduled run outcome: %w", err)
	}
	resultKey := o.artifacts.TurnRunResultKey(scope.ConversationID, scope.TurnID, run.StepIndex)
	if err := o.artifacts.PutBytes(ctx, resultKey, payload, "application/json"); err != nil {
		return "", "", fmt.Errorf("persist scheduled run outcome: %w", err)
	}
	return responseKey, resultKey, nil
}

func (o *ToolOrchestrator) LoadScheduledRunOutcome(ctx context.Context, key string) (*ScheduledRunOutcome, error) {
	if o == nil || o.artifacts == nil {
		return nil, fmt.Errorf("load scheduled run outcome requires artifact store")
	}
	payload, err := o.artifacts.GetBytes(ctx, key)
	if err != nil {
		return nil, err
	}
	var outcome ScheduledRunOutcome
	if err := json.Unmarshal(payload, &outcome); err != nil {
		return nil, fmt.Errorf("decode scheduled run outcome: %w", err)
	}
	return &outcome, nil
}

func (o *ToolOrchestrator) RecoverScheduledRunOutcome(ctx context.Context, scope tool.ToolScope, stepIndex int) (*ScheduledRunOutcome, string, string, bool, error) {
	if o == nil || o.artifacts == nil {
		return nil, "", "", false, fmt.Errorf("recover scheduled run outcome requires artifact store")
	}
	resultKey := o.artifacts.TurnRunResultKey(scope.ConversationID, scope.TurnID, stepIndex)
	outcome, err := o.LoadScheduledRunOutcome(ctx, resultKey)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, "", "", false, nil
		}
		return nil, "", "", false, err
	}
	responseKey := ""
	if outcome.Model != nil && len(outcome.Model.RawResponse) > 0 {
		responseKey = o.artifacts.TurnRunResponseKey(scope.ConversationID, scope.TurnID, stepIndex)
	}
	return outcome, responseKey, resultKey, true, nil
}

func cloneModelTools(source []llm.ModelTool) []llm.ModelTool {
	if len(source) == 0 {
		return nil
	}
	payload, _ := json.Marshal(source)
	var cloned []llm.ModelTool
	_ = json.Unmarshal(payload, &cloned)
	return cloned
}
