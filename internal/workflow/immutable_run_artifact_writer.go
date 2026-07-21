package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
)

func (r *TurnRunner) persistImmutableRunRequest(ctx context.Context, conversationID string, turnID string, stepIndex int, runID string, payload []byte) error {
	writer, ok := r.blobs.(ImmutableRunArtifactStore)
	if !ok || writer == nil {
		return nil
	}
	compressed, checksum, err := compressImmutableRunPayload(payload)
	if err != nil {
		return err
	}
	key := writer.ImmutableRunArtifactKey(conversationID, turnID, stepIndex, runID, immutableRunRequestArtifact)
	if err := writer.PutImmutableBytes(ctx, key, compressed, immutableRunArtifactContentType); err != nil {
		return fmt.Errorf("persist immutable run request: %w", err)
	}
	if indexer, ok := r.runs.(TurnRunArtifactIndexer); ok {
		if err := indexer.SetTurnRunArtifactMetadata(ctx, runID, []RunArtifactMetadata{{
			Name: immutableRunRequestArtifact, ObjectKey: key, ContentType: immutableRunArtifactContentType,
			UncompressedSize: int64(len(payload)), CompressedSize: int64(len(compressed)), SHA256: checksum,
			SchemaVersion: immutableRunArtifactSchemaVersion,
		}}); err != nil {
			return err
		}
	}
	return nil
}

func (r *TurnRunner) persistImmutableRunSuccess(ctx context.Context, conversationID string, turnID string, run *domain.TurnRun, state *ScheduledRunState, outcome *ScheduledRunOutcome) (string, string, error) {
	writer, ok := r.blobs.(ImmutableRunArtifactStore)
	if !ok || writer == nil || run == nil || outcome == nil || outcome.Model == nil {
		return "", "", nil
	}
	write := func(artifact string, payload []byte) (RunArtifactMetadata, error) {
		if len(payload) == 0 {
			return RunArtifactMetadata{}, nil
		}
		compressed, checksum, err := compressImmutableRunPayload(payload)
		if err != nil {
			return RunArtifactMetadata{}, err
		}
		key := writer.ImmutableRunArtifactKey(conversationID, turnID, run.StepIndex, run.ID, artifact)
		if err := writer.PutImmutableBytes(ctx, key, compressed, immutableRunArtifactContentType); err != nil {
			return RunArtifactMetadata{}, err
		}
		return RunArtifactMetadata{
			Name: artifact, ObjectKey: key, ContentType: immutableRunArtifactContentType,
			UncompressedSize: int64(len(payload)), CompressedSize: int64(len(compressed)), SHA256: checksum,
			SchemaVersion: immutableRunArtifactSchemaVersion,
		}, nil
	}

	artifacts := make([]RunArtifactMetadata, 0, 5)
	response, err := write(immutableRunResponseArtifact, outcome.Model.RawResponse)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run response: %w", err)
	}
	if response.ObjectKey != "" {
		artifacts = append(artifacts, response)
	}
	outputItems, err := json.Marshal(outcome.Model.OutputItems)
	if err != nil {
		return "", "", fmt.Errorf("marshal immutable run output items: %w", err)
	}
	output, err := write(immutableRunOutputItemsArtifact, outputItems)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run output items: %w", err)
	}
	if output.ObjectKey != "" {
		artifacts = append(artifacts, output)
	}
	var toolResultsPayload []byte
	if len(outcome.ToolResults) > 0 {
		toolResultsPayload, err = json.Marshal(outcome.ToolResults)
		if err != nil {
			return "", "", fmt.Errorf("marshal immutable run tool results: %w", err)
		}
	}
	toolResults, err := write(immutableRunToolResultsArtifact, toolResultsPayload)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run tool results: %w", err)
	}
	if toolResults.ObjectKey != "" {
		artifacts = append(artifacts, toolResults)
	}
	presentationEvents := []domain.ConversationEvent{}
	if r.completeEvents != nil {
		presentationEvents, err = r.completeEvents.ListConversationEventsByRun(ctx, run.ID)
		if err != nil {
			return "", "", fmt.Errorf("list immutable run presentation events: %w", err)
		}
	}
	presentationPayload, err := json.Marshal(map[string]any{
		"schema_version": immutableRunArtifactSchemaVersion,
		"run_id":         run.ID,
		"events":         presentationEvents,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal immutable run presentation events: %w", err)
	}
	presentation, err := write(immutableRunPresentationArtifact, presentationPayload)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run presentation events: %w", err)
	}
	if presentation.ObjectKey != "" {
		artifacts = append(artifacts, presentation)
	}
	modelItems, err := completeRunCheckpointItems(state, outcome)
	if err != nil {
		return "", "", err
	}
	checkpointPayload, err := json.Marshal(map[string]any{
		"schema_version":        immutableRunArtifactSchemaVersion,
		"conversation_id":       conversationID,
		"turn_id":               turnID,
		"run_id":                run.ID,
		"step_index":            run.StepIndex,
		"model_items":           modelItems,
		"context_window_tokens": outcome.ContextWindowTokens,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal immutable run checkpoint: %w", err)
	}
	checkpoint, err := write(immutableRunCheckpointArtifact, checkpointPayload)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run checkpoint: %w", err)
	}
	if checkpoint.ObjectKey != "" {
		artifacts = append(artifacts, checkpoint)
	}
	if indexer, ok := r.runs.(TurnRunArtifactIndexer); ok {
		if err := indexer.SetTurnRunArtifactMetadata(ctx, run.ID, artifacts); err != nil {
			return "", "", err
		}
	}
	return response.ObjectKey, checkpoint.ObjectKey, nil
}

