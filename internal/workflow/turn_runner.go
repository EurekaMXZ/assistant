package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type TurnRunner struct {
	logger               *log.Logger
	settings             WorkflowSettings
	store                TurnWorkflowRepository
	tools                *ToolOrchestrator
	blobs                TurnArtifactStore
	streamHub            stream.Publisher
	cache                cache.ContextTailAppender
	loader               *ContextLoader
	conversations        ConversationReader
	generatedAttachments GeneratedAttachmentStore
	sandboxes            tool.ConversationSandboxReader
	runs                 TurnRunWorkflowStore
	completeEvents       CompleteEventRunStore
	models               ModelCatalogResolver
	activeMu             sync.Mutex
	activeRuns           map[string]activeTurnRun
}

type activeTurnRun struct {
	runID  string
	cancel context.CancelFunc
}

func (r *TurnRunner) HandleAccepted(ctx context.Context, event WorkflowEvent) error {
	headContext, _, err := r.loader.EnsureHotContext(ctx, event.ConversationID)
	if err != nil {
		return err
	}
	if headContext == nil {
		return nil
	}

	if _, err := r.store.MarkTurnContextReady(ctx, event.TurnID); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}

	return nil
}

func (r *TurnRunner) HandleCancellationRequested(ctx context.Context, event WorkflowEvent) error {
	runID := r.cancelActiveRun(event.TurnID)
	if r.streamHub != nil {
		if err := r.streamHub.Publish(ctx, stream.Event{
			Type:           domain.ConversationEventRunCancelled,
			ConversationID: event.ConversationID,
			TurnID:         event.TurnID,
			RunID:          runID,
		}); err != nil {
			return err
		}
	}
	canceller, ok := r.runs.(TurnCancellationStore)
	if !ok || canceller == nil {
		return errors.New("turn cancellation store is not configured")
	}
	err := canceller.FinalizeTurnCancellation(ctx, event.ConversationID, event.TurnID)
	if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.streamHub == nil {
		return nil
	}
	return r.streamHub.Publish(ctx, stream.Event{
		Type:           stream.EventTurnDone,
		ConversationID: event.ConversationID,
		TurnID:         event.TurnID,
		RunID:          runID,
	})
}

func (r *TurnRunner) registerActiveRun(turnID string, runID string, cancel context.CancelFunc) func() {
	if r == nil || strings.TrimSpace(turnID) == "" || cancel == nil {
		return func() {}
	}
	r.activeMu.Lock()
	if r.activeRuns == nil {
		r.activeRuns = make(map[string]activeTurnRun)
	}
	r.activeRuns[turnID] = activeTurnRun{runID: runID, cancel: cancel}
	r.activeMu.Unlock()
	return func() {
		r.activeMu.Lock()
		if active, ok := r.activeRuns[turnID]; ok && active.runID == runID {
			delete(r.activeRuns, turnID)
		}
		r.activeMu.Unlock()
	}
}

func (r *TurnRunner) cancelActiveRun(turnID string) string {
	if r == nil {
		return ""
	}
	r.activeMu.Lock()
	active, ok := r.activeRuns[turnID]
	r.activeMu.Unlock()
	if !ok {
		return ""
	}
	active.cancel()
	return active.runID
}

