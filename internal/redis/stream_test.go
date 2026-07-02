package redis

import (
	"context"
	"testing"
	"time"

	"github.com/EurekaMXZ/assistant/internal/stream"
	goredis "github.com/redis/go-redis/v9"
)

func TestForwardEventsSkipsInvalidPayloads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := make(chan *goredis.Message, 2)
	events := forwardEvents(ctx, source)

	source <- &goredis.Message{Payload: "not-json"}
	source <- &goredis.Message{Payload: `{"type":"response.output_text.delta","turn_id":"turn-1","delta":"hi"}`}
	close(source)

	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("expected forwarded event")
		}
		if got, want := event, (stream.Event{
			Type:   "response.output_text.delta",
			TurnID: "turn-1",
			Delta:  "hi",
		}); got != want {
			t.Fatalf("event = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for forwarded event")
	}

	if _, ok := <-events; ok {
		t.Fatal("expected forwarded channel to close after source closes")
	}
}

func TestForwardEventsStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := make(chan *goredis.Message)
	events := forwardEvents(ctx, source)

	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected closed event channel after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event channel to close")
	}
}

func TestNewUsesSettings(t *testing.T) {
	hub := New(Settings{
		Addr:          "127.0.0.1:6379",
		Password:      "secret",
		DB:            7,
		ChannelPrefix: "assistant:stream",
	})
	defer hub.Close()

	options := hub.client.Options()
	if options.Addr != "127.0.0.1:6379" {
		t.Fatalf("addr = %q, want %q", options.Addr, "127.0.0.1:6379")
	}
	if options.Password != "secret" {
		t.Fatalf("password = %q, want %q", options.Password, "secret")
	}
	if options.DB != 7 {
		t.Fatalf("db = %d, want %d", options.DB, 7)
	}
	if hub.prefix != "assistant:stream" {
		t.Fatalf("prefix = %q, want %q", hub.prefix, "assistant:stream")
	}
}
