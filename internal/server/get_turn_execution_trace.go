package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/tool"
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
	TurnRunOutputItemsKey(conversationID, turnID string, stepIndex int) string
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
		StreamBlobKey:    turn.StreamBlobKey,
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
	if uc.Artifacts != nil {
		streamEvents, err := loadTraceStreamEvents(ctx, uc.Artifacts, turn.StreamBlobKey)
		if err != nil {
			return nil, err
		}
		trace.StreamEvents = streamEvents
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
			item.OutputItemsBlobKey = uc.Artifacts.TurnRunOutputItemsKey(turn.ConversationID, turn.ID, run.StepIndex)
			data, readErr := uc.Artifacts.GetBytes(ctx, item.OutputItemsBlobKey)
			switch {
			case readErr == nil:
				data = bytes.TrimSpace(data)
				if len(data) == 0 {
					break
				}
				if !json.Valid(data) {
					return nil, fmt.Errorf("turn run output items %q is not valid json", item.OutputItemsBlobKey)
				}
				var outputItems []json.RawMessage
				if err := json.Unmarshal(data, &outputItems); err != nil || outputItems == nil {
					return nil, fmt.Errorf("turn run output items %q must be a json array", item.OutputItemsBlobKey)
				}
				redacted, err := redactEncryptedContentJSON(data)
				if err != nil {
					return nil, fmt.Errorf("redact turn run output items %q: %w", item.OutputItemsBlobKey, err)
				}
				item.OutputItems = redacted
			case errors.Is(readErr, domain.ErrNotFound):
				// Older runs may not have a dedicated output-items artifact yet.
			default:
				return nil, fmt.Errorf("get turn run output items %q: %w", item.OutputItemsBlobKey, readErr)
			}
		}
		trace.Runs = append(trace.Runs, item)
	}

	return trace, nil
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

func loadTraceStreamEvents(ctx context.Context, artifacts turnRunArtifactReader, key string) ([]StreamEventTrace, error) {
	if artifacts == nil || strings.TrimSpace(key) == "" {
		return nil, nil
	}

	data, err := artifacts.GetBytes(ctx, key)
	switch {
	case err == nil:
		lines := bytes.Split(data, []byte{'\n'})
		events := make([]StreamEventTrace, 0, len(lines))
		for index, line := range lines {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			var raw struct {
				Type           string `json:"type"`
				ConversationID string `json:"conversation_id,omitempty"`
				TurnID         string `json:"turn_id,omitempty"`
				ResponseID     string `json:"response_id,omitempty"`
				ToolName       string `json:"tool_name,omitempty"`
				Payload        string `json:"payload,omitempty"`
				Delta          string `json:"delta,omitempty"`
				Text           string `json:"text,omitempty"`
				Error          string `json:"error,omitempty"`
			}
			if err := json.Unmarshal(line, &raw); err != nil {
				return nil, fmt.Errorf("stream event archive %q line %d is not valid json", key, index+1)
			}

			event := StreamEventTrace{
				Type:           raw.Type,
				ConversationID: raw.ConversationID,
				TurnID:         raw.TurnID,
				ResponseID:     raw.ResponseID,
				ToolName:       raw.ToolName,
				Delta:          raw.Delta,
				Text:           raw.Text,
				Error:          raw.Error,
			}

			payload := strings.TrimSpace(raw.Payload)
			if payload != "" {
				if json.Valid([]byte(payload)) {
					redacted, err := redactEncryptedContentJSON([]byte(payload))
					if err != nil {
						return nil, fmt.Errorf("redact stream event archive %q line %d payload: %w", key, index+1, err)
					}
					event.PayloadJSON = redacted
				} else {
					event.PayloadText = raw.Payload
				}
			}
			events = append(events, event)
		}
		return events, nil
	case errors.Is(err, domain.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("get stream event archive %q: %w", key, err)
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
