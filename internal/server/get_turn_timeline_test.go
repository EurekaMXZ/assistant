package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/gin-gonic/gin"
)

type stubTurnTimelineEventLister struct {
	turnID string
	events []domain.TurnStreamEvent
	err    error
}

func TestBuildTimelineDropsDuplicateAndOutOfOrderSequences(t *testing.T) {
	now := time.Now().UTC()
	payloads := []string{
		`{"response_id":"resp-1","item_id":"msg-1","sequence_number":10,"delta":"A"}`,
		`{"response_id":"resp-1","item_id":"msg-1","sequence_number":10,"delta":"duplicate"}`,
		`{"response_id":"resp-1","item_id":"msg-1","sequence_number":9,"delta":"old"}`,
		`{"response_id":"resp-1","item_id":"msg-1","sequence_number":11,"delta":"B"}`,
	}
	events := make([]domain.TurnStreamEvent, 0, len(payloads))
	for index, payload := range payloads {
		raw, err := json.Marshal(stream.Event{
			Type:    "response.output_text.delta",
			Payload: payload,
		})
		if err != nil {
			t.Fatalf("marshal event %d: %v", index, err)
		}
		events = append(events, domain.TurnStreamEvent{
			ID:         fmt.Sprintf("event-%d", index+1),
			EventIndex: int64(index + 1),
			EventType:  "response.output_text.delta",
			Payload:    raw,
			CreatedAt:  now.Add(time.Duration(index) * time.Millisecond),
		})
	}

	items, _, err := buildTimelineFromEvents(events, nil)
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one assistant item", items)
	}
	if items[0].ContentText != "AB" {
		t.Fatalf("content = %q, want %q", items[0].ContentText, "AB")
	}
	if sequence, ok := metadataInt(items[0].Metadata, "sequence_number"); !ok || sequence != 11 {
		t.Fatalf("sequence metadata = %#v, want 11", items[0].Metadata)
	}
}

func TestGetTurnTimelineReconcilesPartialToolEventsFromDurableCalls(t *testing.T) {
	now := time.Now().UTC()
	raw, err := json.Marshal(stream.Event{
		Type:     stream.EventToolStarted,
		ToolName: "Reading_Content",
		Payload:  `{"tool_call_record_id":"tool-1","call_id":"call-1","tool_name":"Reading_Content","status":"started"}`,
	})
	if err != nil {
		t.Fatalf("marshal tool event: %v", err)
	}
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{turn: &domain.Turn{
			ID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing, CreatedAt: now,
		}},
		Runs: &stubTurnRunLister{},
		Events: &stubTurnTimelineEventLister{events: []domain.TurnStreamEvent{{
			ID: "event-1", TurnID: "turn-1", ConversationID: "conv-1", EventIndex: 42,
			EventType: stream.EventToolStarted, Payload: raw, CreatedAt: now.Add(time.Second),
		}}},
		ToolCalls: &stubToolCallLister{calls: []domain.ToolCallRecord{{
			ID: "tool-1", TurnID: "turn-1", TurnRunID: "run-1", CallID: "call-1",
			Namespace: "internet", ToolName: "search", Status: domain.ToolCallStatusCompleted,
			ArgumentsBlobKey: "tool-args", OutputBlobKey: "tool-output", StartedAt: now,
		}}},
		Artifacts: &stubTurnRunArtifactReader{data: map[string][]byte{
			"tool-args":   []byte(`{"query":"latest docs"}`),
			"tool-output": []byte(`{"results":[{"url":"https://example.com"}]}`),
		}},
	}

	timeline, err := uc.Execute(t.Context(), "turn-1")
	if err != nil {
		t.Fatalf("execute timeline: %v", err)
	}
	if timeline.LastEventIndex != 42 {
		t.Fatalf("last event index = %d, want 42", timeline.LastEventIndex)
	}
	if len(timeline.Items) != 1 {
		t.Fatalf("timeline items = %#v, want one reconciled tool", timeline.Items)
	}
	item := timeline.Items[0]
	if item.ID != "tool:tool-1" || item.Title != "internet.search" || item.Status != domain.ToolCallStatusCompleted {
		t.Fatalf("reconciled tool identity = %#v", item)
	}
	if metadataString(item.Metadata, "tool_name") != "internet.search" {
		t.Fatalf("reconciled tool metadata = %#v, want canonical tool name", item.Metadata)
	}
	if !strings.Contains(string(item.Arguments), "latest docs") || !strings.Contains(string(item.Output), "example.com") {
		t.Fatalf("reconciled tool payload = %#v", item)
	}
	presented, ok := newPresentationItemRegistry().Filter(item)
	if !ok {
		t.Fatal("reconciled Tavily tool was dropped by presentation filter")
	}
	if presented.Title != "Searching the Web" || presented.InputText != "latest docs" {
		t.Fatalf("reconciled Tavily presentation = %#v", presented)
	}
	if len(presented.Links) != 1 || presented.Links[0].URL != "https://example.com" {
		t.Fatalf("reconciled Tavily links = %#v", presented.Links)
	}
}