func (r *TurnRunner) HandleContextReady(ctx context.Context, event WorkflowEvent) error {
	turn := &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}
	var err error
	if r.store != nil {
		turn, err = r.store.GetTurn(ctx, event.TurnID)
		if err != nil {
			return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorContextLoadFailed, domain.TurnPublicErrorRequestProcessing, err)
		}
	}
	var modelInput []llm.ModelItem
	if turn.RetryOfTurnID != "" {
		sourceTurnID := variantSourceTurnID(turn)
		modelInput, err = r.retryModelInput(ctx, event.ConversationID, sourceTurnID, turn.ID)
	} else {
		var hot *cache.ContextSnapshot
		hot, _, err = r.loader.EnsureHotContext(ctx, event.ConversationID)
		if err == nil {
			modelInput = buildTurnModelInput(hot)
		}
	}
	if err != nil {
		return r.failTurn(ctx, turn, "", "", domain.TurnErrorContextLoadFailed, domain.TurnPublicErrorRequestProcessing, err)
	}

	scope, err := r.toolScope(ctx, event.ConversationID, event.TurnID)
	if err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorSandboxScopeFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	if r.models == nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing, errors.New("model catalog is not configured"))
	}
	execution, err := r.models.GetTurnExecution(ctx, event.TurnID)
	if err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	if execution == nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing, errors.New("turn has no model execution snapshot"))
	}
	if err := validateExecutionSnapshot(execution); err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	reasoningEffort, reasoningSummary, textVerbosity := modelRequestParameters(execution.DefaultParameters)
	if execution.ReasoningEffort != "" {
		reasoningEffort = execution.ReasoningEffort
	}
	parallelToolCalls := execution.SupportsParallelTools
	input := ToolRunInput{
		Scope:               scope,
		Model:               execution.UpstreamModel,
		ContextWindowTokens: execution.ContextWindowTokens,
		CatalogModelID:      execution.ModelID,
		ModelRevision:       execution.ModelRevision,
		ModelPriceID:        execution.ModelPriceID,
		PricingSnapshot:     execution.PricingSnapshot,
		CredentialID:        execution.CredentialID,
		ProviderBaseURL:     execution.BaseURL,
		ReasoningEffort:     reasoningEffort,
		ReasoningSummary:    reasoningSummary,
		TextVerbosity:       textVerbosity,
		DisableTools:        !execution.SupportsTools,
		Instructions:        r.settings.AgentSystemPrompt,
		Input:               modelInput,
		PromptCacheKey:      conversationPromptCacheKey(event.ConversationID),
		MaxOutputTokens:     execution.MaxOutputTokens,
		ParallelToolCalls:   &parallelToolCalls,
		Metadata: map[string]string{
			"conversation_id": event.ConversationID,
			"turn_id":         event.TurnID,
		},
	}
	state, rawRequest, err := r.tools.PrepareScheduledRun(ctx, input, 1, len(input.Input))
	if err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	requestTokens := estimateModelContextTokens(state.Request.Instructions, state.Request.Input, state.Request.Tools)
	if requestTokens > modelRequestInputLimit(execution.ContextWindowTokens, 0) {
		return r.failTurn(ctx, turn, "", "", domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing,
			fmt.Errorf("model request input estimate %d exceeds context limit", requestTokens))
	}
	stateKey, runRequestKey, err := r.tools.PersistScheduledRunState(ctx, scope, state, rawRequest)
	if err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	requestKey := r.blobs.TurnRequestKey(event.ConversationID, event.TurnID)
	if err := r.blobs.PutBytes(ctx, requestKey, rawRequest, "application/json"); err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, requestKey, "", domain.TurnErrorRequestBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	runID, err := r.runs.StartTurnRun(ctx, event.TurnID, toolRunProviderOpenAIResponses, execution.UpstreamModel, runRequestKey, stateKey)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	run, err := r.runs.GetTurnRun(ctx, runID)
	if err == nil && run != nil {
		if err := r.persistImmutableRunRequest(ctx, event.ConversationID, event.TurnID, run.StepIndex, run.ID, rawRequest); err != nil {
			return err
		}
	}
	return nil
}

func variantSourceTurnID(turn *domain.Turn) string {
	if turn == nil {
		return ""
	}
	sourceTurnID := turn.RetryOfTurnID
	var metadata map[string]any
	if json.Unmarshal(turn.Metadata, &metadata) == nil {
		if selected, ok := metadata["variant_source_turn_id"].(string); ok && strings.TrimSpace(selected) != "" {
			sourceTurnID = selected
		}
	}
	return sourceTurnID
}

