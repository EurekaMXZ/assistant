package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type stubTraceTurnGetter struct {
	turnID string
	turn   *domain.Turn
	err    error
}

func (s *stubTraceTurnGetter) GetTurn(_ context.Context, turnID string) (*domain.Turn, error) {
	s.turnID = turnID
	if s.err != nil {
		return nil, s.err
	}
	return s.turn, nil
}

type stubTurnRunLister struct {
	turnID string
	runs   []domain.TurnRun
	err    error
}

func (s *stubTurnRunLister) ListTurnRunsByTurn(_ context.Context, turnID string) ([]domain.TurnRun, error) {
	s.turnID = turnID
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.TurnRun(nil), s.runs...), nil
}

type stubToolCallLister struct {
	turnID string
	calls  []domain.ToolCallRecord
	err    error
}

func (s *stubToolCallLister) ListToolCallsByTurn(_ context.Context, turnID string) ([]domain.ToolCallRecord, error) {
	s.turnID = turnID
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.ToolCallRecord(nil), s.calls...), nil
}

type stubTurnRunArtifactReader struct {
	key  string
	data map[string][]byte
	err  error
}

func (s *stubTurnRunArtifactReader) GetBytes(_ context.Context, key string) ([]byte, error) {
	s.key = key
	if s.err != nil {
		return nil, s.err
	}
	if s.data == nil {
		return nil, domain.ErrNotFound
	}
	value, ok := s.data[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (s *stubTurnRunArtifactReader) TurnRunOutputItemsKey(conversationID, turnID string, stepIndex int) string {
	return fmt.Sprintf("run-output-items/%s/%s/step-%03d.json", conversationID, turnID, stepIndex)
}

func TestGetTurnExecutionTraceReturnsRunsCallsAndOutputItems(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	completedAt := now.Add(2 * time.Second)
	turns := &stubTraceTurnGetter{
		turn: &domain.Turn{
			ID:               "turn-1",
			ConversationID:   "conv-1",
			Status:           domain.TurnStatusCompleted,
			RequestBlobKey:   "requests/conv-1/turn-1.json",
			ResponseBlobKey:  "responses/conv-1/turn-1.json",
			StreamBlobKey:    "stream-events/conv-1/turn-1.jsonl",
			OpenAIResponseID: "resp_1",
			StartedAt:        &now,
			CompletedAt:      &completedAt,
			CreatedAt:        now,
			UpdatedAt:        completedAt,
		},
	}
	runs := &stubTurnRunLister{
		runs: []domain.TurnRun{
			{
				ID:              "run-1",
				TurnID:          "turn-1",
				StepIndex:       1,
				Provider:        "openai.responses",
				Status:          domain.TurnRunStatusCompleted,
				RequestBlobKey:  "run-request:1",
				ResponseBlobKey: "run-response:1",
				ResponseID:      "resp_1",
				StartedAt:       now,
				CompletedAt:     &completedAt,
				CreatedAt:       now,
				UpdatedAt:       completedAt,
			},
		},
	}
	calls := &stubToolCallLister{
		calls: []domain.ToolCallRecord{
			{
				ID:               "record-1",
				TurnID:           "turn-1",
				TurnRunID:        "run-1",
				CallID:           "mcp_1",
				ToolType:         "mcp",
				Namespace:        "internet",
				ToolName:         "search",
				Status:           domain.ToolCallStatusCompleted,
				ArgumentsBlobKey: "tool-args:1",
				OutputBlobKey:    "tool-output:1",
				StartedAt:        now,
				CompletedAt:      &completedAt,
				CreatedAt:        now,
				UpdatedAt:        completedAt,
			},
		},
	}
	artifacts := &stubTurnRunArtifactReader{
		data: map[string][]byte{
			"run-output-items/conv-1/turn-1/step-001.json": []byte(`[{"type":"reasoning","encrypted_content":"ciphertext"},{"type":"mcp_call","name":"search"}]`),
			"tool-args:1":                       []byte(`{"query":"latest openai news"}`),
			"tool-output:1":                     []byte(`{"results":[{"title":"OpenAI"}]}`),
			"stream-events/conv-1/turn-1.jsonl": []byte("{\"type\":\"tool.started\",\"conversation_id\":\"conv-1\",\"turn_id\":\"turn-1\",\"tool_name\":\"internet.search\",\"payload\":\"{\\\"status\\\":\\\"started\\\"}\"}\n{\"type\":\"response.completed\",\"conversation_id\":\"conv-1\",\"turn_id\":\"turn-1\",\"text\":\"done\"}\n"),
		},
	}
	uc := GetTurnExecutionTrace{
		Turns:     turns,
		Runs:      runs,
		ToolCalls: calls,
		Artifacts: artifacts,
	}

	trace, err := uc.Execute(context.Background(), "turn-1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if turns.turnID != "turn-1" || runs.turnID != "turn-1" || calls.turnID != "turn-1" {
		t.Fatalf("unexpected lookup ids: turn=%q runs=%q calls=%q", turns.turnID, runs.turnID, calls.turnID)
	}
	if trace.TurnID != "turn-1" || len(trace.Runs) != 1 {
		t.Fatalf("unexpected trace: %#v", trace)
	}
	if trace.RequestBlobKey != "requests/conv-1/turn-1.json" || trace.ResponseBlobKey != "responses/conv-1/turn-1.json" {
		t.Fatalf("unexpected turn blob keys: %#v", trace)
	}
	if trace.Runs[0].OutputItemsBlobKey != "run-output-items/conv-1/turn-1/step-001.json" {
		t.Fatalf("unexpected output items blob key: %#v", trace.Runs[0])
	}
	if !json.Valid(trace.Runs[0].OutputItems) {
		t.Fatalf("expected valid output items: %s", trace.Runs[0].OutputItems)
	}
	if strings.Contains(string(trace.Runs[0].OutputItems), "encrypted_content") || strings.Contains(string(trace.Runs[0].OutputItems), "ciphertext") {
		t.Fatalf("expected encrypted reasoning content to be redacted, got %s", trace.Runs[0].OutputItems)
	}
	if len(trace.Runs[0].ToolCalls) != 1 || trace.Runs[0].ToolCalls[0].CallID != "mcp_1" {
		t.Fatalf("unexpected tool call trace: %#v", trace.Runs[0].ToolCalls)
	}
	if trace.Runs[0].ToolCalls[0].Summary != "Searched the web" {
		t.Fatalf("unexpected tool call summary: %#v", trace.Runs[0].ToolCalls[0])
	}
	if len(trace.Runs[0].ToolCalls[0].Details) != 2 || trace.Runs[0].ToolCalls[0].Details[0] != "Query: latest openai news" || trace.Runs[0].ToolCalls[0].Details[1] != "Results: 1" {
		t.Fatalf("unexpected tool call details: %#v", trace.Runs[0].ToolCalls[0].Details)
	}
	if len(trace.StreamEvents) != 2 {
		t.Fatalf("expected 2 stream events, got %#v", trace.StreamEvents)
	}
	if trace.StreamEvents[0].Type != "tool.started" {
		t.Fatalf("unexpected first stream event: %#v", trace.StreamEvents[0])
	}
	if string(trace.StreamEvents[0].PayloadJSON) != `{"status":"started"}` {
		t.Fatalf("unexpected first stream event payload json: %s", trace.StreamEvents[0].PayloadJSON)
	}
	if trace.StreamEvents[1].Type != "response.completed" || trace.StreamEvents[1].Text != "done" {
		t.Fatalf("unexpected second stream event: %#v", trace.StreamEvents[1])
	}
}

func TestGetTurnExecutionTraceIgnoresMissingOutputItemsArtifact(t *testing.T) {
	uc := GetTurnExecutionTrace{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-2",
				ConversationID: "conv-2",
				Status:         domain.TurnStatusProcessing,
			},
		},
		Runs: &stubTurnRunLister{
			runs: []domain.TurnRun{{ID: "run-2", TurnID: "turn-2", StepIndex: 1}},
		},
		ToolCalls: &stubToolCallLister{},
		Artifacts: &stubTurnRunArtifactReader{},
	}

	trace, err := uc.Execute(context.Background(), "turn-2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(trace.Runs) != 1 {
		t.Fatalf("unexpected trace: %#v", trace)
	}
	if len(trace.Runs[0].OutputItems) != 0 {
		t.Fatalf("expected empty output items when artifact missing, got %s", trace.Runs[0].OutputItems)
	}
	if trace.Runs[0].ToolCalls == nil {
		t.Fatal("expected empty tool_calls array, got nil")
	}
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	if !strings.Contains(string(encoded), `"tool_calls":[]`) {
		t.Fatalf("empty tool calls encoded as null: %s", encoded)
	}
}