func TestBuildTimelineCoalescesAndRedactsResponseFailures(t *testing.T) {
	now := time.Now().UTC()
	streamEvents := []stream.Event{
		{Type: stream.EventResponseFailed, ResponseID: "resp-1", Error: "provider secret", Payload: `{"error":"provider secret"}`},
		{Type: stream.EventResponseFailed, ErrorCode: domain.TurnErrorUpstreamRequestFailed, Error: "different secret"},
	}
	events := make([]domain.TurnStreamEvent, 0, len(streamEvents))
	for index, event := range streamEvents {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event %d: %v", index, err)
		}
		events = append(events, domain.TurnStreamEvent{
			ID:         fmt.Sprintf("event-%d", index+1),
			EventIndex: int64(index + 1),
			EventType:  stream.EventResponseFailed,
			Payload:    raw,
			CreatedAt:  now.Add(time.Duration(index) * time.Millisecond),
		})
	}

	items, _, err := buildTimelineFromEvents(events, nil)
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one failure status", items)
	}
	if strings.Contains(items[0].ContentText, "secret") || !strings.Contains(items[0].ContentText, domain.TurnPublicErrorUpstreamRequestFailed) {
		t.Fatalf("failure content was not redacted: %q", items[0].ContentText)
	}
}

func TestBuildTimelineKeepsCreatedResponseIDForSparseEvents(t *testing.T) {
	now := time.Now().UTC()
	streamEvents := []stream.Event{
		{Type: stream.EventResponseCreated, ResponseID: "resp-1"},
		{Type: "response.output_text.delta", Payload: `{"item_id":"msg-1","sequence_number":1,"delta":"A"}`},
		{Type: "response.output_text.done", Payload: `{"item_id":"msg-1","sequence_number":2,"text":"Answer"}`},
	}
	events := make([]domain.TurnStreamEvent, 0, len(streamEvents))
	for index, event := range streamEvents {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event %d: %v", index, err)
		}
		events = append(events, domain.TurnStreamEvent{
			ID:         fmt.Sprintf("event-%d", index+1),
			EventIndex: int64(index + 1),
			EventType:  event.Type,
			Payload:    raw,
			CreatedAt:  now.Add(time.Duration(index) * time.Millisecond),
		})
	}

	items, _, err := buildTimelineFromEvents(events, nil)
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one assistant item", items)
	}
	wantID := stableTimelineAssistantTextItemID("resp-1", "msg-1", 0, 0, "", 0)
	if items[0].ID != wantID || items[0].ContentText != "Answer" {
		t.Fatalf("assistant item = %#v, want id %q with final text", items[0], wantID)
	}
}

func TestStableTimelineOutputIDsIgnoreOptionalProviderItemID(t *testing.T) {
	assistantWithItemID := stableTimelineAssistantTextItemID("resp-1", "msg-1", 2, 0, "", 0)
	assistantWithoutItemID := stableTimelineAssistantTextItemID("resp-1", "", 2, 0, "", 0)
	if assistantWithItemID != assistantWithoutItemID {
		t.Fatalf("assistant IDs differ: %q != %q", assistantWithItemID, assistantWithoutItemID)
	}

	reasoningWithItemID := stableTimelineReasoningPartID("resp-1", "rs-1", 1, 0, "", 0)
	reasoningWithoutItemID := stableTimelineReasoningPartID("resp-1", "", 1, 0, "", 0)
	if reasoningWithItemID != reasoningWithoutItemID {
		t.Fatalf("reasoning IDs differ: %q != %q", reasoningWithItemID, reasoningWithoutItemID)
	}
}

func TestResponseOutputSlotsRestoreItemMappingFromTimeline(t *testing.T) {
	resolver := responseOutputSlotsFromTimeline([]TurnTimelineItem{{
		Type: turnTimelineItemOutputText,
		Metadata: map[string]any{
			"response_id":  "resp-1",
			"item_id":      "msg-1",
			"output_index": 0,
		},
	}})

	slot, ok := resolver.resolve("resp-1", "message", "msg-1", 1)
	if !ok || slot != 0 {
		t.Fatalf("resolved slot = %d, %v; want 0, true", slot, ok)
	}
}

