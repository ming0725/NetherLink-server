package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	authsvc "nlserver/internal/auth"
	"nlserver/internal/realtime"
)

type websocketClientMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type websocketReadyPayload struct {
	ConnectionID string    `json:"connectionId"`
	UserUUID     string    `json:"userUuid"`
	SessionID    string    `json:"sessionId"`
	DeviceID     string    `json:"deviceId"`
	LastEventSeq uint64    `json:"lastEventSeq"`
	ConnectedAt  time.Time `json:"connectedAt"`
}

type websocketSyncRequiredPayload struct {
	Reason       string `json:"reason"`
	LastEventSeq uint64 `json:"lastEventSeq"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	token := webSocketAccessToken(r)
	if token == "" {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_MISSING", "Missing access token")
		return
	}
	if s.authService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Auth service is not configured")
		return
	}
	if s.realtimeHub == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Realtime service is not configured")
		return
	}

	principal, err := s.authService.ValidateAccessToken(r.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, authsvc.ErrTokenExpired):
			writeError(w, r, http.StatusUnauthorized, "TOKEN_EXPIRED", "Access token expired")
		default:
			writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		}
		return
	}

	lastEventSeq := parseLastEventSeq(r)
	websocket.Handler(func(conn *websocket.Conn) {
		s.serveWebSocket(conn, AuthPrincipal{
			AccessToken: token,
			UserID:      principal.UserID,
			SessionID:   principal.SessionID,
			DeviceID:    principal.DeviceID,
		}, lastEventSeq)
	}).ServeHTTP(w, r)
}

func (s *Server) serveWebSocket(conn *websocket.Conn, principal AuthPrincipal, lastEventSeq uint64) {
	defer conn.Close()

	connection, replay, syncRequired := s.realtimeHub.Connect(principal.UserID, principal.SessionID, principal.DeviceID, lastEventSeq)
	if connection == nil {
		return
	}
	defer s.realtimeHub.Disconnect(connection)

	s.realtimeHub.Send(connection, "realtime.ready", websocketReadyPayload{
		ConnectionID: connection.ID(),
		UserUUID:     principal.UserID,
		SessionID:    principal.SessionID,
		DeviceID:     principal.DeviceID,
		LastEventSeq: s.realtimeHub.LatestEventSeq(),
		ConnectedAt:  connection.CreatedAt(),
	})
	if syncRequired {
		s.realtimeHub.Send(connection, "sync.required", websocketSyncRequiredPayload{
			Reason:       "event backlog expired",
			LastEventSeq: lastEventSeq,
		})
	}
	for _, event := range replay {
		connection.Deliver(event)
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case event := <-connection.Events():
				if err := websocket.JSON.Send(conn, event); err != nil {
					return
				}
			case <-connection.Done():
				return
			}
		}
	}()

	for {
		var message websocketClientMessage
		if err := websocket.JSON.Receive(conn, &message); err != nil {
			return
		}
		if strings.EqualFold(strings.TrimSpace(message.Type), "ping") {
			s.realtimeHub.Send(connection, "realtime.pong", map[string]any{
				"receivedAt": time.Now().UTC(),
			})
		}
		select {
		case <-writerDone:
			return
		default:
		}
	}
}

func webSocketAccessToken(r *http.Request) string {
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.URL.Query().Get("accessToken")); token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func parseLastEventSeq(r *http.Request) uint64 {
	for _, value := range []string{
		r.URL.Query().Get("lastEventSeq"),
		r.URL.Query().Get("lastEventId"),
		r.Header.Get("Last-Event-Id"),
	} {
		if parsed := parseEventSeq(value); parsed > 0 {
			return parsed
		}
	}
	return 0
}

func parseEventSeq(value string) uint64 {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "evt_")
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func publishRealtimeEvent(hub *realtime.Hub, userIDs []string, eventType string, payload any) {
	if hub != nil {
		hub.Publish(userIDs, eventType, payload)
	}
}