func (r *TurnRunner) retryModelInput(ctx context.Context, conversationID string, sourceTurnID string, variantTurnID string) ([]llm.ModelItem, error) {
	if r == nil || r.blobs == nil {
		return nil, errors.New("turn artifacts are not configured")
	}
	raw, err := r.blobs.GetBytes(ctx, r.blobs.TurnRequestKey(conversationID, sourceTurnID))
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("get retry source request: %w", err)
		}
		if r.loader == nil || r.store == nil {
			return nil, errors.New("retry context loader is not configured")
		}
		source, sourceErr := r.store.GetTurn(ctx, sourceTurnID)
		if sourceErr != nil {
			return nil, sourceErr
		}
		if source.Status != domain.TurnStatusFailed {
			return nil, fmt.Errorf("get retry source request: %w", err)
		}
		hot, _, loadErr := r.loader.EnsureHotContext(ctx, conversationID)
		if loadErr != nil {
			return nil, loadErr
		}
		return r.replaceRetryUserInput(ctx, conversationID, variantTurnID, buildTurnModelInput(hot))
	}
	var request struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("decode retry source request: %w", err)
	}
	if len(request.Input) == 0 {
		return nil, errors.New("retry source request has no input")
	}
	items := make([]llm.ModelItem, 0, len(request.Input))
	lastUserIndex := -1
	for _, rawItem := range request.Input {
		var header struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if err := json.Unmarshal(rawItem, &header); err != nil {
			return nil, fmt.Errorf("decode retry source input item: %w", err)
		}
		item := llm.ModelItem{
			Type: header.Type,
			Role: header.Role,
			Raw:  append(json.RawMessage(nil), rawItem...),
		}
		if header.Type == llm.ModelItemFunctionCallOutput {
			var output struct {
				CallID string `json:"call_id"`
				Output string `json:"output"`
			}
			if err := json.Unmarshal(rawItem, &output); err != nil {
				return nil, fmt.Errorf("decode retry source tool output: %w", err)
			}
			item.CallID = output.CallID
			item.Output = output.Output
			item = truncateModelContextItem(item, r.tools.modelToolOutputTokenLimit())
		}
		items = append(items, item)
		if header.Role == domain.RoleUser {
			lastUserIndex = len(items) - 1
		}
	}
	return r.replaceRetryUserInputAt(ctx, conversationID, variantTurnID, items, lastUserIndex)
}

func (r *TurnRunner) replaceRetryUserInput(ctx context.Context, conversationID string, variantTurnID string, items []llm.ModelItem) ([]llm.ModelItem, error) {
	lastUserIndex := -1
	for index := range items {
		if items[index].Role == domain.RoleUser {
			lastUserIndex = index
		}
	}
	return r.replaceRetryUserInputAt(ctx, conversationID, variantTurnID, items, lastUserIndex)
}

func (r *TurnRunner) replaceRetryUserInputAt(ctx context.Context, conversationID string, variantTurnID string, items []llm.ModelItem, lastUserIndex int) ([]llm.ModelItem, error) {
	if lastUserIndex == -1 {
		return nil, errors.New("retry source request has no user input")
	}
	if r.store == nil || r.loader == nil {
		return nil, errors.New("retry user message loader is not configured")
	}
	message, err := r.store.GetUserMessageByTurn(ctx, variantTurnID)
	if err != nil {
		return nil, fmt.Errorf("get retry user message: %w", err)
	}
	message.ContextExcluded = false
	replacement, err := r.loader.modelInputItemsForMessage(ctx, conversationID, *message)
	if err != nil {
		return nil, err
	}
	updated := make([]llm.ModelItem, 0, len(items)-1+len(replacement))
	updated = append(updated, items[:lastUserIndex]...)
	updated = append(updated, replacement...)
	updated = append(updated, items[lastUserIndex+1:]...)
	return updated, nil
}

func validateExecutionSnapshot(execution *domain.ModelExecutionSnapshot) error {
	if execution == nil || strings.TrimSpace(execution.ModelID) == "" || strings.TrimSpace(execution.UpstreamModel) == "" {
		return errors.New("model execution snapshot is missing catalog model data")
	}
	if strings.TrimSpace(execution.CredentialID) == "" || strings.TrimSpace(execution.BaseURL) == "" {
		return errors.New("model execution snapshot is missing provider credential data")
	}
	if strings.TrimSpace(execution.ModelPriceID) == "" || len(execution.PricingSnapshot) == 0 || string(execution.PricingSnapshot) == "{}" {
		return errors.New("model execution snapshot is missing published pricing")
	}
	if execution.ContextWindowTokens <= 0 || execution.MaxOutputTokens <= 0 || execution.MaxOutputTokens > execution.ContextWindowTokens {
		return errors.New("model execution snapshot has invalid token limits")
	}
	return nil
}

