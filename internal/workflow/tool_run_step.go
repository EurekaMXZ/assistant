package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	ResponseBlobKey      string                         `json:"response_blob_key,omitempty"`
	ContextWindowTokens  int                            `json:"context_window_tokens,omitempty"`
	Tools                []llm.ModelTool                `json:"tools,omitempty"`
	ContextItems         []llm.ModelItem                `json:"context_items,omitempty"`
	ToolResults          []llm.ModelItem                `json:"tool_results,omitempty"`
	NextState            *ScheduledRunState             `json:"next_state,omitempty"`
	NextRequest          json.RawMessage                `json:"next_request,omitempty"`
	Postprocessed        bool                           `json:"postprocessed"`
	GeneratedImageDrafts []domain.AssistantMessageDraft `json:"generated_image_drafts,omitempty"`
}

type AwaitingInputSignal struct {
	ToolCall *domain.ToolCallRecord
	Prompt   *tool.AskUserPrompt
}

func (s *AwaitingInputSignal) Error() string {
	return "turn run is awaiting user input"
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
	rawRequest, err = canonicalScheduledRunRequest(rawRequest, request.Input)
	if err != nil {
		return nil, nil, err
	}
	if initialInputCount < 0 || initialInputCount > len(request.Input) {
		initialInputCount = len(request.Input)
	}
	return &ScheduledRunState{
		Version: scheduledRunStateVersion, StepIndex: stepIndex,
		InitialInputCount: initialInputCount, Scope: cloneToolScope(input.Scope), Request: request,
	}, rawRequest, nil
}

func canonicalScheduledRunRequest(providerRequest json.RawMessage, items []llm.ModelItem) (json.RawMessage, error) {
	hasImageReferences := false
	for _, item := range items {
		if item.Type == llm.ModelItemImageGenerationCall && strings.Contains(string(item.Raw), `"result_ref"`) {
			hasImageReferences = true
			break
		}
	}
	if !hasImageReferences {
		return providerRequest, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(providerRequest, &payload); err != nil {
		return nil, fmt.Errorf("decode scheduled request manifest: %w", err)
	}
	var input []json.RawMessage
	if err := json.Unmarshal(payload["input"], &input); err != nil {
		return nil, fmt.Errorf("decode scheduled request input manifest: %w", err)
	}
	if len(input) != len(items) {
		return nil, fmt.Errorf("scheduled request input manifest length mismatch")
	}
	changed := false
	for index, item := range items {
		if item.Type != llm.ModelItemImageGenerationCall || len(item.Raw) == 0 || !strings.Contains(string(item.Raw), `"result_ref"`) {
			continue
		}
		input[index] = append(json.RawMessage(nil), item.Raw...)
		changed = true
	}
	if !changed {
		return providerRequest, nil
	}
	payload["input"], _ = json.Marshal(input)
	manifest, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal scheduled request manifest: %w", err)
	}
	return manifest, nil
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
	localCalls, err := localToolCallsAwaitingInputLast(functionCalls)
	if err != nil {
		return err
	}
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
	toolOutputStart := len(nextInput)
	nextInput, nextScope, err := o.executeLocalToolCalls(ctx, run, nextInput, state.Scope, localCalls, toolOutputBudget)
	outcome.ToolResults = cloneModelItems(nextInput[toolOutputStart:])
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
	if err := putCompressedArtifact(ctx, o.artifacts, stateKey, statePayload); err != nil {
		return "", "", fmt.Errorf("persist scheduled run state: %w", err)
	}
	if err := putCompressedArtifact(ctx, o.artifacts, requestKey, rawRequest); err != nil {
		return "", "", fmt.Errorf("persist scheduled run request: %w", err)
	}
	return stateKey, requestKey, nil
}

func (o *ToolOrchestrator) LoadScheduledRunState(ctx context.Context, key string) (*ScheduledRunState, error) {
	if o == nil || o.artifacts == nil {
		return nil, fmt.Errorf("load scheduled run state requires artifact store")
	}
	payload, err := getCompressedArtifact(ctx, o.artifacts, key)
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
	responseKey := ""
	if len(outcome.Model.RawResponse) > 0 {
		writer, ok := o.artifacts.(ImmutableRunArtifactStore)
		if !ok || writer == nil {
			return "", "", fmt.Errorf("persist scheduled run response requires immutable artifact storage")
		}
		responseKey = writer.ImmutableRunArtifactKey(scope.ConversationID, scope.TurnID, run.StepIndex, run.ID, immutableRunResponseArtifact)
		compressed, _, err := compressImmutableRunPayload(outcome.Model.RawResponse)
		if err != nil {
			return "", "", fmt.Errorf("compress scheduled run response: %w", err)
		}
		if err := writer.PutImmutableBytes(ctx, responseKey, compressed, immutableRunArtifactContentType); err != nil {
			return "", "", fmt.Errorf("persist scheduled run response: %w", err)
		}
	}
	storedOutcome := *outcome
	storedOutcome.ResponseBlobKey = responseKey
	storedModel := *outcome.Model
	storedModel.RawResponse = nil
	storedOutcome.Model = &storedModel
	payload, err := json.Marshal(&storedOutcome)
	if err != nil {
		return "", "", fmt.Errorf("marshal scheduled run outcome: %w", err)
	}
	resultKey := o.artifacts.TurnRunResultKey(scope.ConversationID, scope.TurnID, run.StepIndex)
	if err := putCompressedArtifact(ctx, o.artifacts, resultKey, payload); err != nil {
		return "", "", fmt.Errorf("persist scheduled run outcome: %w", err)
	}
	return responseKey, resultKey, nil
}

func (o *ToolOrchestrator) LoadScheduledRunOutcome(ctx context.Context, key string) (*ScheduledRunOutcome, error) {
	if o == nil || o.artifacts == nil {
		return nil, fmt.Errorf("load scheduled run outcome requires artifact store")
	}
	payload, err := getCompressedArtifact(ctx, o.artifacts, key)
	if err != nil {
		return nil, err
	}
	var outcome ScheduledRunOutcome
	if err := json.Unmarshal(payload, &outcome); err != nil {
		return nil, fmt.Errorf("decode scheduled run outcome: %w", err)
	}
	if outcome.Model != nil && len(outcome.Model.RawResponse) == 0 && strings.TrimSpace(outcome.ResponseBlobKey) != "" {
		response, err := getCompressedArtifact(ctx, o.artifacts, outcome.ResponseBlobKey)
		if err != nil {
			return nil, fmt.Errorf("load scheduled run response: %w", err)
		}
		outcome.Model.RawResponse = response
	}
	return &outcome, nil
}

func putCompressedArtifact(ctx context.Context, artifacts interface {
	PutBytes(context.Context, string, []byte, string) error
}, key string, payload []byte) error {
	if !strings.HasSuffix(key, ".zst") {
		return fmt.Errorf("compressed artifact key must end in .zst: %q", key)
	}
	compressed, _, err := compressImmutableRunPayload(payload)
	if err != nil {
		return err
	}
	return artifacts.PutBytes(ctx, key, compressed, immutableRunArtifactContentType)
}

func getCompressedArtifact(ctx context.Context, artifacts interface {
	GetBytes(context.Context, string) ([]byte, error)
}, key string) ([]byte, error) {
	payload, err := artifacts.GetBytes(ctx, key)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(key, ".zst") {
		return nil, fmt.Errorf("compressed artifact key must end in .zst: %q", key)
	}
	return decompressImmutableRunPayload(payload)
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
	responseKey := strings.TrimSpace(outcome.ResponseBlobKey)
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