func TestAppendMissingAssistantMessagesRecognizesCombinedStreamOutput(t *testing.T) {
	items := []TurnTimelineItem{
		{Type: turnTimelineItemOutputText, ContentText: "Preamble"},
		{Type: turnTimelineItemOutputText, ContentText: "Final answer"},
	}
	messages := []domain.Message{{ID: "message-1", ContentText: "PreambleFinal answer"}}

	got := appendMissingAssistantMessages(items, &domain.Turn{}, messages)
	if len(got) != len(items) {
		t.Fatalf("combined persisted message was appended again: %#v", got)
	}
}

func TestBuildTimelineMatchesCompressedCompletedOutputToStreamedItem(t *testing.T) {
	responseID := "resp-1"
	streamEvents := []stream.Event{
		{Type: stream.EventResponseCreated, Payload: `{"type":"response.created","response":{"id":"resp-1"}}`},
		{Type: "response.output_item.added", Payload: `{"type":"response.output_item.added","output_index":0,"item":{"id":"rs-1","type":"reasoning"}}`},
		{Type: "response.output_item.done", Payload: `{"type":"response.output_item.done","output_index":0,"item":{"id":"rs-1","type":"reasoning"}}`},
		{Type: "response.output_item.added", Payload: `{"type":"response.output_item.added","output_index":1,"item":{"id":"msg-1","type":"message","role":"assistant"}}`},
		{Type: "response.output_text.delta", Payload: `{"type":"response.output_text.delta","item_id":"msg-1","output_index":1,"content_index":0,"sequence_number":4,"delta":"Answer"}`},
		{Type: "response.output_text.done", Payload: `{"type":"response.output_text.done","item_id":"msg-1","output_index":1,"content_index":0,"sequence_number":5,"text":"Answer"}`},
		{Type: stream.EventResponseCompleted, Payload: `{"type":"response.completed","response":{"id":"resp-1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Answer"}]}]}}`},
	}
	events := make([]domain.TurnStreamEvent, 0, len(streamEvents))
	for index, event := range streamEvents {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event %d: %v", index, err)
		}
		events = append(events, domain.TurnStreamEvent{
			ID:         fmt.Sprintf("event-%d", index+1),
			EventIndex: int64(index + 1),
			EventType:  event.Type,
			Payload:    raw,
		})
	}

	items, _, err := buildTimelineFromEvents(events, nil)
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	var assistantItems []TurnTimelineItem
	for _, item := range items {
		if item.Type == turnTimelineItemOutputText {
			assistantItems = append(assistantItems, item)
		}
	}
	if len(assistantItems) != 1 {
		t.Fatalf("assistant items = %#v, want one", assistantItems)
	}
	wantID := stableTimelineAssistantTextItemID(responseID, "msg-1", 0, 0, "", 0)
	if assistantItems[0].ID != wantID || assistantItems[0].ContentText != "Answer" {
		t.Fatalf("assistant item = %#v, want id %q", assistantItems[0], wantID)
	}
}

func TestBuildTimelineKeepsStreamedReasoningPartsInsteadOfCombinedFallback(t *testing.T) {
	streamEvents := []stream.Event{
		{Type: stream.EventResponseCreated, Payload: `{"type":"response.created","response":{"id":"resp-1"}}`},
		{Type: "response.output_item.added", Payload: `{"type":"response.output_item.added","output_index":0,"item":{"id":"rs-1","type":"reasoning"}}`},
		{Type: "response.reasoning_summary_text.done", Payload: `{"type":"response.reasoning_summary_text.done","item_id":"rs-1","output_index":0,"summary_index":0,"sequence_number":5,"text":"Part A"}`},
		{Type: "response.reasoning_summary_text.done", Payload: `{"type":"response.reasoning_summary_text.done","item_id":"rs-1","output_index":0,"summary_index":1,"sequence_number":6,"text":"Part B"}`},
		{Type: stream.EventResponseCompleted, Payload: `{"type":"response.completed","response":{"id":"resp-1","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Part APart B"}]}]}}`},
		{Type: stream.EventReasoningSummary, ResponseID: "resp-1", Payload: `{"response_id":"resp-1","items":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Part APart B"}]}]}`},
	}
	events := make([]domain.TurnStreamEvent, 0, len(streamEvents))
	for index, event := range streamEvents {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event %d: %v", index, err)
		}
		events = append(events, domain.TurnStreamEvent{
			ID:         fmt.Sprintf("event-%d", index+1),
			EventIndex: int64(index + 1),
			EventType:  event.Type,
			Payload:    raw,
		})
	}

	items, _, err := buildTimelineFromEvents(events, nil)
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	var reasoningTexts []string
	for _, item := range items {
		if item.Type == turnTimelineItemReasoning {
			reasoningTexts = append(reasoningTexts, item.ContentText)
		}
	}
	if len(reasoningTexts) != 2 || reasoningTexts[0] != "Part A" || reasoningTexts[1] != "Part B" {
		t.Fatalf("reasoning items = %#v, want streamed parts only", reasoningTexts)
	}
}

