package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

const (
	turnTimelineItemStatus          = "status"
	turnTimelineItemReasoning       = "reasoning"
	turnTimelineItemToolCall        = "tool_call"
	turnTimelineItemOutputText      = "output_text"
	turnTimelineItemImageGeneration = "image_generation"
)

var reasoningTitleParagraphPattern = regexp.MustCompile(`^[ \t]*\*\*([^*\r\n]+)\*\*[ \t]*\r?$`)

type turnTimelineEventLister interface {
	ListTurnStreamEventsByTurn(ctx context.Context, turnID string) ([]domain.TurnStreamEvent, error)
}

type turnTimelineMessageLister interface {
	ListAssistantMessagesByTurn(ctx context.Context, turnID string) ([]domain.Message, error)
}

type GetTurnTimeline struct {
	Turns     executionTraceTurnGetter
	Events    turnTimelineEventLister
	Runs      turnRunLister
	ToolCalls toolCallLister
	Messages  turnTimelineMessageLister
	Artifacts turnRunArtifactReader
}

type reasoningTimelineItem struct {
	ID           string
	RunID        string
	ResponseID   string
	ItemID       string
	StepIndex    int
	OutputIndex  int
	SummaryIndex int
	ContentText  string
	Raw          json.RawMessage
	CreatedAt    time.Time
}

type responseStreamPayload struct {
	Type           string                     `json:"type"`
	ResponseID     string                     `json:"response_id"`
	ItemID         string                     `json:"item_id"`
	OutputIndex    int                        `json:"output_index"`
	ContentIndex   int                        `json:"content_index"`
	SummaryIndex   int                        `json:"summary_index"`
	SequenceNumber *int                       `json:"sequence_number"`
	Phase          string                     `json:"phase"`
	Delta          string                     `json:"delta"`
	Text           string                     `json:"text"`
	Item           *responseOutputItemPayload `json:"item"`
	Response       *struct {
		ID     string            `json:"id"`
		Output []json.RawMessage `json:"output"`
	} `json:"response"`
}

type responseOutputContentPayload struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseReasoningSummaryPayload struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseOutputItemPayload struct {
	ID            string                            `json:"id"`
	Type          string                            `json:"type"`
	Status        string                            `json:"status"`
	Role          string                            `json:"role"`
	Phase         string                            `json:"phase"`
	RevisedPrompt string                            `json:"revised_prompt"`
	Result        string                            `json:"result"`
	Content       []responseOutputContentPayload    `json:"content"`
	Summary       []responseReasoningSummaryPayload `json:"summary"`
}

type reasoningOutputPart struct {
	ItemID       string
	OutputIndex  int
	SummaryIndex int
	Text         string
	Raw          json.RawMessage
}

func (uc GetTurnTimeline) Execute(ctx context.Context, turnID string) (*TurnTimeline, error) {
	if uc.Turns == nil {
		return nil, errors.New("get turn timeline use case requires turn getter")
	}
	if uc.Runs == nil {
		return nil, errors.New("get turn timeline use case requires turn run lister")
	}

	turn, err := uc.Turns.GetTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}

	runs, err := uc.Runs.ListTurnRunsByTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}

	reasoningItems, err := uc.loadReasoningItems(ctx, turn, runs)
	if err != nil {
		return nil, err
	}

	timeline := &TurnTimeline{
		TurnID:         turn.ID,
		ConversationID: turn.ConversationID,
		Status:         turn.Status,
	}

	var events []domain.TurnStreamEvent
	if uc.Events != nil {
		events, err = uc.Events.ListTurnStreamEventsByTurn(ctx, turnID)
		if err != nil {
			return nil, err
		}
	}
	if len(events) > 0 {
		timeline.LastEventIndex = events[len(events)-1].EventIndex
	}

	assistantMessages, err := uc.loadAssistantMessages(ctx, turnID)
	if err != nil {
		return nil, err
	}

	if len(events) > 0 {
		if containsReasoningSummaryStreamEvents(events) {
			reasoningItems = nil
		}
		items, _, err := buildTimelineFromEvents(events, reasoningItems)
		if err != nil {
			return nil, err
		}
		items, err = uc.reconcileDurableToolCalls(ctx, turn.ID, items)
		if err != nil {
			return nil, err
		}
		items = appendMissingAssistantMessages(items, turn, assistantMessages)
		timeline.Items = items
		return timeline, nil
	}

	items, err := uc.buildFallbackTimeline(ctx, turn, runs, reasoningItems, assistantMessages)
	if err != nil {
		return nil, err
	}
	timeline.Items = items
	return timeline, nil
}

