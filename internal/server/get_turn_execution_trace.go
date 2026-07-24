package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
	"github.com/klauspost/compress/zstd"
)

type executionTraceTurnGetter interface {
	GetTurn(ctx context.Context, turnID string) (*domain.Turn, error)
}

type turnRunLister interface {
	ListTurnRunsByTurn(ctx context.Context, turnID string) ([]domain.TurnRun, error)
}

type toolCallLister interface {
	ListToolCallsByTurn(ctx context.Context, turnID string) ([]domain.ToolCallRecord, error)
}

type turnRunArtifactReader interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

type GetTurnExecutionTrace struct {
	Turns     executionTraceTurnGetter
	Runs      turnRunLister
	ToolCalls toolCallLister
	Artifacts turnRunArtifactReader
}

func (uc GetTurnExecutionTrace) Execute(ctx context.Context, turnID string) (*TurnExecutionTrace, error) {
	if uc.Turns == nil {
		return nil, errors.New("get turn execution trace use case requires turn getter")
	}
	if uc.Runs == nil {
		return nil, errors.New("get turn execution trace use case requires turn run lister")
	}
	if uc.ToolCalls == nil {
		return nil, errors.New("get turn execution trace use case requires tool call lister")
	}

	turn, err := uc.Turns.GetTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}

	runs, err := uc.Runs.ListTurnRunsByTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}
	calls, err := uc.ToolCalls.ListToolCallsByTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}

	callsByRun := make(map[string][]ToolCallTrace, len(runs))
	for _, call := range calls {
		item := ToolCallTrace{
			ID:           call.ID,
			TurnID:       call.TurnID,
			TurnRunID:    call.TurnRunID,
			CallID:       call.CallID,
			ToolType:     call.ToolType,
			Namespace:    call.Namespace,
			ToolName:     call.ToolName,
			Status:       call.Status,
			ErrorMessage: call.ErrorMessage,
			StartedAt:    call.StartedAt,
			CompletedAt:  call.CompletedAt,
			FailedAt:     call.FailedAt,
			CreatedAt:    call.CreatedAt,
			UpdatedAt:    call.UpdatedAt,
		}
		if uc.Artifacts != nil {
			arguments, err := loadTraceJSONArtifact(ctx, uc.Artifacts, call.ArgumentsBlobKey, "tool call arguments")
			if err != nil {
				return nil, err
			}

			output, err := loadTraceArtifactBytes(ctx, uc.Artifacts, call.OutputBlobKey, "tool call output")
			if err != nil {
				return nil, err
			}

			presentation := tool.BuildPublicToolPresentation(call.Namespace, "", call.ToolName, call.Status, arguments, output, traceToolErrorMessage(call.Status, call.ErrorMessage, output))
			item.Summary = presentation.Summary
			item.Details = presentation.Details
		} else {
			presentation := tool.BuildPublicToolPresentation(call.Namespace, "", call.ToolName, call.Status, nil, nil, call.ErrorMessage)
			item.Summary = presentation.Summary
			item.Details = presentation.Details
		}
		callsByRun[call.TurnRunID] = append(callsByRun[call.TurnRunID], item)
	}

	trace := &TurnExecutionTrace{
		TurnID:           turn.ID,
		ConversationID:   turn.ConversationID,
		Status:           turn.Status,
		RequestBlobKey:   turn.RequestBlobKey,
		ResponseBlobKey:  turn.ResponseBlobKey,
		OpenAIResponseID: turn.OpenAIResponseID,
		ErrorCode:        turn.ErrorCode,
		ErrorMessage:     turn.ErrorMessage,
		StartedAt:        turn.StartedAt,
		CompletedAt:      turn.CompletedAt,
		FailedAt:         turn.FailedAt,
		CreatedAt:        turn.CreatedAt,
		UpdatedAt:        turn.UpdatedAt,
		Runs:             make([]TurnRunTrace, 0, len(runs)),
	}
	for _, run := range runs {
		toolCalls := callsByRun[run.ID]
		item := TurnRunTrace{
			ID:                       run.ID,
			TurnID:                   run.TurnID,
			StepIndex:                run.StepIndex,
			Provider:                 run.Provider,
			Model:                    run.Model,
			Status:                   run.Status,
			RequestBlobKey:           run.RequestBlobKey,
			ResponseBlobKey:          run.ResponseBlobKey,
			ResponseID:               run.ResponseID,
			InputTokens:              run.InputTokens,
			CacheReadInputTokens:     run.CacheReadInputTokens,
			CacheCreationInputTokens: run.CacheCreationInputTokens,
			OutputTokens:             run.OutputTokens,
			ReasoningOutputTokens:    run.ReasoningOutputTokens,
			TotalTokens:              run.TotalTokens,
			BillingCurrency:          run.BillingCurrency,
			BillingAmountNanos:       run.BillingAmountNanos,
			ErrorMessage:             run.ErrorMessage,
			StartedAt:                run.StartedAt,
			CompletedAt:              run.CompletedAt,
			FailedAt:                 run.FailedAt,
			CreatedAt:                run.CreatedAt,
			UpdatedAt:                run.UpdatedAt,
			ToolCalls:                nonNilSlice(toolCalls),
		}
		if uc.Artifacts != nil {
			outputItemsKey := runArtifactObjectKey(run.ArtifactMetadata, "output-items.json.zst")
			if outputItemsKey != "" {
				data, readErr := uc.Artifacts.GetBytes(ctx, outputItemsKey)
				switch {
				case readErr == nil:
					data, readErr = decodeTraceRunArtifact(data, run.ArtifactMetadata, "output-items.json.zst")
					if readErr != nil {
						return nil, fmt.Errorf("decode turn run output items %q: %w", outputItemsKey, readErr)
					}
					data = bytes.TrimSpace(data)
					if len(data) == 0 {
						break
					}
					if !json.Valid(data) {
						return nil, fmt.Errorf("turn run output items %q is not valid json", outputItemsKey)
					}
					var outputItems []json.RawMessage
					if err := json.Unmarshal(data, &outputItems); err != nil || outputItems == nil {
						return nil, fmt.Errorf("turn run output items %q must be a json array", outputItemsKey)
					}
					redacted, err := redactEncryptedContentJSON(data)
					if err != nil {
						return nil, fmt.Errorf("redact turn run output items %q: %w", outputItemsKey, err)
					}
					item.OutputItems = redacted
				case errors.Is(readErr, domain.ErrNotFound):
				default:
					return nil, fmt.Errorf("get turn run output items %q: %w", outputItemsKey, readErr)
				}
			}
		}
		trace.Runs = append(trace.Runs, item)
	}

	return trace, nil
}

