package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"github.com/EurekaMXZ/assistant/internal/stream"
)

type accumulatedTextItem struct {
	text             strings.Builder
	lastTransportSeq int64
	event            stream.Event
}

type CompleteEventAccumulator struct {
	mu    sync.Mutex
	items map[string]*accumulatedTextItem
}

func NewCompleteEventAccumulator() *CompleteEventAccumulator {
	return &CompleteEventAccumulator{items: make(map[string]*accumulatedTextItem)}
}

func (a *CompleteEventAccumulator) Apply(event stream.Event) ([]domain.ConversationEventInput, error) {
	if a == nil || event.ConversationID == "" || event.TurnID == "" {
		return nil, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := accumulatorItemKey(event)
	switch event.Type {
	case "response.output_text.delta":
		item := a.items[key]
		if item == nil {
			item = &accumulatedTextItem{event: event}
			a.items[key] = item
		}
		if event.TransportSeq > 0 && event.TransportSeq <= item.lastTransportSeq {
			return nil, nil
		}
		item.text.WriteString(event.Delta)
		item.event = event
		item.lastTransportSeq = event.TransportSeq
		return nil, nil
	case "response.output_text.done":
		item := a.items[key]
		text := event.Text
		if text == "" && item != nil {
			text = item.text.String()
		}
		delete(a.items, key)
		return []domain.ConversationEventInput{completedTextEvent(event, text, false)}, nil
	case stream.EventResponseFailed, domain.ConversationEventRunCancelled:
		result := a.flushRun(event, true)
		terminalType, status := domain.ConversationEventRunFailed, "failed"
		if event.Type == domain.ConversationEventRunCancelled {
			terminalType, status = domain.ConversationEventRunCancelled, "cancelled"
		}
		result = append(result, terminalRunEvent(event, terminalType, status))
		return result, nil
	case stream.EventResponseCompleted:
		result := a.flushRun(event, false)
		result = append(result, terminalRunEvent(event, domain.ConversationEventRunCompleted, "completed"))
		return result, nil
	case stream.EventResponseStarted, stream.EventResponseCreated:
		return nil, nil
	case stream.EventReasoningSummary:
		return []domain.ConversationEventInput{directSemanticEvent(event, domain.ConversationEventReasoningSummaryDone, "completed")}, nil
	case stream.EventToolStarted:
		return nil, nil
	case stream.EventToolCompleted:
		return []domain.ConversationEventInput{directSemanticEvent(event, domain.ConversationEventToolCallCompleted, "completed")}, nil
	case stream.EventToolFailed:
		return []domain.ConversationEventInput{directSemanticEvent(event, domain.ConversationEventToolCallFailed, "failed")}, nil
	case stream.EventInteractionAwaiting:
		return nil, nil
	case stream.EventInteractionDone:
		return []domain.ConversationEventInput{directSemanticEvent(event, domain.ConversationEventInteractionCompleted, "completed")}, nil
	case stream.EventInteractionCancelled:
		return []domain.ConversationEventInput{directSemanticEvent(event, domain.ConversationEventInteractionCancelled, "cancelled")}, nil
	default:
		return nil, nil
	}
}

func terminalRunEvent(event stream.Event, eventType string, status string) domain.ConversationEventInput {
	payload, _ := json.Marshal(map[string]string{
		"response_id": strings.TrimSpace(event.ResponseID),
		"status":      status,
		"error_code":  strings.TrimSpace(event.ErrorCode),
		"error":       strings.TrimSpace(event.Error),
	})
	identity := strings.TrimSpace(event.ResponseID)
	if identity == "" {
		identity = status
	}
	keyPrefix := "run:" + event.RunID
	if event.RunID == "" {
		keyPrefix = "turn:" + event.TurnID
	}
	return domain.ConversationEventInput{
		ConversationID: event.ConversationID,
		TurnID:         event.TurnID,
		TurnRunID:      event.RunID,
		EventKey:       fmt.Sprintf("%s:%s:%s", keyPrefix, eventType, identity),
		SchemaVersion:  1,
		EventType:      eventType,
		Payload:        payload,
	}
}

func directSemanticEvent(event stream.Event, eventType string, status string) domain.ConversationEventInput {
	payload := json.RawMessage(event.Payload)
	if len(payload) == 0 || !json.Valid(payload) {
		payload, _ = json.Marshal(map[string]any{
			"response_id": event.ResponseID,
			"item_id":     event.ItemID,
			"tool_name":   event.ToolName,
			"status":      status,
			"error_code":  event.ErrorCode,
			"error":       event.Error,
		})
	}
	identity := event.ItemID
	var toolIdentity struct {
		ToolCallRecordID string `json:"tool_call_record_id"`
		ToolCallID       string `json:"tool_call_id"`
		CallID           string `json:"call_id"`
	}
	if json.Unmarshal(payload, &toolIdentity) == nil {
		if toolIdentity.ToolCallRecordID != "" {
			identity = toolIdentity.ToolCallRecordID
		} else if toolIdentity.ToolCallID != "" {
			identity = toolIdentity.ToolCallID
		} else if toolIdentity.CallID != "" {
			identity = toolIdentity.CallID
		}
	}
	if identity == "" {
		identity = event.ResponseID
	}
	if identity == "" {
		identity = event.ToolName
	}
	keyPrefix := "run:" + event.RunID
	if event.RunID == "" {
		keyPrefix = "turn:" + event.TurnID
	}
	return domain.ConversationEventInput{
		ConversationID:  event.ConversationID,
		TurnID:          event.TurnID,
		TurnRunID:       event.RunID,
		EventKey:        fmt.Sprintf("%s:%s:%s:%s", keyPrefix, eventType, identity, status),
		SchemaVersion:   1,
		EventType:       eventType,
		Payload:         payload,
		ContextIncluded: false,
	}
}

func (a *CompleteEventAccumulator) flushRun(event stream.Event, interrupted bool) []domain.ConversationEventInput {
	result := make([]domain.ConversationEventInput, 0)
	for key, item := range a.items {
		if item.event.ConversationID != event.ConversationID || item.event.TurnID != event.TurnID || (event.RunID != "" && item.event.RunID != event.RunID) {
			continue
		}
		result = append(result, completedTextEvent(item.event, item.text.String(), interrupted))
		delete(a.items, key)
	}
	return result
}

func completedTextEvent(event stream.Event, text string, interrupted bool) domain.ConversationEventInput {
	eventType := domain.ConversationEventOutputTextCompleted
	status := "completed"
	if interrupted {
		eventType = domain.ConversationEventOutputTextInterrupted
		status = "interrupted"
	}
	modelItem := llm.ModelItem{ID: event.ItemID, Type: llm.ModelItemMessage, Status: status, Role: domain.RoleAssistant, Content: text}
	payload, _ := json.Marshal(map[string]any{
		"response_id":   event.ResponseID,
		"item_id":       event.ItemID,
		"output_index":  event.OutputIndex,
		"content_index": event.ContentIndex,
		"text":          text,
		"status":        status,
		"model_item":    modelItem,
	})
	return domain.ConversationEventInput{
		ConversationID:  event.ConversationID,
		TurnID:          event.TurnID,
		TurnRunID:       event.RunID,
		EventKey:        fmt.Sprintf("run:%s:item:%s:output:%d:content:%d:%s", event.RunID, event.ItemID, event.OutputIndex, event.ContentIndex, status),
		SchemaVersion:   1,
		EventType:       eventType,
		Payload:         payload,
		ContextIncluded: false,
	}
}

func accumulatorItemKey(event stream.Event) string {
	return fmt.Sprintf("%s:%s:%d:%d", event.RunID, event.ItemID, event.OutputIndex, event.ContentIndex)
}
