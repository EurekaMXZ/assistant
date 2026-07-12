package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

const (
	timelineMutationUpsert   = "upsert"
	timelineMutationDelta    = "delta"
	timelineMutationDone     = "done"
	timelineMutationTerminal = "terminal"

	responseEventOutputTextDelta    = "response.output_text.delta"
	responseEventOutputTextDone     = "response.output_text.done"
	responseEventReasoningPartAdded = "response.reasoning_summary_part.added"
	responseEventReasoningTextDelta = "response.reasoning_summary_text.delta"
	responseEventReasoningTextDone  = "response.reasoning_summary_text.done"
)

type normalizedTimelineEvent struct {
	Event     stream.Event
	CreatedAt time.Time
}

type timelineMutation struct {
	Kind     string
	Item     TurnTimelineItem
	Delta    TurnStreamItemDelta
	Terminal TurnStreamDone
}

type responseOutputSlotResolver struct {
	byItem  map[string]int
	byIndex map[string]int
	next    map[string]int
}

func newResponseOutputSlotResolver() *responseOutputSlotResolver {
	return &responseOutputSlotResolver{
		byItem:  map[string]int{},
		byIndex: map[string]int{},
		next:    map[string]int{},
	}
}

func outputSlotKey(responseID string, itemType string) string {
	return strings.TrimSpace(responseID) + "\x00" + strings.TrimSpace(itemType)
}

func (r *responseOutputSlotResolver) track(responseID string, rawIndex int, item responseOutputItemPayload) {
	if r == nil || strings.TrimSpace(responseID) == "" || strings.TrimSpace(item.Type) == "" {
		return
	}
	key := outputSlotKey(responseID, item.Type)
	indexKey := fmt.Sprintf("%s\x00%d", key, rawIndex)
	if _, exists := r.byIndex[indexKey]; exists {
		return
	}
	slot := r.next[key]
	r.next[key] = slot + 1
	r.byIndex[indexKey] = slot
	if itemID := strings.TrimSpace(item.ID); itemID != "" {
		r.byItem[key+"\x00"+itemID] = slot
	}
}

func (r *responseOutputSlotResolver) bind(responseID string, itemType string, itemID string, slot int) {
	if r == nil || strings.TrimSpace(responseID) == "" || strings.TrimSpace(itemType) == "" || strings.TrimSpace(itemID) == "" {
		return
	}
	key := outputSlotKey(responseID, itemType)
	r.byItem[key+"\x00"+strings.TrimSpace(itemID)] = slot
	if r.next[key] <= slot {
		r.next[key] = slot + 1
	}
}

func (r *responseOutputSlotResolver) resolve(responseID string, itemType string, itemID string, rawIndex int) (int, bool) {
	if r == nil {
		return 0, false
	}
	key := outputSlotKey(responseID, itemType)
	if itemID = strings.TrimSpace(itemID); itemID != "" {
		if slot, ok := r.byItem[key+"\x00"+itemID]; ok {
			return slot, true
		}
	}
	slot, ok := r.byIndex[fmt.Sprintf("%s\x00%d", key, rawIndex)]
	return slot, ok
}

type timelineReducer struct {
	turn              *domain.Turn
	items             []TurnTimelineItem
	itemIndexes       map[string]int
	lastSequences     map[string]int
	lastResponseID    string
	outputSlots       *responseOutputSlotResolver
	assistantDone     map[string]struct{}
	reasoningDone     map[string]struct{}
	reasoningFallback []reasoningTimelineItem
	reasoningUsed     []bool
	eventIndex        int64
	hasAssistantText  bool
}

func isTimelineReducerEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case stream.EventResponseStarted,
		stream.EventResponseCreated,
		"response.output_item.added",
		"response.output_item.done",
		responseEventOutputTextDelta,
		responseEventOutputTextDone,
		responseEventReasoningPartAdded,
		responseEventReasoningTextDelta,
		responseEventReasoningTextDone,
		"response.reasoning_summary_part.done",
		stream.EventReasoningSummary,
		stream.EventToolStarted,
		stream.EventToolCompleted,
		stream.EventToolFailed,
		stream.EventResponseCompleted,
		stream.EventResponseFailed,
		stream.EventTurnDone:
		return true
	default:
		return false
	}
}

