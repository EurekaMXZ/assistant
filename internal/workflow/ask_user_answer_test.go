package workflow

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

func TestAskUserAnswerValidatesAndIsIdempotent(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	argumentsKey := artifacts.ToolCallArgumentsKey("conv-1", "turn-1", "call-ask")
	arguments := []byte(`{"prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}]}`)
	if err := artifacts.PutBytes(t.Context(), argumentsKey, arguments, "application/json"); err != nil {
		t.Fatal(err)
	}
	record := &domain.ToolCallRecord{
		ID: "tool-1", TurnID: "turn-1", TurnRunID: "run-1", CallID: "call-ask",
		ToolName: tool.AskUser, Status: domain.ToolCallStatusAwaitingInput, ArgumentsBlobKey: argumentsKey,
	}
	calls := &stubToolCallStore{recordsByID: map[string]*domain.ToolCallRecord{record.ID: record}}
	publisher := &recordingPublisher{}
	service := AskUserAnswerService{
		Calls: calls, Turns: &stubTurnWorkflowStore{turn: &domain.Turn{ID: "turn-1", ConversationID: "conv-1"}},
		Artifacts: artifacts, Publisher: publisher,
	}

	base := AskUserAnswerInput{OwnerUserID: "user-1", TurnID: "turn-1", ToolCallID: record.ID, IdempotencyKey: "answer-1"}
	invalid := base
	invalid.OptionID = "missing"
	if _, err := service.Answer(t.Context(), invalid); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("invalid option error = %v", err)
	}
	missingKey := base
	missingKey.OptionID = "cancel"
	missingKey.IdempotencyKey = ""
	if _, err := service.Answer(t.Context(), missingKey); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("missing key error = %v", err)
	}

	base.OptionID = "cancel"
	interaction, err := service.Answer(t.Context(), base)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if interaction.ID != "ask-user:tool-1" || interaction.Status != "completed" || len(publisher.events) != 1 || publisher.events[0].Type != stream.EventInteractionDone {
		t.Fatalf("interaction=%#v events=%#v", interaction, publisher.events)
	}
	outputKey := artifacts.ToolCallOutputKey("conv-1", "turn-1", "call-ask")
	var output tool.AskUserAnswer
	if err := json.Unmarshal(artifacts.data[outputKey], &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "cancelled" || output.OptionID != "cancel" || !output.UserReported {
		t.Fatalf("answer output = %#v", output)
	}
	if _, err := service.Answer(t.Context(), base); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("idempotent replay published %d events", len(publisher.events))
	}
	differentKey := base
	differentKey.IdempotencyKey = "answer-2"
	if _, err := service.Answer(t.Context(), differentKey); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("different key error = %v", err)
	}
	different := base
	different.OptionID = "yes"
	if _, err := service.Answer(t.Context(), different); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("different answer error = %v", err)
	}
}

func TestAskUserAnswerRecoversAfterObjectWriteBeforeFinalize(t *testing.T) {
	artifacts := &stubToolArtifactStore{}
	argumentsKey := artifacts.ToolCallArgumentsKey("conv-1", "turn-1", "call-ask")
	if err := artifacts.PutBytes(t.Context(), argumentsKey, []byte(`{"prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}]}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	record := &domain.ToolCallRecord{
		ID: "tool-1", TurnID: "turn-1", TurnRunID: "run-1", CallID: "call-ask",
		ToolName: tool.AskUser, Status: domain.ToolCallStatusAwaitingInput, ArgumentsBlobKey: argumentsKey,
	}
	calls := &stubToolCallStore{
		recordsByID: map[string]*domain.ToolCallRecord{record.ID: record},
		finalizeErr: errors.New("database unavailable after object write"),
	}
	service := AskUserAnswerService{
		Calls: calls, Turns: &stubTurnWorkflowStore{turn: &domain.Turn{ID: "turn-1", ConversationID: "conv-1"}},
		Artifacts: artifacts, Publisher: &recordingPublisher{},
	}
	input := AskUserAnswerInput{
		OwnerUserID: "user-1", TurnID: "turn-1", ToolCallID: record.ID,
		OptionID: "yes", IdempotencyKey: "answer-1",
	}
	if _, err := service.Answer(t.Context(), input); err == nil {
		t.Fatal("answer unexpectedly finalized")
	}
	if !record.AnswerOutputPending || record.AnswerKey != input.IdempotencyKey {
		t.Fatalf("answer declaration was not retained: %#v", record)
	}
	outputKey := artifacts.ToolCallOutputKey("conv-1", "turn-1", "call-ask")
	if len(artifacts.data[outputKey]) == 0 {
		t.Fatal("declared answer output was not persisted")
	}
	conflicting := input
	conflicting.OptionID = "cancel"
	if _, err := service.Answer(t.Context(), conflicting); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting recovery error = %v", err)
	}
	calls.finalizeErr = nil
	interaction, err := service.Answer(t.Context(), input)
	if err != nil {
		t.Fatalf("recover answer finalize: %v", err)
	}
	if interaction.Answer == nil || interaction.Answer.OptionID != "yes" || record.Status != domain.ToolCallStatusCompleted || record.AnswerOutputPending {
		t.Fatalf("recovered interaction=%#v record=%#v", interaction, record)
	}
}