func (uc GetTurnTimeline) loadReasoningItems(ctx context.Context, turn *domain.Turn, runs []domain.TurnRun) ([]reasoningTimelineItem, error) {
	if uc.Artifacts == nil || turn == nil || len(runs) == 0 {
		return nil, nil
	}

	items := make([]reasoningTimelineItem, 0, len(runs))
	for _, run := range runs {
		key := uc.Artifacts.TurnRunOutputItemsKey(turn.ConversationID, turn.ID, run.StepIndex)
		data, err := uc.Artifacts.GetBytes(ctx, key)
		switch {
		case err == nil:
			parts, rawErr := extractReasoningParts(data)
			if rawErr != nil {
				return nil, fmt.Errorf("extract reasoning items from %q: %w", key, rawErr)
			}
			if len(parts) == 0 {
				continue
			}
			for _, part := range parts {
				responseID := strings.TrimSpace(run.ResponseID)
				items = append(items, reasoningTimelineItem{
					ID: stableTimelineReasoningPartID(
						responseID,
						part.ItemID,
						part.OutputIndex,
						part.SummaryIndex,
						run.ID,
						run.StepIndex,
					),
					RunID:        run.ID,
					ResponseID:   responseID,
					ItemID:       part.ItemID,
					StepIndex:    run.StepIndex,
					OutputIndex:  part.OutputIndex,
					SummaryIndex: part.SummaryIndex,
					ContentText:  part.Text,
					Raw:          part.Raw,
					CreatedAt:    reasoningTimelineTimestamp(run),
				})
			}
		case errors.Is(err, domain.ErrNotFound):
			continue
		default:
			return nil, fmt.Errorf("get turn run output items %q: %w", key, err)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].StepIndex != items[j].StepIndex {
			return items[i].StepIndex < items[j].StepIndex
		}
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})

	return items, nil
}

func (uc GetTurnTimeline) loadAssistantMessages(ctx context.Context, turnID string) ([]domain.Message, error) {
	if uc.Messages == nil {
		return nil, nil
	}

	messages, err := uc.Messages.ListAssistantMessagesByTurn(ctx, turnID)
	switch {
	case err == nil:
		return messages, nil
	case errors.Is(err, domain.ErrNotFound):
		return nil, nil
	default:
		return nil, err
	}
}

func (uc GetTurnTimeline) buildFallbackTimeline(ctx context.Context, turn *domain.Turn, runs []domain.TurnRun, reasoningItems []reasoningTimelineItem, assistantMessages []domain.Message) ([]TurnTimelineItem, error) {
	items := make([]TurnTimelineItem, 0, 1+len(reasoningItems))
	if item := fallbackStatusItem(turn); item != nil {
		items = append(items, *item)
	}

	for _, reasoning := range reasoningItems {
		items = append(items, turnTimelineReasoningItem(reasoning))
	}

	if uc.ToolCalls != nil {
		calls, err := uc.ToolCalls.ListToolCallsByTurn(ctx, turn.ID)
		if err != nil {
			return nil, err
		}
		for _, call := range calls {
			item, itemErr := uc.buildFallbackToolCallItem(ctx, call)
			if itemErr != nil {
				return nil, itemErr
			}
			items = append(items, item)
		}
	}

	for index := range assistantMessages {
		if item := fallbackAssistantMessageItem(turn, &assistantMessages[index], index); item != nil {
			items = append(items, *item)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})

	return splitReasoningTimelineItems(items), nil
}