func modelRequestParameters(raw json.RawMessage) (string, string, string) {
	var parameters struct {
		ReasoningEffort  string `json:"reasoning_effort"`
		ReasoningSummary string `json:"reasoning_summary"`
		TextVerbosity    string `json:"text_verbosity"`
	}
	_ = json.Unmarshal(raw, &parameters)
	return parameters.ReasoningEffort, parameters.ReasoningSummary, parameters.TextVerbosity
}

func conversationPromptCacheKey(conversationID string) string {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(conversationID))
	return fmt.Sprintf("assistant-conversation-%x", digest[:8])
}

func (r *TurnRunner) HandleTurnRunRequested(ctx context.Context, event WorkflowEvent) error {
	runID := strings.TrimSpace(event.TurnRunID)
	if runID == "" {
		return fmt.Errorf("workflow event %s has no turn_run_id", event.ID)
	}
	run, err := r.runs.GetTurnRun(ctx, runID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	switch run.Status {
	case domain.TurnRunStatusCompleted:
		return r.continueCompletedTurnRun(ctx, event, run)
	case domain.TurnRunStatusFailed, domain.TurnRunStatusRunning, domain.TurnRunStatusCancelRequested, domain.TurnRunStatusCancelled:
		return nil
	case domain.TurnRunStatusQueued:
	default:
		return fmt.Errorf("unsupported turn run status %q", run.Status)
	}

	executionParent, cancelExecution := context.WithCancel(ctx)
	unregisterActiveRun := r.registerActiveRun(run.TurnID, run.ID, cancelExecution)
	stopLease := func() error { return nil }
	defer func() {
		unregisterActiveRun()
		cancelExecution()
		_ = stopLease()
	}()

	claimed, lease, err := r.runs.ClaimTurnRun(ctx, run.ID)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := r.ensureImmutableRunRequest(ctx, event.ConversationID, claimed); err != nil {
		return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, err)
	}
	runCtx, stop := startTurnRunLeaseHeartbeat(executionParent, r.runs, lease, r.settings.WorkerLeaseTimeout)
	stopLease = stop
	if runCtx.Err() != nil {
		return nil
	}
	var state *ScheduledRunState
	var outcome *ScheduledRunOutcome
	responseKey := claimed.ResponseBlobKey
	resultKey := claimed.ResultBlobKey
	if strings.TrimSpace(claimed.ResultBlobKey) != "" {
		outcome, err = r.tools.LoadScheduledRunOutcome(runCtx, claimed.ResultBlobKey)
		if err != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, err)
		}
		if outcome.Model == nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, fmt.Errorf("checkpointed run has no model result"))
		}
	} else {
		state, err = r.tools.LoadScheduledRunState(runCtx, claimed.StateBlobKey)
		if err != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, err)
		}
		if err := r.hydrateScheduledRunState(runCtx, claimed.TurnID, state); err != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, err)
		}
		recovered, recoveredResponseKey, recoveredResultKey, found, recoverErr := r.tools.RecoverScheduledRunOutcome(runCtx, state.Scope, claimed.StepIndex)
		if recoverErr != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, recoverErr)
		}
		if found {
			outcome, responseKey, resultKey = recovered, recoveredResponseKey, recoveredResultKey
		} else {
			providerState, cloneErr := cloneScheduledRunState(state)
			if cloneErr != nil {
				return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, cloneErr)
			}
			if err := r.loader.hydrateScheduledRunImages(runCtx, providerState); err != nil {
				return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, err)
			}
			if r.streamHub != nil {
				_ = r.streamHub.Publish(runCtx, stream.Event{
					Type: stream.EventResponseStarted, ConversationID: event.ConversationID, TurnID: event.TurnID,
				})
			}
			outcome, err = r.tools.RequestScheduledRun(runCtx, providerState, r.modelEventHandler(runCtx, &domain.Turn{
				ID: claimed.TurnID, ConversationID: event.ConversationID,
			}, claimed.ID))
			if err != nil {
				if leaseErr := stopLease(); leaseErr != nil {
					return leaseErr
				}
				return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
			}
			responseKey, resultKey, err = r.tools.PersistScheduledRunOutcome(runCtx, state.Scope, claimed, outcome)
			if err != nil {
				return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
			}
		}
		if err := r.runs.CheckpointScheduledTurnRun(ctx, lease, outcome.Model.ResponseID, responseKey, resultKey); err != nil {
			return err
		}
	}
	if !outcome.Postprocessed {
		if state == nil {
			state, err = r.tools.LoadScheduledRunState(runCtx, claimed.StateBlobKey)
			if err != nil {
				return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
			}
			if err := r.hydrateScheduledRunState(runCtx, claimed.TurnID, state); err != nil {
				return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
			}
		}
		if err := r.tools.PostprocessScheduledRun(runCtx, claimed, state, outcome); err != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
		}
		responseKey, resultKey, err = r.tools.PersistScheduledRunOutcome(runCtx, state.Scope, claimed, outcome)
		if err != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
		}
		if err := r.runs.CheckpointScheduledTurnRun(ctx, lease, outcome.Model.ResponseID, responseKey, resultKey); err != nil {
			return err
		}
	}
	if err := r.externalizeGeneratedImages(ctx, &domain.Turn{ID: claimed.TurnID, ConversationID: event.ConversationID}, outcome); err != nil {
		return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
	}
	if state == nil && outcome.NextState == nil {
		state, err = r.tools.LoadScheduledRunState(runCtx, claimed.StateBlobKey)
		if err != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
		}
	}
	immutableResponseKey, checkpointKey, err := r.persistImmutableRunSuccess(ctx, event.ConversationID, claimed.TurnID, claimed, state, outcome)
	if err != nil {
		return r.failScheduledTurnRun(ctx, event, claimed, lease, outcome, err)
	}
	if immutableResponseKey != "" {
		responseKey = immutableResponseKey
		claimed.ResponseBlobKey = immutableResponseKey
	}
	claimed.CheckpointBlobKey = checkpointKey
	compactTriggerTokens := r.compactTriggerTokens(ctx, event.TurnID, outcome.ContextWindowTokens)
	settled, err := r.runs.CompleteScheduledTurnRun(
		ctx, lease, outcome.Model.ResponseID, responseKey, resultKey,
		outcome.Model.Usage, billableImageGenerationCount(outcome.Model), compactTriggerTokens,
	)
	if err != nil {
		if leaseErr := stopLease(); leaseErr != nil {
			return leaseErr
		}
		return err
	}
	_ = stopLease()
	if settled != nil && settled.Status == domain.TurnRunStatusFailed {
		return r.publishTurnFailure(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID},
			domain.TurnErrorBillingSettlementFailed, domain.TurnPublicErrorBillingRequired, domain.ErrPaymentRequired)
	}
	completedRun := claimed
	if settled != nil {
		completedRun = settled
	}
	completedRun.Status = domain.TurnRunStatusCompleted
	completedRun.ResponseBlobKey = responseKey
	completedRun.ResultBlobKey = resultKey
	return r.finishScheduledTurnRun(ctx, event, completedRun, outcome)
}

