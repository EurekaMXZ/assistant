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
	turnTimelineItemInteraction     = "interaction"
)

var reasoningTitleParagraphPattern = regexp.MustCompile(`^[ \t]*\*\*([^*\r\n]+)\*\*[ \t]*\r?$`)

type turnTimelineConversationEventLister interface {
	ListConversationEventsByTurn(ctx context.Context, turnID string) ([]domain.ConversationEvent, error)
}

type turnTimelineGeneratedImageLister interface {
	ListGeneratedImageAssetsByTurn(ctx context.Context, turnID string) ([]domain.GeneratedImageAsset, error)
}

type GetTurnTimeline struct {
	Turns           executionTraceTurnGetter
	CompleteEvents  turnTimelineConversationEventLister
	GeneratedImages turnTimelineGeneratedImageLister
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
	turn, err := uc.Turns.GetTurn(ctx, turnID)
	if err != nil {
		return nil, err
	}

	timeline := &TurnTimeline{
		TurnID:         turn.ID,
		ConversationID: turn.ConversationID,
		Status:         turn.Status,
	}

	if uc.CompleteEvents == nil {
		return nil, errors.New("get turn timeline use case requires complete event store")
	}
	completeEvents, err := uc.CompleteEvents.ListConversationEventsByTurn(ctx, turn.ID)
	if err != nil {
		return nil, err
	}
	if len(completeEvents) > 0 {
		timeline.LastEventIndex = completeEvents[len(completeEvents)-1].EventSeq
	}
	items, err := buildTimelineFromConversationEvents(completeEvents, turn.ID)
	if err != nil {
		return nil, err
	}
	timeline.Items = appendMissingTerminalStatus(items, turn)
	return uc.appendGeneratedImageAssets(ctx, timeline)
}

func (uc GetTurnTimeline) appendGeneratedImageAssets(ctx context.Context, timeline *TurnTimeline) (*TurnTimeline, error) {
	if timeline == nil || uc.GeneratedImages == nil {
		return timeline, nil
	}
	assets, err := uc.GeneratedImages.ListGeneratedImageAssetsByTurn(ctx, timeline.TurnID)
	if err != nil {
		return nil, err
	}
	if len(assets) == 0 {
		return timeline, nil
	}
	selected := make(map[string]domain.GeneratedImageAsset)
	for _, asset := range assets {
		key := strings.TrimSpace(asset.ResponseID) + ":" + strings.TrimSpace(asset.ItemID)
		current, exists := selected[key]
		if !exists || asset.Kind == domain.GeneratedImageKindFinal || (current.Kind != domain.GeneratedImageKindFinal && asset.Revision > current.Revision) {
			selected[key] = asset
		}
	}
	indexes := make(map[string]int, len(timeline.Items))
	for index, item := range timeline.Items {
		indexes[item.ID] = index
	}
	for _, asset := range selected {
		status := "generating"
		if asset.Kind == domain.GeneratedImageKindFinal {
			status = "completed"
		}
		itemID := stableTimelineImageGenerationItemID(asset.ResponseID, asset.ItemID, 0, asset.TurnRunID, int64(asset.Revision))
		image := generatedImageTimelineImage(asset)
		if index, exists := indexes[itemID]; exists {
			timeline.Items[index].Status = status
			timeline.Items[index].Image = image
			continue
		}
		timeline.Items = append(timeline.Items, TurnTimelineItem{
			ID: itemID, Type: turnTimelineItemImageGeneration, Title: "图片生成", Status: status,
			Image: image, CreatedAt: asset.CreatedAt,
			Metadata: compactMetadata(map[string]any{
				"response_id": asset.ResponseID,
				"item_id":     asset.ItemID,
			}),
		})
	}
	sort.SliceStable(timeline.Items, func(i, j int) bool {
		return timeline.Items[i].CreatedAt.Before(timeline.Items[j].CreatedAt)
	})
	return timeline, nil
}

func generatedImageTimelineImage(asset domain.GeneratedImageAsset) *TurnTimelineImage {
	return &TurnTimelineImage{
		AssetID: asset.ID, Kind: asset.Kind, Revision: asset.Revision,
		ContentType: asset.ContentType, SizeBytes: asset.SizeBytes,
		Width: asset.Width, Height: asset.Height, AttachmentID: asset.AttachmentID,
	}
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

func appendMissingTerminalStatus(items []TurnTimelineItem, turn *domain.Turn) []TurnTimelineItem {
	item := fallbackStatusItem(turn)
	if item == nil {
		return items
	}
	for _, existing := range items {
		if existing.Type == turnTimelineItemStatus && existing.Status == item.Status {
			return items
		}
	}
	return insertTimelineItemByCreatedAt(items, *item)
}

func buildTimelineFromConversationEvents(events []domain.ConversationEvent, turnID string) ([]TurnTimelineItem, error) {
	reducer := newTimelineReducer(nil, nil, nil)
	for _, stored := range events {
		if stored.TurnID != turnID {
			continue
		}
		event, ok := timelineEventFromConversationEvent(stored)
		if !ok {
			continue
		}
		if _, err := reducer.Apply(normalizedTimelineEvent{Event: event, CreatedAt: stored.CreatedAt}); err != nil {
			return nil, fmt.Errorf("reduce conversation event %q: %w", stored.ID, err)
		}
	}
	return reducer.FinalItems(), nil
}

func timelineEventFromConversationEvent(stored domain.ConversationEvent) (stream.Event, bool) {
	event := stream.Event{
		ConversationID: stored.ConversationID,
		TurnID:         stored.TurnID,
		RunID:          stored.TurnRunID,
		Payload:        string(stored.Payload),
		EventIndex:     stored.EventSeq,
	}
	switch stored.EventType {
	case domain.ConversationEventReasoningSummaryDone:
		event.Type = stream.EventReasoningSummary
	case domain.ConversationEventToolCallStarted:
		event.Type = stream.EventToolStarted
	case domain.ConversationEventToolCallCompleted:
		event.Type = stream.EventToolCompleted
	case domain.ConversationEventToolCallFailed:
		event.Type = stream.EventToolFailed
	case domain.ConversationEventInteractionAwaiting:
		event.Type = stream.EventInteractionAwaiting
	case domain.ConversationEventInteractionCompleted:
		event.Type = stream.EventInteractionDone
	case domain.ConversationEventInteractionCancelled:
		event.Type = stream.EventInteractionCancelled
	case domain.ConversationEventOutputTextCompleted, domain.ConversationEventOutputTextInterrupted:
		event.Type = responseEventOutputTextDone
	case domain.ConversationEventTurnCompleted:
		event.Type = stream.EventTurnDone
	default:
		return stream.Event{}, false
	}
	return event, true
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

func newToolTimelineItem(createdAt time.Time, event stream.Event, payload stream.ToolStreamPayload) TurnTimelineItem {
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
		CreatedAt: createdAt,
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
		createdAt := turn.CreatedAt
		if turn.FailedAt != nil {
			createdAt = *turn.FailedAt
		}
		item := &TurnTimelineItem{
			ID:        stableTimelineStatusID("turn-failed", "", 0),
			Type:      turnTimelineItemStatus,
			Title:     "Status",
			Status:    "failed",
			CreatedAt: createdAt,
		}
		item.ContentText = failureContentText(publicError)
		return item
	}

	return nil
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