func newTimelineReducer(turn *domain.Turn, snapshot []TurnTimelineItem, reasoning []reasoningTimelineItem) *timelineReducer {
	r := &timelineReducer{
		turn:              turn,
		items:             append([]TurnTimelineItem(nil), snapshot...),
		itemIndexes:       make(map[string]int, len(snapshot)),
		lastSequences:     make(map[string]int, len(snapshot)),
		outputSlots:       newResponseOutputSlotResolver(),
		assistantDone:     map[string]struct{}{},
		reasoningDone:     map[string]struct{}{},
		reasoningFallback: append([]reasoningTimelineItem(nil), reasoning...),
		reasoningUsed:     make([]bool, len(reasoning)),
	}
	for index, item := range r.items {
		r.itemIndexes[item.ID] = index
		if sequence, ok := metadataInt(item.Metadata, "sequence_number"); ok {
			r.lastSequences[item.ID] = sequence
		}
		if responseID := metadataString(item.Metadata, "response_id"); responseID != "" {
			r.lastResponseID = responseID
		}
		if item.Status != "completed" {
			continue
		}
		switch item.Type {
		case turnTimelineItemOutputText:
			r.assistantDone[item.ID] = struct{}{}
			r.hasAssistantText = r.hasAssistantText || strings.TrimSpace(item.ContentText) != ""
		case turnTimelineItemReasoning:
			responseID := metadataString(item.Metadata, "response_id")
			if responseID == "" {
				responseID = responseIDFromPresentationItemID(item.ID, "reasoning")
			}
			r.reasoningDone[reasoningContentKey(responseID, item.ContentText)] = struct{}{}
		}
	}
	return r
}

func (r *timelineReducer) responseID(candidate string) string {
	if candidate = strings.TrimSpace(candidate); candidate != "" {
		r.lastResponseID = candidate
	}
	return strings.TrimSpace(r.lastResponseID)
}

func (r *timelineReducer) Apply(input normalizedTimelineEvent) ([]timelineMutation, error) {
	r.eventIndex++
	event := input.Event
	switch event.Type {
	case stream.EventResponseStarted:
		return nil, nil
	case stream.EventResponseCreated:
		payload, _ := decodeResponseStreamPayload(event)
		responseID := payload.ResponseID
		if payload.Response != nil && strings.TrimSpace(payload.Response.ID) != "" {
			responseID = payload.Response.ID
		}
		responseID = r.responseID(responseID)
		return r.appendReasoningForResponse(responseID, true), nil
	case "response.output_item.added", "response.output_item.done":
		payload, ok := decodeResponseStreamPayload(event)
		if ok && payload.Item != nil {
			r.outputSlots.track(r.responseID(payload.ResponseID), payload.OutputIndex, *payload.Item)
		}
		return nil, nil
	case responseEventOutputTextDelta:
		return r.reduceOutputText(event, input.CreatedAt, true)
	case responseEventOutputTextDone:
		return r.reduceOutputText(event, input.CreatedAt, false)
	case responseEventReasoningPartAdded:
		return r.reduceReasoning(event, input.CreatedAt, "added")
	case responseEventReasoningTextDelta:
		return r.reduceReasoning(event, input.CreatedAt, "delta")
	case responseEventReasoningTextDone:
		return r.reduceReasoning(event, input.CreatedAt, "done")
	case "response.reasoning_summary_part.done":
		return r.reduceReasoning(event, input.CreatedAt, "part_done")
	case stream.EventReasoningSummary:
		return r.reduceReasoningSummary(event, input.CreatedAt)
	case stream.EventToolStarted, stream.EventToolCompleted, stream.EventToolFailed:
		return r.reduceTool(event, input.CreatedAt)
	case stream.EventResponseCompleted:
		return r.reduceResponseCompleted(event, input.CreatedAt)
	case stream.EventResponseFailed:
		return r.reduceResponseFailed(event, input.CreatedAt), nil
	case stream.EventTurnDone:
		return []timelineMutation{{Kind: timelineMutationTerminal, Terminal: r.turnDone(domain.TurnStatusCompleted, "", "")}}, nil
	default:
		return nil, nil
	}
}