func runArtifactObjectKey(rawMetadata json.RawMessage, name string) string {
	var metadata map[string]struct {
		ObjectKey string `json:"object_key"`
	}
	if len(rawMetadata) == 0 || json.Unmarshal(rawMetadata, &metadata) != nil {
		return ""
	}
	return strings.TrimSpace(metadata[name].ObjectKey)
}

func decodeTraceRunArtifact(compressed []byte, rawMetadata json.RawMessage, name string) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	payload, err := decoder.DecodeAll(compressed, nil)
	decoder.Close()
	if err != nil {
		return nil, err
	}
	var metadata map[string]struct {
		SHA256 string `json:"sha256"`
	}
	if len(rawMetadata) > 0 && json.Unmarshal(rawMetadata, &metadata) == nil {
		if expected := strings.TrimSpace(metadata[name].SHA256); expected != "" {
			digest := sha256.Sum256(payload)
			if !strings.EqualFold(expected, hex.EncodeToString(digest[:])) {
				return nil, errors.New("checksum mismatch")
			}
		}
	}
	return payload, nil
}

func traceToolErrorMessage(status string, errorMessage string, output []byte) string {
	errorMessage = strings.TrimSpace(errorMessage)
	if errorMessage != "" {
		return errorMessage
	}
	if status != domain.ToolCallStatusFailed {
		return ""
	}

	output = bytes.TrimSpace(output)
	if len(output) == 0 || json.Valid(output) {
		return ""
	}
	return string(output)
}

func loadTraceJSONArtifact(ctx context.Context, artifacts turnRunArtifactReader, key string, label string) (json.RawMessage, error) {
	if artifacts == nil || strings.TrimSpace(key) == "" {
		return nil, nil
	}

	data, err := artifacts.GetBytes(ctx, key)
	switch {
	case err == nil:
		data = bytes.TrimSpace(data)
		if len(data) == 0 {
			return nil, nil
		}
		if !json.Valid(data) {
			return nil, fmt.Errorf("%s %q is not valid json", label, key)
		}
		return redactEncryptedContentJSON(data)
	case errors.Is(err, domain.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("get %s %q: %w", label, key, err)
	}
}

func loadTraceArtifactBytes(ctx context.Context, artifacts turnRunArtifactReader, key string, label string) ([]byte, error) {
	if artifacts == nil || strings.TrimSpace(key) == "" {
		return nil, nil
	}

	data, err := artifacts.GetBytes(ctx, key)
	switch {
	case err == nil:
		data = bytes.TrimSpace(data)
		if len(data) == 0 {
			return nil, nil
		}
		return data, nil
	case errors.Is(err, domain.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("get %s %q: %w", label, key, err)
	}
}

func redactEncryptedContentJSON(data []byte) (json.RawMessage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}

	redacted, err := json.Marshal(redactEncryptedContent(value))
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), redacted...), nil
}

func redactEncryptedContent(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "encrypted_content" {
				continue
			}
			redacted[key] = redactEncryptedContent(item)
		}
		return redacted
	case []any:
		redacted := make([]any, 0, len(typed))
		for _, item := range typed {
			redacted = append(redacted, redactEncryptedContent(item))
		}
		return redacted
	default:
		return value
	}
}
