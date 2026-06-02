package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultRedisChannel = "nl:realtime:events"

type RedisFanout struct {
	client  *redis.Client
	hub     *Hub
	channel string
	nodeID  string
}

type redisFanoutMessage struct {
	NodeID  string   `json:"nodeId"`
	UserIDs []string `json:"userIds"`
	Event   Event    `json:"event"`
}

func NewRedisFanout(client *redis.Client, hub *Hub) *RedisFanout {
	return &RedisFanout{
		client:  client,
		hub:     hub,
		channel: defaultRedisChannel,
		nodeID:  fmt.Sprintf("node_%d", time.Now().UTC().UnixNano()),
	}
}

func (f *RedisFanout) Start(ctx context.Context, logger *slog.Logger) {
	if f == nil || f.client == nil || f.hub == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	pubsub := f.client.Subscribe(ctx, f.channel)
	go func() {
		defer pubsub.Close()
		for {
			message, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.WarnContext(ctx, "realtime redis fanout receive failed", "error", err)
				continue
			}

			var payload redisFanoutMessage
			if err := json.Unmarshal([]byte(message.Payload), &payload); err != nil {
				logger.WarnContext(ctx, "realtime redis fanout payload decode failed", "error", err)
				continue
			}
			if payload.NodeID == f.nodeID {
				continue
			}
			f.hub.PublishEvent(payload.UserIDs, payload.Event)
		}
	}()
}

func (f *RedisFanout) Publish(ctx context.Context, userIDs []string, event Event) error {
	if f == nil || f.client == nil {
		return nil
	}

	data, err := json.Marshal(redisFanoutMessage{
		NodeID:  f.nodeID,
		UserIDs: userIDs,
		Event:   event,
	})
	if err != nil {
		return err
	}
	return f.client.Publish(ctx, f.channel, data).Err()
}
