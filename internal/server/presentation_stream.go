package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/EurekaMXZ/assistant/internal/tool"
)

const presentationEventConversationUpdated = "conversation.updated"

type presentationFrame struct {
	Event    string
	Payload  any
	Terminal bool
}

type presentationEventRegistry struct{}

func newPresentationEventRegistry() *presentationEventRegistry {
	return &presentationEventRegistry{}
}

func (r *presentationEventRegistry) Filter(state *presentationStreamState, event stream.Event, createdAt time.Time) ([]presentationFrame, error) {
	if r == nil || state == nil {
		return nil, nil
	}
	if event.Type == stream.EventConversationUpdated {
		return handlePresentationConversationUpdated(state, event, createdAt)
	}
	if !isTimelineReducerEvent(event.Type) {
		return nil, nil
	}
	if event.EventIndex > 0 && event.EventIndex <= state.snapshotEventIndex {
		return nil, nil
	}
	state.reducer.outputSlots = state.outputSlots
	mutations, err := state.reducer.Apply(normalizedTimelineEvent{Event: event, CreatedAt: createdAt})
	if err != nil {
		return nil, err
	}
	return state.frames(mutations), nil
}

type presentationItemHandler func(TurnTimelineItem) TurnTimelineItem

type presentationItemRegistry struct {
	handlers map[string]presentationItemHandler
}

func newPresentationItemRegistry() *presentationItemRegistry {
	registry := &presentationItemRegistry{handlers: map[string]presentationItemHandler{}}
	registry.Register(turnTimelineItemOutputText, filterOutputTextPresentationItem)
	registry.Register(turnTimelineItemReasoning, filterReasoningPresentationItem)
	registry.Register(turnTimelineItemToolCall, filterToolPresentationItem)
	registry.Register(turnTimelineItemImageGeneration, filterImagePresentationItem)
	registry.Register(turnTimelineItemStatus, filterStatusPresentationItem)
	return registry
}

func (r *presentationItemRegistry) Register(itemType string, handler presentationItemHandler) {
	if r == nil || strings.TrimSpace(itemType) == "" || handler == nil {
		return
	}
	r.handlers[strings.TrimSpace(itemType)] = handler
}

func (r *presentationItemRegistry) Filter(item TurnTimelineItem) (TurnTimelineItem, bool) {
	if r == nil {
		return TurnTimelineItem{}, false
	}
	handler, ok := r.handlers[strings.TrimSpace(item.Type)]
	if !ok {
		return TurnTimelineItem{}, false
	}
	return handler(item), true
}

func (r *presentationItemRegistry) FilterAll(items []TurnTimelineItem) []TurnTimelineItem {
	filtered := make([]TurnTimelineItem, 0, len(items))
	for _, item := range items {
		if next, ok := r.Filter(item); ok {
			filtered = append(filtered, next)
		}
	}
	return filtered
}

func filterOutputTextPresentationItem(item TurnTimelineItem) TurnTimelineItem {
	metadata := map[string]any{
		"phase": metadataString(item.Metadata, "phase"),
	}
	if sequence, ok := metadataInt(item.Metadata, "sequence_number"); ok && item.ContentText != "" {
		metadata["sequence_number"] = sequence
	}
	return TurnTimelineItem{
		ID:          item.ID,
		Type:        turnTimelineItemOutputText,
		Title:       "Assistant",
		Status:      item.Status,
		ContentText: item.ContentText,
		Metadata:    compactMetadata(metadata),
		CreatedAt:   item.CreatedAt,
	}
}

func filterReasoningPresentationItem(item TurnTimelineItem) TurnTimelineItem {
	metadata := map[string]any{}
	if sequence, ok := metadataInt(item.Metadata, "sequence_number"); ok && item.ContentText != "" {
		metadata["sequence_number"] = sequence
	}
	return TurnTimelineItem{
		ID:          item.ID,
		Type:        turnTimelineItemReasoning,
		Title:       "Reasoning",
		Status:      item.Status,
		ContentText: item.ContentText,
		Metadata:    compactMetadata(metadata),
		CreatedAt:   item.CreatedAt,
	}
}

func filterToolPresentationItem(item TurnTimelineItem) TurnTimelineItem {
	toolName := metadataString(item.Metadata, "tool_name")
	if toolName == "" {
		toolName = strings.TrimSpace(item.Title)
	}
	presentation := tool.BuildPublicToolPresentation(
		"",
		"",
		toolName,
		item.Status,
		item.Arguments,
		item.Output,
		"",
	)
	links := make([]TurnTimelineLink, 0, len(presentation.Links))
	for _, link := range presentation.Links {
		links = append(links, TurnTimelineLink{URL: link.URL, Label: link.Label})
	}
	if len(links) == 0 {
		links = nil
	}
	title := toolName
	if presentation.Title != "" {
		title = presentation.Title
	}
	summary := presentation.Summary
	details := presentation.Details
	if presentation.Title != "" {
		summary = ""
		details = nil
	}
	return TurnTimelineItem{
		ID:               item.ID,
		Type:             turnTimelineItemToolCall,
		Title:            title,
		Status:           item.Status,
		Summary:          summary,
		Details:          details,
		InputLabel:       presentation.InputLabel,
		InputText:        presentation.InputText,
		Links:            links,
		Command:          presentation.Command,
		WorkingDirectory: presentation.WorkingDirectory,
		CommandOutput:    presentation.CommandOutput,
		ExitCode:         presentation.ExitCode,
		TimedOut:         presentation.TimedOut,
		Metadata: compactMetadata(map[string]any{
			"tool_name": toolName,
		}),
		CreatedAt: item.CreatedAt,
	}
}

