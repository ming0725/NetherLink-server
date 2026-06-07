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

type PresenceState struct {
	UserID     string    `json:"userUuid"`
	Status     string    `json:"status"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Connection struct {
	id        string
	userID    string
	sessionID string
	deviceID  string
	createdAt time.Time
	send      chan Event
	done      chan struct{}
	doneOnce  sync.Once
}

type Hub struct {
	nextSeq       atomic.Uint64
	nextConn      atomic.Uint64
	maxUserEvents int

	memoryReplayRequests      atomic.Uint64
	memoryReplayEvents        atomic.Uint64
	memoryReplaySyncRequired  atomic.Uint64
	durableReplayRequests     atomic.Uint64
	durableReplayEvents       atomic.Uint64
	durableReplaySyncRequired atomic.Uint64
	durableReplayFailures     atomic.Uint64

	mu              sync.Mutex
	connections     map[string]*Connection
	userConnections map[string]map[string]*Connection
	userEvents      map[string][]Event
	presence        map[string]PresenceState
}

type HubSnapshot struct {
	OnlineConnections         int    `json:"onlineConnections"`
	OnlineUsers               int    `json:"onlineUsers"`
	BufferedUsers             int    `json:"bufferedUsers"`
	PresenceUsers             int    `json:"presenceUsers"`
	LatestEventSeq            uint64 `json:"latestEventSeq"`
	MaxReplayEvents           int    `json:"maxReplayEvents"`
	MemoryReplayRequests      uint64 `json:"memoryReplayRequests"`
	MemoryReplayEvents        uint64 `json:"memoryReplayEvents"`
	MemoryReplaySyncRequired  uint64 `json:"memoryReplaySyncRequired"`
	DurableReplayRequests     uint64 `json:"durableReplayRequests"`
	DurableReplayEvents       uint64 `json:"durableReplayEvents"`
	DurableReplaySyncRequired uint64 `json:"durableReplaySyncRequired"`
	DurableReplayFailures     uint64 `json:"durableReplayFailures"`
}

func NewHub() *Hub {
	return &Hub{
		maxUserEvents:   defaultMaxUserEvents,
		connections:     make(map[string]*Connection),
		userConnections: make(map[string]map[string]*Connection),
		userEvents:      make(map[string][]Event),
		presence:        make(map[string]PresenceState),
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
	h.recordMemoryReplay(len(replay), syncRequired)

	return conn, replay, syncRequired
}

func (h *Hub) ReplayForUser(userID string, lastEventSeq uint64) ([]Event, bool) {
	if h == nil {
		return nil, false
	}

	h.mu.Lock()
	replay, syncRequired := replayEvents(h.userEvents[userID], h.maxUserEvents, lastEventSeq)
	h.mu.Unlock()
	h.recordMemoryReplay(len(replay), syncRequired)

	return replay, syncRequired
}

func (h *Hub) Disconnect(conn *Connection) bool {
	if h == nil || conn == nil {
		return false
	}

	lastUserConnection := false
	h.mu.Lock()
	if _, ok := h.connections[conn.id]; ok {
		delete(h.connections, conn.id)
		if userConns := h.userConnections[conn.userID]; userConns != nil {
			delete(userConns, conn.id)
			if len(userConns) == 0 {
				delete(h.userConnections, conn.userID)
				lastUserConnection = true
			}
		}
	}
	h.mu.Unlock()
	conn.close()
	return lastUserConnection
}

func (h *Hub) DisconnectSession(userID string, sessionID string, eventType string, payload any) int {
	if h == nil || userID == "" || sessionID == "" {
		return 0
	}

	event := h.newControlEvent(eventType, payload)
	var targets []*Connection

	h.mu.Lock()
	if userConns := h.userConnections[userID]; userConns != nil {
		for connID, conn := range userConns {
			if conn.sessionID != sessionID {
				continue
			}
			targets = append(targets, conn)
			delete(userConns, connID)
			delete(h.connections, connID)
		}
		if len(userConns) == 0 {
			delete(h.userConnections, userID)
		}
	}
	h.mu.Unlock()

	for _, conn := range targets {
		conn.deliverRequired(event)
	}
	return len(targets)
}

func (h *Hub) HasUserConnections(userID string) bool {
	if h == nil || userID == "" {
		return false
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.userConnections[userID]) > 0
}

func (h *Hub) SetPresence(userID string, status string, lastSeenAt time.Time) PresenceState {
	if h == nil {
		return PresenceState{}
	}
	if lastSeenAt.IsZero() {
		lastSeenAt = time.Now().UTC()
	}
	state := PresenceState{
		UserID:     userID,
		Status:     status,
		LastSeenAt: lastSeenAt.UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	h.mu.Lock()
	h.presence[userID] = state
	h.mu.Unlock()

	return state
}

func (h *Hub) Presence(userID string) (PresenceState, bool) {
	if h == nil {
		return PresenceState{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.presence[userID]
	return state, ok
}

func (h *Hub) Publish(userIDs []string, eventType string, payload any) Event {
	event := h.newEvent(eventType, payload)
	h.PublishEvent(userIDs, event)
	return event
}

func (h *Hub) PublishEvent(userIDs []string, event Event) {
	if h == nil || len(userIDs) == 0 {
		return
	}
	if event.EventSeq > 0 && event.EventID == "" {
		event.EventID = fmt.Sprintf("evt_%d", event.EventSeq)
	}
	h.observeEventSeq(event.EventSeq)

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

func (h *Hub) MaxReplayEvents() int {
	if h == nil {
		return 0
	}
	return h.maxUserEvents
}

func (h *Hub) RecordDurableReplay(eventCount int, syncRequired bool, failed bool) {
	if h == nil {
		return
	}
	h.durableReplayRequests.Add(1)
	if eventCount > 0 {
		h.durableReplayEvents.Add(uint64(eventCount))
	}
	if syncRequired {
		h.durableReplaySyncRequired.Add(1)
	}
	if failed {
		h.durableReplayFailures.Add(1)
	}
}

func (h *Hub) Snapshot() HubSnapshot {
	if h == nil {
		return HubSnapshot{}
	}

	h.mu.Lock()
	onlineConnections := len(h.connections)
	onlineUsers := len(h.userConnections)
	bufferedUsers := len(h.userEvents)
	presenceUsers := len(h.presence)
	h.mu.Unlock()

	return HubSnapshot{
		OnlineConnections:         onlineConnections,
		OnlineUsers:               onlineUsers,
		BufferedUsers:             bufferedUsers,
		PresenceUsers:             presenceUsers,
		LatestEventSeq:            h.nextSeq.Load(),
		MaxReplayEvents:           h.maxUserEvents,
		MemoryReplayRequests:      h.memoryReplayRequests.Load(),
		MemoryReplayEvents:        h.memoryReplayEvents.Load(),
		MemoryReplaySyncRequired:  h.memoryReplaySyncRequired.Load(),
		DurableReplayRequests:     h.durableReplayRequests.Load(),
		DurableReplayEvents:       h.durableReplayEvents.Load(),
		DurableReplaySyncRequired: h.durableReplaySyncRequired.Load(),
		DurableReplayFailures:     h.durableReplayFailures.Load(),
	}
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

func (c *Connection) deliverRequired(event Event) bool {
	if c == nil {
		return false
	}
	select {
	case c.send <- event:
		return true
	case <-c.done:
		return false
	case <-time.After(100 * time.Millisecond):
		c.close()
		return false
	}
}

func (c *Connection) close() {
	if c == nil {
		return
	}
	c.doneOnce.Do(func() {
		close(c.done)
	})
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

func (h *Hub) observeEventSeq(seq uint64) {
	if h == nil || seq == 0 {
		return
	}
	for {
		current := h.nextSeq.Load()
		if current >= seq || h.nextSeq.CompareAndSwap(current, seq) {
			return
		}
	}
}

func (h *Hub) recordMemoryReplay(eventCount int, syncRequired bool) {
	if h == nil {
		return
	}
	h.memoryReplayRequests.Add(1)
	if eventCount > 0 {
		h.memoryReplayEvents.Add(uint64(eventCount))
	}
	if syncRequired {
		h.memoryReplaySyncRequired.Add(1)
	}
}

func MarshalPayload(payload any) json.RawMessage {
	return marshalPayload(payload)
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
