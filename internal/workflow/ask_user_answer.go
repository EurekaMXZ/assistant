package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

type AskUserAnswerInput struct {
	OwnerUserID    string
	TurnID         string
	ToolCallID     string
	OptionID       string
	IdempotencyKey string
}

type AskUserAnswerService struct {
	Calls     ToolCallStore
	Turns     TurnWorkflowRepository
	Artifacts ToolArtifactStore
	Publisher stream.Publisher
}

func (s AskUserAnswerService) Answer(ctx context.Context, input AskUserAnswerInput) (*tool.AskUserInteraction, error) {
	input.OwnerUserID = strings.TrimSpace(input.OwnerUserID)
	input.TurnID = strings.TrimSpace(input.TurnID)
	input.ToolCallID = strings.TrimSpace(input.ToolCallID)
	input.OptionID = strings.TrimSpace(input.OptionID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.OwnerUserID == "" || input.TurnID == "" || input.ToolCallID == "" || input.OptionID == "" {
		return nil, domain.NewValidationError("option_id is required")
	}
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return nil, domain.NewValidationError("Idempotency-Key is required and must be at most 128 bytes")
	}
	if s.Calls == nil || s.Turns == nil || s.Artifacts == nil {
		return nil, errors.New("ask_user answer service is not configured")
	}

	record, err := s.Calls.GetToolCallForAnswer(ctx, input.OwnerUserID, input.TurnID, input.ToolCallID)
	if err != nil {
		return nil, err
	}
	if record.ToolName != tool.AskUser || record.Namespace != "" {
		return nil, domain.ErrConflict
	}
	if record.Status != domain.ToolCallStatusAwaitingInput && record.Status != domain.ToolCallStatusCompleted {
		return nil, domain.ErrConflict
	}
	arguments, err := s.Artifacts.GetBytes(ctx, record.ArgumentsBlobKey)
	if err != nil {
		return nil, fmt.Errorf("load ask_user arguments: %w", err)
	}
	prompt, err := tool.DecodeAskUserPrompt(arguments)
	if err != nil {
		return nil, err
	}
	var selected *tool.AskUserOption
	for index := range prompt.Options {
		if prompt.Options[index].ID == input.OptionID {
			selected = &prompt.Options[index]
			break
		}
	}
	if selected == nil {
		return nil, domain.NewValidationError("option_id is not valid for this interaction")
	}
	answerStatus := "answered"
	if selected.ID == "cancel" {
		answerStatus = "cancelled"
	}
	answer := tool.AskUserAnswer{Status: answerStatus, OptionID: selected.ID, Label: selected.Label, UserReported: true}
	output, err := json.Marshal(answer)
	if err != nil {
		return nil, fmt.Errorf("marshal ask_user answer: %w", err)
	}
	turn, err := s.Turns.GetTurn(ctx, input.TurnID)
	if err != nil {
		return nil, err
	}
	outputKey := s.Artifacts.ToolCallOutputKey(turn.ConversationID, input.TurnID, record.CallID)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(output))
	claim, err := s.Calls.ClaimAwaitingInputAnswer(
		ctx, input.OwnerUserID, input.TurnID, input.ToolCallID, input.IdempotencyKey,
		fingerprint, selected.ID, outputKey,
	)
	if err != nil {
		return nil, err
	}
	interaction := &tool.AskUserInteraction{
		ID: "ask-user:" + claim.ToolCall.ID, ToolCallID: claim.ToolCall.ID,
		Prompt: prompt.Prompt, Kind: prompt.Kind, Options: append([]tool.AskUserOption(nil), prompt.Options...),
		Action: prompt.Action, Answer: &answer, Status: "completed",
	}
	interactionPayload, err := json.Marshal(interaction)
	if err != nil {
		return nil, fmt.Errorf("marshal completed ask_user interaction: %w", err)
	}
	answered := claim.ToolCall
	replayed := claim.Finalized
	if !claim.Finalized {
		if err := s.Artifacts.PutImmutableBytes(ctx, outputKey, output, "application/json"); err != nil {
			return nil, fmt.Errorf("persist declared ask_user answer: %w", err)
		}
		answered, replayed, err = s.Calls.FinalizeAwaitingInputAnswer(
			ctx, input.OwnerUserID, input.TurnID, input.ToolCallID, input.IdempotencyKey,
			fingerprint, selected.ID, outputKey, interactionPayload,
		)
		if err != nil {
			return nil, err
		}
	}
	if !replayed && s.Publisher != nil {
		_ = s.Publisher.Publish(ctx, stream.Event{
			Type: stream.EventInteractionDone, ConversationID: turn.ConversationID, TurnID: turn.ID,
			RunID: answered.TurnRunID, ItemID: interaction.ID, Payload: string(interactionPayload),
		})
	}
	return interaction, nil
}