func filterImagePresentationItem(item TurnTimelineItem) TurnTimelineItem {
	return TurnTimelineItem{
		ID:          item.ID,
		Type:        turnTimelineItemImageGeneration,
		Title:       item.Title,
		Status:      item.Status,
		ContentText: item.ContentText,
		CreatedAt:   item.CreatedAt,
	}
}

func filterStatusPresentationItem(item TurnTimelineItem) TurnTimelineItem {
	return TurnTimelineItem{
		ID:          item.ID,
		Type:        turnTimelineItemStatus,
		Title:       "Status",
		Status:      item.Status,
		ContentText: item.ContentText,
		CreatedAt:   item.CreatedAt,
	}
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

type presentationStreamState struct {
	items              *presentationItemRegistry
	reducer            *timelineReducer
	outputSlots        *responseOutputSlotResolver
	snapshotEventIndex int64
}

func newPresentationStreamState(turn *domain.Turn, items *presentationItemRegistry, snapshot []TurnTimelineItem) *presentationStreamState {
	reducer := newTimelineReducer(turn, snapshot, nil)
	return &presentationStreamState{items: items, reducer: reducer, outputSlots: reducer.outputSlots}
}

func responseIDFromPresentationItemID(itemID string, itemType string) string {
	prefix := strings.TrimSpace(itemType) + ":"
	remainder := strings.TrimPrefix(strings.TrimSpace(itemID), prefix)
	if remainder == itemID {
		return ""
	}
	responseID, _, _ := strings.Cut(remainder, ":")
	return strings.TrimSpace(responseID)
}

func metadataInt(metadata map[string]any, key string) (int, bool) {
	switch value := metadata[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func (s *presentationStreamState) responseID(candidate string) string {
	return s.reducer.responseID(candidate)
}

func (s *presentationStreamState) frames(mutations []timelineMutation) []presentationFrame {
	frames := make([]presentationFrame, 0, len(mutations))
	for _, mutation := range mutations {
		switch mutation.Kind {
		case timelineMutationUpsert, timelineMutationDone:
			item, ok := s.items.Filter(mutation.Item)
			if !ok {
				continue
			}
			eventType := streamUIEventItemUpsert
			if mutation.Kind == timelineMutationDone {
				eventType = streamUIEventItemDone
			}
			frames = append(frames, presentationFrame{Event: eventType, Payload: item})
		case timelineMutationDelta:
			frames = append(frames, presentationFrame{Event: streamUIEventItemDelta, Payload: mutation.Delta})
		case timelineMutationTerminal:
			frames = append(frames, presentationFrame{Event: streamUIEventTurnDone, Payload: mutation.Terminal, Terminal: true})
		}
	}
	return frames
}

func presentationFailure(errorCode string) (string, string) {
	errorCode = strings.TrimSpace(errorCode)
	switch errorCode {
	case domain.TurnErrorUpstreamRequestFailed:
		return errorCode, domain.TurnPublicErrorUpstreamRequestFailed
	case domain.TurnErrorToolStepLimitExceeded:
		return errorCode, domain.TurnPublicErrorToolStepLimitExceeded
	case domain.TurnErrorContextLoadFailed,
		domain.TurnErrorSandboxScopeFailed,
		domain.TurnErrorRequestPrepareFailed,
		domain.TurnErrorRequestBlobFailed,
		domain.TurnErrorModelStreamFailed,
		domain.TurnErrorBackendRequestFailed,
		domain.TurnErrorResponseBlobFailed,
		domain.TurnErrorGeneratedImageFailed,
		domain.TurnErrorModelContextBlobFailed,
		domain.TurnErrorTurnFinalizeFailed:
		return errorCode, domain.TurnPublicErrorRequestProcessing
	default:
		return "", domain.TurnPublicErrorRequestProcessing
	}
}

type conversationPresentationUpdate struct {
	ConversationID string  `json:"conversation_id"`
	Title          *string `json:"title,omitempty"`
}

func handlePresentationConversationUpdated(_ *presentationStreamState, event stream.Event, _ time.Time) ([]presentationFrame, error) {
	var payload struct {
		ConversationID string  `json:"conversation_id"`
		Title          *string `json:"title"`
	}
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		return nil, fmt.Errorf("decode conversation presentation update: %w", err)
	}
	payload.ConversationID = strings.TrimSpace(payload.ConversationID)
	if payload.ConversationID == "" {
		payload.ConversationID = strings.TrimSpace(event.ConversationID)
	}
	return []presentationFrame{{
		Event: presentationEventConversationUpdated,
		Payload: conversationPresentationUpdate{
			ConversationID: payload.ConversationID,
			Title:          payload.Title,
		},
	}}, nil
}
