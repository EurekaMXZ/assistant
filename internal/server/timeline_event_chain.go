package server

import (
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

type timelineEventHandler interface {
	EventTypes() []string
	Handle(*timelineReducer, normalizedTimelineEvent) ([]timelineMutation, error)
}

type timelineEventChain struct {
	handlers []timelineEventHandler
}

func newTimelineEventChain(handlers ...timelineEventHandler) *timelineEventChain {
	registered := map[string]struct{}{}
	chain := &timelineEventChain{handlers: append([]timelineEventHandler(nil), handlers...)}
	for _, handler := range chain.handlers {
		for _, eventType := range handler.EventTypes() {
			eventType = strings.TrimSpace(eventType)
			if eventType == "" {
				panic("timeline event handler registered an empty event type")
			}
			if _, exists := registered[eventType]; exists {
				panic("timeline event handler registered duplicate event type: " + eventType)
			}
			registered[eventType] = struct{}{}
		}
	}
	return chain
}

func newDefaultTimelineEventChain() *timelineEventChain {
	return newTimelineEventChain(
		responseStartedTimelineHandler{},
		responseCreatedTimelineHandler{},
		responseOutputItemTimelineHandler{},
		outputTextDeltaTimelineHandler{},
		outputTextDoneTimelineHandler{},
		reasoningPartAddedTimelineHandler{},
		reasoningTextDeltaTimelineHandler{},
		reasoningTextDoneTimelineHandler{},
		reasoningPartDoneTimelineHandler{},
		reasoningSummaryTimelineHandler{},
		toolTimelineHandler{},
		interactionTimelineHandler{},
		responseCompletedTimelineHandler{},
		responseFailedTimelineHandler{},
		turnDoneTimelineHandler{},
	)
}

func (c *timelineEventChain) EventTypes() []string {
	if c == nil {
		return nil
	}
	eventTypes := make([]string, 0)
	for _, handler := range c.handlers {
		eventTypes = append(eventTypes, handler.EventTypes()...)
	}
	return eventTypes
}

func (c *timelineEventChain) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, bool, error) {
	if c == nil || reducer == nil {
		return nil, false, nil
	}
	eventType := strings.TrimSpace(input.Event.Type)
	for _, handler := range c.handlers {
		if !timelineHandlerAccepts(handler, eventType) {
			continue
		}
		mutations, err := handler.Handle(reducer, input)
		return mutations, true, err
	}
	return nil, false, nil
}

func timelineHandlerAccepts(handler timelineEventHandler, eventType string) bool {
	for _, registered := range handler.EventTypes() {
		if strings.TrimSpace(registered) == eventType {
			return true
		}
	}
	return false
}

type responseStartedTimelineHandler struct{}

func (responseStartedTimelineHandler) EventTypes() []string {
	return []string{stream.EventResponseStarted}
}

func (responseStartedTimelineHandler) Handle(_ *timelineReducer, _ normalizedTimelineEvent) ([]timelineMutation, error) {
	return nil, nil
}

type responseCreatedTimelineHandler struct{}

func (responseCreatedTimelineHandler) EventTypes() []string {
	return []string{stream.EventResponseCreated}
}

func (responseCreatedTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	payload, _ := decodeResponseStreamPayload(input.Event)
	responseID := payload.ResponseID
	if payload.Response != nil && strings.TrimSpace(payload.Response.ID) != "" {
		responseID = payload.Response.ID
	}
	responseID = reducer.responseID(responseID)
	return reducer.appendReasoningForResponse(responseID, true), nil
}

type responseOutputItemTimelineHandler struct{}

func (responseOutputItemTimelineHandler) EventTypes() []string {
	return []string{"response.output_item.added", "response.output_item.done"}
}

func (responseOutputItemTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	payload, ok := decodeResponseStreamPayload(input.Event)
	if ok && payload.Item != nil {
		reducer.outputSlots.track(reducer.responseID(payload.ResponseID), payload.OutputIndex, *payload.Item)
	}
	return nil, nil
}

type outputTextDeltaTimelineHandler struct{}

func (outputTextDeltaTimelineHandler) EventTypes() []string {
	return []string{responseEventOutputTextDelta}
}

func (outputTextDeltaTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceOutputText(input.Event, input.CreatedAt, true)
}

type outputTextDoneTimelineHandler struct{}

func (outputTextDoneTimelineHandler) EventTypes() []string {
	return []string{responseEventOutputTextDone}
}

func (outputTextDoneTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceOutputText(input.Event, input.CreatedAt, false)
}

type reasoningPartAddedTimelineHandler struct{}

func (reasoningPartAddedTimelineHandler) EventTypes() []string {
	return []string{responseEventReasoningPartAdded}
}

func (reasoningPartAddedTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceReasoning(input.Event, input.CreatedAt, "added")
}

type reasoningTextDeltaTimelineHandler struct{}

func (reasoningTextDeltaTimelineHandler) EventTypes() []string {
	return []string{responseEventReasoningTextDelta}
}

func (reasoningTextDeltaTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceReasoning(input.Event, input.CreatedAt, "delta")
}

type reasoningTextDoneTimelineHandler struct{}

func (reasoningTextDoneTimelineHandler) EventTypes() []string {
	return []string{responseEventReasoningTextDone}
}

func (reasoningTextDoneTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceReasoning(input.Event, input.CreatedAt, "done")
}

type reasoningPartDoneTimelineHandler struct{}

func (reasoningPartDoneTimelineHandler) EventTypes() []string {
	return []string{"response.reasoning_summary_part.done"}
}

func (reasoningPartDoneTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceReasoning(input.Event, input.CreatedAt, "part_done")
}

type reasoningSummaryTimelineHandler struct{}

func (reasoningSummaryTimelineHandler) EventTypes() []string {
	return []string{stream.EventReasoningSummary}
}

func (reasoningSummaryTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceReasoningSummary(input.Event, input.CreatedAt)
}

type toolTimelineHandler struct{}

func (toolTimelineHandler) EventTypes() []string {
	return []string{stream.EventToolStarted, stream.EventToolCompleted, stream.EventToolFailed}
}

type interactionTimelineHandler struct{}

func (interactionTimelineHandler) EventTypes() []string {
	return []string{stream.EventInteractionAwaiting, stream.EventInteractionDone, stream.EventInteractionCancelled}
}

func (interactionTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceInteraction(input.Event, input.CreatedAt)
}

func (toolTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceTool(input.Event, input.CreatedAt)
}

type responseCompletedTimelineHandler struct{}

func (responseCompletedTimelineHandler) EventTypes() []string {
	return []string{stream.EventResponseCompleted}
}

func (responseCompletedTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceResponseCompleted(input.Event, input.CreatedAt)
}

type responseFailedTimelineHandler struct{}

func (responseFailedTimelineHandler) EventTypes() []string {
	return []string{stream.EventResponseFailed}
}

func (responseFailedTimelineHandler) Handle(reducer *timelineReducer, input normalizedTimelineEvent) ([]timelineMutation, error) {
	return reducer.reduceResponseFailed(input.Event, input.CreatedAt), nil
}

type turnDoneTimelineHandler struct{}

func (turnDoneTimelineHandler) EventTypes() []string {
	return []string{stream.EventTurnDone}
}

func (turnDoneTimelineHandler) Handle(reducer *timelineReducer, _ normalizedTimelineEvent) ([]timelineMutation, error) {
	return []timelineMutation{{Kind: timelineMutationTerminal, Terminal: reducer.turnDone(domain.TurnStatusCompleted, "", "")}}, nil
}
