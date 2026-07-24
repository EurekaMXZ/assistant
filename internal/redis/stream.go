package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
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
	replayTTL     time.Duration
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
	replayTTL := settings.ReplayTTL
	if replayTTL <= 0 {
		replayTTL = time.Hour
	}
	return &Hub{
		client:        newClient(settings),
		prefix:        settings.ChannelPrefix,
		contextPrefix: contextPrefix,
		contextTTL:    contextTTL,
		replayTTL:     replayTTL,
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
	replayEvent := event
	if replayEvent.Type == stream.EventResponseCompleted {
		replayEvent.Payload = replayPayloadWithoutGeneratedImageData(replayEvent.Payload)
		payload, err = json.Marshal(replayEvent)
		if err != nil {
			return fmt.Errorf("marshal replay stream event: %w", err)
		}
	}
	if err := h.cacheReplayEvent(ctx, replayEvent, payload); err != nil {
		return err
	}

	payload, err = json.Marshal(event)
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
	sub, replay, _, events, err := h.SubscribeEventsWithReplay(ctx, turnID)
	if err != nil {
		return nil, nil, err
	}
	return sub, prependReplayEvents(ctx, replay, events), nil
}

func (h *Hub) SubscribeEventsWithReplay(ctx context.Context, turnID string) (io.Closer, []stream.Event, bool, <-chan stream.Event, error) {
	sub, channel, err := h.Subscribe(ctx, turnID)
	if err != nil {
		return nil, nil, false, nil, err
	}
	replay, found, err := h.ReplayEvents(ctx, turnID)
	if err != nil {
		_ = sub.Close()
		return nil, nil, false, nil, err
	}
	return sub, replay, found, forwardEventsAfterReplay(ctx, replay, channel), nil
}

func (h *Hub) ReplayEvents(ctx context.Context, turnID string) ([]stream.Event, bool, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil, false, nil
	}
	entries, err := h.client.LRange(ctx, h.replayKey(turnID), 0, -1).Result()
	if err != nil {
		return nil, false, fmt.Errorf("load turn replay events: %w", err)
	}
	if len(entries) == 0 {
		return nil, false, nil
	}
	deltas, err := h.client.HGetAll(ctx, h.deltaKey(turnID)).Result()
	if err != nil {
		return nil, false, fmt.Errorf("load turn replay deltas: %w", err)
	}
	events := make([]stream.Event, 0, len(entries))
	for _, entry := range entries {
		var event stream.Event
		if err := json.Unmarshal([]byte(entry), &event); err != nil {
			continue
		}
		if event.Type == "response.output_text.delta" {
			encoded, ok := deltas[replayDeltaField(event)]
			if !ok {
				continue
			}
			if err := json.Unmarshal([]byte(encoded), &event); err != nil {
				continue
			}
		}
		events = append(events, event)
	}
	return events, true, nil
}

func (h *Hub) channel(turnID string) string {
	return fmt.Sprintf("%s:%s", h.prefix, turnID)
}

func (h *Hub) replayKey(turnID string) string {
	return fmt.Sprintf("%s:replay:%s", h.prefix, turnID)
}

func (h *Hub) deltaKey(turnID string) string {
	return fmt.Sprintf("%s:replay-deltas:%s", h.prefix, turnID)
}

func (h *Hub) cacheReplayEvent(ctx context.Context, event stream.Event, payload []byte) error {
	turnID := strings.TrimSpace(event.TurnID)
	if turnID == "" {
		return nil
	}
	if event.Type == "response.output_text.delta" {
		return h.cacheReplayDelta(ctx, event, payload)
	}

	pipeline := h.client.Pipeline()
	pipeline.RPush(ctx, h.replayKey(turnID), payload)
	pipeline.Expire(ctx, h.replayKey(turnID), h.replayTTL)
	if event.Type == "response.output_text.done" {
		pipeline.HDel(ctx, h.deltaKey(turnID), replayDeltaField(event))
	}
	if _, err := pipeline.Exec(ctx); err != nil {
		return fmt.Errorf("cache turn replay event: %w", err)
	}
	return nil
}

func (h *Hub) cacheReplayDelta(ctx context.Context, event stream.Event, payload []byte) error {
	turnID := strings.TrimSpace(event.TurnID)
	field := replayDeltaField(event)
	previous, err := h.client.HGet(ctx, h.deltaKey(turnID), field).Result()
	first := false
	if err == goredis.Nil {
		first = true
	} else if err != nil {
		return fmt.Errorf("load turn replay delta: %w", err)
	}
	if !first {
		var cached stream.Event
		if err := json.Unmarshal([]byte(previous), &cached); err != nil {
			return fmt.Errorf("decode turn replay delta: %w", err)
		}
		event.Delta = cached.Delta + event.Delta
	}
	event.Payload = replayDeltaPayload(event.Payload, event.Delta)
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal turn replay delta: %w", err)
	}

	pipeline := h.client.Pipeline()
	pipeline.HSet(ctx, h.deltaKey(turnID), field, encoded)
	pipeline.Expire(ctx, h.deltaKey(turnID), 15*time.Minute)
	pipeline.Expire(ctx, h.replayKey(turnID), h.replayTTL)
	if first {
		pipeline.RPush(ctx, h.replayKey(turnID), payload)
	}
	if _, err := pipeline.Exec(ctx); err != nil {
		return fmt.Errorf("cache turn replay delta: %w", err)
	}
	return nil
}