func TestGetTurnExecutionTraceRejectsNullOutputItems(t *testing.T) {
	uc := GetTurnExecutionTrace{
		Turns:     &stubTraceTurnGetter{turn: &domain.Turn{ID: "turn-null", ConversationID: "conv-null"}},
		Runs:      &stubTurnRunLister{runs: []domain.TurnRun{{ID: "run-null", TurnID: "turn-null", StepIndex: 1}}},
		ToolCalls: &stubToolCallLister{},
		Artifacts: &stubTurnRunArtifactReader{data: map[string][]byte{
			"run-output-items/conv-null/turn-null/step-001.json": []byte(`null`),
		}},
	}
	if _, err := uc.Execute(t.Context(), "turn-null"); err == nil || !strings.Contains(err.Error(), "must be a json array") {
		t.Fatalf("error = %v, want output items array validation", err)
	}
}

func TestGetTurnExecutionTraceReturnsArtifactReadError(t *testing.T) {
	uc := GetTurnExecutionTrace{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-3",
				ConversationID: "conv-3",
				Status:         domain.TurnStatusProcessing,
			},
		},
		Runs: &stubTurnRunLister{
			runs: []domain.TurnRun{{ID: "run-3", TurnID: "turn-3", StepIndex: 1}},
		},
		ToolCalls: &stubToolCallLister{},
		Artifacts: &stubTurnRunArtifactReader{err: errors.New("minio unavailable")},
	}

	_, err := uc.Execute(context.Background(), "turn-3")
	if err == nil || err.Error() != `get turn run output items "run-output-items/conv-3/turn-3/step-001.json": minio unavailable` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetTurnExecutionTraceReturnsInvalidArtifactJSONError(t *testing.T) {
	uc := GetTurnExecutionTrace{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-4",
				ConversationID: "conv-4",
				Status:         domain.TurnStatusProcessing,
			},
		},
		Runs: &stubTurnRunLister{
			runs: []domain.TurnRun{{ID: "run-4", TurnID: "turn-4", StepIndex: 1}},
		},
		ToolCalls: &stubToolCallLister{},
		Artifacts: &stubTurnRunArtifactReader{
			data: map[string][]byte{
				"run-output-items/conv-4/turn-4/step-001.json": []byte(`{"oops"`),
			},
		},
	}

	_, err := uc.Execute(context.Background(), "turn-4")
	if err == nil || err.Error() != `turn run output items "run-output-items/conv-4/turn-4/step-001.json" is not valid json` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetTurnExecutionTraceReturnsPlainTextToolCallOutput(t *testing.T) {
	now := time.Unix(1710000100, 0).UTC()
	uc := GetTurnExecutionTrace{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-5",
				ConversationID: "conv-5",
				Status:         domain.TurnStatusFailed,
				StreamBlobKey:  "stream-events/conv-5/turn-5.jsonl",
			},
		},
		Runs: &stubTurnRunLister{
			runs: []domain.TurnRun{{ID: "run-5", TurnID: "turn-5", StepIndex: 1, StartedAt: now, CreatedAt: now, UpdatedAt: now}},
		},
		ToolCalls: &stubToolCallLister{
			calls: []domain.ToolCallRecord{
				{
					ID:               "record-5",
					TurnID:           "turn-5",
					TurnRunID:        "run-5",
					CallID:           "call-5",
					ToolType:         "function",
					ToolName:         "sandbox.exec",
					Status:           domain.ToolCallStatusFailed,
					ArgumentsBlobKey: "tool-args:5",
					OutputBlobKey:    "tool-output:5",
					StartedAt:        now,
					CreatedAt:        now,
					UpdatedAt:        now,
				},
			},
		},
		Artifacts: &stubTurnRunArtifactReader{
			data: map[string][]byte{
				"tool-args:5":                       []byte(`{"command":"pwd"}`),
				"tool-output:5":                     []byte(`permission denied`),
				"stream-events/conv-5/turn-5.jsonl": []byte("{\"type\":\"tool.failed\",\"conversation_id\":\"conv-5\",\"turn_id\":\"turn-5\",\"tool_name\":\"sandbox.exec\",\"payload\":\"permission denied\",\"error\":\"permission denied\"}\n"),
			},
		},
	}

	trace, err := uc.Execute(context.Background(), "turn-5")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(trace.Runs) != 1 || len(trace.Runs[0].ToolCalls) != 1 {
		t.Fatalf("unexpected trace: %#v", trace)
	}
	call := trace.Runs[0].ToolCalls[0]
	if call.Summary != "Sandbox command failed" {
		t.Fatalf("unexpected tool call summary: %#v", call)
	}
	if len(call.Details) != 2 || call.Details[0] != "Command: pwd" || call.Details[1] != "Error: permission denied" {
		t.Fatalf("unexpected tool call details: %#v", call.Details)
	}
	if len(trace.StreamEvents) != 1 {
		t.Fatalf("unexpected stream events: %#v", trace.StreamEvents)
	}
	if trace.StreamEvents[0].PayloadText != "permission denied" {
		t.Fatalf("unexpected stream event payload text: %#v", trace.StreamEvents[0])
	}
}