func TestSplitReasoningTimelineItemAtTitleParagraphs(t *testing.T) {
	item := TurnTimelineItem{
		ID:   "reasoning:resp-1:0:0",
		Type: turnTimelineItemReasoning,
		ContentText: "**First title**\n\nFirst body.\nSentence with **inline emphasis**\ncontinues.\n\n" +
			"```text\n**not a title**\n```\n\n**Second title**\n\nSecond body.\n\n**Third title**\nThird body.",
		Metadata: map[string]any{"sequence_number": 12},
	}

	sections := splitReasoningTimelineItem(item)
	if len(sections) != 3 {
		t.Fatalf("sections = %#v, want three", sections)
	}
	if sections[0].ID != item.ID || sections[1].ID != item.ID+":section:1" || sections[2].ID != item.ID+":section:2" {
		t.Fatalf("unexpected section IDs: %#v", sections)
	}
	if !strings.HasPrefix(sections[0].ContentText, "**First title**") ||
		!strings.HasPrefix(sections[1].ContentText, "**Second title**") ||
		!strings.HasPrefix(sections[2].ContentText, "**Third title**") {
		t.Fatalf("unexpected section content: %#v", sections)
	}
	if !strings.Contains(sections[0].ContentText, "**inline emphasis**") {
		t.Fatalf("inline emphasis was treated as a title boundary: %q", sections[0].ContentText)
	}
	if !strings.Contains(sections[0].ContentText, "**not a title**") {
		t.Fatalf("fenced content was treated as a title boundary: %q", sections[0].ContentText)
	}
}

func (s *stubTurnTimelineEventLister) ListTurnStreamEventsByTurn(_ context.Context, turnID string) ([]domain.TurnStreamEvent, error) {
	s.turnID = turnID
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.TurnStreamEvent(nil), s.events...), nil
}

type stubTurnTimelineMessageLister struct {
	turnID   string
	messages []domain.Message
	err      error
}

func (s *stubTurnTimelineMessageLister) ListAssistantMessagesByTurn(_ context.Context, turnID string) ([]domain.Message, error) {
	s.turnID = turnID
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.Message(nil), s.messages...), nil
}

