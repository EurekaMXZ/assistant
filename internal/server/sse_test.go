package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/stream"
	"github.com/gin-gonic/gin"
)

type stubSubscription struct {
	closed *bool
}

func (s stubSubscription) Close() error {
	if s.closed != nil {
		*s.closed = true
	}
	return nil
}

type stubStreamSubscriber struct {
	channel      <-chan stream.Event
	err          error
	subscribeHit int
	closed       bool
}

func (s *stubStreamSubscriber) SubscribeEvents(context.Context, string) (io.Closer, <-chan stream.Event, error) {
	s.subscribeHit++
	if s.err != nil {
		return nil, nil, s.err
	}
	return stubSubscription{closed: &s.closed}, s.channel, nil
}

func TestHandleStreamTurnCompletedWritesTerminalEventWithoutSubscribing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	streamHub := &stubStreamSubscriber{}
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			return &domain.Turn{
				ID:               "turn-1",
				ConversationID:   "conv-1",
				Status:           domain.TurnStatusCompleted,
				OpenAIResponseID: "resp-1",
			}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				return &TurnTimeline{
					TurnID:         "turn-1",
					ConversationID: "conv-1",
					Status:         domain.TurnStatusCompleted,
					Items: []TurnTimelineItem{{
						ID:          "assistant:resp-1",
						Type:        turnTimelineItemOutputText,
						Status:      "completed",
						ContentText: "done",
						CreatedAt:   time.Unix(1710000000, 0).UTC(),
					}},
				}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-1/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if streamHub.subscribeHit != 0 {
		t.Fatalf("expected no subscription for completed turn, got %d", streamHub.subscribeHit)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: turn.snapshot") {
		t.Fatalf("expected snapshot SSE event, got %q", body)
	}
	if !strings.Contains(body, `"type":"output_text"`) || !strings.Contains(body, `"content_text":"done"`) {
		t.Fatalf("expected assistant snapshot payload, got %q", body)
	}
	if !strings.Contains(body, "event: turn.done") {
		t.Fatalf("expected terminal SSE event, got %q", body)
	}
}

