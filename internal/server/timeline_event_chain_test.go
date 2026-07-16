package server

import (
	"testing"

	"github.com/EurekaMXZ/assistant/internal/stream"
)

type recordingTimelineEventHandler struct {
	eventTypes []string
	calls      *int
}

func (h recordingTimelineEventHandler) EventTypes() []string {
	return h.eventTypes
}

func (h recordingTimelineEventHandler) Handle(_ *timelineReducer, _ normalizedTimelineEvent) ([]timelineMutation, error) {
	*h.calls++
	return nil, nil
}

func TestTimelineEventChainStopsAfterFirstMatchingHandler(t *testing.T) {
	firstCalls := 0
	secondCalls := 0
	chain := &timelineEventChain{handlers: []timelineEventHandler{
		recordingTimelineEventHandler{eventTypes: []string{"event.delta"}, calls: &firstCalls},
		recordingTimelineEventHandler{eventTypes: []string{"event.delta"}, calls: &secondCalls},
	}}

	_, handled, err := chain.Handle(&timelineReducer{}, normalizedTimelineEvent{Event: stream.Event{Type: "event.delta"}})
	if err != nil {
		t.Fatalf("handle event: %v", err)
	}
	if !handled || firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("handled = %v, calls = (%d, %d), want first handler only", handled, firstCalls, secondCalls)
	}
}

func TestTimelineEventChainRejectsDuplicateRegistrations(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate event registration did not panic")
		}
	}()
	calls := 0
	newTimelineEventChain(
		recordingTimelineEventHandler{eventTypes: []string{"event.delta"}, calls: &calls},
		recordingTimelineEventHandler{eventTypes: []string{"event.delta"}, calls: &calls},
	)
}

func TestTimelineEventChainReportsHandledWithoutMutations(t *testing.T) {
	reducer := newTimelineReducer(nil, nil, nil)
	mutations, handled, err := reducer.eventChain.Handle(reducer, normalizedTimelineEvent{
		Event: stream.Event{Type: stream.EventResponseStarted},
	})
	if err != nil {
		t.Fatalf("handle response.started: %v", err)
	}
	if !handled || len(mutations) != 0 {
		t.Fatalf("handled = %v, mutations = %#v, want handled with no output", handled, mutations)
	}
}