func TestGetTurnTimelineBuildsItemsFromPersistedEvents(t *testing.T) {
	now := time.Unix(1710001000, 0).UTC()
	runs := &stubTurnRunLister{
		runs: []domain.TurnRun{
			{
				ID:          "run-1",
				TurnID:      "turn-1",
				StepIndex:   1,
				ResponseID:  "resp_1",
				StartedAt:   now,
				CompletedAt: ptrTime(now.Add(2 * time.Second)),
				CreatedAt:   now,
				UpdatedAt:   now.Add(2 * time.Second),
			},
			{
				ID:          "run-2",
				TurnID:      "turn-1",
				StepIndex:   2,
				ResponseID:  "resp_2",
				StartedAt:   now.Add(3 * time.Second),
				CompletedAt: ptrTime(now.Add(5 * time.Second)),
				CreatedAt:   now.Add(3 * time.Second),
				UpdatedAt:   now.Add(5 * time.Second),
			},
		},
	}
	events := &stubTurnTimelineEventLister{
		events: []domain.TurnStreamEvent{
			{
				ID:             "evt-1",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     1,
				EventType:      "response.started",
				Payload:        json.RawMessage(`{"type":"response.started","conversation_id":"conv-1","turn_id":"turn-1"}`),
				CreatedAt:      now,
			},
			{
				ID:             "evt-2",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     2,
				EventType:      "response.created",
				Payload:        json.RawMessage(`{"type":"response.created","conversation_id":"conv-1","turn_id":"turn-1","response_id":"resp_1"}`),
				CreatedAt:      now.Add(1 * time.Second),
			},
			{
				ID:             "evt-3",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     3,
				EventType:      "reasoning.summary",
				Payload:        json.RawMessage(`{"type":"reasoning.summary","conversation_id":"conv-1","turn_id":"turn-1","response_id":"resp_1","payload":"{\"turn_run_id\":\"run-1\",\"response_id\":\"resp_1\",\"step_index\":1,\"items\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Need to search for the latest update.\"}]}]}"}`),
				CreatedAt:      now.Add(1500 * time.Millisecond),
			},
			{
				ID:             "evt-4",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     4,
				EventType:      "tool.started",
				Payload:        json.RawMessage(`{"type":"tool.started","conversation_id":"conv-1","turn_id":"turn-1","tool_name":"internet.search","payload":"{\"tool_call_record_id\":\"tool-1\",\"turn_run_id\":\"run-1\",\"call_id\":\"call_1\",\"tool_name\":\"internet.search\",\"tool_type\":\"function\",\"namespace\":\"internet\",\"status\":\"started\",\"summary\":\"Searching the web\",\"details\":[\"Query: latest OpenAI news\"]}"}`),
				CreatedAt:      now.Add(2 * time.Second),
			},
			{
				ID:             "evt-5",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     5,
				EventType:      "tool.completed",
				Payload:        json.RawMessage(`{"type":"tool.completed","conversation_id":"conv-1","turn_id":"turn-1","tool_name":"internet.search","payload":"{\"tool_call_record_id\":\"tool-1\",\"turn_run_id\":\"run-1\",\"call_id\":\"call_1\",\"tool_name\":\"internet.search\",\"tool_type\":\"function\",\"namespace\":\"internet\",\"status\":\"completed\",\"arguments\":{\"query\":\"latest OpenAI news\"},\"output\":{\"results\":[1,2,3,4,5]}}"}`),
				CreatedAt:      now.Add(3 * time.Second),
			},
			{
				ID:             "evt-6",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     6,
				EventType:      "response.created",
				Payload:        json.RawMessage(`{"type":"response.created","conversation_id":"conv-1","turn_id":"turn-1","response_id":"resp_2"}`),
				CreatedAt:      now.Add(4 * time.Second),
			},
			{
				ID:             "evt-7",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     7,
				EventType:      "reasoning.summary",
				Payload:        json.RawMessage(`{"type":"reasoning.summary","conversation_id":"conv-1","turn_id":"turn-1","response_id":"resp_2","payload":"{\"turn_run_id\":\"run-2\",\"response_id\":\"resp_2\",\"step_index\":2,\"items\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"The search results answer the question.\"}]}]}"}`),
				CreatedAt:      now.Add(4500 * time.Millisecond),
			},
			{
				ID:             "evt-8",
				TurnID:         "turn-1",
				ConversationID: "conv-1",
				EventIndex:     8,
				EventType:      "response.completed",
				Payload:        json.RawMessage(`{"type":"response.completed","conversation_id":"conv-1","turn_id":"turn-1","response_id":"resp_2","text":"Final answer"}`),
				CreatedAt:      now.Add(6 * time.Second),
			},
		},
	}
	artifacts := &stubTurnRunArtifactReader{
		data: map[string][]byte{
			"run-output-items/conv-1/turn-1/step-001.json": []byte(`[{"type":"reasoning","summary":[{"type":"summary_text","text":"Need to search for the latest update."}],"encrypted_content":"ciphertext"}]`),
			"run-output-items/conv-1/turn-1/step-002.json": []byte(`[{"type":"reasoning","summary":[{"type":"summary_text","text":"The search results answer the question."}]}]`),
		},
	}
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-1",
				ConversationID: "conv-1",
				Status:         domain.TurnStatusCompleted,
				CreatedAt:      now,
				UpdatedAt:      now.Add(6 * time.Second),
			},
		},
		Events:    events,
		Runs:      runs,
		Messages:  &stubTurnTimelineMessageLister{messages: []domain.Message{{ContentText: "Final answer", CreatedAt: now.Add(6 * time.Second)}}},
		Artifacts: artifacts,
	}

	timeline, err := uc.Execute(context.Background(), "turn-1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if timeline.TurnID != "turn-1" || timeline.ConversationID != "conv-1" {
		t.Fatalf("unexpected timeline identity: %#v", timeline)
	}
	if events.turnID != "turn-1" || runs.turnID != "turn-1" {
		t.Fatalf("unexpected lookup ids: events=%q runs=%q", events.turnID, runs.turnID)
	}
	if len(timeline.Items) != 4 {
		t.Fatalf("expected 4 timeline items, got %#v", timeline.Items)
	}

	if timeline.Items[0].Type != turnTimelineItemReasoning || !strings.Contains(timeline.Items[0].ContentText, "Need to search for the latest update.") {
		t.Fatalf("unexpected first item: %#v", timeline.Items[0])
	}
	if timeline.Items[1].Type != turnTimelineItemToolCall || !strings.Contains(string(timeline.Items[1].Arguments), `"query":"latest OpenAI news"`) {
		t.Fatalf("unexpected tool item: %#v", timeline.Items[1])
	}
	if !strings.Contains(string(timeline.Items[1].Output), `"results":[1,2,3,4,5]`) {
		t.Fatalf("unexpected tool output: %#v", timeline.Items[1])
	}
	if timeline.Items[2].Type != turnTimelineItemReasoning || !strings.Contains(timeline.Items[2].ContentText, "The search results answer the question.") {
		t.Fatalf("unexpected second reasoning item: %#v", timeline.Items[2])
	}
	if timeline.Items[3].Type != turnTimelineItemOutputText || timeline.Items[3].ContentText != "Final answer" {
		t.Fatalf("unexpected final item: %#v", timeline.Items[3])
	}
}

