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

	aisvc "nlserver/internal/ai"
	authsvc "nlserver/internal/auth"
	chatsvc "nlserver/internal/chat"
	"nlserver/internal/realtime"
	"nlserver/internal/repository"
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

type websocketAuthSessionRevokedPayload struct {
	SessionID string    `json:"sessionId"`
	DeviceID  string    `json:"deviceId"`
	Reason    string    `json:"reason"`
	RevokedAt time.Time `json:"revokedAt"`
}

type websocketSyncRequiredPayload struct {
	Reason       string `json:"reason"`
	LastEventSeq uint64 `json:"lastEventSeq"`
}

type websocketEventReplayPayload struct {
	LastEventSeq uint64           `json:"lastEventSeq"`
	Events       []realtime.Event `json:"events"`
}

type websocketErrorPayload struct {
	RequestType string `json:"requestType"`
	Code        string `json:"code"`
	Message     string `json:"message"`
}

type websocketResumePayload struct {
	LastEventID  string `json:"lastEventId"`
	LastEventSeq uint64 `json:"lastEventSeq"`
}

type websocketSendMessagePayload struct {
	ConversationID string `json:"conversationId"`
	sendMessageRequest
}

type websocketRecallMessagePayload struct {
	ConversationID string `json:"conversationId"`
	MessageID      string `json:"messageId"`
}

type websocketChatReadPayload struct {
	ConversationID string `json:"conversationId"`
	markConversationReadRequest
}

type websocketChatReadUpdatedPayload struct {
	ConversationID    string  `json:"conversationId"`
	UserUUID          string  `json:"userUuid"`
	LastReadMessageID *string `json:"lastReadMessageId"`
	UnreadCount       int     `json:"unreadCount"`
}

type websocketPresenceUpdatePayload struct {
	Status string `json:"status"`
}

