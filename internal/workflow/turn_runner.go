package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

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
	models               ModelCatalogResolver
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

func (r *TurnRunner) HandleContextReady(ctx context.Context, event WorkflowEvent) error {
	hot, _, err := r.loader.EnsureHotContext(ctx, event.ConversationID)
	if err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorContextLoadFailed, domain.TurnPublicErrorRequestProcessing, err)
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
		Scope:             scope,
		Model:             execution.UpstreamModel,
		CatalogModelID:    execution.ModelID,
		ModelRevision:     execution.ModelRevision,
		ModelPriceID:      execution.ModelPriceID,
		PricingSnapshot:   execution.PricingSnapshot,
		CredentialID:      execution.CredentialID,
		ProviderBaseURL:   execution.BaseURL,
		ReasoningEffort:   reasoningEffort,
		ReasoningSummary:  reasoningSummary,
		TextVerbosity:     textVerbosity,
		DisableTools:      !execution.SupportsTools,
		Instructions:      r.settings.AgentSystemPrompt,
		Input:             buildTurnModelInput(hot),
		PromptCacheKey:    conversationPromptCacheKey(event.ConversationID),
		MaxOutputTokens:   execution.MaxOutputTokens,
		ParallelToolCalls: &parallelToolCalls,
		Metadata: map[string]string{
			"conversation_id": event.ConversationID,
			"turn_id":         event.TurnID,
		},
	}
	state, rawRequest, err := r.tools.PrepareScheduledRun(ctx, input, 1, len(input.Input))
	if err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestPrepareFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	stateKey, runRequestKey, err := r.tools.PersistScheduledRunState(ctx, scope, state, rawRequest)
	if err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, "", "", domain.TurnErrorRequestBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	requestKey := r.blobs.TurnRequestKey(event.ConversationID, event.TurnID)
	if err := r.blobs.PutBytes(ctx, requestKey, rawRequest, "application/json"); err != nil {
		return r.failTurn(ctx, &domain.Turn{ID: event.TurnID, ConversationID: event.ConversationID}, requestKey, "", domain.TurnErrorRequestBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	if _, err := r.runs.StartTurnRun(ctx, event.TurnID, toolRunProviderOpenAIResponses, execution.UpstreamModel, runRequestKey, stateKey); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
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
	case domain.TurnRunStatusFailed, domain.TurnRunStatusRunning:
		return nil
	case domain.TurnRunStatusQueued:
	default:
		return fmt.Errorf("unsupported turn run status %q", run.Status)
	}

	claimed, lease, err := r.runs.ClaimTurnRun(ctx, run.ID)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	runCtx, stopLease := startTurnRunLeaseHeartbeat(ctx, r.runs, lease, r.settings.WorkerLeaseTimeout)
	defer stopLease()
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
		recovered, recoveredResponseKey, recoveredResultKey, found, recoverErr := r.tools.RecoverScheduledRunOutcome(runCtx, state.Scope, claimed.StepIndex)
		if recoverErr != nil {
			return r.failScheduledTurnRun(ctx, event, claimed, lease, nil, recoverErr)
		}
		if found {
			outcome, responseKey, resultKey = recovered, recoveredResponseKey, recoveredResultKey
		} else {
			if r.streamHub != nil {
				_ = r.streamHub.Publish(runCtx, stream.Event{
					Type: stream.EventResponseStarted, ConversationID: event.ConversationID, TurnID: event.TurnID,
				})
			}
			outcome, err = r.tools.RequestScheduledRun(runCtx, state, r.modelEventHandler(runCtx, &domain.Turn{
				ID: claimed.TurnID, ConversationID: event.ConversationID,
			}))
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
	settled, err := r.runs.CompleteScheduledTurnRun(
		ctx, lease, outcome.Model.ResponseID, responseKey, resultKey,
		outcome.Model.Usage, billableImageGenerationCount(outcome.Model),
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
	claimed.Status = domain.TurnRunStatusCompleted
	claimed.ResponseBlobKey = responseKey
	claimed.ResultBlobKey = resultKey
	return r.finishScheduledTurnRun(ctx, event, claimed, outcome)
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
		_, err = r.runs.ScheduleNextTurnRun(ctx, run.TurnID, run.ID, outcome.NextState.StepIndex, toolRunProviderOpenAIResponses, outcome.NextState.Request.Model, requestKey, stateKey)
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}

	turn := &domain.Turn{ID: run.TurnID, ConversationID: event.ConversationID}
	responseKey := r.blobs.TurnResponseKey(turn.ConversationID, turn.ID)
	if len(outcome.Model.RawResponse) > 0 {
		if err := r.blobs.PutBytes(ctx, responseKey, outcome.Model.RawResponse, "application/json"); err != nil {
			return r.failTurn(ctx, turn, r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), domain.TurnErrorResponseBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
		}
	}
	toolRun := &ToolRunResult{Model: outcome.Model, Tools: outcome.Tools, ContextItems: outcome.ContextItems}
	if err := r.persistTurnModelContext(ctx, turn, toolRun); err != nil {
		return r.failTurn(ctx, turn, r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), domain.TurnErrorModelContextBlobFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	generatedImageDrafts, err := r.generatedImageDrafts(ctx, turn, outcome.Model)
	if err != nil {
		return r.failTurn(ctx, turn, r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), domain.TurnErrorGeneratedImageFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	summary := domain.TurnRunSummary{
		RequestBlobKey: r.blobs.TurnRequestKey(turn.ConversationID, turn.ID), ResponseBlobKey: responseKey,
		StreamBlobKey: r.blobs.TurnStreamKey(turn.ConversationID, turn.ID), ResponseID: outcome.Model.ResponseID,
		InputTokens: outcome.Model.Usage.InputTokens, OutputTokens: outcome.Model.Usage.OutputTokens,
		TotalTokens: outcome.Model.Usage.TotalTokens, Model: run.Model,
	}
	assistantDrafts := assistantMessageDraftsFromRun(toolRun, outcome.Model)
	assistantDrafts = append(assistantDrafts, generatedImageDrafts...)
	_, assistantMessages, updatedHead, _, err := r.store.FinalizeTurnSuccess(ctx, turn.ID, assistantDrafts, summary, r.settings.CompactTriggerTokens)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return r.failTurn(ctx, turn, summary.RequestBlobKey, summary.StreamBlobKey, domain.TurnErrorTurnFinalizeFailed, domain.TurnPublicErrorRequestProcessing, err)
	}
	if updatedHead != nil {
		for _, assistantMessage := range assistantMessages {
			r.cache.AppendTailMessage(turn.ConversationID, *updatedHead, assistantMessage)
		}
	}
	_ = r.streamHub.Publish(ctx, stream.Event{
		Type: stream.EventTurnDone, ConversationID: turn.ConversationID, TurnID: turn.ID, ResponseID: outcome.Model.ResponseID,
	})
	return nil
}

func (r *TurnRunner) failScheduledTurnRun(ctx context.Context, event WorkflowEvent, run *domain.TurnRun, lease TurnRunLease, outcome *ScheduledRunOutcome, cause error) error {
	responseID, responseKey, resultKey := "", "", ""
	var modelResult *llm.ModelResult
	if outcome != nil {
		modelResult = outcome.Model
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
	if _, err := r.runs.FailScheduledTurnRun(ctx, lease, responseID, responseKey, resultKey,
		publicToolRunError(cause), requestKey, streamKey, code, publicMessage); err != nil {
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
	if err := r.store.FinalizeTurnFailure(ctx, turn.ID, requestKey, streamKey, code, publicMessage); err != nil {
		if r.logger != nil {
			r.logger.Printf("mark turn %s failed: %v", turn.ID, err)
		}
		return err
	}
	return r.publishTurnFailure(ctx, turn, code, publicMessage, cause)
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

func (r *TurnRunner) modelEventHandler(ctx context.Context, turn *domain.Turn) llm.ModelEventHandler {
	return func(evt llm.ModelEvent) error {
		if strings.TrimSpace(evt.Type) == "" {
			return nil
		}

		streamEvent := stream.Event{
			Type:           evt.Type,
			ConversationID: turn.ConversationID,
			TurnID:         turn.ID,
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