func TestGetTurnTimelineFallsBackWithoutPersistedEvents(t *testing.T) {
	now := time.Unix(1710002000, 0).UTC()
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-2",
				ConversationID: "conv-2",
				Status:         domain.TurnStatusCompleted,
				CreatedAt:      now,
				UpdatedAt:      now.Add(4 * time.Second),
			},
		},
		Events: &stubTurnTimelineEventLister{},
		Runs: &stubTurnRunLister{
			runs: []domain.TurnRun{
				{
					ID:          "run-2",
					TurnID:      "turn-2",
					StepIndex:   1,
					ResponseID:  "resp_2",
					StartedAt:   now,
					CompletedAt: ptrTime(now.Add(2 * time.Second)),
					CreatedAt:   now,
					UpdatedAt:   now.Add(2 * time.Second),
				},
			},
		},
		ToolCalls: &stubToolCallLister{
			calls: []domain.ToolCallRecord{
				{
					ID:               "tool-record-2",
					TurnID:           "turn-2",
					TurnRunID:        "run-2",
					CallID:           "call_2",
					ToolType:         "function",
					Namespace:        "internet",
					ToolName:         "search",
					Status:           domain.ToolCallStatusCompleted,
					ArgumentsBlobKey: "tool-args:2",
					OutputBlobKey:    "tool-output:2",
					StartedAt:        now.Add(1 * time.Second),
					CompletedAt:      ptrTime(now.Add(2 * time.Second)),
					CreatedAt:        now.Add(1 * time.Second),
					UpdatedAt:        now.Add(2 * time.Second),
				},
			},
		},
		Messages: &stubTurnTimelineMessageLister{
			messages: []domain.Message{{
				ContentText: "Historical final answer",
				CreatedAt:   now.Add(4 * time.Second),
			}},
		},
		Artifacts: &stubTurnRunArtifactReader{
			data: map[string][]byte{
				"run-output-items/conv-2/turn-2/step-001.json": []byte(`[{"type":"reasoning","summary":[{"type":"summary_text","text":"I should search before answering."}],"encrypted_content":"ciphertext"}]`),
				"tool-args:2":   []byte(`{"query":"OpenAI"}`),
				"tool-output:2": []byte(`{"results":[{"title":"OpenAI"}]}`),
			},
		},
	}

	timeline, err := uc.Execute(context.Background(), "turn-2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(timeline.Items) != 3 {
		t.Fatalf("expected 3 fallback items, got %#v", timeline.Items)
	}
	if timeline.Items[0].Type != turnTimelineItemReasoning || !strings.Contains(timeline.Items[0].ContentText, "I should search before answering.") {
		t.Fatalf("unexpected fallback reasoning item: %#v", timeline.Items[0])
	}
	if timeline.Items[1].Type != turnTimelineItemToolCall || !strings.Contains(string(timeline.Items[1].Arguments), `"query":"OpenAI"`) {
		t.Fatalf("unexpected fallback tool item: %#v", timeline.Items[1])
	}
	if !strings.Contains(string(timeline.Items[1].Output), `"title":"OpenAI"`) {
		t.Fatalf("unexpected fallback tool output: %#v", timeline.Items[1])
	}
	if timeline.Items[2].Type != turnTimelineItemOutputText || timeline.Items[2].ContentText != "Historical final answer" {
		t.Fatalf("unexpected fallback final answer: %#v", timeline.Items[2])
	}
}

