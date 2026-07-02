package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/EurekaMXZ/assistant/internal/stream"
	goredis "github.com/redis/go-redis/v9"
)

var _ stream.Publisher = (*Hub)(nil)
var _ io.Closer = (*Hub)(nil)

type Hub struct {
	client *goredis.Client
	prefix string
}

func New(settings Settings) *Hub {
	return &Hub{
		client: newClient(settings),
		prefix: settings.ChannelPrefix,
	}
}

func newClient(settings Settings) *goredis.Client {
	return goredis.NewClient(&goredis.Options{
		Addr:     settings.Addr,
		Password: settings.Password,
		DB:       settings.DB,
	})
}

func (h *Hub) Close() error {
	if h == nil || h.client == nil {
		return nil
	}
	return h.client.Close()
}

func (h *Hub) Ping(ctx context.Context) error {
	return h.client.Ping(ctx).Err()
}

func (h *Hub) Publish(ctx context.Context, event stream.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal stream event: %w", err)
	}

	if err := h.client.Publish(ctx, h.channel(event.TurnID), payload).Err(); err != nil {
		return fmt.Errorf("publish stream event: %w", err)
	}

	return nil
}

func (h *Hub) Subscribe(ctx context.Context, turnID string) (*goredis.PubSub, <-chan *goredis.Message, error) {
	sub := h.client.Subscribe(ctx, h.channel(turnID))

	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := sub.Receive(readyCtx); err != nil {
		_ = sub.Close()
		return nil, nil, fmt.Errorf("subscribe stream channel: %w", err)
	}

	return sub, sub.Channel(), nil
}

func (h *Hub) SubscribeEvents(ctx context.Context, turnID string) (io.Closer, <-chan stream.Event, error) {
	sub, channel, err := h.Subscribe(ctx, turnID)
	if err != nil {
		return nil, nil, err
	}

	return sub, forwardEvents(ctx, channel), nil
}

func (h *Hub) channel(turnID string) string {
	return fmt.Sprintf("%s:%s", h.prefix, turnID)
}

func forwardEvents(ctx context.Context, channel <-chan *goredis.Message) <-chan stream.Event {
	events := make(chan stream.Event)
	go func() {
		defer close(events)

		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-channel:
				if !ok {
					return
				}

				var event stream.Event
				if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
					continue
				}

				select {
				case <-ctx.Done():
					return
				case events <- event:
				}
			}
		}
	}()

	return events
}