type websocketPresenceUpdatedPayload struct {
	UserUUID   string    `json:"userUuid"`
	Status     string    `json:"status"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type websocketMessageSentPayload struct {
	ClientMessageID string                       `json:"clientMessageId"`
	Message         messageResponse              `json:"message"`
	Refs            *referencedResourcesResponse `json:"refs,omitempty"`
}

type websocketAIMessagePayload struct {
	ConversationID  string   `json:"conversationId"`
	Message         string   `json:"message"`
	ClientMessageID string   `json:"clientMessageId"`
	AIFileIDs       []string `json:"aiFileIds"`
	Model           string   `json:"model"`
	ThinkingType    string   `json:"thinkingType"`
	ReasoningEffort string   `json:"reasoningEffort"`
}

type websocketAIStreamCancelPayload struct {
	StreamID string `json:"streamId"`
}

type websocketAIStreamStartedPayload struct {
	StreamID        string `json:"streamId"`
	ConversationID  string `json:"conversationId"`
	ClientMessageID string `json:"clientMessageId,omitempty"`
}

type websocketAIStreamChunkPayload struct {
	StreamID string `json:"streamId"`
	Delta    string `json:"delta"`
}

type websocketAIStreamThinkingPayload struct {
	StreamID string `json:"streamId"`
	Active   bool   `json:"active"`
}

type websocketAIStreamDonePayload struct {
	StreamID         string                   `json:"streamId"`
	UserMessage      aiMessageResponse        `json:"userMessage"`
	AssistantMessage aiMessageResponse        `json:"assistantMessage"`
	UsedFiles        []aiUploadedFileResponse `json:"usedFiles"`
	FinishReason     string                   `json:"finishReason,omitempty"`
	Title            *string                  `json:"title,omitempty"`
}

type websocketAIStreamCancelledPayload struct {
	StreamID         string                   `json:"streamId"`
	UserMessage      *aiMessageResponse       `json:"userMessage,omitempty"`
	AssistantMessage *aiMessageResponse       `json:"assistantMessage,omitempty"`
	UsedFiles        []aiUploadedFileResponse `json:"usedFiles,omitempty"`
	FinishReason     string                   `json:"finishReason,omitempty"`
	Title            *string                  `json:"title,omitempty"`
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

	authPrincipal := AuthPrincipal{
		AccessToken: token,
		UserID:      principal.UserID,
		SessionID:   principal.SessionID,
		DeviceID:    principal.DeviceID,
	}
	setAccessLogPrincipal(r.Context(), authPrincipal)

	lastEventSeq := parseLastEventSeq(r)
	websocket.Server{
		Handshake: acceptWebSocketOrigin,
		Handler: func(conn *websocket.Conn) {
			s.serveWebSocket(r.Context(), conn, authPrincipal, lastEventSeq)
		},
	}.ServeHTTP(w, r)
}

func acceptWebSocketOrigin(config *websocket.Config, req *http.Request) error {
	origin, err := websocket.Origin(config, req)
	if err != nil {
		return err
	}
	config.Origin = origin
	return nil
}

func (s *Server) serveWebSocket(ctx context.Context, conn *websocket.Conn, principal AuthPrincipal, lastEventSeq uint64) {
	defer conn.Close()

	connection, replay, syncRequired := s.realtimeHub.Connect(principal.UserID, principal.SessionID, principal.DeviceID, lastEventSeq)
	if connection == nil {
		return
	}
	defer func() {
		if s.aiStreams != nil {
			s.aiStreams.CancelConnection(connection.ID())
		}
		if s.realtimeHub.Disconnect(connection) {
			s.handleWebSocketPresenceDisconnect(ctx, principal.UserID)
		}
	}()
	s.handleWebSocketPresenceConnect(ctx, principal.UserID)

	if durableReplay, durableSyncRequired, ok := s.loadDurableReplay(ctx, principal.UserID, lastEventSeq); ok {
		replay = durableReplay
		syncRequired = durableSyncRequired
	}
	replay = filterSelfOfflinePresenceEvents(principal.UserID, replay)

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
				if event.Type == "auth.session.revoked" && websocketAuthSessionRevokedMatches(event, connection.SessionID()) {
					_ = conn.Close()
					return
				}
			case <-connection.Done():
				_ = conn.Close()
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
		case "resume":
			s.handleWebSocketResume(ctx, connection, principal, message.Payload)
		case "chat.message.send":
			s.handleWebSocketSendMessage(ctx, connection, principal, message.Payload)
		case "chat.message.recall":
			s.handleWebSocketRecallMessage(ctx, connection, principal, message.Payload)
		case "chat.read":
			s.handleWebSocketChatRead(ctx, connection, principal, message.Payload)
		case "presence.update":
			s.handleWebSocketPresenceUpdate(ctx, connection, principal, message.Payload)
		case "ai.message.send":
			s.handleWebSocketAIMessageSend(ctx, connection, principal, message.Payload)
		case "ai.stream.cancel":
			s.handleWebSocketAIStreamCancel(connection, principal, message.Payload)
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

func (s *Server) handleWebSocketResume(ctx context.Context, conn *realtime.Connection, principal AuthPrincipal, payload json.RawMessage) {
	var req websocketResumePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendWebSocketError(conn, "resume", "VALIDATION_ERROR", "Invalid resume payload")
		return
	}
	lastEventSeq := req.LastEventSeq
	if lastEventSeq == 0 {
		lastEventSeq = parseEventSeq(req.LastEventID)
	}
	if lastEventSeq == 0 {
		s.sendWebSocketError(conn, "resume", "VALIDATION_ERROR", "lastEventSeq or lastEventId is required")
		return
	}

	replay, syncRequired, ok := s.loadDurableReplay(ctx, principal.UserID, lastEventSeq)
	if !ok {
		replay, syncRequired = s.realtimeHub.ReplayForUser(principal.UserID, lastEventSeq)
	}
	if syncRequired {
		s.realtimeHub.Send(conn, "sync.required", websocketSyncRequiredPayload{
			Reason:       "event backlog expired",
			LastEventSeq: lastEventSeq,
		})
		return
	}
	replay = filterSelfOfflinePresenceEvents(principal.UserID, replay)

	s.realtimeHub.Send(conn, "event.replay", websocketEventReplayPayload{
		LastEventSeq: lastEventSeq,
		Events:       replay,
	})
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
		Refs:            s.makeRealtimeMessageRefs(ctx, req.ConversationID, response),
	})
}

func (s *Server) handleWebSocketRecallMessage(ctx context.Context, conn *realtime.Connection, principal AuthPrincipal, payload json.RawMessage) {
	if s.chatService == nil {
		s.sendWebSocketError(conn, "chat.message.recall", "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	var req websocketRecallMessagePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendWebSocketError(conn, "chat.message.recall", "VALIDATION_ERROR", "Invalid recall payload")
		return
	}
	if err := validateUUID(req.ConversationID, "conversationId"); err != nil {
		s.sendWebSocketError(conn, "chat.message.recall", err.Code, err.Message)
		return
	}
	if err := validateUUID(req.MessageID, "messageId"); err != nil {
		s.sendWebSocketError(conn, "chat.message.recall", err.Code, err.Message)
		return
	}

	message, err := s.chatService.RecallMessage(ctx, chatsvc.RecallMessageInput{
		UserID:         principal.UserID,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
	})
	if err != nil {
		code, message := chatErrorCodeAndMessage(err)
		s.sendWebSocketError(conn, "chat.message.recall", code, message)
		return
	}

	response, err := s.makeMessageResponseFromContext(ctx, message)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to build websocket recall response", "error", err)
		s.sendWebSocketError(conn, "chat.message.recall", "INTERNAL_ERROR", "Internal server error")
		return
	}

	s.publishConversationMessage(ctx, req.ConversationID, "chat.message.recalled", response)
}

func (s *Server) handleWebSocketChatRead(ctx context.Context, conn *realtime.Connection, principal AuthPrincipal, payload json.RawMessage) {
	if s.chatService == nil {
		s.sendWebSocketError(conn, "chat.read", "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	var req websocketChatReadPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendWebSocketError(conn, "chat.read", "VALIDATION_ERROR", "Invalid read payload")
		return
	}
	if err := validateUUID(req.ConversationID, "conversationId"); err != nil {
		s.sendWebSocketError(conn, "chat.read", err.Code, err.Message)
		return
	}
	if req.LastReadMessageID != nil {
		if err := validateUUID(*req.LastReadMessageID, "lastReadMessageId"); err != nil {
			s.sendWebSocketError(conn, "chat.read", err.Code, err.Message)
			return
		}
	}

	participant, err := s.chatService.MarkConversationRead(ctx, chatsvc.MarkConversationReadInput{
		UserID:            principal.UserID,
		ConversationID:    req.ConversationID,
		LastReadMessageID: req.LastReadMessageID,
		LastReadAt:        req.LastReadAt,
	})
	if err != nil {
		code, message := chatErrorCodeAndMessage(err)
		s.sendWebSocketError(conn, "chat.read", code, message)
		return
	}

	s.publishConversationSyncState(ctx, participant, "read")
	s.publishChatReadUpdated(ctx, participant)
}

func (s *Server) handleWebSocketPresenceUpdate(ctx context.Context, conn *realtime.Connection, principal AuthPrincipal, payload json.RawMessage) {
	var req websocketPresenceUpdatePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendWebSocketError(conn, "presence.update", "VALIDATION_ERROR", "Invalid presence payload")
		return
	}
	status := normalizeSelectablePresenceStatus(req.Status)
	if status == "" {
		s.sendWebSocketError(conn, "presence.update", "VALIDATION_ERROR", "Invalid presence status")
		return
	}

	now := time.Now().UTC()
	if s.repos.Users != nil {
		if err := s.repos.Users.UpdatePresenceStatus(ctx, principal.UserID, status); err != nil {
			s.logger.WarnContext(ctx, "failed to update persisted presence status", "error", err, "user_id", principal.UserID, "status", status)
		}
	}
	if s.repos.Users != nil && status != "invisible" {
		if err := s.repos.Users.TouchLastSeen(ctx, principal.UserID, now); err != nil {
			s.logger.WarnContext(ctx, "failed to touch user last seen for presence update", "error", err, "user_id", principal.UserID)
		}
	}
	state := s.realtimeHub.SetPresence(principal.UserID, status, now)
	s.publishPresenceRealtimeEvent(ctx, principal.UserID, state.Status, state.LastSeenAt)
}

func (s *Server) handleWebSocketPresenceConnect(ctx context.Context, userID string) {
	if s.realtimeHub == nil {
		return
	}
	current, ok := s.realtimeHub.Presence(userID)
	if ok && current.Status != "offline" {
		return
	}

	now := time.Now().UTC()
	status := s.persistedPresenceStatus(ctx, userID)
	if status == "" {
		status = "online"
	}
	if s.repos.Users != nil && status != "invisible" {
		if err := s.repos.Users.TouchLastSeen(ctx, userID, now); err != nil {
			s.logger.WarnContext(ctx, "failed to touch user last seen for websocket connect", "error", err, "user_id", userID)
		}
	}
	state := s.realtimeHub.SetPresence(userID, status, now)
	s.publishPresenceConnectRealtimeEvent(ctx, userID, state.Status, state.LastSeenAt)
}

func (s *Server) handleWebSocketPresenceDisconnect(ctx context.Context, userID string) {
	if s.realtimeHub == nil {
		return
	}
	current, ok := s.realtimeHub.Presence(userID)
	if !ok || current.Status == "offline" || current.Status == "invisible" {
		return
	}

	now := time.Now().UTC()
	if s.repos.Users != nil {
		if err := s.repos.Users.TouchLastSeen(ctx, userID, now); err != nil {
			s.logger.WarnContext(ctx, "failed to touch user last seen for websocket disconnect", "error", err, "user_id", userID)
		}
	}
	state := s.realtimeHub.SetPresence(userID, "offline", now)
	s.publishPresenceRealtimeEventToFriends(ctx, userID, state.Status, state.LastSeenAt)
}

func (s *Server) persistedPresenceStatus(ctx context.Context, userID string) string {
	if s.repos.Users == nil {
		return ""
	}
	user, err := s.repos.Users.GetByID(ctx, userID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load persisted presence status", "error", err, "user_id", userID)
		return ""
	}
	return normalizeSelectablePresenceStatus(user.PresenceStatus)
}

func makePresenceUpdatedPayload(userID string, status string, lastSeenAt time.Time) websocketPresenceUpdatedPayload {
	return websocketPresenceUpdatedPayload{
		UserUUID:   userID,
		Status:     status,
		LastSeenAt: lastSeenAt,
	}
}

func filterSelfOfflinePresenceEvents(userID string, events []realtime.Event) []realtime.Event {
	if userID == "" || len(events) == 0 {
		return events
	}
	filtered := events[:0]
	for _, event := range events {
		if isSelfOfflinePresenceEvent(userID, event) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func isSelfOfflinePresenceEvent(userID string, event realtime.Event) bool {
	if event.Type != "presence.updated" {
		return false
	}
	var payload websocketPresenceUpdatedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false
	}
	return payload.UserUUID == userID && payload.Status == "offline"
}

func externallyVisiblePresenceStatus(status string) string {
	if status == "invisible" {
		return "offline"
	}
	if status == "" {
		return "offline"
	}
	return status
}

func (s *Server) sendWebSocketError(conn *realtime.Connection, requestType string, code string, message string) {
	s.realtimeHub.Send(conn, "client.error", websocketErrorPayload{
		RequestType: requestType,
		Code:        code,
		Message:     message,
	})
}

func websocketAuthSessionRevokedMatches(event realtime.Event, sessionID string) bool {
	var payload websocketAuthSessionRevokedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return true
	}
	return payload.SessionID == "" || payload.SessionID == sessionID
}

func (s *Server) handleWebSocketAIMessageSend(ctx context.Context, conn *realtime.Connection, principal AuthPrincipal, payload json.RawMessage) {
	if s.aiService == nil {
		s.sendWebSocketError(conn, "ai.message.send", "SERVICE_NOT_READY", "AI service is not configured")
		return
	}
	var req websocketAIMessagePayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendWebSocketError(conn, "ai.message.send", "VALIDATION_ERROR", "Invalid AI message payload")
		return
	}
	if err := validateUUID(req.ConversationID, "conversationId"); err != nil {
		s.sendWebSocketError(conn, "ai.message.send", err.Code, err.Message)
		return
	}
	for _, aiFileID := range req.AIFileIDs {
		if err := validateUUID(aiFileID, "aiFileId"); err != nil {
			s.sendWebSocketError(conn, "ai.message.send", err.Code, err.Message)
			return
		}
	}

	receiptClaim, reqErr, err := s.claimAIStreamReceipt(ctx, principal.UserID, aiStreamOperationType(req.ConversationID), req.ClientMessageID)
	if reqErr != nil {
		s.sendWebSocketError(conn, "ai.message.send", reqErr.Code, reqErr.Message)
		return
	}
	if err != nil {
		if req.ClientMessageID != "" && s.repos.OperationReceipts == nil {
			s.sendWebSocketError(conn, "ai.message.send", "SERVICE_NOT_READY", "Operation receipt repository is not configured")
			return
		}
		s.sendWebSocketError(conn, "ai.message.send", "INTERNAL_ERROR", "Internal server error")
		return
	}
	if receiptClaim.Key != "" && !receiptClaim.Claimed {
		s.replayWebSocketAIStreamReceipt(conn, req, receiptClaim.Receipt)
		return
	}

	if s.aiStreams == nil {
		s.aiStreams = newAIStreamRegistry()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	streamID := s.aiStreams.Add(principal.UserID, conn.ID(), cancel)
	if streamID == "" {
		cancel()
		s.sendWebSocketError(conn, "ai.message.send", "SERVICE_NOT_READY", "AI stream registry is not configured")
		return
	}
	s.realtimeHub.Send(conn, "ai.stream.started", websocketAIStreamStartedPayload{
		StreamID:        streamID,
		ConversationID:  req.ConversationID,
		ClientMessageID: req.ClientMessageID,
	})

	go s.runWebSocketAIStream(streamCtx, conn, principal, streamID, req, receiptClaim)
}

func (s *Server) replayWebSocketAIStreamReceipt(conn *realtime.Connection, req websocketAIMessagePayload, receipt repository.OperationReceipt) {
	streamID := newAIStreamID()
	finalEvent, done, cancelled, code, message := aiStreamFinalFromReceipt(receipt, streamID)
	if code != "" {
		s.sendWebSocketError(conn, "ai.message.send", code, message)
		return
	}
	s.realtimeHub.Send(conn, "ai.stream.started", websocketAIStreamStartedPayload{
		StreamID:        streamID,
		ConversationID:  req.ConversationID,
		ClientMessageID: req.ClientMessageID,
	})
	if finalEvent == "ai.stream.cancelled" {
		s.realtimeHub.Send(conn, "ai.stream.cancelled", makeWebSocketAIStreamCancelledPayload(cancelled))
		return
	}
	s.realtimeHub.Send(conn, "ai.stream.done", makeWebSocketAIStreamDonePayload(done))
}

func makeWebSocketAIStreamDonePayload(done aiStreamDoneResponse) websocketAIStreamDonePayload {
	return websocketAIStreamDonePayload{
		StreamID:         done.StreamID,
		UserMessage:      done.UserMessage,
		AssistantMessage: done.AssistantMessage,
		UsedFiles:        done.UsedFiles,
		FinishReason:     done.FinishReason,
		Title:            done.Title,
	}
}

func makeWebSocketAIStreamCancelledPayload(cancelled aiStreamCancelledResponse) websocketAIStreamCancelledPayload {
	return websocketAIStreamCancelledPayload{
		StreamID:         cancelled.StreamID,
		UserMessage:      cancelled.UserMessage,
		AssistantMessage: cancelled.AssistantMessage,
		UsedFiles:        cancelled.UsedFiles,
		FinishReason:     cancelled.FinishReason,
		Title:            cancelled.Title,
	}
}

func (s *Server) runWebSocketAIStream(ctx context.Context, conn *realtime.Connection, principal AuthPrincipal, streamID string, req websocketAIMessagePayload, receiptClaim aiStreamReceiptClaim) {
	defer func() {
		if s.aiStreams != nil {
			s.aiStreams.Remove(streamID)
		}
	}()
	thinkingActive := false
	sendThinking := func(active bool) {
		if thinkingActive == active {
			return
		}
		thinkingActive = active
		s.sendWebSocketAIThinking(conn, streamID, active)
	}
	result, err := s.aiService.StreamMessage(ctx, aisvc.StreamMessageInput{
		SendMessageInput: aisvc.SendMessageInput{
			UserID:          principal.UserID,
			ConversationID:  req.ConversationID,
			Message:         req.Message,
			ClientMessageID: req.ClientMessageID,
			AIFileIDs:       req.AIFileIDs,
			Model:           req.Model,
			ThinkingType:    req.ThinkingType,
			ReasoningEffort: req.ReasoningEffort,
		},
		OnDelta: func(delta aisvc.StreamMessageDelta) error {
			select {
			case <-conn.Done():
				return context.Canceled
			default:
			}
			if delta.Thinking {
				sendThinking(true)
			}
			if delta.Content == "" {
				return nil
			}
			sendThinking(false)
			s.realtimeHub.Send(conn, "ai.stream.chunk", websocketAIStreamChunkPayload{
				StreamID: streamID,
				Delta:    delta.Content,
			})
			return nil
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			cancelled := makeAIStreamCancelledResponse(streamID, result)
			completeCtx, completeCancel := context.WithTimeout(context.Background(), aiStreamReceiptCleanupTimeout)
			if err := s.completeAIStreamCancelledReceipt(completeCtx, principal.UserID, receiptClaim, cancelled); err != nil {
				s.logger.WarnContext(completeCtx, "failed to complete cancelled AI websocket stream receipt", "error", err, "operation_type", receiptClaim.OperationType)
			}
			completeCancel()
			sendThinking(false)
			s.realtimeHub.Send(conn, "ai.stream.cancelled", makeWebSocketAIStreamCancelledPayload(cancelled))
			return
		}
		s.deleteAIStreamReceipt(principal.UserID, receiptClaim)
		code, message := aiErrorCodeAndMessage(err)
		sendThinking(false)
		s.sendWebSocketError(conn, "ai.message.send", code, message)
		return
	}
	done := aiStreamDoneResponse{
		StreamID:         streamID,
		UserMessage:      makeAIMessageResponse(result.UserMessage),
		AssistantMessage: makeAIMessageResponse(result.AssistantMessage),
		UsedFiles:        makeAIUploadedFileResponses(result.UsedFiles),
		FinishReason:     result.FinishReason,
		Title:            result.Title,
	}
	completeCtx, completeCancel := context.WithTimeout(context.Background(), aiStreamReceiptCleanupTimeout)
	if err := s.completeAIStreamReceipt(completeCtx, principal.UserID, receiptClaim, done); err != nil {
		s.logger.WarnContext(completeCtx, "failed to complete AI websocket stream receipt", "error", err, "operation_type", receiptClaim.OperationType)
	}
	completeCancel()
	sendThinking(false)
	s.realtimeHub.Send(conn, "ai.stream.done", makeWebSocketAIStreamDonePayload(done))
}

func (s *Server) sendWebSocketAIThinking(conn *realtime.Connection, streamID string, active bool) {
	s.realtimeHub.Send(conn, "ai.stream.thinking", websocketAIStreamThinkingPayload{
		StreamID: streamID,
		Active:   active,
	})
}

func (s *Server) handleWebSocketAIStreamCancel(conn *realtime.Connection, principal AuthPrincipal, payload json.RawMessage) {
	var req websocketAIStreamCancelPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendWebSocketError(conn, "ai.stream.cancel", "VALIDATION_ERROR", "Invalid AI stream cancel payload")
		return
	}
	if !s.aiStreams.Cancel(req.StreamID, principal.UserID) {
		s.sendWebSocketError(conn, "ai.stream.cancel", "NOT_FOUND", "AI stream not found")
		return
	}
}

func aiErrorCodeAndMessage(err error) (string, string) {
	switch {
	case errors.Is(err, aisvc.ErrValidation):
		return "VALIDATION_ERROR", "Invalid AI request"
	case errors.Is(err, aisvc.ErrForbidden):
		return "FORBIDDEN", "Forbidden"
	case errors.Is(err, aisvc.ErrNotReady):
		return "AI_FILE_NOT_READY", "AI file is not ready"
	case errors.Is(err, aisvc.ErrParseFailed):
		return "AI_FILE_PARSE_FAILED", "AI file parse failed"
	case errors.Is(err, aisvc.ErrUnavailable):
		return "SERVICE_NOT_READY", "AI service is not configured"
	case errors.Is(err, repository.ErrNotFound):
		return "NOT_FOUND", "Resource not found"
	case errors.Is(err, repository.ErrConflict):
		return "CONFLICT", "Resource conflict"
	default:
		return "INTERNAL_ERROR", "Internal server error"
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

func normalizePresenceStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "online", "mining", "airplane", "invisible", "offline":
		return strings.ToLower(strings.TrimSpace(value))
	case "busy", "dnd":
		return "mining"
	case "away":
		return "airplane"
	default:
		return ""
	}
}

func normalizeSelectablePresenceStatus(value string) string {
	status := normalizePresenceStatus(value)
	if status == "offline" {
		return ""
	}
	return status
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
			if s.realtimeFanout != nil {
				if err := s.realtimeFanout.Publish(ctx, userIDs, realtimeEvent); err != nil {
					s.logger.WarnContext(ctx, "failed to publish realtime redis fanout", "error", err, "event_type", eventType)
				}
			}
			return realtimeEvent
		}
		s.logger.WarnContext(ctx, "failed to persist realtime event; falling back to memory", "error", err, "event_type", eventType)
	}
	return s.realtimeHub.Publish(userIDs, eventType, payload)
}