func (r *TurnRunner) persistImmutableRunFailure(ctx context.Context, conversationID string, turnID string, run *domain.TurnRun, cause error) error {
	writer, ok := r.blobs.(ImmutableRunArtifactStore)
	if !ok || writer == nil || run == nil || cause == nil {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version":  immutableRunArtifactSchemaVersion,
		"conversation_id": conversationID,
		"turn_id":         turnID,
		"run_id":          run.ID,
		"step_index":      run.StepIndex,
		"error":           cause.Error(),
	})
	if err != nil {
		return fmt.Errorf("marshal immutable run failure: %w", err)
	}
	compressed, checksum, err := compressImmutableRunPayload(payload)
	if err != nil {
		return err
	}
	key := writer.ImmutableRunArtifactKey(conversationID, turnID, run.StepIndex, run.ID, immutableRunFailureArtifact)
	if err := writer.PutImmutableBytes(ctx, key, compressed, immutableRunArtifactContentType); err != nil {
		return fmt.Errorf("persist immutable run failure: %w", err)
	}
	if indexer, ok := r.runs.(TurnRunArtifactIndexer); ok {
		if err := indexer.SetTurnRunArtifactMetadata(ctx, run.ID, []RunArtifactMetadata{{
			Name: immutableRunFailureArtifact, ObjectKey: key, ContentType: immutableRunArtifactContentType,
			UncompressedSize: int64(len(payload)), CompressedSize: int64(len(compressed)), SHA256: checksum,
			SchemaVersion: immutableRunArtifactSchemaVersion,
		}}); err != nil {
			return err
		}
	}
	return nil
}

func completeRunCheckpointItems(state *ScheduledRunState, outcome *ScheduledRunOutcome) ([]llm.ModelItem, error) {
	if outcome == nil {
		return nil, fmt.Errorf("build run checkpoint: outcome is required")
	}
	if outcome.NextState != nil {
		return cloneModelItems(outcome.NextState.Request.Input), nil
	}
	if state == nil {
		return nil, fmt.Errorf("build run checkpoint: scheduled run state is required")
	}
	items := cloneModelItems(state.Request.Input)
	items = append(items, cloneModelItems(outcome.ContextItems)...)
	return items, nil
}
