package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

func TestPresentationEventRegistryDropsUnregisteredEvents(t *testing.T) {
	items := newPresentationItemRegistry()
	state := newPresentationStreamState(&domain.Turn{ID: "turn-1", ConversationID: "conv-1"}, items, nil)
	frames, err := newPresentationEventRegistry().Filter(state, stream.Event{
		Type:    "response.function_call_arguments.done",
		Payload: `{"arguments":"secret"}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter event: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("unregistered event produced presentation frames: %#v", frames)
	}
}

func TestPresentationItemRegistryFiltersToolFields(t *testing.T) {
	item, ok := newPresentationItemRegistry().Filter(TurnTimelineItem{
		ID:        "tool:1",
		Type:      turnTimelineItemToolCall,
		Title:     "internet.search",
		Status:    "completed",
		Arguments: json.RawMessage(`{"query":"public query","api_key":"secret-key"}`),
		Output:    json.RawMessage(`{"results":[{"url":"https://user:password@www.example.com/news?token=secret&lang=en#private","content":"secret-output"}],"token":"secret-response"}`),
		Metadata:  map[string]any{"error": "secret-error"},
	})
	if !ok {
		t.Fatal("registered tool item was dropped")
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	text := string(encoded)
	if strings.Contains(text, "secret-key") || strings.Contains(text, "secret-output") || strings.Contains(text, "secret-response") || strings.Contains(text, "secret-error") || strings.Contains(text, "password") || strings.Contains(text, "token") || strings.Contains(text, "private") {
		t.Fatalf("tool presentation leaked internal fields: %s", text)
	}
	if item.Title != "Searching the Web" || item.InputLabel != "Keywords" || item.InputText != "public query" {
		t.Fatalf("tool presentation lost public summary: %s", text)
	}
	if len(item.Links) != 1 || item.Links[0].URL != "https://www.example.com/news?lang=en" || item.Links[0].Label != "example.com" {
		t.Fatalf("unexpected public links: %#v", item.Links)
	}

	if _, ok := newPresentationItemRegistry().Filter(TurnTimelineItem{ID: "raw:1", Type: "provider_raw"}); ok {
		t.Fatal("unregistered item type was not dropped")
	}
}

func TestPresentationItemRegistryAllowsSandboxCommandOutputOnly(t *testing.T) {
	item, ok := newPresentationItemRegistry().Filter(TurnTimelineItem{
		ID:        "tool:sandbox",
		Type:      turnTimelineItemToolCall,
		Title:     "sandbox.exec",
		Status:    "completed",
		Arguments: json.RawMessage(`{"command":"printf","args":["hello"]}`),
		Output:    json.RawMessage(`{"conversation_id":"conv-1","result":{"runtime_id":"runtime-secret","command":"printf","args":["hello"],"output":"hello","exit_code":0}}`),
		Metadata:  map[string]any{"error": "private-error"},
	})
	if !ok {
		t.Fatal("sandbox command item was dropped")
	}
	if item.Title != "命令执行完成" || item.Command != "printf hello" || item.CommandOutput != "hello" {
		t.Fatalf("unexpected sandbox command presentation: %#v", item)
	}
	if item.ExitCode == nil || *item.ExitCode != 0 {
		t.Fatalf("ExitCode = %#v", item.ExitCode)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "runtime-secret") || strings.Contains(text, "private-error") || strings.Contains(text, `"arguments"`) || strings.Contains(text, `"output"`) {
		t.Fatalf("sandbox presentation leaked non-public fields: %s", text)
	}
}

func TestPresentationOutputDeclarationDoesNotConsumeDeltaSequence(t *testing.T) {
	state := newPresentationStreamState(
		&domain.Turn{ID: "turn-1", ConversationID: "conv-1"},
		newPresentationItemRegistry(),
		nil,
	)
	frames, err := newPresentationEventRegistry().Filter(state, stream.Event{
		Type:    responseEventOutputTextDelta,
		Payload: `{"response_id":"response-1","item_id":"item-1","sequence_number":7,"delta":"#"}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter first output delta: %v", err)
	}
	if len(frames) != 2 || frames[0].Event != streamUIEventItemUpsert || frames[1].Event != streamUIEventItemDelta {
		t.Fatalf("first output delta frames = %#v", frames)
	}

	declaration, ok := frames[0].Payload.(TurnTimelineItem)
	if !ok {
		t.Fatalf("item.upsert payload has type %T", frames[0].Payload)
	}
	if _, exists := metadataInt(declaration.Metadata, "sequence_number"); exists {
		t.Fatalf("output declaration consumed delta sequence: %#v", declaration.Metadata)
	}
	delta, ok := frames[1].Payload.(TurnStreamItemDelta)
	if !ok || delta.SequenceNumber == nil || *delta.SequenceNumber != 7 {
		t.Fatalf("item.delta lost sequence: %#v", frames[1].Payload)
	}

	frames, err = newPresentationEventRegistry().Filter(state, stream.Event{
		Type:    responseEventOutputTextDelta,
		Payload: `{"response_id":"response-1","item_id":"item-1","sequence_number":8,"delta":"\n\n"}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter whitespace output delta: %v", err)
	}
	if len(frames) != 1 || frames[0].Event != streamUIEventItemDelta {
		t.Fatalf("whitespace output delta frames = %#v", frames)
	}
	whitespace, ok := frames[0].Payload.(TurnStreamItemDelta)
	if !ok || whitespace.Delta != "\n\n" {
		t.Fatalf("whitespace output delta was not preserved: %#v", frames[0].Payload)
	}

	reasoning, ok := newPresentationItemRegistry().Filter(TurnTimelineItem{
		ID:       "reasoning-1",
		Type:     turnTimelineItemReasoning,
		Status:   "streaming",
		Metadata: map[string]any{"sequence_number": 8},
	})
	if !ok {
		t.Fatal("reasoning declaration was dropped")
	}
	if _, exists := metadataInt(reasoning.Metadata, "sequence_number"); exists {
		t.Fatalf("reasoning declaration consumed delta sequence: %#v", reasoning.Metadata)
	}
}

func TestPresentationStreamDropsEventsAlreadyIncludedInSnapshot(t *testing.T) {
	const responseID = "response-1"
	const itemID = "item-1"
	presentationItemID := stableTimelineAssistantTextItemID(responseID, itemID, 0, 0, "", 0)
	state := newPresentationStreamState(
		&domain.Turn{ID: "turn-1", ConversationID: "conv-1"},
		newPresentationItemRegistry(),
		[]TurnTimelineItem{{
			ID:       presentationItemID,
			Type:     turnTimelineItemOutputText,
			Metadata: map[string]any{"sequence_number": 10},
		}},
	)
	registry := newPresentationEventRegistry()

	frames, err := registry.Filter(state, stream.Event{
		Type:    "response.output_text.delta",
		Payload: `{"response_id":"response-1","item_id":"item-1","sequence_number":10,"delta":"duplicate"}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter duplicate delta: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("snapshot duplicate produced presentation frames: %#v", frames)
	}

	frames, err = registry.Filter(state, stream.Event{
		Type:    "response.output_text.delta",
		Payload: `{"response_id":"response-1","item_id":"item-1","sequence_number":11,"delta":"new"}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter new delta: %v", err)
	}
	if len(frames) != 1 || frames[0].Event != streamUIEventItemDelta {
		t.Fatalf("new delta frames = %#v, want one item.delta", frames)
	}
}

func TestPresentationProviderFailureWaitsForDurableFailure(t *testing.T) {
	state := newPresentationStreamState(
		&domain.Turn{ID: "turn-1", ConversationID: "conv-1"},
		newPresentationItemRegistry(),
		nil,
	)
	registry := newPresentationEventRegistry()

	frames, err := registry.Filter(state, stream.Event{
		Type:       stream.EventResponseFailed,
		ResponseID: "response-1",
		Payload:    `{"error":{"message":"provider secret"}}`,
		Error:      "provider secret",
	}, time.Now())
	if err != nil {
		t.Fatalf("filter provider failure: %v", err)
	}
	if len(frames) != 1 || frames[0].Terminal {
		t.Fatalf("provider failure frames = %#v, want one non-terminal status", frames)
	}
	encoded, err := json.Marshal(frames)
	if err != nil {
		t.Fatalf("marshal provider failure frames: %v", err)
	}
	if strings.Contains(string(encoded), "provider secret") {
		t.Fatalf("provider failure leaked raw error: %s", encoded)
	}

	frames, err = registry.Filter(state, stream.Event{
		Type:       stream.EventResponseFailed,
		ResponseID: "response-1",
		ErrorCode:  domain.TurnErrorUpstreamRequestFailed,
		Error:      domain.TurnPublicErrorUpstreamRequestFailed,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter durable failure: %v", err)
	}
	if len(frames) != 2 || !frames[1].Terminal || frames[1].Event != streamUIEventTurnDone {
		t.Fatalf("durable failure frames = %#v, want status followed by terminal turn.done", frames)
	}
}

func TestPresentationStreamMatchesCompressedCompletedOutputToStreamedItem(t *testing.T) {
	state := newPresentationStreamState(
		&domain.Turn{ID: "turn-1", ConversationID: "conv-1"},
		newPresentationItemRegistry(),
		nil,
	)
	registry := newPresentationEventRegistry()
	events := []stream.Event{
		{Type: stream.EventResponseCreated, Payload: `{"type":"response.created","response":{"id":"resp-1"}}`},
		{Type: "response.output_item.added", Payload: `{"type":"response.output_item.added","output_index":0,"item":{"id":"rs-1","type":"reasoning"}}`},
		{Type: "response.output_item.added", Payload: `{"type":"response.output_item.added","output_index":1,"item":{"id":"msg-1","type":"message","role":"assistant"}}`},
		{Type: "response.output_text.done", Payload: `{"type":"response.output_text.done","item_id":"msg-1","output_index":1,"content_index":0,"sequence_number":5,"text":"Answer"}`},
		{Type: stream.EventResponseCompleted, Payload: `{"type":"response.completed","response":{"id":"resp-1","output":[{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Answer"}]}]}}`},
	}
	var doneIDs []string
	var phaseEnrichment *TurnTimelineItem
	for _, event := range events {
		frames, err := registry.Filter(state, event, time.Now())
		if err != nil {
			t.Fatalf("filter %s: %v", event.Type, err)
		}
		for _, frame := range frames {
			if frame.Event == streamUIEventItemUpsert {
				item, ok := frame.Payload.(TurnTimelineItem)
				if ok && item.Type == turnTimelineItemOutputText && metadataString(item.Metadata, "phase") != "" {
					phaseEnrichment = &item
				}
			}
			if frame.Event != streamUIEventItemDone {
				continue
			}
			item, ok := frame.Payload.(TurnTimelineItem)
			if ok && item.Type == turnTimelineItemOutputText {
				doneIDs = append(doneIDs, item.ID)
			}
		}
	}
	if len(doneIDs) != 1 {
		t.Fatalf("assistant done IDs = %#v, want completed response fallback to be suppressed", doneIDs)
	}
	if phaseEnrichment == nil || phaseEnrichment.ID != doneIDs[0] || metadataString(phaseEnrichment.Metadata, "phase") != "commentary" {
		t.Fatalf("phase enrichment = %#v, want same output item with commentary phase", phaseEnrichment)
	}
}

func TestPresentationReasoningDoneEmitsOneItemPerTitleParagraph(t *testing.T) {
	state := newPresentationStreamState(
		&domain.Turn{ID: "turn-1", ConversationID: "conv-1"},
		newPresentationItemRegistry(),
		nil,
	)
	registry := newPresentationEventRegistry()
	_, err := registry.Filter(state, stream.Event{
		Type:    stream.EventResponseCreated,
		Payload: `{"type":"response.created","response":{"id":"resp-1"}}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter response.created: %v", err)
	}

	frames, err := registry.Filter(state, stream.Event{
		Type: "response.reasoning_summary_text.done",
		Payload: `{"type":"response.reasoning_summary_text.done","item_id":"rs-1","output_index":0,"summary_index":0,"sequence_number":5,` +
			`"text":"**First title**\n\nFirst body.\n\n**Second title**\n\nSecond body."}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("filter reasoning done: %v", err)
	}
	if len(frames) != 2 || frames[0].Event != streamUIEventItemDone || frames[1].Event != streamUIEventItemDone {
		t.Fatalf("reasoning frames = %#v, want two item.done frames", frames)
	}
	first := frames[0].Payload.(TurnTimelineItem)
	second := frames[1].Payload.(TurnTimelineItem)
	if first.ID != "reasoning:resp-1:0:0" || second.ID != first.ID+":section:1" {
		t.Fatalf("reasoning frame IDs = %q, %q", first.ID, second.ID)
	}
}
