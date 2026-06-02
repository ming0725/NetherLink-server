package tests

import (
	"encoding/json"
	"testing"
	"time"

	"nlserver/internal/realtime"
)

func TestRealtimeHubPublishesOnlyToTargetUsers(t *testing.T) {
	hub := realtime.NewHub()
	userConn, _, _ := hub.Connect("user_a", "session_a", "device_a", 0)
	otherConn, _, _ := hub.Connect("user_b", "session_b", "device_b", 0)
	defer hub.Disconnect(userConn)
	defer hub.Disconnect(otherConn)

	hub.Publish([]string{"user_a"}, "chat.message.created", map[string]string{"messageId": "message_1"})

	event := receiveHubEvent(t, userConn)
	if event.Type != "chat.message.created" {
		t.Fatalf("event type = %q, want chat.message.created", event.Type)
	}
	if event.EventSeq == 0 || event.EventID == "" {
		t.Fatalf("event id/seq not assigned: %+v", event)
	}

	select {
	case unexpected := <-otherConn.Events():
		t.Fatalf("other user received event: %+v", unexpected)
	default:
	}
}

func TestRealtimeHubReplaysUserEventsAfterLastEventSeq(t *testing.T) {
	hub := realtime.NewHub()
	hub.Publish([]string{"user_a"}, "event.before", nil)
	first := hub.Publish([]string{"user_a"}, "event.target", map[string]string{"ok": "true"})
	hub.Publish([]string{"user_b"}, "event.other-user", nil)
	second := hub.Publish([]string{"user_a"}, "event.after", nil)

	conn, replay, syncRequired := hub.Connect("user_a", "session_a", "device_a", first.EventSeq-1)
	defer hub.Disconnect(conn)

	if syncRequired {
		t.Fatal("syncRequired = true, want false")
	}
	if len(replay) != 2 {
		t.Fatalf("replay length = %d, want 2", len(replay))
	}
	if replay[0].EventSeq != first.EventSeq || replay[1].EventSeq != second.EventSeq {
		t.Fatalf("replay seqs = %d,%d want %d,%d", replay[0].EventSeq, replay[1].EventSeq, first.EventSeq, second.EventSeq)
	}

	var payload map[string]string
	if err := json.Unmarshal(replay[0].Payload, &payload); err != nil {
		t.Fatalf("payload decode failed: %v", err)
	}
	if payload["ok"] != "true" {
		t.Fatalf("payload = %#v, want ok=true", payload)
	}
}

func TestRealtimeHubPublishesPersistedEvent(t *testing.T) {
	hub := realtime.NewHub()
	conn, _, _ := hub.Connect("user_a", "session_a", "device_a", 0)
	defer hub.Disconnect(conn)

	hub.PublishEvent([]string{"user_a"}, realtime.Event{
		EventSeq:  42,
		Type:      "chat.message.created",
		EmittedAt: time.Now().UTC(),
		Payload:   realtime.MarshalPayload(map[string]string{"messageId": "message_42"}),
	})

	event := receiveHubEvent(t, conn)
	if event.EventSeq != 42 || event.EventID != "evt_42" {
		t.Fatalf("event id/seq = %q/%d, want evt_42/42", event.EventID, event.EventSeq)
	}
	if hub.LatestEventSeq() != 42 {
		t.Fatalf("latest seq = %d, want 42", hub.LatestEventSeq())
	}
}

func receiveHubEvent(t *testing.T, conn *realtime.Connection) realtime.Event {
	t.Helper()

	select {
	case event := <-conn.Events():
		return event
	default:
		t.Fatal("expected event")
	}
	return realtime.Event{}
}