func (uc GetTurnTimeline) buildFallbackToolCallItem(ctx context.Context, call domain.ToolCallRecord) (TurnTimelineItem, error) {
	var (
		arguments json.RawMessage
		output    []byte
		err       error
	)

	if uc.Artifacts != nil {
		arguments, err = loadTraceJSONArtifact(ctx, uc.Artifacts, call.ArgumentsBlobKey, "tool call arguments")
		if err != nil {
			return TurnTimelineItem{}, err
		}

		output, err = loadTraceArtifactBytes(ctx, uc.Artifacts, call.OutputBlobKey, "tool call output")
		if err != nil {
			return TurnTimelineItem{}, err
		}
	}

	normalizedOutput, err := normalizeTimelineOutput(output)
	if err != nil {
		return TurnTimelineItem{}, err
	}
	return TurnTimelineItem{
		ID:        stableTimelineToolID(call.ID, call.CallID, call.ToolName),
		Type:      turnTimelineItemToolCall,
		Title:     stableTimelineToolTitle(call.Namespace, call.ToolName),
		Status:    call.Status,
		Arguments: cloneRawJSON(arguments),
		Output:    normalizedOutput,
		Metadata:  fallbackToolCallMetadata(call),
		CreatedAt: call.StartedAt,
	}, nil
}

func (uc GetTurnTimeline) reconcileDurableToolCalls(ctx context.Context, turnID string, items []TurnTimelineItem) ([]TurnTimelineItem, error) {
	if uc.ToolCalls == nil {
		return items, nil
	}
	calls, err := uc.ToolCalls.ListToolCallsByTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}
	for _, call := range calls {
		durable, err := uc.buildFallbackToolCallItem(ctx, call)
		if err != nil {
			return nil, err
		}
		if index := durableToolCallIndex(items, call); index != -1 {
			items[index] = reconcileDurableToolCallItem(items[index], durable)
			continue
		}
		items = insertTimelineItemByCreatedAt(items, durable)
	}
	return items, nil
}

func durableToolCallIndex(items []TurnTimelineItem, call domain.ToolCallRecord) int {
	id := stableTimelineToolID(call.ID, call.CallID, call.ToolName)
	for index, item := range items {
		if item.Type != turnTimelineItemToolCall {
			continue
		}
		if item.ID == id || metadataString(item.Metadata, "tool_call_record_id") == call.ID {
			return index
		}
		if call.CallID != "" && metadataString(item.Metadata, "call_id") == call.CallID {
			return index
		}
	}
	return -1
}

func reconcileDurableToolCallItem(existing TurnTimelineItem, durable TurnTimelineItem) TurnTimelineItem {
	if len(durable.Arguments) == 0 {
		durable.Arguments = existing.Arguments
	}
	if len(durable.Output) == 0 {
		durable.Output = existing.Output
	}
	if durable.Summary == "" {
		durable.Summary = existing.Summary
	}
	if len(durable.Details) == 0 {
		durable.Details = existing.Details
	}
	if !existing.CreatedAt.IsZero() {
		durable.CreatedAt = existing.CreatedAt
	}
	durable.Metadata = mergeTimelineMetadata(existing.Metadata, durable.Metadata)
	return durable
}

func insertTimelineItemByCreatedAt(items []TurnTimelineItem, item TurnTimelineItem) []TurnTimelineItem {
	insertAt := len(items)
	if !item.CreatedAt.IsZero() {
		for index := range items {
			if !items[index].CreatedAt.IsZero() && items[index].CreatedAt.After(item.CreatedAt) {
				insertAt = index
				break
			}
		}
	}
	items = append(items, TurnTimelineItem{})
	copy(items[insertAt+1:], items[insertAt:])
	items[insertAt] = item
	return items
}

func buildTimelineFromEvents(events []domain.TurnStreamEvent, reasoningItems []reasoningTimelineItem) ([]TurnTimelineItem, bool, error) {
	reducer := newTimelineReducer(nil, nil, reasoningItems)
	for _, stored := range events {
		event, err := decodeStoredStreamEvent(stored)
		if err != nil {
			return nil, false, err
		}
		if _, err := reducer.Apply(normalizedTimelineEvent{Event: event, CreatedAt: stored.CreatedAt}); err != nil {
			return nil, false, fmt.Errorf("reduce stored stream event %q: %w", stored.ID, err)
		}
	}
	return reducer.FinalItems(), reducer.HasAssistantText(), nil
}