func (r *TurnRunner) ensureImmutableRunRequest(ctx context.Context, conversationID string, run *domain.TurnRun) error {
	if r == nil || run == nil || strings.TrimSpace(run.RequestBlobKey) == "" {
		return nil
	}
	if _, ok := r.blobs.(ImmutableRunArtifactStore); !ok {
		return nil
	}
	payload, err := r.blobs.GetBytes(ctx, run.RequestBlobKey)
	if err != nil {
		return fmt.Errorf("load run request artifact: %w", err)
	}
	if strings.HasSuffix(run.RequestBlobKey, ".zst") {
		payload, err = decompressImmutableRunPayload(payload)
		if err != nil {
			return err
		}
	}
	return r.persistImmutableRunRequest(ctx, conversationID, run.TurnID, run.StepIndex, run.ID, payload)
}

func (r *TurnRunner) continueCompletedTurnRun(ctx context.Context, event WorkflowEvent, run *domain.TurnRun) error {
	if run == nil || strings.TrimSpace(run.ResultBlobKey) == "" {
		return fmt.Errorf("completed turn run has no result artifact")
	}
	outcome, err := r.tools.LoadScheduledRunOutcome(ctx, run.ResultBlobKey)
	if err != nil {
		return err
	}
	return r.finishScheduledTurnRun(ctx, event, run, outcome)
}

