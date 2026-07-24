package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/gin-gonic/gin"
)

type stubTurnTimelineConversationEventLister struct {
	events []domain.ConversationEvent
	err    error
}

func (s *stubTurnTimelineConversationEventLister) ListConversationEventsByTurn(context.Context, string) ([]domain.ConversationEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.ConversationEvent(nil), s.events...), nil
}

func TestBuildTimelineRestoresCancelledInteractionAnswer(t *testing.T) {
	now := time.Now().UTC()
	payload := json.RawMessage(`{"id":"ask-user:tool-1","tool_call_id":"tool-1","prompt":"Continue?","kind":"single_choice","options":[{"id":"yes","label":"Yes","tone":"primary"},{"id":"cancel","label":"Cancel","tone":"neutral"}],"answer":{"status":"cancelled","option_id":"cancelled","label":"已取消","user_reported":false},"status":"cancelled"}`)
	items, err := buildTimelineFromConversationEvents([]domain.ConversationEvent{{
		ID: "event-1", ConversationID: "conv-1", TurnID: "turn-1", TurnRunID: "run-1",
		EventSeq: 1, EventType: domain.ConversationEventInteractionCancelled, Payload: payload, CreatedAt: now,
	}}, "turn-1")
	if err != nil {
		t.Fatalf("build cancelled interaction timeline: %v", err)
	}
	if len(items) != 1 || items[0].Status != domain.ToolCallStatusCancelled || items[0].Answer == nil || items[0].Answer.OptionID != "cancelled" {
		t.Fatalf("cancelled interaction timeline = %#v", items)
	}
}

func TestGetTurnTimelineUsesCompletedConversationEvents(t *testing.T) {
	now := time.Now().UTC()
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{turn: &domain.Turn{
			ID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusCompleted, CreatedAt: now,
		}},
		CompleteEvents: &stubTurnTimelineConversationEventLister{events: []domain.ConversationEvent{{
			ID: "event-1", ConversationID: "conv-1", TurnID: "turn-1", TurnRunID: "run-1", EventSeq: 1,
			EventType: domain.ConversationEventOutputTextCompleted,
			Payload:   json.RawMessage(`{"response_id":"resp-1","item_id":"item-1","output_index":0,"content_index":0,"text":"Final answer","status":"completed"}`),
			CreatedAt: now,
		}}},
	}

	timeline, err := uc.Execute(context.Background(), "turn-1")
	if err != nil {
		t.Fatalf("execute timeline: %v", err)
	}
	if timeline.LastEventIndex != 1 || len(timeline.Items) != 1 || timeline.Items[0].ContentText != "Final answer" {
		t.Fatalf("timeline = %#v", timeline)
	}
}

func TestGetTurnTimelineRequiresCompleteEvents(t *testing.T) {
	now := time.Now().UTC()
	uc := GetTurnTimeline{
		Turns: &stubTraceTurnGetter{turn: &domain.Turn{
			ID: "turn-2", ConversationID: "conv-2", Status: domain.TurnStatusCompleted, CreatedAt: now,
		}},
	}

	if _, err := uc.Execute(context.Background(), "turn-2"); err == nil || err.Error() != "get turn timeline use case requires complete event store" {
		t.Fatalf("error = %v", err)
	}
}

func TestTurnTimelineRouteIsNotPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newTestServer(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-3/timeline", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