func splitReasoningTimelineItems(items []TurnTimelineItem) []TurnTimelineItem {
	result := make([]TurnTimelineItem, 0, len(items))
	for _, item := range items {
		result = append(result, splitReasoningTimelineItem(item)...)
	}
	return result
}

func reasoningContentKey(responseID string, content string) string {
	return strings.TrimSpace(responseID) + "\x00" + strings.TrimSpace(content)
}

func reasoningContentCovered(done map[string]struct{}, responseID string, content string) bool {
	target := strings.TrimSpace(content)
	if target == "" {
		return false
	}
	prefix := strings.TrimSpace(responseID) + "\x00"
	covered := 0
	for key := range done {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		part := strings.TrimPrefix(key, prefix)
		if part == target {
			return true
		}
		if part != "" && strings.Contains(target, part) {
			covered += len(part)
		}
	}
	return covered >= len(target)
}

func splitReasoningTimelineItem(item TurnTimelineItem) []TurnTimelineItem {
	if item.Type != turnTimelineItemReasoning {
		return []TurnTimelineItem{item}
	}
	text := strings.TrimSpace(item.ContentText)
	titleStarts := reasoningTitleParagraphStarts(text)
	if len(titleStarts) == 0 {
		return []TurnTimelineItem{item}
	}

	starts := make([]int, 0, len(titleStarts)+1)
	if prefix := strings.TrimSpace(text[:titleStarts[0]]); prefix != "" {
		starts = append(starts, 0)
	}
	starts = append(starts, titleStarts...)
	if len(starts) <= 1 {
		return []TurnTimelineItem{item}
	}

	sections := make([]TurnTimelineItem, 0, len(starts))
	for index, start := range starts {
		end := len(text)
		if index+1 < len(starts) {
			end = starts[index+1]
		}
		content := strings.TrimSpace(text[start:end])
		if content == "" {
			continue
		}
		section := item
		section.ContentText = content
		if index > 0 {
			section.ID = fmt.Sprintf("%s:section:%d", item.ID, index)
			section.Raw = nil
		}
		section.Metadata = mergeTimelineMetadata(item.Metadata, map[string]any{"section_index": index})
		sections = append(sections, section)
	}
	if len(sections) == 0 {
		return []TurnTimelineItem{item}
	}
	return sections
}

func reasoningTitleParagraphStarts(text string) []int {
	starts := make([]int, 0)
	inFence := false
	fence := ""
	for offset := 0; offset < len(text); {
		lineEnd := strings.IndexByte(text[offset:], '\n')
		if lineEnd == -1 {
			lineEnd = len(text)
		} else {
			lineEnd += offset
		}
		line := text[offset:lineEnd]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if !inFence {
				inFence = true
				fence = marker
			} else if marker == fence {
				inFence = false
				fence = ""
			}
		} else if !inFence && reasoningTitleParagraphPattern.MatchString(line) {
			starts = append(starts, offset)
		}
		if lineEnd == len(text) {
			break
		}
		offset = lineEnd + 1
	}
	return starts
}

func decodeStoredStreamEvent(stored domain.TurnStreamEvent) (stream.Event, error) {
	var event stream.Event
	if err := json.Unmarshal(stored.Payload, &event); err != nil {
		return stream.Event{}, fmt.Errorf("decode stored stream event: %w", err)
	}
	event.EventIndex = stored.EventIndex
	return event, nil
}