func TestGetTurnExecutionTraceReturnsInvalidToolCallArgumentsError(t *testing.T) {
	uc := GetTurnExecutionTrace{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-6",
				ConversationID: "conv-6",
				Status:         domain.TurnStatusProcessing,
			},
		},
		Runs: &stubTurnRunLister{
			runs: []domain.TurnRun{{ID: "run-6", TurnID: "turn-6", StepIndex: 1}},
		},
		ToolCalls: &stubToolCallLister{
			calls: []domain.ToolCallRecord{
				{
					ID:               "record-6",
					TurnID:           "turn-6",
					TurnRunID:        "run-6",
					CallID:           "call-6",
					ToolType:         "function",
					ToolName:         "sandbox.exec",
					Status:           domain.ToolCallStatusFailed,
					ArgumentsBlobKey: "tool-args:6",
				},
			},
		},
		Artifacts: &stubTurnRunArtifactReader{
			data: map[string][]byte{
				"tool-args:6": []byte(`{"bad"`),
			},
		},
	}

	_, err := uc.Execute(context.Background(), "turn-6")
	if err == nil || err.Error() != `tool call arguments "tool-args:6" is not valid json` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetTurnExecutionTraceReturnsInvalidStreamArchiveLineError(t *testing.T) {
	uc := GetTurnExecutionTrace{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-7",
				ConversationID: "conv-7",
				Status:         domain.TurnStatusFailed,
				StreamBlobKey:  "stream-events/conv-7/turn-7.jsonl",
			},
		},
		Runs:      &stubTurnRunLister{},
		ToolCalls: &stubToolCallLister{},
		Artifacts: &stubTurnRunArtifactReader{
			data: map[string][]byte{
				"stream-events/conv-7/turn-7.jsonl": []byte("{\"type\":\"response.started\",\"conversation_id\":\"conv-7\",\"turn_id\":\"turn-7\"}\nnot-json\n"),
			},
		},
	}

	_, err := uc.Execute(context.Background(), "turn-7")
	if err == nil || err.Error() != `stream event archive "stream-events/conv-7/turn-7.jsonl" line 2 is not valid json` {
		t.Fatalf("unexpected error: %v", err)
	}
}
