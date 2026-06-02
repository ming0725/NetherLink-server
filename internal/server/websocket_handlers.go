package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	authsvc "nlserver/internal/auth"
	chatsvc "nlserver/internal/chat"
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

type websocketErrorPayload struct {
	RequestType string `json:"requestType"`
	Code        string `json:"code"`
	Message     string `json:"message"`
}

type websocketSendMessagePayload struct {
	ConversationID string `json:"conversationId"`
	sendMessageRequest
}

type websocketMessageSentPayload struct {
	ClientMessageID string          `json:"clientMessageId"`
	Message         messageResponse `json:"message"`
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
		s.serveWebSocket(r.Context(), conn, AuthPrincipal{
			AccessToken: token,
			UserID:      principal.UserID,
			SessionID:   principal.SessionID,
			DeviceID:    principal.DeviceID,
		}, lastEventSeq)
	}).ServeHTTP(w, r)
}

func (s *Server) serveWebSocket(ctx context.Context, conn *websocket.Conn, principal AuthPrincipal, lastEventSeq uint64) {
	defer conn.Close()

	connection, replay, syncRequired := s.realtimeHub.Connect(principal.UserID, principal.SessionID, principal.DeviceID, lastEventSeq)
	if connection == nil {
		return
	}
	defer s.realtimeHub.Disconnect(connection)

	if durableReplay, durableSyncRequired, ok := s.loadDurableReplay(ctx, principal.UserID, lastEventSeq); ok {
		replay = durableReplay
		syncRequired = durableSyncRequired
	}

	s.realtimeHub.Send(connection, "realtime.ready", websocketReadyPayload{
		ConnectionID: connection.ID(),
		UserUUID:     principal.UserID,
		SessionID:    principal.SessionID,
		DeviceID:     principal.DeviceID,
		LastEventSeq: s.latestRealtimeEventSeq(ctx),
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
		switch strings.ToLower(strings.TrimSpace(message.Type)) {
		case "ping":
			s.realtimeHub.Send(connection, "realtime.pong", map[string]any{
				"receivedAt": time.Now().UTC(),
			})
		case "chat.message.send":
			s.handleWebSocketSendMessage(ctx, connection, principal, message.Payload)
		default:
			s.sendWebSocketError(connection, message.Type, "UNKNOWN_MESSAGE_TYPE", "Unsupported WebSocket message type")
		}
		select {
		case <-writerDone:
			return
		default:
		}
	}
}

func (s *Server) loadDurableReplay(ctx context.Context, userID string, lastEventSeq uint64) ([]realtime.Event, bool, bool) {
	if s.repos.RealtimeEvents == nil || lastEventSeq == 0 {
		return nil, false, false
	}

	events, hasMore, err := s.repos.RealtimeEvents.ListForUserAfter(ctx, userID, lastEventSeq, s.realtimeHub.MaxReplayEvents())
	if err != nil {
		s.realtimeHub.RecordDurableReplay(0, false, true)
		s.logger.WarnContext(ctx, "failed to load durable realtime replay", "error", err, "user_id", userID)
		return nil, false, false
	}

	replay := make([]realtime.Event, 0, len(events))
	for _, event := range events {
		replay = append(replay, realtime.Event{
			EventID:   event.EventID,
			EventSeq:  event.EventSeq,
			Type:      event.Type,
			EmittedAt: event.EmittedAt,
			Payload:   event.Payload,
		})
	}
	s.realtimeHub.RecordDurableReplay(len(replay), hasMore, false)
	return replay, hasMore, true
}

func (s *Server) latestRealtimeEventSeq(ctx context.Context) uint64 {
	if s.repos.RealtimeEvents != nil {
		latest, err := s.repos.RealtimeEvents.LatestSeq(ctx)
		if err == nil {
			return latest
		}
		s.logger.WarnContext(ctx, "failed to load latest realtime event seq", "error", err)
	}
	return s.realtimeHub.LatestEventSeq()
}

func (s *Server) handleWebSocketSendMessage(ctx context.Context, conn *realtime.Connection, principal AuthPrincipal, payload json.RawMessage) {
	if s.chatService == nil {
		s.sendWebSocketError(conn, "chat.message.send", "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	var req websocketSendMessagePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendWebSocketError(conn, "chat.message.send", "VALIDATION_ERROR", "Invalid message payload")
		return
	}
	if err := validateUUID(req.ConversationID, "conversationId"); err != nil {
		s.sendWebSocketError(conn, "chat.message.send", err.Code, err.Message)
		return
	}
	if err := validateSendMessageRequest(req.sendMessageRequest); err != nil {
		s.sendWebSocketError(conn, "chat.message.send", err.Code, err.Message)
		return
	}

	attachments := make([]chatsvc.AttachmentInput, 0, len(req.Attachments))
	for _, attachment := range req.Attachments {
		attachments = append(attachments, chatsvc.AttachmentInput(attachment))
	}

	message, err := s.chatService.SendMessage(ctx, chatsvc.SendMessageInput{
		UserID:              principal.UserID,
		ConversationID:      req.ConversationID,
		ClientMessageID:     req.ClientMessageID,
		ClientSentAt:        req.ClientSentAt,
		Type:                req.Type,
		Content:             req.Content,
		Attachments:         attachments,
		ReferencedMessageID: req.ReferencedMessageID,
	})
	if err != nil {
		code, message := chatErrorCodeAndMessage(err)
		s.sendWebSocketError(conn, "chat.message.send", code, message)
		return
	}

	response, err := s.makeMessageResponseFromContext(ctx, message)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to build websocket message response", "error", err)
		s.sendWebSocketError(conn, "chat.message.send", "INTERNAL_ERROR", "Internal server error")
		return
	}

	s.publishConversationMessage(ctx, req.ConversationID, "chat.message.created", response)
	s.publishAllConversationSyncStates(ctx, req.ConversationID, "message.created")
	s.realtimeHub.Send(conn, "chat.message.sent", websocketMessageSentPayload{
		ClientMessageID: req.ClientMessageID,
		Message:         response,
	})
}

func (s *Server) sendWebSocketError(conn *realtime.Connection, requestType string, code string, message string) {
	s.realtimeHub.Send(conn, "client.error", websocketErrorPayload{
		RequestType: requestType,
		Code:        code,
		Message:     message,
	})
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

func (s *Server) publishRealtimeEvent(ctx context.Context, userIDs []string, eventType string, payload any) realtime.Event {
	if s.realtimeHub == nil {
		return realtime.Event{}
	}
	if s.repos.RealtimeEvents != nil {
		event, err := s.repos.RealtimeEvents.Create(ctx, userIDs, eventType, realtime.MarshalPayload(payload))
		if err == nil {
			realtimeEvent := realtime.Event{
				EventID:   event.EventID,
				EventSeq:  event.EventSeq,
				Type:      event.Type,
				EmittedAt: event.EmittedAt,
				Payload:   event.Payload,
			}
			s.realtimeHub.PublishEvent(userIDs, realtimeEvent)
			if err := s.realtimeFanout.Publish(ctx, userIDs, realtimeEvent); err != nil {
				s.logger.WarnContext(ctx, "failed to publish realtime redis fanout", "error", err, "event_type", eventType)
			}
			return realtimeEvent
		}
		s.logger.WarnContext(ctx, "failed to persist realtime event; falling back to memory", "error", err, "event_type", eventType)
	}
	return s.realtimeHub.Publish(userIDs, eventType, payload)
}