func decodeResponseStreamPayload(event stream.Event) (responseStreamPayload, bool) {
	var payload responseStreamPayload
	raw := strings.TrimSpace(event.Payload)
	if raw != "" && json.Valid([]byte(raw)) {
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			if strings.TrimSpace(payload.ResponseID) == "" {
				payload.ResponseID = strings.TrimSpace(event.ResponseID)
			}
			if payload.Delta == "" {
				payload.Delta = event.Delta
			}
			if strings.TrimSpace(payload.Text) == "" {
				payload.Text = event.Text
			}
			return payload, true
		}
	}

	if !strings.HasPrefix(strings.TrimSpace(event.Type), "response.") {
		return responseStreamPayload{}, false
	}
	return responseStreamPayload{
		Type:       event.Type,
		ResponseID: strings.TrimSpace(event.ResponseID),
		Delta:      event.Delta,
		Text:       event.Text,
	}, true
}

func decodeToolTimelinePayload(raw string) (stream.ToolStreamPayload, error) {
	payload := strings.TrimSpace(raw)
	if payload == "" {
		return stream.ToolStreamPayload{}, nil
	}

	var decoded stream.ToolStreamPayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return stream.ToolStreamPayload{}, err
	}
	return decoded, nil
}

func decodeReasoningTimelineItems(event stream.Event, eventID string, eventIndex int64, createdAt time.Time) ([]TurnTimelineItem, error) {
	var payload stream.ReasoningStreamPayload
	raw := strings.TrimSpace(event.Payload)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, fmt.Errorf("decode reasoning stream payload: %w", err)
		}
	}
	if strings.TrimSpace(payload.ResponseID) == "" {
		payload.ResponseID = strings.TrimSpace(event.ResponseID)
	}
	parts, err := reasoningPartsFromRawItems(payload.Items)
	if err != nil {
		return nil, fmt.Errorf("decode reasoning stream payload items: %w", err)
	}
	if len(parts) == 0 {
		summary := strings.TrimSpace(payload.Summary)
		if summary == "" {
			summary = strings.TrimSpace(event.Text)
		}
		if summary == "" {
			return nil, nil
		}
		parts = append(parts, reasoningOutputPart{Text: summary})
	}

	fallbackID := strings.TrimSpace(payload.TurnRunID)
	if fallbackID == "" {
		fallbackID = strings.TrimSpace(eventID)
	}
	fallbackStep := payload.StepIndex
	if fallbackStep <= 0 {
		fallbackStep = int(eventIndex)
	}

	items := make([]TurnTimelineItem, 0, len(parts))
	for _, part := range parts {
		items = append(items, TurnTimelineItem{
			ID: stableTimelineReasoningPartID(
				payload.ResponseID,
				part.ItemID,
				part.OutputIndex,
				part.SummaryIndex,
				fallbackID,
				fallbackStep,
			),
			Type:        turnTimelineItemReasoning,
			Title:       "Reasoning",
			Status:      "completed",
			ContentText: strings.TrimSpace(part.Text),
			Raw:         cloneRawJSON(part.Raw),
			Metadata: compactMetadata(map[string]any{
				"response_id":   payload.ResponseID,
				"item_id":       part.ItemID,
				"step_index":    payload.StepIndex,
				"output_index":  part.OutputIndex,
				"summary_index": part.SummaryIndex,
			}),
			CreatedAt: createdAt,
		})
	}
	return items, nil
}

func reasoningSummaryResponseID(event stream.Event) string {
	var payload stream.ReasoningStreamPayload
	if raw := strings.TrimSpace(event.Payload); raw != "" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	if responseID := strings.TrimSpace(payload.ResponseID); responseID != "" {
		return responseID
	}
	return strings.TrimSpace(event.ResponseID)
}

func newToolTimelineItem(stored domain.TurnStreamEvent, event stream.Event, payload stream.ToolStreamPayload) TurnTimelineItem {
	title := strings.TrimSpace(payload.ToolName)
	if title == "" {
		title = strings.TrimSpace(event.ToolName)
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = toolStatusFromEventType(event.Type)
	}

	return TurnTimelineItem{
		ID:        stableTimelineToolID(payload.ToolCallRecordID, payload.CallID, title),
		Type:      turnTimelineItemToolCall,
		Title:     title,
		Status:    status,
		Arguments: cloneRawJSON(payload.Arguments),
		Output:    cloneRawJSON(payload.Output),
		Summary:   strings.TrimSpace(payload.Summary),
		Details:   append([]string(nil), payload.Details...),
		Metadata:  toolTimelineMetadata(payload),
		CreatedAt: stored.CreatedAt,
	}
}