func (r *timelineReducer) FinalItems() []TurnTimelineItem {
	r.appendRemainingReasoning()
	return splitReasoningTimelineItems(append([]TurnTimelineItem(nil), r.items...))
}

func (r *timelineReducer) HasAssistantText() bool {
	return r.hasAssistantText
}

func (r *timelineReducer) acceptSequence(itemID string, sequence *int) bool {
	if sequence == nil {
		return true
	}
	if previous, ok := r.lastSequences[itemID]; ok && *sequence <= previous {
		return false
	}
	r.lastSequences[itemID] = *sequence
	return true
}

func (r *timelineReducer) item(itemID string) (TurnTimelineItem, bool) {
	index, ok := r.itemIndexes[itemID]
	if !ok {
		return TurnTimelineItem{}, false
	}
	return r.items[index], true
}

func (r *timelineReducer) store(item TurnTimelineItem) {
	if index, ok := r.itemIndexes[item.ID]; ok {
		r.items[index] = item
		return
	}
	r.itemIndexes[item.ID] = len(r.items)
	r.items = append(r.items, item)
}

func (r *timelineReducer) reduceOutputText(event stream.Event, createdAt time.Time, delta bool) ([]timelineMutation, error) {
	payload, ok := decodeResponseStreamPayload(event)
	if !ok {
		return nil, nil
	}
	text := payload.Text
	if delta {
		text = payload.Delta
		if text == "" {
			return nil, nil
		}
	} else if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	responseID := r.responseID(payload.ResponseID)
	outputSlot := payload.OutputIndex
	if slot, tracked := r.outputSlots.resolve(responseID, "message", payload.ItemID, payload.OutputIndex); tracked {
		outputSlot = slot
	}
	itemID := stableTimelineAssistantTextItemID(responseID, payload.ItemID, outputSlot, payload.ContentIndex, "", r.eventIndex)
	if !r.acceptSequence(itemID, payload.SequenceNumber) {
		return nil, nil
	}
	metadata := compactMetadata(map[string]any{
		"response_id":     responseID,
		"item_id":         strings.TrimSpace(payload.ItemID),
		"output_index":    outputSlot,
		"content_index":   payload.ContentIndex,
		"phase":           strings.TrimSpace(payload.Phase),
		"sequence_number": sequenceValue(payload.SequenceNumber),
	})
	item, exists := r.item(itemID)
	mutations := make([]timelineMutation, 0, 2)
	if !exists {
		item = TurnTimelineItem{ID: itemID, Type: turnTimelineItemOutputText, Title: "Assistant", Status: "streaming", Metadata: metadata, CreatedAt: createdAt}
		r.store(item)
		if delta {
			mutations = append(mutations, timelineMutation{Kind: timelineMutationUpsert, Item: item})
		}
	}
	if delta {
		item.ContentText += text
		item.Status = "streaming"
		item.Metadata = mergeTimelineMetadata(item.Metadata, metadata)
		r.store(item)
		mutations = append(mutations, timelineMutation{Kind: timelineMutationDelta, Delta: TurnStreamItemDelta{
			ItemID: itemID, ItemType: turnTimelineItemOutputText, Delta: text, SequenceNumber: payload.SequenceNumber, CreatedAt: createdAt,
		}})
		return mutations, nil
	}
	item.ContentText = text
	item.Status = "completed"
	item.Metadata = mergeTimelineMetadata(item.Metadata, metadata)
	r.store(item)
	r.assistantDone[itemID] = struct{}{}
	r.hasAssistantText = true
	return []timelineMutation{{Kind: timelineMutationDone, Item: item}}, nil
}

func (r *timelineReducer) reasoningItem(payload responseStreamPayload, status string, text string, createdAt time.Time) TurnTimelineItem {
	responseID := r.responseID(payload.ResponseID)
	outputSlot := payload.OutputIndex
	if slot, tracked := r.outputSlots.resolve(responseID, "reasoning", payload.ItemID, payload.OutputIndex); tracked {
		outputSlot = slot
	}
	return TurnTimelineItem{
		ID:   stableTimelineReasoningPartID(responseID, payload.ItemID, outputSlot, payload.SummaryIndex, "", int(r.eventIndex)),
		Type: turnTimelineItemReasoning, Title: "Reasoning", Status: status, ContentText: text,
		Metadata: compactMetadata(map[string]any{
			"response_id": responseID, "item_id": strings.TrimSpace(payload.ItemID), "output_index": outputSlot,
			"summary_index": payload.SummaryIndex, "sequence_number": sequenceValue(payload.SequenceNumber),
		}),
		CreatedAt: createdAt,
	}
}

