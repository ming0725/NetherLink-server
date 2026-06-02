package realtime

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const defaultMaxUserEvents = 256

type Event struct {
	EventID   string          `json:"eventId"`
	EventSeq  uint64          `json:"eventSeq"`
	Type      string          `json:"type"`
	EmittedAt time.Time       `json:"emittedAt"`
	Payload   json.RawMessage `json:"payload"`
}

type Connection struct {
	id        string
	userID    string
	sessionID string
	deviceID  string
	createdAt time.Time
	send      chan Event
	done      chan struct{}
}

type Hub struct {
	nextSeq       atomic.Uint64
	nextConn      atomic.Uint64
	maxUserEvents int

	mu              sync.Mutex
	connections     map[string]*Connection
	userConnections map[string]map[string]*Connection
	userEvents      map[string][]Event
}

func NewHub() *Hub {
	return &Hub{
		maxUserEvents:   defaultMaxUserEvents,
		connections:     make(map[string]*Connection),
		userConnections: make(map[string]map[string]*Connection),
		userEvents:      make(map[string][]Event),
	}
}

func (h *Hub) Connect(userID string, sessionID string, deviceID string, lastEventSeq uint64) (*Connection, []Event, bool) {
	if h == nil {
		return nil, nil, false
	}

	conn := &Connection{
		id:        fmt.Sprintf("conn_%d", h.nextConn.Add(1)),
		userID:    userID,
		sessionID: sessionID,
		deviceID:  deviceID,
		createdAt: time.Now().UTC(),
		send:      make(chan Event, 64),
		done:      make(chan struct{}),
	}

	h.mu.Lock()
	h.connections[conn.id] = conn
	if h.userConnections[userID] == nil {
		h.userConnections[userID] = make(map[string]*Connection)
	}
	h.userConnections[userID][conn.id] = conn
	replay, syncRequired := replayEvents(h.userEvents[userID], h.maxUserEvents, lastEventSeq)
	h.mu.Unlock()

	return conn, replay, syncRequired
}

func (h *Hub) Disconnect(conn *Connection) {
	if h == nil || conn == nil {
		return
	}

	h.mu.Lock()
	if _, ok := h.connections[conn.id]; ok {
		delete(h.connections, conn.id)
		if userConns := h.userConnections[conn.userID]; userConns != nil {
			delete(userConns, conn.id)
			if len(userConns) == 0 {
				delete(h.userConnections, conn.userID)
			}
		}
		close(conn.done)
	}
	h.mu.Unlock()
}

func (h *Hub) Publish(userIDs []string, eventType string, payload any) Event {
	event := h.newEvent(eventType, payload)
	if h == nil || len(userIDs) == 0 {
		return event
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	seen := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}

		h.userEvents[userID] = appendTrimmed(h.userEvents[userID], event, h.maxUserEvents)
		for _, conn := range h.userConnections[userID] {
			conn.deliver(event)
		}
	}

	return event
}

func (h *Hub) Send(conn *Connection, eventType string, payload any) Event {
	event := h.newControlEvent(eventType, payload)
	if conn != nil {
		conn.deliver(event)
	}
	return event
}

func (h *Hub) OnlineConnectionCount() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.connections)
}

func (h *Hub) LatestEventSeq() uint64 {
	if h == nil {
		return 0
	}
	return h.nextSeq.Load()
}

func (c *Connection) ID() string {
	if c == nil {
		return ""
	}
	return c.id
}

func (c *Connection) UserID() string {
	if c == nil {
		return ""
	}
	return c.userID
}

func (c *Connection) SessionID() string {
	if c == nil {
		return ""
	}
	return c.sessionID
}

func (c *Connection) DeviceID() string {
	if c == nil {
		return ""
	}
	return c.deviceID
}

func (c *Connection) CreatedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.createdAt
}

func (c *Connection) Events() <-chan Event {
	if c == nil {
		return nil
	}
	return c.send
}

func (c *Connection) Deliver(event Event) {
	if c != nil {
		c.deliver(event)
	}
}

func (c *Connection) Done() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.done
}

func (c *Connection) deliver(event Event) {
	select {
	case c.send <- event:
	case <-c.done:
	default:
	}
}

func (h *Hub) newEvent(eventType string, payload any) Event {
	seq := uint64(0)
	if h != nil {
		seq = h.nextSeq.Add(1)
	}
	return Event{
		EventID:   fmt.Sprintf("evt_%d", seq),
		EventSeq:  seq,
		Type:      eventType,
		EmittedAt: time.Now().UTC(),
		Payload:   marshalPayload(payload),
	}
}

func (h *Hub) newControlEvent(eventType string, payload any) Event {
	return Event{
		EventID:   "",
		EventSeq:  0,
		Type:      eventType,
		EmittedAt: time.Now().UTC(),
		Payload:   marshalPayload(payload),
	}
}

func appendTrimmed(events []Event, event Event, maxEvents int) []Event {
	events = append(events, event)
	if maxEvents <= 0 || len(events) <= maxEvents {
		return events
	}
	return events[len(events)-maxEvents:]
}

func replayEvents(events []Event, maxEvents int, lastEventSeq uint64) ([]Event, bool) {
	if lastEventSeq == 0 {
		return nil, false
	}
	if len(events) == 0 {
		return nil, false
	}

	syncRequired := len(events) >= maxEvents && events[0].EventSeq > lastEventSeq+1
	replay := make([]Event, 0)
	for _, event := range events {
		if event.EventSeq > lastEventSeq {
			replay = append(replay, event)
		}
	}
	return replay, syncRequired
}

func marshalPayload(payload any) json.RawMessage {
	if payload == nil {
		return json.RawMessage(`{}`)
	}
	if raw, ok := payload.(json.RawMessage); ok && len(raw) > 0 {
		return raw
	}
	data, err := json.Marshal(payload)
	if err != nil || len(data) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}