func mergeToolTimelineItem(existing TurnTimelineItem, payload stream.ToolStreamPayload) TurnTimelineItem {
	if text := strings.TrimSpace(payload.Status); text != "" {
		existing.Status = text
	}
	if text := strings.TrimSpace(payload.Summary); text != "" {
		existing.Summary = text
	}
	if len(payload.Details) > 0 {
		existing.Details = append([]string(nil), payload.Details...)
	}
	if len(payload.Arguments) > 0 {
		existing.Arguments = cloneRawJSON(payload.Arguments)
	}
	if len(payload.Output) > 0 {
		existing.Output = cloneRawJSON(payload.Output)
	}
	existing.Metadata = toolTimelineMetadata(payload)
	return existing
}

func toolTimelineMetadata(payload stream.ToolStreamPayload) map[string]any {
	metadata := map[string]any{
		"tool_call_record_id": strings.TrimSpace(payload.ToolCallRecordID),
		"call_id":             strings.TrimSpace(payload.CallID),
		"tool_name":           strings.TrimSpace(payload.ToolName),
		"error":               strings.TrimSpace(payload.Error),
	}
	return compactMetadata(metadata)
}

func fallbackToolCallMetadata(call domain.ToolCallRecord) map[string]any {
	return compactMetadata(map[string]any{
		"tool_call_record_id": call.ID,
		"call_id":             call.CallID,
		"tool_name":           stableTimelineToolTitle(call.Namespace, call.ToolName),
		"error":               strings.TrimSpace(call.ErrorMessage),
	})
}

func turnTimelineReasoningItem(reasoning reasoningTimelineItem) TurnTimelineItem {
	return TurnTimelineItem{
		ID:          reasoning.ID,
		Type:        turnTimelineItemReasoning,
		Title:       "Reasoning",
		Status:      "completed",
		ContentText: strings.TrimSpace(reasoning.ContentText),
		Raw:         cloneRawJSON(reasoning.Raw),
		Metadata: compactMetadata(map[string]any{
			"response_id":   reasoning.ResponseID,
			"item_id":       reasoning.ItemID,
			"step_index":    reasoning.StepIndex,
			"output_index":  reasoning.OutputIndex,
			"summary_index": reasoning.SummaryIndex,
		}),
		CreatedAt: reasoning.CreatedAt,
	}
}

func fallbackStatusItem(turn *domain.Turn) *TurnTimelineItem {
	if turn == nil {
		return nil
	}

	switch turn.Status {
	case domain.TurnStatusFailed:
		_, publicError := presentationFailure(turn.ErrorCode)
		item := &TurnTimelineItem{
			ID:        stableTimelineStatusID("turn-failed", "", 0),
			Type:      turnTimelineItemStatus,
			Title:     "Status",
			Status:    "failed",
			CreatedAt: turn.CreatedAt,
		}
		item.ContentText = failureContentText(publicError)
		return item
	}

	return nil
}

func fallbackAssistantMessageItem(turn *domain.Turn, message *domain.Message, index int) *TurnTimelineItem {
	if message == nil || strings.TrimSpace(message.ContentText) == "" {
		return nil
	}

	createdAt := message.CreatedAt
	if createdAt.IsZero() && turn != nil {
		createdAt = turn.UpdatedAt
	}

	return &TurnTimelineItem{
		ID:          "assistant:" + firstMessageFallbackID(message, index),
		Type:        turnTimelineItemOutputText,
		Title:       "Assistant",
		Status:      "completed",
		ContentText: message.ContentText,
		Metadata: compactMetadata(map[string]any{
			"response_id": turnResponseID(turn),
			"message_id":  message.ID,
		}),
		CreatedAt: createdAt,
	}
}