func (r *timelineReducer) reduceReasoning(event stream.Event, createdAt time.Time, action string) ([]timelineMutation, error) {
	payload, ok := decodeResponseStreamPayload(event)
	if !ok {
		return nil, nil
	}
	item := r.reasoningItem(payload, "streaming", "", createdAt)
	if !r.acceptSequence(item.ID, payload.SequenceNumber) {
		return nil, nil
	}
	existing, exists := r.item(item.ID)
	if exists {
		item = existing
		item.Metadata = mergeTimelineMetadata(item.Metadata, r.reasoningItem(payload, "", "", createdAt).Metadata)
	}
	switch action {
	case "added":
		if exists {
			return nil, nil
		}
		r.store(item)
		return []timelineMutation{{Kind: timelineMutationUpsert, Item: item}}, nil
	case "delta":
		if payload.Delta == "" {
			return nil, nil
		}
		mutations := make([]timelineMutation, 0, 2)
		if !exists {
			r.store(item)
			mutations = append(mutations, timelineMutation{Kind: timelineMutationUpsert, Item: item})
		}
		item.ContentText += payload.Delta
		item.Status = "streaming"
		r.store(item)
		mutations = append(mutations, timelineMutation{Kind: timelineMutationDelta, Delta: TurnStreamItemDelta{
			ItemID: item.ID, ItemType: turnTimelineItemReasoning, Delta: payload.Delta, SequenceNumber: payload.SequenceNumber, CreatedAt: createdAt,
		}})
		return mutations, nil
	case "done":
		if strings.TrimSpace(payload.Text) == "" {
			return nil, nil
		}
		item.ContentText = payload.Text
	case "part_done":
		if !exists {
			r.store(item)
		}
	}
	item.Status = "completed"
	r.store(item)
	responseID := metadataString(item.Metadata, "response_id")
	sections := splitReasoningTimelineItem(item)
	mutations := make([]timelineMutation, 0, len(sections))
	for _, section := range sections {
		r.reasoningDone[reasoningContentKey(responseID, section.ContentText)] = struct{}{}
		mutations = append(mutations, timelineMutation{Kind: timelineMutationDone, Item: section})
	}
	return mutations, nil
}

func (r *timelineReducer) reduceReasoningSummary(event stream.Event, createdAt time.Time) ([]timelineMutation, error) {
	responseID := reasoningSummaryResponseID(event)
	if responseID == "" {
		responseID = r.responseID(event.ResponseID)
	} else {
		r.responseID(responseID)
	}
	if strings.TrimSpace(event.ResponseID) == "" {
		event.ResponseID = responseID
	}
	items, err := decodeReasoningTimelineItems(event, "", r.eventIndex, createdAt)
	if err != nil {
		return nil, err
	}
	mutations := make([]timelineMutation, 0, len(items))
	for _, item := range items {
		mutations = append(mutations, r.addReasoningFallbackItem(responseID, item)...)
	}
	return mutations, nil
}

func (r *timelineReducer) addReasoningFallbackItem(responseID string, item TurnTimelineItem) []timelineMutation {
	if reasoningContentCovered(r.reasoningDone, responseID, item.ContentText) {
		return nil
	}
	sections := splitReasoningTimelineItem(item)
	mutations := make([]timelineMutation, 0, len(sections))
	for _, section := range sections {
		key := reasoningContentKey(responseID, section.ContentText)
		if _, exists := r.reasoningDone[key]; exists {
			continue
		}
		r.reasoningDone[key] = struct{}{}
		r.store(section)
		mutations = append(mutations, timelineMutation{Kind: timelineMutationDone, Item: section})
	}
	return mutations
}