func TestGetTurnTimelineReplaysMultipleOutputTextItems(t *testing.T) {
	now := time.Unix(1710002500, 0).UTC()
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{
			turn: &domain.Turn{
				ID:             "turn-text",
				ConversationID: "conv-text",
				Status:         domain.TurnStatusCompleted,
				CreatedAt:      now,
				UpdatedAt:      now.Add(4 * time.Second),
			},
		},
		Runs: &stubTurnRunLister{},
		Events: &stubTurnTimelineEventLister{events: []domain.TurnStreamEvent{
			{
				ID:             "evt-text-1",
				TurnID:         "turn-text",
				ConversationID: "conv-text",
				EventIndex:     1,
				EventType:      "response.output_text.delta",
				Payload:        json.RawMessage(`{"type":"response.output_text.delta","conversation_id":"conv-text","turn_id":"turn-text","response_id":"resp_text","payload":"{\"type\":\"response.output_text.delta\",\"response_id\":\"resp_text\",\"item_id\":\"msg_pre\",\"output_index\":0,\"content_index\":0,\"delta\":\"Preamble\"}","delta":"Preamble"}`),
				CreatedAt:      now,
			},
			{
				ID:             "evt-text-2",
				TurnID:         "turn-text",
				ConversationID: "conv-text",
				EventIndex:     2,
				EventType:      "response.output_text.done",
				Payload:        json.RawMessage(`{"type":"response.output_text.done","conversation_id":"conv-text","turn_id":"turn-text","response_id":"resp_text","payload":"{\"type\":\"response.output_text.done\",\"response_id\":\"resp_text\",\"item_id\":\"msg_pre\",\"output_index\":0,\"content_index\":0,\"text\":\"Preamble\"}","text":"Preamble"}`),
				CreatedAt:      now.Add(time.Second),
			},
			{
				ID:             "evt-text-3",
				TurnID:         "turn-text",
				ConversationID: "conv-text",
				EventIndex:     3,
				EventType:      "reasoning.summary",
				Payload:        json.RawMessage(`{"type":"reasoning.summary","conversation_id":"conv-text","turn_id":"turn-text","response_id":"resp_text","payload":"{\"turn_run_id\":\"run-text\",\"response_id\":\"resp_text\",\"step_index\":1,\"items\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Checking details.\"}]}]}"}`),
				CreatedAt:      now.Add(2 * time.Second),
			},
			{
				ID:             "evt-text-4",
				TurnID:         "turn-text",
				ConversationID: "conv-text",
				EventIndex:     4,
				EventType:      "response.output_text.done",
				Payload:        json.RawMessage(`{"type":"response.output_text.done","conversation_id":"conv-text","turn_id":"turn-text","response_id":"resp_text","payload":"{\"type\":\"response.output_text.done\",\"response_id\":\"resp_text\",\"item_id\":\"msg_final\",\"output_index\":2,\"content_index\":0,\"text\":\"Final\"}","text":"Final"}`),
				CreatedAt:      now.Add(3 * time.Second),
			},
		}},
	}

	timeline, err := uc.Execute(context.Background(), "turn-text")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(timeline.Items) != 3 {
		t.Fatalf("expected 3 items, got %#v", timeline.Items)
	}
	if timeline.Items[0].Type != turnTimelineItemOutputText || timeline.Items[0].ContentText != "Preamble" {
		t.Fatalf("unexpected preamble item: %#v", timeline.Items[0])
	}
	if timeline.Items[1].Type != turnTimelineItemReasoning {
		t.Fatalf("unexpected reasoning item: %#v", timeline.Items[1])
	}
	if timeline.Items[2].Type != turnTimelineItemOutputText || timeline.Items[2].ContentText != "Final" {
		t.Fatalf("unexpected final item: %#v", timeline.Items[2])
	}
}

func TestGetTurnTimelineEnrichesStreamedOutputTextPhase(t *testing.T) {
	now := time.Unix(1710002600, 0).UTC()
	events := []domain.TurnStreamEvent{
		storedTurnEvent("phase-done", 1, "response.output_text.done", `{"type":"response.output_text.done","response_id":"resp_phase","item_id":"msg_phase","output_index":0,"content_index":0,"text":"Progress"}`, now),
		storedTurnEvent("phase-completed", 2, "response.completed", `{"type":"response.completed","response_id":"resp_phase","response":{"id":"resp_phase","output":[{"id":"msg_phase","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Progress"}]}]}}`, now.Add(time.Second)),
	}
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{turn: &domain.Turn{
			ID:             "turn-phase",
			ConversationID: "conv-phase",
			Status:         domain.TurnStatusProcessing,
			CreatedAt:      now,
			UpdatedAt:      now.Add(time.Second),
		}},
		Runs:   &stubTurnRunLister{},
		Events: &stubTurnTimelineEventLister{events: events},
	}

	timeline, err := uc.Execute(context.Background(), "turn-phase")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(timeline.Items) != 1 {
		t.Fatalf("items = %#v, want one output text item", timeline.Items)
	}
	if timeline.Items[0].ContentText != "Progress" || metadataString(timeline.Items[0].Metadata, "phase") != "commentary" {
		t.Fatalf("output text phase was not enriched: %#v", timeline.Items[0])
	}
}