func firstMessageFallbackID(message *domain.Message, index int) string {
	if message != nil && strings.TrimSpace(message.ID) != "" {
		return strings.TrimSpace(message.ID)
	}
	return fmt.Sprintf("assistant-message-%d", index)
}

func appendMissingAssistantMessages(items []TurnTimelineItem, turn *domain.Turn, messages []domain.Message) []TurnTimelineItem {
	seenContent := make(map[string]struct{}, len(items))
	var streamedContent strings.Builder
	for _, item := range items {
		if item.Type == turnTimelineItemOutputText && strings.TrimSpace(item.ContentText) != "" {
			seenContent[strings.TrimSpace(item.ContentText)] = struct{}{}
			streamedContent.WriteString(item.ContentText)
		}
	}
	if text := strings.TrimSpace(streamedContent.String()); text != "" {
		seenContent[text] = struct{}{}
	}
	for index := range messages {
		text := strings.TrimSpace(messages[index].ContentText)
		if text == "" {
			continue
		}
		if _, ok := seenContent[text]; ok {
			continue
		}
		if item := fallbackAssistantMessageItem(turn, &messages[index], index); item != nil {
			items = append(items, *item)
			seenContent[text] = struct{}{}
		}
	}
	return items
}

func extractReasoningParts(data []byte) ([]reasoningOutputPart, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return nil, err
	}
	return reasoningPartsFromRawItems(rawItems)
}

func reasoningPartsFromRawItems(rawItems []json.RawMessage) ([]reasoningOutputPart, error) {
	parts := make([]reasoningOutputPart, 0)
	for outputIndex, raw := range rawItems {
		var item responseOutputItemPayload
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		if item.Type != "reasoning" {
			continue
		}
		for summaryIndex, summary := range item.Summary {
			text := strings.TrimSpace(summary.Text)
			if text == "" {
				continue
			}
			rawSummary, err := json.Marshal(summary)
			if err != nil {
				return nil, err
			}
			parts = append(parts, reasoningOutputPart{
				ItemID:       strings.TrimSpace(item.ID),
				OutputIndex:  outputIndex,
				SummaryIndex: summaryIndex,
				Text:         text,
				Raw:          rawSummary,
			})
		}
	}
	return parts, nil
}

func normalizeTimelineOutput(output []byte) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}
	if json.Valid([]byte(trimmed)) {
		return redactEncryptedContentJSON([]byte(trimmed))
	}
	wrapped, err := json.Marshal(trimmed)
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), wrapped...), nil
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func toolStatusFromEventType(eventType string) string {
	switch eventType {
	case stream.EventToolStarted:
		return stream.ToolEventStatusStarted
	case stream.EventToolFailed:
		return stream.ToolEventStatusFailed
	default:
		return stream.ToolEventStatusCompleted
	}
}

func compactMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}

	compacted := make(map[string]any, len(metadata))
	for key, value := range metadata {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
		case nil:
			continue
		}
		compacted[key] = value
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func mergeTimelineMetadata(existing map[string]any, incoming map[string]any) map[string]any {
	if len(existing) == 0 {
		return incoming
	}
	merged := make(map[string]any, len(existing)+len(incoming))
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range incoming {
		merged[key] = value
	}
	return merged
}

func stableTimelineReasoningPartID(responseID string, itemID string, outputIndex int, summaryIndex int, fallback string, fallbackIndex int) string {
	responseID = strings.TrimSpace(responseID)
	itemID = strings.TrimSpace(itemID)
	if responseID != "" {
		return fmt.Sprintf("reasoning:%s:%d:%d", responseID, outputIndex, summaryIndex)
	}
	if itemID != "" {
		return fmt.Sprintf("reasoning:%s:%d", itemID, summaryIndex)
	}
	if strings.TrimSpace(fallback) != "" {
		return fmt.Sprintf("reasoning:%s:%d:%d", strings.TrimSpace(fallback), fallbackIndex, summaryIndex)
	}
	return fmt.Sprintf("reasoning:event:%d:%d:%d", fallbackIndex, outputIndex, summaryIndex)
}