func (r *timelineReducer) appendReasoningForResponse(responseID string, includePending bool) []timelineMutation {
	mutations := make([]timelineMutation, 0)
	for index, reasoning := range r.reasoningFallback {
		if r.reasoningUsed[index] {
			continue
		}
		itemResponseID := strings.TrimSpace(reasoning.ResponseID)
		if responseID != "" && itemResponseID == responseID {
			r.reasoningUsed[index] = true
			mutations = append(mutations, r.addReasoningFallbackItem(responseID, turnTimelineReasoningItem(reasoning))...)
		}
	}
	if includePending {
		for index, reasoning := range r.reasoningFallback {
			if r.reasoningUsed[index] || strings.TrimSpace(reasoning.ResponseID) != "" {
				continue
			}
			r.reasoningUsed[index] = true
			mutations = append(mutations, r.addReasoningFallbackItem(responseID, turnTimelineReasoningItem(reasoning))...)
			break
		}
	}
	return mutations
}

func (r *timelineReducer) appendRemainingReasoning() []timelineMutation {
	mutations := make([]timelineMutation, 0)
	for index, reasoning := range r.reasoningFallback {
		if r.reasoningUsed[index] {
			continue
		}
		r.reasoningUsed[index] = true
		mutations = append(mutations, r.addReasoningFallbackItem(strings.TrimSpace(reasoning.ResponseID), turnTimelineReasoningItem(reasoning))...)
	}
	return mutations
}

func (r *timelineReducer) reduceTool(event stream.Event, createdAt time.Time) ([]timelineMutation, error) {
	payload, err := decodeToolTimelinePayload(event.Payload)
	if err != nil {
		return nil, err
	}
	incoming := newToolTimelineItem(domain.TurnStreamEvent{EventIndex: r.eventIndex, CreatedAt: createdAt}, event, payload)
	if existing, ok := r.item(incoming.ID); ok {
		incoming = mergeToolTimelineItem(existing, payload)
	}
	r.store(incoming)
	return []timelineMutation{{Kind: timelineMutationUpsert, Item: incoming}}, nil
}

