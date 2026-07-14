package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/gin-gonic/gin"
)

const (
	streamUIEventTurnSnapshot = "turn.snapshot"
	streamUIEventItemUpsert   = "item.upsert"
	streamUIEventItemDelta    = "item.delta"
	streamUIEventItemDone     = "item.done"
	streamUIEventTurnDone     = "turn.done"
)

var turnStreamTerminalPollInterval = 2 * time.Second

func (a *API) handleGetTurn(c *gin.Context) {
	turn, err := a.useCases.Turns.GetTurn(c.Request.Context(), currentUser(c).ID, c.Param("turnID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"turn": turn})
}

func (a *API) handleGetTurnExecutionTrace(c *gin.Context) {
	trace, err := a.useCases.Turns.GetTurnExecutionTrace(c.Request.Context(), currentUser(c).ID, c.Param("turnID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"trace": trace})
}

func (a *API) handleStreamTurn(c *gin.Context) {
	turn, err := a.useCases.Turns.GetTurn(c.Request.Context(), currentUser(c).ID, c.Param("turnID"))
	if err != nil {
		writeAPIError(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(c, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	userID := currentUser(c).ID
	terminal := turn.Status == domain.TurnStatusCompleted || turn.Status == domain.TurnStatusFailed

	var (
		sub     io.Closer
		channel <-chan stream.Event
	)
	if !terminal {
		if a.streamHub == nil {
			writeError(c, http.StatusServiceUnavailable, "streaming is not configured")
			return
		}
		var err error
		sub, channel, err = a.streamHub.SubscribeEvents(c.Request.Context(), turn.ID)
		if err != nil {
			writeError(c, http.StatusServiceUnavailable, err.Error())
			return
		}
		defer sub.Close()
	}

	itemRegistry := newPresentationItemRegistry()
	eventRegistry := newPresentationEventRegistry()
	snapshot, lastResponseID, outputSlots, err := a.loadTurnStreamSnapshot(c.Request.Context(), userID, turn, itemRegistry)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	presentationState := newPresentationStreamState(turn, itemRegistry, snapshot.Items)
	presentationState.responseID(lastResponseID)
	presentationState.outputSlots = outputSlots
	if err := writeSSE(c.Writer, streamUIEventTurnSnapshot, snapshot); err != nil {
		return
	}
	c.Writer.Flush()

	if !terminal && (snapshot.Status == domain.TurnStatusCompleted || snapshot.Status == domain.TurnStatusFailed) {
		if refreshed, refreshErr := a.useCases.Turns.GetTurn(c.Request.Context(), userID, turn.ID); refreshErr == nil && refreshed != nil {
			turn = refreshed
		}
		terminal = true
	}
	if terminal {
		errorCode := ""
		publicError := ""
		if snapshot.Status == domain.TurnStatusFailed {
			errorCode, publicError = presentationFailure(turn.ErrorCode)
		}
		_ = writeSSE(c.Writer, streamUIEventTurnDone, TurnStreamDone{
			TurnID:         turn.ID,
			ConversationID: turn.ConversationID,
			Status:         snapshot.Status,
			ErrorCode:      errorCode,
			Error:          publicError,
		})
		c.Writer.Flush()
		return
	}

	if a.streamHub == nil {
		writeError(c, http.StatusServiceUnavailable, "streaming is not configured")
		return
	}

	keepAliveTicker := time.NewTicker(30 * time.Second)
	defer keepAliveTicker.Stop()
	terminalTicker := time.NewTicker(turnStreamTerminalPollInterval)
	defer terminalTicker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-keepAliveTicker.C:
			_, _ = fmt.Fprintf(c.Writer, ": keep-alive\n\n")
			c.Writer.Flush()
		case <-terminalTicker.C:
			refreshed, refreshErr := a.useCases.Turns.GetTurn(c.Request.Context(), userID, turn.ID)
			if refreshErr != nil || refreshed == nil || (refreshed.Status != domain.TurnStatusCompleted && refreshed.Status != domain.TurnStatusFailed) {
				continue
			}
			finalSnapshot, _, _, snapshotErr := a.loadTurnStreamSnapshot(c.Request.Context(), userID, refreshed, itemRegistry)
			if snapshotErr != nil {
				continue
			}
			if err := writeSSE(c.Writer, streamUIEventTurnSnapshot, finalSnapshot); err != nil {
				return
			}
			errorCode := ""
			publicError := ""
			if refreshed.Status == domain.TurnStatusFailed {
				errorCode, publicError = presentationFailure(refreshed.ErrorCode)
			}
			if err := writeSSE(c.Writer, streamUIEventTurnDone, TurnStreamDone{
				TurnID:         refreshed.ID,
				ConversationID: refreshed.ConversationID,
				Status:         refreshed.Status,
				ErrorCode:      errorCode,
				Error:          publicError,
			}); err != nil {
				return
			}
			c.Writer.Flush()
			return
		case message, ok := <-channel:
			if !ok {
				return
			}

			done, err := a.writeStreamUIEvents(c.Writer, presentationState, eventRegistry, message, time.Now().UTC())
			if err != nil {
				return
			}
			c.Writer.Flush()

			if done {
				return
			}
		}
	}
}

func (a *API) loadTurnStreamSnapshot(ctx context.Context, ownerUserID string, turn *domain.Turn, items *presentationItemRegistry) (TurnStreamSnapshot, string, *responseOutputSlotResolver, error) {
	if turn == nil {
		return TurnStreamSnapshot{}, "", newResponseOutputSlotResolver(), nil
	}
	timeline, err := a.useCases.Turns.GetTurnTimeline(ctx, ownerUserID, turn.ID)
	if err == nil && timeline != nil {
		return TurnStreamSnapshot{
			TurnID:         timeline.TurnID,
			ConversationID: timeline.ConversationID,
			Status:         timeline.Status,
			Items:          items.FilterAll(timeline.Items),
		}, lastTimelineResponseID(timeline.Items, turnResponseID(turn)), responseOutputSlotsFromTimeline(timeline.Items), nil
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return TurnStreamSnapshot{}, "", nil, err
	}

	snapshot := TurnStreamSnapshot{
		TurnID:         turn.ID,
		ConversationID: turn.ConversationID,
		Status:         turn.Status,
		Items:          []TurnTimelineItem{},
	}
	if len(snapshot.Items) == 0 {
		if item := fallbackStatusItem(turn); item != nil {
			if filtered, ok := items.Filter(*item); ok {
				snapshot.Items = append(snapshot.Items, filtered)
			}
		}
	}
	return snapshot, turnResponseID(turn), newResponseOutputSlotResolver(), nil
}

func responseOutputSlotsFromTimeline(items []TurnTimelineItem) *responseOutputSlotResolver {
	resolver := newResponseOutputSlotResolver()
	for _, item := range items {
		responseID := metadataString(item.Metadata, "response_id")
		itemID := metadataString(item.Metadata, "item_id")
		slot, ok := metadataInt(item.Metadata, "output_index")
		if !ok {
			continue
		}
		switch item.Type {
		case turnTimelineItemOutputText:
			resolver.bind(responseID, "message", itemID, slot)
		case turnTimelineItemReasoning:
			resolver.bind(responseID, "reasoning", itemID, slot)
		case turnTimelineItemImageGeneration:
			resolver.bind(responseID, "image_generation_call", itemID, slot)
		}
	}
	return resolver
}

func lastTimelineResponseID(items []TurnTimelineItem, fallback string) string {
	responseID := strings.TrimSpace(fallback)
	for _, item := range items {
		if candidate := metadataString(item.Metadata, "response_id"); candidate != "" {
			responseID = candidate
		}
	}
	return responseID
}

func (a *API) writeStreamUIEvents(w http.ResponseWriter, state *presentationStreamState, registry *presentationEventRegistry, event stream.Event, createdAt time.Time) (bool, error) {
	frames, err := registry.Filter(state, event, createdAt)
	if err != nil {
		return false, err
	}
	terminal := false
	for _, frame := range frames {
		if err := writeSSE(w, frame.Event, frame.Payload); err != nil {
			return false, err
		}
		terminal = terminal || frame.Terminal
	}
	return terminal, nil
}

func failureContentText(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "Turn failed."
	}
	return "Turn failed: " + message
}

func writeSSE(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}

	return nil
}