func (r *TurnRunner) finishScheduledTurnRun(ctx context.Context, event WorkflowEvent, run *domain.TurnRun, outcome *ScheduledRunOutcome) error {
	if run == nil || outcome == nil || outcome.Model == nil {
		return fmt.Errorf("completed turn run has no model outcome")
	}
	if outcome.NextState != nil {
		stateKey, requestKey, err := r.tools.PersistScheduledRunState(ctx, outcome.NextState.Scope, outcome.NextState, outcome.NextRequest)
		if err != nil {
			return err
		}
		nextRunID, err := r.runs.ScheduleNextTurnRun(ctx, run.TurnID, run.ID, outcome.NextState.StepIndex, toolRunProviderOpenAIResponses, outcome.NextState.Request.Model, requestKey, stateKey)
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return r.persistImmutableRunRequest(ctx, event.ConversationID, run.TurnID, outcome.NextState.StepIndex, nextRunID, outcome.NextRequest)
	}

	turn := &domain.Turn{ID: run.TurnID, ConversationID: event.ConversationID}
	if err := r.externalizeGeneratedImages(ctx, turn, outcome); err != nil {
		return r.failTurn(ctx, turn, r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), domain.TurnErrorGeneratedImageFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	responseKey := r.blobs.TurnResponseKey(turn.ConversationID, turn.ID)
	if len(outcome.Model.RawResponse) > 0 {
		if err := r.blobs.PutBytes(ctx, responseKey, outcome.Model.RawResponse, "application/json"); err != nil {
			return r.failTurn(ctx, turn, r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), domain.TurnErrorResponseBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
		}
	}
	if run.ResponseBlobKey != "" {
		responseKey = run.ResponseBlobKey
	}
	toolRun := &ToolRunResult{Model: outcome.Model, Tools: outcome.Tools, ContextItems: outcome.ContextItems}
	if err := r.persistTurnModelContext(ctx, turn, toolRun); err != nil {
		return r.failTurn(ctx, turn, r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), domain.TurnErrorModelContextBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	summary := domain.TurnRunSummary{
		RunID: run.ID, CheckpointBlobKey: run.CheckpointBlobKey,
		CheckpointChecksum: immutableRunArtifactChecksum(run.ArtifactMetadata, immutableRunCheckpointArtifact),
		RequestBlobKey:     r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), ResponseBlobKey: responseKey,
		StreamBlobKey: r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), ResponseID: outcome.Model.ResponseID,
		InputTokens: outcome.Model.Usage.InputTokens, OutputTokens: outcome.Model.Usage.OutputTokens,
		TotalTokens: outcome.Model.Usage.TotalTokens, ContextWindowTokens: outcome.ContextWindowTokens, Model: run.Model,
	}
	assistantDrafts := assistantMessageDraftsFromRun(toolRun, outcome.Model)
	assistantDrafts = append(assistantDrafts, outcome.GeneratedImageDrafts...)
	compactTriggerTokens := r.compactTriggerTokens(ctx, turn.ID, outcome.ContextWindowTokens)
	completedTurn, assistantMessages, updatedHead, _, err := r.store.FinalizeTurnSuccess(ctx, turn.ID, assistantDrafts, summary, compactTriggerTokens)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return r.failTurn(ctx, turn, summary.RequestBlobKey, summary.StreamBlobKey, domain.TurnErrorTurnFinalizeFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	if updatedHead != nil && (completedTurn == nil || completedTurn.RetryOfTurnID == "") {
		for _, assistantMessage := range assistantMessages {
			r.cache.AppendTailMessage(turn.ConversationID, *updatedHead, assistantMessage)
		}
		_, _, _ = r.loader.EnsureHotContext(ctx, turn.ConversationID)
	}
	_ = r.streamHub.Publish(ctx, stream.Event{
		Type: stream.EventTurnDone, ConversationID: turn.ConversationID, TurnID: turn.ID, ResponseID: outcome.Model.ResponseID,
	})
	return nil
}

func (r *TurnRunner) failScheduledTurnRun(ctx context.Context, event WorkflowEvent, run *domain.TurnRun, lease TurnRunLease, outcome *ScheduledRunOutcome, cause error) error {
	if err := r.persistImmutableRunFailure(ctx, event.ConversationID, event.TurnID, run, cause); err != nil && r.logger != nil {
		r.logger.Printf("persist failed run artifact for %s: %v", run.ID, err)
	}
	responseID, responseKey, resultKey := "", "", ""
	var modelResult *llm.ModelResult
	contextWindowTokens := 0
	if outcome != nil {
		modelResult = outcome.Model
		contextWindowTokens = outcome.ContextWindowTokens
	}
	if modelResult != nil {
		responseID = modelResult.ResponseID
		if persistedResponseKey, persistedResultKey, err := r.tools.PersistScheduledRunOutcome(ctx, tool.ToolScope{
			ConversationID: event.ConversationID, TurnID: event.TurnID,
		}, run, outcome); err == nil {
			responseKey, resultKey = persistedResponseKey, persistedResultKey
		}
	}
	code, publicMessage := classifyInitialToolRunFailure(cause, modelResult)
	requestKey := r.blobs.TurnRequestKey(event.ConversationID, event.TurnID)
	streamKey := r.blobs.TurnStreamKey(event.ConversationID, event.TurnID)
	compactTriggerTokens := r.compactTriggerTokens(ctx, event.TurnID, contextWindowTokens)
	if _, err := r.runs.FailScheduledTurnRun(ctx, lease, responseID, responseKey, resultKey,
		publicToolRunError(cause), requestKey, streamKey, code, publicMessage, compactTriggerTokens); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return fmt.Errorf("fail scheduled turn run: %w", err)
	}
	turn := &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}
	return r.publishTurnFailure(ctx, turn, code, publicMessage, cause)
}

