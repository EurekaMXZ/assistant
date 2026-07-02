package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

func TestTimelineReducerDurableAndLiveContract(t *testing.T) {
	now := time.Unix(1710010000, 0).UTC()
	tests := []struct {
		name   string
		events []stream.Event
	}{
		{
			name: "sparse response with sequences reasoning tools and completion",
			events: []stream.Event{
				{Type: stream.EventResponseCreated, Payload: `{"type":"response.created","response":{"id":"resp-contract"}}`},
				{Type: "response.output_item.added", Payload: `{"type":"response.output_item.added","output_index":0,"item":{"id":"reasoning-1","type":"reasoning"}}`},
				{Type: responseEventReasoningPartAdded, Payload: `{"item_id":"reasoning-1","output_index":0,"summary_index":0,"sequence_number":10}`},
				{Type: responseEventReasoningTextDelta, Payload: `{"item_id":"reasoning-1","output_index":0,"summary_index":0,"sequence_number":11,"delta":"**Check**\n\nInspecting."}`},
				{Type: responseEventReasoningTextDelta, Payload: `{"item_id":"reasoning-1","output_index":0,"summary_index":0,"sequence_number":11,"delta":" duplicate"}`},
				{Type: responseEventReasoningTextDelta, Payload: `{"item_id":"reasoning-1","output_index":0,"summary_index":0,"sequence_number":9,"delta":" old"}`},
				{Type: responseEventReasoningTextDone, Payload: `{"item_id":"reasoning-1","output_index":0,"summary_index":0,"sequence_number":12,"text":"**Check**\n\nInspecting."}`},
				{Type: "response.reasoning_summary_part.done", Payload: `{"item_id":"reasoning-1","output_index":0,"summary_index":0,"sequence_number":13}`},
				{Type: "response.output_item.added", Payload: `{"type":"response.output_item.added","output_index":1,"item":{"id":"message-1","type":"message","role":"assistant"}}`},
				{Type: responseEventOutputTextDelta, Payload: `{"item_id":"message-1","output_index":1,"content_index":0,"sequence_number":20,"delta":"Hel"}`},
				{Type: responseEventOutputTextDelta, Payload: `{"item_id":"message-1","output_index":1,"content_index":0,"sequence_number":20,"delta":"duplicate"}`},
				{Type: responseEventOutputTextDelta, Payload: `{"item_id":"message-1","output_index":1,"content_index":0,"sequence_number":19,"delta":"old"}`},
				{Type: responseEventOutputTextDelta, Payload: `{"item_id":"message-1","output_index":1,"content_index":0,"sequence_number":21,"delta":"lo"}`},
				{Type: responseEventOutputTextDone, Payload: `{"item_id":"message-1","output_index":1,"content_index":0,"sequence_number":22,"text":"Hello"}`},
				{Type: stream.EventToolStarted, ToolName: "internet.search", Payload: `{"tool_call_record_id":"tool-1","call_id":"call-1","tool_name":"internet.search","status":"started","arguments":{"query":"contract"}}`},
				{Type: stream.EventToolCompleted, ToolName: "internet.search", Payload: `{"tool_call_record_id":"tool-1","call_id":"call-1","tool_name":"internet.search","status":"completed","arguments":{"query":"contract"},"output":{"results":[]}}`},
				{Type: stream.EventResponseCompleted, Payload: `{"type":"response.completed","response":{"id":"resp-contract","output":[{"id":"reasoning-1","type":"reasoning","summary":[{"type":"summary_text","text":"**Check**\n\nInspecting."}]},{"id":"message-1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]},{"id":"image-1","type":"image_generation_call","status":"completed","revised_prompt":"A diagram","result":"image-data"}]}}`},
				{Type: stream.EventTurnDone},
			},
		},
		{
			name: "provider and durable failure terminals",
			events: []stream.Event{
				{Type: stream.EventResponseCreated, ResponseID: "resp-failed"},
				{Type: stream.EventResponseFailed, Payload: `{"error":{"message":"provider secret"}}`, Error: "provider secret"},
				{Type: stream.EventResponseFailed, ErrorCode: domain.TurnErrorUpstreamRequestFailed, Error: domain.TurnPublicErrorUpstreamRequestFailed},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := make([]domain.TurnStreamEvent, 0, len(tt.events))
			for index, event := range tt.events {
				raw, err := json.Marshal(event)
				if err != nil {
					t.Fatalf("marshal event %d: %v", index, err)
				}
				stored = append(stored, domain.TurnStreamEvent{
					ID: fmt.Sprintf("contract-event-%d", index+1), EventIndex: int64(index + 1),
					EventType: event.Type, Payload: raw, CreatedAt: now.Add(time.Duration(index) * time.Millisecond),
				})
			}

			durable, _, err := buildTimelineFromEvents(stored, nil)
			if err != nil {
				t.Fatalf("durable replay: %v", err)
			}
			items := newPresentationItemRegistry()
			want := items.FilterAll(durable)
			state := newPresentationStreamState(&domain.Turn{ID: "turn-contract", ConversationID: "conversation-contract"}, items, nil)
			registry := newPresentationEventRegistry()
			live := newContractLiveTimeline()
			for index, event := range tt.events {
				frames, err := registry.Filter(state, event, now.Add(time.Duration(index)*time.Millisecond))
				if err != nil {
					t.Fatalf("live event %d (%s): %v", index, event.Type, err)
				}
				live.apply(t, frames)
			}

			if !reflect.DeepEqual(live.items, want) {
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				gotJSON, _ := json.MarshalIndent(live.items, "", "  ")
				t.Fatalf("live timeline differs from durable replay\nwant: %s\n got: %s", wantJSON, gotJSON)
			}
			if live.terminals == 0 {
				t.Fatal("event sequence did not emit a terminal presentation frame")
			}
		})
	}
}

type contractLiveTimeline struct {
	items     []TurnTimelineItem
	indexes   map[string]int
	terminals int
}

func newContractLiveTimeline() *contractLiveTimeline {
	return &contractLiveTimeline{indexes: map[string]int{}}
}

func (s *contractLiveTimeline) apply(t *testing.T, frames []presentationFrame) {
	t.Helper()
	for _, frame := range frames {
		switch frame.Event {
		case streamUIEventItemUpsert, streamUIEventItemDone:
			item, ok := frame.Payload.(TurnTimelineItem)
			if !ok {
				t.Fatalf("%s payload has type %T", frame.Event, frame.Payload)
			}
			if index, exists := s.indexes[item.ID]; exists {
				s.items[index] = item
			} else {
				s.indexes[item.ID] = len(s.items)
				s.items = append(s.items, item)
			}
		case streamUIEventItemDelta:
			delta, ok := frame.Payload.(TurnStreamItemDelta)
			if !ok {
				t.Fatalf("item.delta payload has type %T", frame.Payload)
			}
			index, exists := s.indexes[delta.ItemID]
			if !exists {
				t.Fatalf("item.delta references unknown item %q", delta.ItemID)
			}
			s.items[index].ContentText += delta.Delta
		case streamUIEventTurnDone:
			if !frame.Terminal {
				t.Fatal("turn.done frame is not terminal")
			}
			s.terminals++
		}
	}
}