func TestGetTurnTimelinePreservesReasoningSummaryParts(t *testing.T) {
	now := time.Unix(1710002750, 0).UTC()
	events := []domain.TurnStreamEvent{
		storedTurnEvent("reasoning-created", 1, "response.created", `{"type":"response.created","response_id":"resp_parts"}`, now),
		storedTurnEvent("reasoning-part-0", 2, "response.reasoning_summary_part.added", `{"type":"response.reasoning_summary_part.added","response_id":"resp_parts","item_id":"rs_parts","output_index":0,"summary_index":0}`, now.Add(time.Second)),
		storedTurnEvent("reasoning-delta-0", 3, "response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","response_id":"resp_parts","item_id":"rs_parts","output_index":0,"summary_index":0,"delta":"**First check**\n\nInspecting."}`, now.Add(2*time.Second)),
		storedTurnEvent("reasoning-done-0", 4, "response.reasoning_summary_text.done", `{"type":"response.reasoning_summary_text.done","response_id":"resp_parts","item_id":"rs_parts","output_index":0,"summary_index":0,"text":"**First check**\n\nInspecting."}`, now.Add(3*time.Second)),
		storedTurnEvent("reasoning-part-1", 5, "response.reasoning_summary_part.added", `{"type":"response.reasoning_summary_part.added","response_id":"resp_parts","item_id":"rs_parts","output_index":0,"summary_index":1}`, now.Add(4*time.Second)),
		storedTurnEvent("reasoning-delta-1", 6, "response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","response_id":"resp_parts","item_id":"rs_parts","output_index":0,"summary_index":1,"delta":"**Second check**\n\nVerifying."}`, now.Add(5*time.Second)),
		storedTurnEvent("reasoning-completed", 7, "response.completed", `{"type":"response.completed","response":{"id":"resp_parts","output":[{"id":"rs_parts","type":"reasoning","summary":[{"type":"summary_text","text":"**First check**\n\nInspecting."},{"type":"summary_text","text":"**Second check**\n\nVerifying."}]},{"id":"msg_parts","type":"message","role":"assistant","content":[{"type":"output_text","text":"Final"}]}]}}`, now.Add(6*time.Second)),
		storedTurnEvent("reasoning-summary", 8, "reasoning.summary", `{"turn_run_id":"run-parts","response_id":"resp_parts","step_index":1,"items":[{"id":"rs_parts","type":"reasoning","summary":[{"type":"summary_text","text":"**First check**\n\nInspecting."},{"type":"summary_text","text":"**Second check**\n\nVerifying."}]}]}`, now.Add(7*time.Second)),
	}
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{turn: &domain.Turn{
			ID:             "turn-parts",
			ConversationID: "conv-parts",
			Status:         domain.TurnStatusCompleted,
			CreatedAt:      now,
			UpdatedAt:      now.Add(7 * time.Second),
		}},
		Runs:   &stubTurnRunLister{},
		Events: &stubTurnTimelineEventLister{events: events},
	}

	timeline, err := uc.Execute(context.Background(), "turn-parts")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(timeline.Items) != 3 {
		t.Fatalf("expected two reasoning parts and one assistant item, got %#v", timeline.Items)
	}
	for index, expected := range []string{"**First check**\n\nInspecting.", "**Second check**\n\nVerifying."} {
		item := timeline.Items[index]
		if item.Type != turnTimelineItemReasoning || item.ContentText != expected {
			t.Fatalf("unexpected reasoning part %d: %#v", index, item)
		}
		if item.Metadata["summary_index"] != index {
			t.Fatalf("summary index %d was not preserved: %#v", index, item.Metadata)
		}
	}
	if timeline.Items[0].ID == timeline.Items[1].ID {
		t.Fatalf("reasoning parts must have distinct ids: %#v", timeline.Items)
	}
	if timeline.Items[2].Type != turnTimelineItemOutputText || timeline.Items[2].ContentText != "Final" {
		t.Fatalf("unexpected assistant item: %#v", timeline.Items[2])
	}
}

func storedTurnEvent(id string, index int64, eventType string, payload string, createdAt time.Time) domain.TurnStreamEvent {
	var raw map[string]any
	_ = json.Unmarshal([]byte(payload), &raw)
	eventPayload, _ := json.Marshal(map[string]any{
		"type":            eventType,
		"conversation_id": "conv-parts",
		"turn_id":         "turn-parts",
		"response_id":     raw["response_id"],
		"payload":         payload,
	})
	return domain.TurnStreamEvent{
		ID:             id,
		TurnID:         "turn-parts",
		ConversationID: "conv-parts",
		EventIndex:     index,
		EventType:      eventType,
		Payload:        eventPayload,
		CreatedAt:      createdAt,
	}
}

func TestTurnTimelineRouteIsNotPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-3/timeline", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
