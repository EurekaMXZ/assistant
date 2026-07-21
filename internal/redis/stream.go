package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"time"

	"github.com/EurekaMXZ/assistant/internal/cache"
	"github.com/EurekaMXZ/assistant/internal/stream"
	goredis "github.com/redis/go-redis/v9"
)

var _ stream.Publisher = (*Hub)(nil)
var _ io.Closer = (*Hub)(nil)
var _ cache.SharedContextSnapshotCache = (*Hub)(nil)

type Hub struct {
	client        *goredis.Client
	prefix        string
	contextPrefix string
	contextTTL    time.Duration
}

func New(settings Settings) *Hub {
	contextPrefix := settings.ContextPrefix
	if contextPrefix == "" {
		contextPrefix = "context"
	}
	contextTTL := settings.ContextTTL
	if contextTTL <= 0 {
		contextTTL = 2 * time.Hour
	}
	return &Hub{
		client:        newClient(settings),
		prefix:        settings.ChannelPrefix,
		contextPrefix: contextPrefix,
		contextTTL:    contextTTL,
	}
}

func (h *Hub) GetContextSnapshot(ctx context.Context, conversationID string, version int64) (*cache.ContextSnapshot, bool, error) {
	payload, err := h.client.Get(ctx, h.contextKey(conversationID, version)).Bytes()
	if err != nil {
		if err == goredis.Nil {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get shared context snapshot: %w", err)
	}
	snapshot, err := cache.DecodeContextSnapshot(payload)
	if err != nil {
		return nil, false, err
	}
	if snapshot.ConversationID != conversationID || snapshot.Version != version {
		return nil, false, fmt.Errorf("shared context snapshot identity mismatch")
	}
	return snapshot, true, nil
}

func (h *Hub) PutContextSnapshot(ctx context.Context, snapshot *cache.ContextSnapshot) error {
	payload, err := cache.EncodeContextSnapshot(snapshot)
	if err != nil {
		return err
	}
	ttl := h.contextTTL
	if jitter := ttl / 10; jitter > 0 {
		ttl = ttl - jitter + time.Duration(rand.Int64N(int64(2*jitter)+1))
	}
	if err := h.client.Set(ctx, h.contextKey(snapshot.ConversationID, snapshot.Version), payload, ttl).Err(); err != nil {
		return fmt.Errorf("put shared context snapshot: %w", err)
	}
	return nil
}

func (h *Hub) contextKey(conversationID string, version int64) string {
	return fmt.Sprintf("%s:%s:%d", h.contextPrefix, conversationID, version)
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