func stableTimelineToolID(recordID string, callID string, toolName string) string {
	switch {
	case strings.TrimSpace(recordID) != "":
		return "tool:" + strings.TrimSpace(recordID)
	case strings.TrimSpace(callID) != "":
		return "tool:" + strings.TrimSpace(callID)
	default:
		return "tool:" + strings.TrimSpace(toolName)
	}
}

func stableTimelineAssistantItemID(responseID string, fallback string, fallbackIndex int64) string {
	if strings.TrimSpace(responseID) != "" {
		return "assistant:" + strings.TrimSpace(responseID)
	}
	if strings.TrimSpace(fallback) != "" {
		return "assistant:" + strings.TrimSpace(fallback)
	}
	return fmt.Sprintf("assistant:%d", fallbackIndex)
}

func stableTimelineAssistantTextItemID(responseID string, itemID string, outputIndex int, contentIndex int, fallback string, fallbackIndex int64) string {
	responseID = strings.TrimSpace(responseID)
	itemID = strings.TrimSpace(itemID)
	if responseID != "" {
		return fmt.Sprintf("assistant:%s:%d:%d", responseID, outputIndex, contentIndex)
	}
	if itemID != "" {
		return fmt.Sprintf("assistant:%s:%d", itemID, contentIndex)
	}
	if strings.TrimSpace(fallback) != "" {
		return fmt.Sprintf("assistant:%s:%d:%d", strings.TrimSpace(fallback), outputIndex, contentIndex)
	}
	return fmt.Sprintf("assistant:%d:%d:%d", fallbackIndex, outputIndex, contentIndex)
}

func stableTimelineImageGenerationItemID(responseID string, itemID string, outputIndex int, fallback string, fallbackIndex int64) string {
	responseID = strings.TrimSpace(responseID)
	itemID = strings.TrimSpace(itemID)
	if itemID != "" {
		if responseID != "" {
			return fmt.Sprintf("image:%s:%s", responseID, itemID)
		}
		return "image:" + itemID
	}
	if responseID != "" {
		return fmt.Sprintf("image:%s:%d", responseID, outputIndex)
	}
	if strings.TrimSpace(fallback) != "" {
		return fmt.Sprintf("image:%s:%d", strings.TrimSpace(fallback), outputIndex)
	}
	return fmt.Sprintf("image:%d:%d", fallbackIndex, outputIndex)
}

func stableTimelineStatusID(kind string, responseID string, eventIndex int64) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "status"
	}
	if strings.TrimSpace(responseID) != "" {
		return fmt.Sprintf("status:%s:%s", kind, strings.TrimSpace(responseID))
	}
	if eventIndex > 0 {
		return fmt.Sprintf("status:%s:%d", kind, eventIndex)
	}
	return "status:" + kind
}

func turnResponseID(turn *domain.Turn) string {
	if turn == nil {
		return ""
	}
	return strings.TrimSpace(turn.OpenAIResponseID)
}

func stableTimelineToolTitle(namespace string, toolName string) string {
	namespace = strings.TrimSpace(namespace)
	toolName = strings.TrimSpace(toolName)
	if namespace == "" || toolName == "" || strings.HasPrefix(toolName, namespace+".") {
		return toolName
	}
	return namespace + "." + toolName
}

func timelineRunTimestamp(run domain.TurnRun) time.Time {
	switch {
	case run.CompletedAt != nil:
		return *run.CompletedAt
	case run.FailedAt != nil:
		return *run.FailedAt
	case !run.UpdatedAt.IsZero():
		return run.UpdatedAt
	default:
		return run.StartedAt
	}
}

func reasoningTimelineTimestamp(run domain.TurnRun) time.Time {
	switch {
	case !run.StartedAt.IsZero():
		return run.StartedAt
	case !run.CreatedAt.IsZero():
		return run.CreatedAt
	default:
		return timelineRunTimestamp(run)
	}
}

func containsReasoningSummaryStreamEvents(events []domain.TurnStreamEvent) bool {
	for _, event := range events {
		switch event.EventType {
		case "response.reasoning_summary_part.added", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.reasoning_summary_part.done":
			return true
		}
	}
	return false
}