func replayDeltaField(event stream.Event) string {
	return fmt.Sprintf("%s:%s:%d:%d", event.RunID, event.ItemID, event.OutputIndex, event.ContentIndex)
}

func replayDeltaPayload(payload string, delta string) string {
	if !json.Valid([]byte(payload)) {
		return payload
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &object); err != nil {
		return payload
	}
	encoded, err := json.Marshal(delta)
	if err != nil {
		return payload
	}
	object["delta"] = encoded
	updated, err := json.Marshal(object)
	if err != nil {
		return payload
	}
	return string(updated)
}

func replayPayloadWithoutGeneratedImageData(payload string) string {
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &envelope) != nil {
		return payload
	}
	responseRaw, ok := envelope["response"]
	if !ok {
		return payload
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(responseRaw, &response) != nil {
		return payload
	}
	outputRaw, ok := response["output"]
	if !ok {
		return payload
	}
	var output []json.RawMessage
	if json.Unmarshal(outputRaw, &output) != nil {
		return payload
	}

	changed := false
	for index, rawItem := range output {
		var item map[string]json.RawMessage
		if json.Unmarshal(rawItem, &item) != nil {
			continue
		}
		var itemType string
		if json.Unmarshal(item["type"], &itemType) != nil || itemType != "image_generation_call" {
			continue
		}
		if _, ok := item["result"]; !ok {
			continue
		}
		delete(item, "result")
		encoded, err := json.Marshal(item)
		if err != nil {
			return payload
		}
		output[index] = encoded
		changed = true
	}
	if !changed {
		return payload
	}
	encodedOutput, err := json.Marshal(output)
	if err != nil {
		return payload
	}
	response["output"] = encodedOutput
	encodedResponse, err := json.Marshal(response)
	if err != nil {
		return payload
	}
	envelope["response"] = encodedResponse
	encodedEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return payload
	}
	return string(encodedEnvelope)
}

func forwardEvents(ctx context.Context, channel <-chan *goredis.Message) <-chan stream.Event {
	return forwardReplayEvents(ctx, nil, channel)
}

func forwardReplayEvents(ctx context.Context, replay []stream.Event, channel <-chan *goredis.Message) <-chan stream.Event {
	events := make(chan stream.Event)
	go func() {
		defer close(events)
		for _, event := range replay {
			select {
			case <-ctx.Done():
				return
			case events <- event:
			}
		}

		forwardLiveEvents(ctx, events, channel, nil)
	}()

	return events
}

func prependReplayEvents(ctx context.Context, replay []stream.Event, channel <-chan stream.Event) <-chan stream.Event {
	events := make(chan stream.Event)
	go func() {
		defer close(events)
		for _, event := range replay {
			select {
			case <-ctx.Done():
				return
			case events <- event:
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-channel:
				if !ok {
					return
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

func forwardEventsAfterReplay(ctx context.Context, replay []stream.Event, channel <-chan *goredis.Message) <-chan stream.Event {
	events := make(chan stream.Event)
	go func() {
		defer close(events)
		forwardLiveEvents(ctx, events, channel, replayDeltaGate(replay))
	}()
	return events
}

func forwardLiveEvents(ctx context.Context, events chan<- stream.Event, channel <-chan *goredis.Message, replayDeltas map[string]string) {
	consumedDeltas := make(map[string]string, len(replayDeltas))
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
			if event.Type == "response.output_text.delta" {
				field := replayDeltaField(event)
				if replayed, ok := replayDeltas[field]; ok {
					consumed := consumedDeltas[field] + event.Delta
					if strings.HasPrefix(replayed, consumed) {
						if consumed == replayed {
							delete(replayDeltas, field)
							delete(consumedDeltas, field)
						} else {
							consumedDeltas[field] = consumed
						}
						continue
					}
					delete(replayDeltas, field)
					delete(consumedDeltas, field)
				}
			}

			select {
			case <-ctx.Done():
				return
			case events <- event:
			}
		}
	}
}

func replayDeltaGate(replay []stream.Event) map[string]string {
	deltas := make(map[string]string)
	for _, event := range replay {
		if event.Type == "response.output_text.delta" && event.Delta != "" {
			deltas[replayDeltaField(event)] = event.Delta
		}
	}
	return deltas
}
