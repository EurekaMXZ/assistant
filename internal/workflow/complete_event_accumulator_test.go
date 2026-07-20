package workflow

import (
	"strings"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

func TestCompleteEventAccumulatorOrdersAndDeduplicatesDeltas(t *testing.T) {
	accumulator := NewCompleteEventAccumulator()
	base := stream.Event{ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ItemID: "item-1"}
	for _, event := range []stream.Event{
		{Type: "response.output_text.delta", ConversationID: base.ConversationID, TurnID: base.TurnID, RunID: base.RunID, ItemID: base.ItemID, TransportSeq: 1, Delta: "a"},
		{Type: "response.output_text.delta", ConversationID: base.ConversationID, TurnID: base.TurnID, RunID: base.RunID, ItemID: base.ItemID, TransportSeq: 1, Delta: "duplicate"},
		{Type: "response.output_text.delta", ConversationID: base.ConversationID, TurnID: base.TurnID, RunID: base.RunID, ItemID: base.ItemID, TransportSeq: 2, Delta: "b"},
	} {
		if completed, err := accumulator.Apply(event); err != nil || len(completed) != 0 {
			t.Fatalf("delta apply = %#v, %v", completed, err)
		}
	}
	completed, err := accumulator.Apply(stream.Event{Type: "response.output_text.done", ConversationID: base.ConversationID, TurnID: base.TurnID, RunID: base.RunID, ItemID: base.ItemID})
	if err != nil || len(completed) != 1 || !strings.Contains(string(completed[0].Payload), `"text":"ab"`) {
		t.Fatalf("completed = %#v, %v", completed, err)
	}
}

func TestCompleteEventAccumulatorFlushesCancellationAsInterrupted(t *testing.T) {
	accumulator := NewCompleteEventAccumulator()
	_, _ = accumulator.Apply(stream.Event{Type: "response.output_text.delta", ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1", ItemID: "item-1", TransportSeq: 1, Delta: "partial"})
	completed, err := accumulator.Apply(stream.Event{Type: domain.ConversationEventRunCancelled, ConversationID: "conv-1", TurnID: "turn-1", RunID: "run-1"})
	if err != nil || len(completed) != 2 || completed[0].EventType != domain.ConversationEventOutputTextInterrupted || completed[1].EventType != domain.ConversationEventRunCancelled {
		t.Fatalf("cancel flush = %#v, %v", completed, err)
	}
}
