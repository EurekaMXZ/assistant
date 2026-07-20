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
		if err := indexer.SetTurnRunArtifactMetadata(ctx, runID, key, "", "", "", "", "", checksum, "", int64(len(payload)), 0, immutableRunArtifactSchemaVersion, 0); err != nil {
			return err
		}
	}
	return nil
}

func (r *TurnRunner) persistImmutableRunSuccess(ctx context.Context, conversationID string, turnID string, run *domain.TurnRun, outcome *ScheduledRunOutcome) (string, string, error) {
	writer, ok := r.blobs.(ImmutableRunArtifactStore)
	if !ok || writer == nil || run == nil || outcome == nil || outcome.Model == nil {
		return "", "", nil
	}
	write := func(artifact string, payload []byte) (string, string, error) {
		if len(payload) == 0 {
			return "", "", nil
		}
		compressed, checksum, err := compressImmutableRunPayload(payload)
		if err != nil {
			return "", "", err
		}
		key := writer.ImmutableRunArtifactKey(conversationID, turnID, run.StepIndex, run.ID, artifact)
		if err := writer.PutImmutableBytes(ctx, key, compressed, immutableRunArtifactContentType); err != nil {
			return "", "", err
		}
		return key, checksum, nil
	}

	responseKey, responseChecksum, err := write(immutableRunResponseArtifact, outcome.Model.RawResponse)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run response: %w", err)
	}
	outputItems, err := json.Marshal(outcome.Model.OutputItems)
	if err != nil {
		return "", "", fmt.Errorf("marshal immutable run output items: %w", err)
	}
	outputKey, _, err := write(immutableRunOutputItemsArtifact, outputItems)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run output items: %w", err)
	}
	toolResults := make([]llm.ModelItem, 0)
	for _, item := range outcome.ContextItems {
		if item.Type == "function_call_output" {
			toolResults = append(toolResults, item)
		}
	}
	var toolResultsPayload []byte
	if len(toolResults) > 0 {
		toolResultsPayload, err = json.Marshal(toolResults)
		if err != nil {
			return "", "", fmt.Errorf("marshal immutable run tool results: %w", err)
		}
	}
	toolResultsKey, _, err := write(immutableRunToolResultsArtifact, toolResultsPayload)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run tool results: %w", err)
	}
	presentationPayload, err := json.Marshal(map[string]any{
		"schema_version": immutableRunArtifactSchemaVersion,
		"run_id":         run.ID,
		"output_items":   outcome.Model.OutputItems,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal immutable run presentation events: %w", err)
	}
	presentationKey, _, err := write(immutableRunPresentationArtifact, presentationPayload)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run presentation events: %w", err)
	}
	checkpointPayload, err := json.Marshal(map[string]any{
		"schema_version":        immutableRunArtifactSchemaVersion,
		"conversation_id":       conversationID,
		"turn_id":               turnID,
		"run_id":                run.ID,
		"step_index":            run.StepIndex,
		"model_items":           outcome.ContextItems,
		"context_window_tokens": outcome.ContextWindowTokens,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal immutable run checkpoint: %w", err)
	}
	checkpointKey, _, err := write(immutableRunCheckpointArtifact, checkpointPayload)
	if err != nil {
		return "", "", fmt.Errorf("persist immutable run checkpoint: %w", err)
	}
	if indexer, ok := r.runs.(TurnRunArtifactIndexer); ok {
		if err := indexer.SetTurnRunArtifactMetadata(ctx, run.ID, "", responseKey, outputKey, toolResultsKey, presentationKey, checkpointKey, "", responseChecksum, 0, int64(len(outcome.Model.RawResponse)), 0, immutableRunArtifactSchemaVersion); err != nil {
			return "", "", err
		}
	}
	return responseKey, checkpointKey, nil
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
	compressed, _, err := compressImmutableRunPayload(payload)
	if err != nil {
		return err
	}
	key := writer.ImmutableRunArtifactKey(conversationID, turnID, run.StepIndex, run.ID, immutableRunFailureArtifact)
	if err := writer.PutImmutableBytes(ctx, key, compressed, immutableRunArtifactContentType); err != nil {
		return fmt.Errorf("persist immutable run failure: %w", err)
	}
	return nil
}
