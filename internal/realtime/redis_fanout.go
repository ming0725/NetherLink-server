package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultRedisChannel = "nl:realtime:events"

type RedisFanout struct {
	client  *redis.Client
	hub     *Hub
	channel string
	nodeID  string

	running atomic.Bool

	published       atomic.Uint64
	publishFailures atomic.Uint64
	received        atomic.Uint64
	receiveFailures atomic.Uint64
	decodeFailures  atomic.Uint64
	ignoredSelf     atomic.Uint64
	delivered       atomic.Uint64

	mu                 sync.Mutex
	lastPublishAt      time.Time
	lastPublishErrorAt time.Time
	lastPublishError   string
	lastReceiveAt      time.Time
	lastReceiveErrorAt time.Time
	lastReceiveError   string
	lastDecodeErrorAt  time.Time
	lastDecodeError    string
}

type redisFanoutMessage struct {
	NodeID  string   `json:"nodeId"`
	UserIDs []string `json:"userIds"`
	Event   Event    `json:"event"`
}

type RedisFanoutSnapshot struct {
	Enabled            bool      `json:"enabled"`
	Running            bool      `json:"running"`
	Channel            string    `json:"channel"`
	NodeID             string    `json:"nodeId"`
	Published          uint64    `json:"published"`
	PublishFailures    uint64    `json:"publishFailures"`
	Received           uint64    `json:"received"`
	ReceiveFailures    uint64    `json:"receiveFailures"`
	DecodeFailures     uint64    `json:"decodeFailures"`
	IgnoredSelf        uint64    `json:"ignoredSelf"`
	Delivered          uint64    `json:"delivered"`
	LastPublishAt      time.Time `json:"lastPublishAt,omitempty"`
	LastPublishErrorAt time.Time `json:"lastPublishErrorAt,omitempty"`
	LastPublishError   string    `json:"lastPublishError,omitempty"`
	LastReceiveAt      time.Time `json:"lastReceiveAt,omitempty"`
	LastReceiveErrorAt time.Time `json:"lastReceiveErrorAt,omitempty"`
	LastReceiveError   string    `json:"lastReceiveError,omitempty"`
	LastDecodeErrorAt  time.Time `json:"lastDecodeErrorAt,omitempty"`
	LastDecodeError    string    `json:"lastDecodeError,omitempty"`
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
	f.running.Store(true)
	go func() {
		defer func() {
			f.running.Store(false)
			_ = pubsub.Close()
		}()
		for {
			message, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				f.recordReceiveError(err)
				logger.WarnContext(ctx, "realtime redis fanout receive failed", "error", err)
				continue
			}
			f.recordReceive()

			var payload redisFanoutMessage
			if err := json.Unmarshal([]byte(message.Payload), &payload); err != nil {
				f.recordDecodeError(err)
				logger.WarnContext(ctx, "realtime redis fanout payload decode failed", "error", err)
				continue
			}
			if payload.NodeID == f.nodeID {
				f.ignoredSelf.Add(1)
				continue
			}
			f.hub.PublishEvent(payload.UserIDs, payload.Event)
			f.delivered.Add(1)
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
	if err := f.client.Publish(ctx, f.channel, data).Err(); err != nil {
		f.recordPublishError(err)
		return err
	}
	f.recordPublish()
	return nil
}

func (f *RedisFanout) Snapshot() RedisFanoutSnapshot {
	if f == nil || f.client == nil {
		return RedisFanoutSnapshot{}
	}

	f.mu.Lock()
	lastPublishAt := f.lastPublishAt
	lastPublishErrorAt := f.lastPublishErrorAt
	lastPublishError := f.lastPublishError
	lastReceiveAt := f.lastReceiveAt
	lastReceiveErrorAt := f.lastReceiveErrorAt
	lastReceiveError := f.lastReceiveError
	lastDecodeErrorAt := f.lastDecodeErrorAt
	lastDecodeError := f.lastDecodeError
	f.mu.Unlock()

	return RedisFanoutSnapshot{
		Enabled:            true,
		Running:            f.running.Load(),
		Channel:            f.channel,
		NodeID:             f.nodeID,
		Published:          f.published.Load(),
		PublishFailures:    f.publishFailures.Load(),
		Received:           f.received.Load(),
		ReceiveFailures:    f.receiveFailures.Load(),
		DecodeFailures:     f.decodeFailures.Load(),
		IgnoredSelf:        f.ignoredSelf.Load(),
		Delivered:          f.delivered.Load(),
		LastPublishAt:      lastPublishAt,
		LastPublishErrorAt: lastPublishErrorAt,
		LastPublishError:   lastPublishError,
		LastReceiveAt:      lastReceiveAt,
		LastReceiveErrorAt: lastReceiveErrorAt,
		LastReceiveError:   lastReceiveError,
		LastDecodeErrorAt:  lastDecodeErrorAt,
		LastDecodeError:    lastDecodeError,
	}
}

func (f *RedisFanout) recordPublish() {
	f.published.Add(1)
	f.mu.Lock()
	f.lastPublishAt = time.Now().UTC()
	f.mu.Unlock()
}

func (f *RedisFanout) recordPublishError(err error) {
	f.publishFailures.Add(1)
	f.mu.Lock()
	f.lastPublishErrorAt = time.Now().UTC()
	f.lastPublishError = err.Error()
	f.mu.Unlock()
}

func (f *RedisFanout) recordReceive() {
	f.received.Add(1)
	f.mu.Lock()
	f.lastReceiveAt = time.Now().UTC()
	f.mu.Unlock()
}

func (f *RedisFanout) recordReceiveError(err error) {
	f.receiveFailures.Add(1)
	f.mu.Lock()
	f.lastReceiveErrorAt = time.Now().UTC()
	f.lastReceiveError = err.Error()
	f.mu.Unlock()
}

func (f *RedisFanout) recordDecodeError(err error) {
	f.decodeFailures.Add(1)
	f.mu.Lock()
	f.lastDecodeErrorAt = time.Now().UTC()
	f.lastDecodeError = err.Error()
	f.mu.Unlock()
}