func (r *TurnRunner) persistTurnModelContext(ctx context.Context, turn *domain.Turn, run *ToolRunResult) error {
	if r == nil || r.blobs == nil || turn == nil || run == nil || len(run.ContextItems) == 0 {
		return nil
	}

	payload, err := marshalModelContextItems(run.ContextItems)
	if err != nil {
		return fmt.Errorf("marshal turn model context: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}

	key := r.blobs.TurnModelContextKey(turn.ConversationID, turn.ID)
	if err := r.blobs.PutBytes(ctx, key, payload, turnModelContextContentType); err != nil {
		return fmt.Errorf("persist turn model context: %w", err)
	}
	return nil
}

func (r *TurnRunner) toolScope(ctx context.Context, conversationID string, turnID string) (tool.ToolScope, error) {
	scope := tool.ToolScope{
		ConversationID: conversationID,
		TurnID:         turnID,
	}
	if r == nil || r.sandboxes == nil {
		return scope, nil
	}

	sandbox, err := r.sandboxes.GetUsableConversationSandbox(ctx, conversationID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return scope, nil
		}
		return scope, err
	}
	scope.HasSandbox = sandbox != nil && (sandbox.Status == domain.SandboxStatusActive || sandbox.Status == domain.SandboxStatusStopped) && sandbox.DestroyedAt == nil
	return scope, nil
}

func (r *TurnRunner) failTurn(ctx context.Context, turn *domain.Turn, requestKey string, streamKey string, code string, publicMessage string, cause error) error {
	if turn == nil {
		return nil
	}
	compactTriggerTokens := r.compactTriggerTokens(ctx, turn.ID, 0)
	if err := r.store.FinalizeTurnFailure(ctx, turn.ID, requestKey, streamKey, code, publicMessage, compactTriggerTokens); err != nil {
		if r.logger != nil {
			r.logger.Printf("mark turn %s failed: %v", turn.ID, err)
		}
		return err
	}
	return r.publishTurnFailure(ctx, turn, code, publicMessage, cause)
}

func (r *TurnRunner) compactTriggerTokens(ctx context.Context, turnID string, contextWindowTokens int) int {
	if contextWindowTokens <= 0 && r != nil && r.models != nil && strings.TrimSpace(turnID) != "" {
		if execution, err := r.models.GetTurnExecution(ctx, turnID); err == nil && execution != nil {
			contextWindowTokens = execution.ContextWindowTokens
		}
	}
	if r == nil {
		return compactTriggerTokenLimit(0, contextWindowTokens)
	}
	return compactTriggerTokenLimit(r.settings.CompactTriggerTokens, contextWindowTokens)
}

func (r *TurnRunner) hydrateScheduledRunState(ctx context.Context, turnID string, state *ScheduledRunState) error {
	if state == nil || state.Request.ContextWindowTokens > 0 {
		return nil
	}
	if r == nil || r.models == nil {
		return errors.New("model catalog is not configured")
	}
	execution, err := r.models.GetTurnExecution(ctx, turnID)
	if err != nil {
		return err
	}
	if execution == nil || execution.ContextWindowTokens <= 0 {
		return errors.New("turn model context window is unavailable")
	}
	state.Request.ContextWindowTokens = execution.ContextWindowTokens
	return nil
}

func (r *TurnRunner) publishTurnFailure(ctx context.Context, turn *domain.Turn, code string, publicMessage string, cause error) error {
	if turn != nil && cause != nil && r.logger != nil {
		r.logger.Printf("turn %s failed (%s): %v", turn.ID, code, cause)
	}
	if turn == nil || r.streamHub == nil {
		return nil
	}
	return r.streamHub.Publish(ctx, stream.Event{
		Type:           stream.EventResponseFailed,
		ConversationID: turn.ConversationID,
		TurnID:         turn.ID,
		ErrorCode:      code,
		Error:          publicMessage,
	})
}

func (r *TurnRunner) modelEventHandler(ctx context.Context, turn *domain.Turn, runID string) llm.ModelEventHandler {
	var transportSeq int64
	return func(evt llm.ModelEvent) error {
		if strings.TrimSpace(evt.Type) == "" {
			return nil
		}

		transportSeq++
		streamEvent := stream.Event{
			Type:           evt.Type,
			ConversationID: turn.ConversationID,
			TurnID:         turn.ID,
			RunID:          runID,
			ItemID:         evt.ItemID,
			TransportSeq:   transportSeq,
			OutputIndex:    evt.OutputIndex,
			ContentIndex:   evt.ContentIndex,
			ResponseID:     evt.ResponseID,
			Delta:          evt.Delta,
			Text:           evt.Text,
			Error:          evt.Error,
		}
		if evt.Type == "response.failed" || evt.Type == "error" {
			streamEvent.ErrorCode = domain.TurnErrorUpstreamRequestFailed
			streamEvent.Error = domain.TurnPublicErrorUpstreamRequestFailed
			payload, _ := json.Marshal(map[string]string{
				"type":       evt.Type,
				"error_code": streamEvent.ErrorCode,
				"error":      streamEvent.Error,
			})
			streamEvent.Payload = string(payload)
		} else if len(evt.Raw) > 0 {
			streamEvent.Payload = string(evt.Raw)
		}
		if publishErr := r.streamHub.Publish(ctx, streamEvent); publishErr != nil {
			r.logger.Printf("publish model stream event %s for turn %s: %v", evt.Type, turn.ID, publishErr)
		}
		return nil
	}
}

func assistantMessageDraftsFromRun(run *ToolRunResult, result *llm.ModelResult) []domain.AssistantMessageDraft {
	var items []llm.ModelItem
	if run != nil && len(run.ContextItems) > 0 {
		items = run.ContextItems
	} else if result != nil {
		items = result.OutputItems
	}

	drafts := make([]domain.AssistantMessageDraft, 0)
	for index, item := range items {
		if item.Type != llm.ModelItemMessage || item.Role != domain.RoleAssistant || strings.TrimSpace(item.Content) == "" {
			continue
		}
		metadata, _ := json.Marshal(map[string]any{
			"display_kind":  "assistant_text",
			"model_item_id": item.ID,
			"output_index":  index,
			"phase":         strings.TrimSpace(item.Phase),
			"source":        "model_output",
		})
		drafts = append(drafts, domain.AssistantMessageDraft{
			ContentText: item.Content,
			Metadata:    metadata,
		})
	}
	if len(drafts) == 0 && result != nil && strings.TrimSpace(result.FinalText) != "" {
		metadata, _ := json.Marshal(map[string]any{"display_kind": "assistant_text", "source": "final_text"})
		drafts = append(drafts, domain.AssistantMessageDraft{ContentText: result.FinalText, Metadata: metadata})
	}
	return drafts
}

func classifyInitialToolRunFailure(err error, result *llm.ModelResult) (string, string) {
	switch {
	case errors.Is(err, llm.ErrUpstreamRequestFailed):
		return domain.TurnErrorUpstreamRequestFailed, domain.TurnPublicErrorUpstreamRequestFailed
	case result == nil:
		return domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing
	default:
		return domain.TurnErrorBackendRequestFailed, domain.TurnPublicErrorRequestProcessing
	}
}