func TestHandleStreamTurnKeepsOutputTextInFailedSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	streamHub := &stubStreamSubscriber{}
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			return &domain.Turn{
				ID:             "turn-failed",
				ConversationID: "conv-1",
				Status:         domain.TurnStatusFailed,
				ErrorCode:      domain.TurnErrorUpstreamRequestFailed,
			}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				return &TurnTimeline{
					TurnID:         "turn-failed",
					ConversationID: "conv-1",
					Status:         domain.TurnStatusFailed,
					Items: []TurnTimelineItem{{
						ID:          "assistant:resp-1:0:0",
						Type:        turnTimelineItemOutputText,
						Status:      "completed",
						ContentText: "Partial answer before failure",
						CreatedAt:   time.Unix(1710000000, 0).UTC(),
					}},
				}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-failed/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"output_text"`) || !strings.Contains(body, `"content_text":"Partial answer before failure"`) {
		t.Fatalf("failed snapshot dropped output text: %q", body)
	}
	if !strings.Contains(body, `"status":"failed"`) || !strings.Contains(body, "event: turn.done") {
		t.Fatalf("failed snapshot lost terminal state: %q", body)
	}
}

func TestHandleStreamTurnClosesWhenSnapshotBecomesTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channel := make(chan stream.Event)
	streamHub := &stubStreamSubscriber{channel: channel}
	lookups := 0
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			lookups++
			status := domain.TurnStatusProcessing
			if lookups > 1 {
				status = domain.TurnStatusCompleted
			}
			return &domain.Turn{ID: "turn-race", ConversationID: "conv-1", Status: status}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				return &TurnTimeline{TurnID: "turn-race", ConversationID: "conv-1", Status: domain.TurnStatusCompleted}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-race/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if streamHub.subscribeHit != 1 || !streamHub.closed {
		t.Fatalf("expected transient subscription to close, hit=%d closed=%v", streamHub.subscribeHit, streamHub.closed)
	}
	if !strings.Contains(rec.Body.String(), `"status":"completed"`) || !strings.Contains(rec.Body.String(), "event: turn.done") {
		t.Fatalf("expected terminal snapshot stream, got %q", rec.Body.String())
	}
}

func TestFallbackTurnStreamSnapshotUsesEmptyItemsArray(t *testing.T) {
	api := &API{useCases: UseCases{Turns: TurnUseCases{
		GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
			return nil, domain.ErrNotFound
		},
	}}}
	snapshot, _, _, err := api.loadTurnStreamSnapshot(
		t.Context(),
		"user-1",
		&domain.Turn{ID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing},
		newPresentationItemRegistry(),
	)
	if err != nil {
		t.Fatalf("load fallback snapshot: %v", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if !strings.Contains(string(encoded), `"items":[]`) {
		t.Fatalf("empty snapshot items encoded as null: %s", encoded)
	}
}

func TestHandleStreamTurnTranslatesLiveEventsIntoUIItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channel := make(chan stream.Event, 6)
	channel <- stream.Event{
		Type:           stream.EventReasoningSummary,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ResponseID:     "resp-1",
		Payload:        `{"turn_run_id":"run-1","response_id":"resp-1","step_index":1,"items":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Need to inspect first."}]}]}`,
	}
	channel <- stream.Event{
		Type:           stream.EventToolCompleted,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ToolName:       "internet.search",
		Payload:        `{"tool_call_record_id":"tool-1","turn_run_id":"run-1","call_id":"call-1","tool_name":"internet.search","tool_type":"function","namespace":"internet","status":"completed","arguments":{"query":"OpenAI"},"output":{"results":[1,2,3,4,5]}}`,
	}
	channel <- stream.Event{
		Type:           "response.output_text.delta",
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ResponseID:     "resp-1",
		Payload:        `{"type":"response.output_text.delta","response_id":"resp-1","item_id":"msg-1","output_index":0,"content_index":0,"delta":"Final "}`,
		Delta:          "Final ",
	}
	channel <- stream.Event{
		Type:           stream.EventResponseCompleted,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ResponseID:     "resp-1",
		Text:           "Final answer",
	}
	channel <- stream.Event{
		Type:           stream.EventConversationUpdated,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		Payload:        `{"conversation_id":"conv-1","title":"Filtered title","secret":"hidden"}`,
	}
	channel <- stream.Event{
		Type:           stream.EventTurnDone,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
	}
	close(channel)

	streamHub := &stubStreamSubscriber{channel: channel}
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			return &domain.Turn{ID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				return &TurnTimeline{TurnID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-1/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: turn.snapshot") {
		t.Fatalf("expected snapshot event, got %q", body)
	}
	if !strings.Contains(body, "event: item.done") || !strings.Contains(body, `"type":"reasoning"`) {
		t.Fatalf("expected completed reasoning item, got %q", body)
	}
	if !strings.Contains(body, `"type":"tool_call"`) || !strings.Contains(body, `"title":"Searching the Web"`) || !strings.Contains(body, `"input_label":"Keywords"`) || !strings.Contains(body, `"input_text":"OpenAI"`) {
		t.Fatalf("expected tool item upsert, got %q", body)
	}
	if strings.Contains(body, `"arguments"`) || strings.Contains(body, `"output"`) {
		t.Fatalf("raw tool payload leaked, got %q", body)
	}
	if !strings.Contains(body, "event: item.delta") || !strings.Contains(body, `"item_id":"assistant:resp-1:0:0"`) {
		t.Fatalf("expected assistant delta event, got %q", body)
	}
	if !strings.Contains(body, "event: item.done") || !strings.Contains(body, `"content_text":"Final answer"`) {
		t.Fatalf("expected assistant done event, got %q", body)
	}
	if !strings.Contains(body, "event: turn.done") {
		t.Fatalf("expected turn done event, got %q", body)
	}
	if !strings.Contains(body, "event: conversation.updated") || !strings.Contains(body, `"title":"Filtered title"`) || strings.Contains(body, `"secret"`) {
		t.Fatalf("expected filtered conversation update, got %q", body)
	}
}

func TestHandleStreamTurnFiltersProviderEventsIntoCanonicalItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channel := make(chan stream.Event, 4)
	channel <- stream.Event{
		Type:           "response.output_text.delta",
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ResponseID:     "resp-1",
		Payload:        `{"type":"response.output_text.delta","response_id":"resp-1","item_id":"msg-1","output_index":1,"content_index":0,"delta":"Hello"}`,
	}
	channel <- stream.Event{
		Type:           "response.function_call_arguments.done",
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		Payload:        `{"type":"response.function_call_arguments.done","arguments":"secret arguments"}`,
	}
	channel <- stream.Event{
		Type:           "response.completed",
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ResponseID:     "resp-1",
		Payload:        `{"type":"response.completed","response":{"id":"resp-1","status":"completed","instructions":"secret prompt","tools":[{"name":"internet.search"}],"usage":{"total_tokens":10},"output":[]}}`,
	}
	close(channel)

	streamHub := &stubStreamSubscriber{channel: channel}
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			return &domain.Turn{ID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				return &TurnTimeline{TurnID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-1/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "event: response.") {
		t.Fatalf("provider event name leaked, got %q", body)
	}
	if !strings.Contains(body, "event: item.upsert") || !strings.Contains(body, "event: item.delta") || !strings.Contains(body, `"delta":"Hello"`) {
		t.Fatalf("expected canonical assistant item events, got %q", body)
	}
	if strings.Contains(body, "secret prompt") || strings.Contains(body, "secret arguments") || strings.Contains(body, "internet.search") || strings.Contains(body, "total_tokens") {
		t.Fatalf("expected response payload to be sanitized, got %q", body)
	}
}

func TestHandleStreamTurnSanitizesProviderFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channel := make(chan stream.Event, 2)
	channel <- stream.Event{
		Type:           stream.EventResponseFailed,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ResponseID:     "resp-1",
		Error:          "sensitive provider detail",
		Payload:        `{"type":"response.failed","response":{"id":"resp-1","status":"failed","error":{"message":"sensitive provider detail"}}}`,
	}
	channel <- stream.Event{
		Type:           stream.EventResponseFailed,
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		ResponseID:     "resp-1",
		ErrorCode:      domain.TurnErrorUpstreamRequestFailed,
		Error:          domain.TurnPublicErrorUpstreamRequestFailed,
	}
	close(channel)

	streamHub := &stubStreamSubscriber{channel: channel}
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			return &domain.Turn{ID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				return &TurnTimeline{TurnID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-1/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "sensitive provider detail") {
		t.Fatalf("provider detail leaked in SSE response: %q", body)
	}
	if !strings.Contains(body, domain.TurnPublicErrorUpstreamRequestFailed) {
		t.Fatalf("expected public upstream error, got %q", body)
	}
	if !strings.Contains(body, `"error_code":"`+domain.TurnErrorUpstreamRequestFailed+`"`) {
		t.Fatalf("expected upstream error code, got %q", body)
	}
	if !strings.Contains(body, "event: turn.done") {
		t.Fatalf("expected terminal event, got %q", body)
	}
}

func TestHandleStreamTurnReconcilesDurableTerminalState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousInterval := turnStreamTerminalPollInterval
	turnStreamTerminalPollInterval = time.Millisecond
	t.Cleanup(func() { turnStreamTerminalPollInterval = previousInterval })

	channel := make(chan stream.Event)
	streamHub := &stubStreamSubscriber{channel: channel}
	turnLookups := 0
	timelineLookups := 0
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			turnLookups++
			status := domain.TurnStatusProcessing
			if turnLookups > 1 {
				status = domain.TurnStatusCompleted
			}
			return &domain.Turn{ID: "turn-1", ConversationID: "conv-1", Status: status}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				timelineLookups++
				status := domain.TurnStatusProcessing
				var items []TurnTimelineItem
				if timelineLookups > 1 {
					status = domain.TurnStatusCompleted
					items = []TurnTimelineItem{{
						ID:          "assistant:final",
						Type:        turnTimelineItemOutputText,
						Status:      "completed",
						ContentText: "durable final answer",
					}}
				}
				return &TurnTimeline{TurnID: "turn-1", ConversationID: "conv-1", Status: status, Items: items}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-1/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Count(body, "event: turn.snapshot") != 2 {
		t.Fatalf("expected initial and reconciled snapshots, got %q", body)
	}
	if !strings.Contains(body, "durable final answer") || !strings.Contains(body, "event: turn.done") {
		t.Fatalf("expected reconciled final state, got %q", body)
	}
}

func TestHandleStreamTurnKeepsSnapshotResponseIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channel := make(chan stream.Event, 1)
	channel <- stream.Event{
		Type:      stream.EventResponseFailed,
		ErrorCode: domain.TurnErrorUpstreamRequestFailed,
		Error:     domain.TurnPublicErrorUpstreamRequestFailed,
	}
	close(channel)

	streamHub := &stubStreamSubscriber{channel: channel}
	srv := newTestServerWithStream(UseCases{
		Auth: AuthUseCases{AuthenticateAccessToken: func(context.Context, string) (*domain.User, error) {
			return &domain.User{ID: "user-1", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil
		}},
		Turns: TurnUseCases{GetTurn: func(context.Context, string, string) (*domain.Turn, error) {
			return &domain.Turn{ID: "turn-1", ConversationID: "conv-1", Status: domain.TurnStatusProcessing}, nil
		},
			GetTurnTimeline: func(context.Context, string, string) (*TurnTimeline, error) {
				return &TurnTimeline{
					TurnID:         "turn-1",
					ConversationID: "conv-1",
					Status:         domain.TurnStatusProcessing,
					Items: []TurnTimelineItem{{
						ID:          "status:response-failed:resp-1",
						Type:        turnTimelineItemStatus,
						Status:      "failed",
						ContentText: failureContentText(domain.TurnPublicErrorUpstreamRequestFailed),
						Metadata:    map[string]any{"response_id": "resp-1"},
					}},
				}, nil
			}},
	}, streamHub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/turns/turn-1/stream", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Count(body, `"id":"status:response-failed:resp-1"`) != 2 {
		t.Fatalf("expected snapshot and durable failure to share one item identity, got %q", body)
	}
	if strings.Contains(body, `"id":"status:response-failed"`) {
		t.Fatalf("durable failure lost the snapshot response identity: %q", body)
	}
}