func (r *timelineReducer) reduceResponseCompleted(event stream.Event, createdAt time.Time) ([]timelineMutation, error) {
	payload, ok := decodeResponseStreamPayload(event)
	if !ok || payload.Response == nil {
		if strings.TrimSpace(event.Text) == "" {
			return nil, nil
		}
		responseID := r.responseID(event.ResponseID)
		item := TurnTimelineItem{
			ID: stableTimelineAssistantItemID(responseID, "", r.eventIndex), Type: turnTimelineItemOutputText,
			Title: "Assistant", Status: "completed", ContentText: event.Text,
			Metadata: compactMetadata(map[string]any{"response_id": responseID}), CreatedAt: createdAt,
		}
		r.store(item)
		r.hasAssistantText = true
		return []timelineMutation{{Kind: timelineMutationDone, Item: item}}, nil
	}

	responseID := strings.TrimSpace(payload.Response.ID)
	if responseID == "" {
		responseID = payload.ResponseID
	}
	responseID = r.responseID(responseID)
	mutations := r.appendReasoningForResponse(responseID, false)
	completedSlots := map[string]int{}
	for outputIndex, raw := range payload.Response.Output {
		var output responseOutputItemPayload
		if err := json.Unmarshal(raw, &output); err != nil {
			continue
		}
		outputSlot := completedSlots[output.Type]
		completedSlots[output.Type] = outputSlot + 1
		if tracked, found := r.outputSlots.resolve(responseID, output.Type, output.ID, outputIndex); found {
			outputSlot = tracked
		}
		switch output.Type {
		case "reasoning":
			for summaryIndex, summary := range output.Summary {
				if strings.TrimSpace(summary.Text) == "" {
					continue
				}
				rawSummary, _ := json.Marshal(summary)
				item := TurnTimelineItem{
					ID:   stableTimelineReasoningPartID(responseID, output.ID, outputSlot, summaryIndex, "", int(r.eventIndex)),
					Type: turnTimelineItemReasoning, Title: "Reasoning", Status: "completed", ContentText: strings.TrimSpace(summary.Text), Raw: rawSummary,
					Metadata:  compactMetadata(map[string]any{"response_id": responseID, "item_id": strings.TrimSpace(output.ID), "output_index": outputSlot, "summary_index": summaryIndex}),
					CreatedAt: createdAt,
				}
				mutations = append(mutations, r.addReasoningFallbackItem(responseID, item)...)
			}
		case "message":
			if output.Role != domain.RoleAssistant {
				continue
			}
			for contentIndex, content := range output.Content {
				if (content.Type != "output_text" && content.Type != "text") || strings.TrimSpace(content.Text) == "" {
					continue
				}
				itemID := stableTimelineAssistantTextItemID(responseID, output.ID, outputSlot, contentIndex, "", r.eventIndex)
				metadata := compactMetadata(map[string]any{"response_id": responseID, "item_id": strings.TrimSpace(output.ID), "output_index": outputSlot, "content_index": contentIndex, "phase": strings.TrimSpace(output.Phase)})
				if _, streamed := r.assistantDone[itemID]; streamed {
					if strings.TrimSpace(output.Phase) != "" {
						item, _ := r.item(itemID)
						item.Metadata = mergeTimelineMetadata(item.Metadata, metadata)
						r.store(item)
						mutations = append(mutations, timelineMutation{Kind: timelineMutationUpsert, Item: item})
					}
					continue
				}
				item := TurnTimelineItem{ID: itemID, Type: turnTimelineItemOutputText, Title: "Assistant", Status: "completed", ContentText: content.Text, Metadata: metadata, CreatedAt: createdAt}
				r.store(item)
				r.assistantDone[itemID] = struct{}{}
				r.hasAssistantText = true
				mutations = append(mutations, timelineMutation{Kind: timelineMutationDone, Item: item})
			}
		case "image_generation_call":
			if strings.TrimSpace(output.Result) == "" {
				continue
			}
			status := strings.TrimSpace(output.Status)
			if status == "" || status == "generating" {
				status = "completed"
			}
			item := TurnTimelineItem{
				ID: stableTimelineImageGenerationItemID(responseID, output.ID, outputSlot, "", r.eventIndex), Type: turnTimelineItemImageGeneration,
				Title: "图片生成", Status: status, ContentText: strings.TrimSpace(output.RevisedPrompt),
				Metadata:  compactMetadata(map[string]any{"response_id": responseID, "item_id": strings.TrimSpace(output.ID), "output_index": outputSlot, "has_image_result": true}),
				CreatedAt: createdAt,
			}
			r.store(item)
			mutations = append(mutations, timelineMutation{Kind: timelineMutationDone, Item: item})
		}
	}
	return mutations, nil
}

func (r *timelineReducer) reduceResponseFailed(event stream.Event, createdAt time.Time) []timelineMutation {
	responseID := r.responseID(event.ResponseID)
	providerEvent := strings.TrimSpace(event.Payload) != ""
	errorCode, publicError := presentationFailure(event.ErrorCode)
	if providerEvent {
		errorCode = domain.TurnErrorUpstreamRequestFailed
		publicError = domain.TurnPublicErrorUpstreamRequestFailed
	}
	item := TurnTimelineItem{
		ID: stableTimelineStatusID("response-failed", responseID, 0), Type: turnTimelineItemStatus,
		Title: "Status", Status: "failed", ContentText: failureContentText(publicError),
		Metadata: compactMetadata(map[string]any{"response_id": responseID}), CreatedAt: createdAt,
	}
	r.store(item)
	mutations := []timelineMutation{{Kind: timelineMutationUpsert, Item: item}}
	if !providerEvent {
		mutations = append(mutations, timelineMutation{Kind: timelineMutationTerminal, Terminal: r.turnDone(domain.TurnStatusFailed, errorCode, publicError)})
	}
	return mutations
}

func (r *timelineReducer) turnDone(status string, errorCode string, publicError string) TurnStreamDone {
	done := TurnStreamDone{Status: status, ErrorCode: errorCode, Error: publicError}
	if r.turn != nil {
		done.TurnID = r.turn.ID
		done.ConversationID = r.turn.ConversationID
	}
	return done
}

func sequenceValue(sequence *int) any {
	if sequence == nil {
		return nil
	}
	return *sequence
}
