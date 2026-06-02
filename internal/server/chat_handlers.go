package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	chatsvc "nlserver/internal/chat"
	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

type sendMessageRequest struct {
	ClientMessageID     string                      `json:"clientMessageId"`
	ClientSentAt        *time.Time                  `json:"clientSentAt"`
	Type                string                      `json:"type"`
	Content             chatsvc.MessageContentInput `json:"content"`
	Attachments         []messageAttachmentRequest  `json:"attachments"`
	ReferencedMessageID *string                     `json:"referencedMessageId"`
}

type messageAttachmentRequest struct {
	FileID      string  `json:"fileId"`
	DisplayName *string `json:"displayName"`
	DurationMS  *int    `json:"durationMs"`
	Width       *int    `json:"width"`
	Height      *int    `json:"height"`
}

type openDirectConversationRequest struct {
	PeerUserUUID string `json:"peerUserUuid"`
}

type openGroupConversationRequest struct {
	GroupID string `json:"groupId"`
}

type updateConversationSettingsRequest struct {
	IsPinned *bool `json:"isPinned"`
	IsDND    *bool `json:"isDnd"`
	Hidden   *bool `json:"hidden"`
}

type markConversationReadRequest struct {
	LastReadMessageID *string    `json:"lastReadMessageId"`
	LastReadAt        *time.Time `json:"lastReadAt"`
}

type markConversationUnreadRequest struct {
	LastReadMessageID *string `json:"lastReadMessageId"`
	UnreadCount       *int    `json:"unreadCount"`
}

type conversationsResponse struct {
	Conversations []conversationResponse `json:"conversations"`
}

type conversationDetailResponse struct {
	Conversation conversationResponse `json:"conversation"`
}

type conversationSettingsResponse struct {
	SyncState conversationSyncStateResponse `json:"syncState"`
}

type conversationResponse struct {
	ConversationID string                        `json:"conversationId"`
	Type           string                        `json:"type"`
	Summary        conversationSummaryResponse   `json:"summary"`
	SyncState      conversationSyncStateResponse `json:"syncState"`
	LastMessage    *messageResponse              `json:"lastMessage,omitempty"`
	LastMessageAt  *time.Time                    `json:"lastMessageAt,omitempty"`
	Version        int                           `json:"version"`
	CreatedAt      time.Time                     `json:"createdAt"`
	UpdatedAt      time.Time                     `json:"updatedAt"`
}

type conversationSummaryResponse struct {
	Title             string                            `json:"title"`
	AvatarURL         *string                           `json:"avatarUrl"`
	AvatarVersion     int                               `json:"avatarVersion"`
	AvatarEtag        *string                           `json:"avatarEtag"`
	AvatarContentHash *string                           `json:"avatarContentHash"`
	PeerUser          *conversationUserSummaryResponse  `json:"peerUser,omitempty"`
	Group             *conversationGroupSummaryResponse `json:"group,omitempty"`
}

type conversationUserSummaryResponse struct {
	UserUUID     string     `json:"userUuid"`
	UserID       string     `json:"userId"`
	NickName     string     `json:"nickName"`
	Gender       string     `json:"gender"`
	GenderCustom *string    `json:"genderCustom,omitempty"`
	Status       string     `json:"status"`
	Signature    *string    `json:"signature,omitempty"`
	Region       *string    `json:"region,omitempty"`
	LastSeenAt   *time.Time `json:"lastSeenAt,omitempty"`
	Version      int        `json:"version"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type conversationGroupSummaryResponse struct {
	GroupID           string    `json:"groupId"`
	Name              string    `json:"name"`
	OwnerUUID         string    `json:"ownerUuid"`
	AvatarURL         *string   `json:"avatarUrl"`
	AvatarVersion     int       `json:"avatarVersion"`
	AvatarEtag        *string   `json:"avatarEtag"`
	AvatarContentHash *string   `json:"avatarContentHash"`
	Introduction      *string   `json:"introduction,omitempty"`
	Announcement      *string   `json:"announcement,omitempty"`
	MemberCount       int       `json:"memberCount"`
	Version           int       `json:"version"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type conversationSyncStateResponse struct {
	IsPinned          bool       `json:"isPinned"`
	IsDND             bool       `json:"isDnd"`
	LastReadMessageID *string    `json:"lastReadMessageId"`
	LastReadAt        *time.Time `json:"lastReadAt"`
	UnreadCount       int        `json:"unreadCount"`
	HiddenAt          *time.Time `json:"hiddenAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type messageDetailResponse struct {
	Message messageResponse `json:"message"`
}

type messagesResponse struct {
	Messages      []messageResponse  `json:"messages"`
	HasMoreBefore bool               `json:"hasMoreBefore"`
	HasMoreAfter  bool               `json:"hasMoreAfter"`
	SyncCursor    *messageSyncCursor `json:"syncCursor,omitempty"`
}

type messageSyncCursor struct {
	FirstMessageSeq *int64 `json:"firstMessageSeq,omitempty"`
	LastMessageSeq  *int64 `json:"lastMessageSeq,omitempty"`
}

type messageResponse struct {
	MessageID           string                      `json:"messageId"`
	ConversationID      string                      `json:"conversationId"`
	SenderUUID          string                      `json:"senderUuid"`
	MessageSeq          int64                       `json:"messageSeq"`
	Type                string                      `json:"type"`
	Content             messageContentResponse      `json:"content"`
	Attachments         []messageAttachmentResponse `json:"attachments"`
	ReferencedMessageID *string                     `json:"referencedMessageId,omitempty"`
	ClientMessageID     string                      `json:"clientMessageId"`
	ClientSentAt        *time.Time                  `json:"clientSentAt,omitempty"`
	ServerReceivedAt    time.Time                   `json:"serverReceivedAt"`
	CreatedAt           time.Time                   `json:"createdAt"`
	EditedAt            *time.Time                  `json:"editedAt,omitempty"`
	RecalledAt          *time.Time                  `json:"recalledAt,omitempty"`
}

type messageContentResponse struct {
	Text string `json:"text,omitempty"`
	JSON any    `json:"json,omitempty"`
}

type messageAttachmentResponse struct {
	AttachmentID string  `json:"attachmentId"`
	FileID       string  `json:"fileId"`
	DisplayName  *string `json:"displayName,omitempty"`
	MimeType     string  `json:"mimeType"`
	SizeBytes    int64   `json:"sizeBytes"`
	URL          *string `json:"url,omitempty"`
	ETag         *string `json:"etag,omitempty"`
	ContentHash  *string `json:"contentHash,omitempty"`
	Version      int     `json:"version"`
	DurationMS   *int    `json:"durationMs,omitempty"`
	Width        *int    `json:"width,omitempty"`
	Height       *int    `json:"height,omitempty"`
}

type realtimeMessagePayload struct {
	Message messageResponse `json:"message"`
}

type realtimeConversationSyncPayload struct {
	ConversationID string                        `json:"conversationId"`
	UserUUID       string                        `json:"userUuid"`
	Reason         string                        `json:"reason"`
	SyncState      conversationSyncStateResponse `json:"syncState"`
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	limit, offset, reqErr := parsePagination(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	entries, err := s.chatService.ListConversations(r.Context(), chatsvc.ListConversationsInput{
		UserID:  principal.UserID,
		Keyword: r.URL.Query().Get("keyword"),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	responses, err := s.makeConversationResponses(r, entries)
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationsResponse{Conversations: responses})
}

func (s *Server) handleOpenDirectConversation(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	var req openDirectConversationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if err := validateUUID(req.PeerUserUUID, "peerUserUuid"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	entry, err := s.chatService.OpenDirectConversation(r.Context(), chatsvc.OpenDirectConversationInput{
		UserID:     principal.UserID,
		PeerUserID: req.PeerUserUUID,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	response, err := s.makeConversationResponse(r, entry)
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationDetailResponse{Conversation: response})
}

func (s *Server) handleOpenGroupConversation(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	var req openGroupConversationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if err := validateUUID(req.GroupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	entry, err := s.chatService.OpenGroupConversation(r.Context(), chatsvc.OpenGroupConversationInput{
		UserID:  principal.UserID,
		GroupID: req.GroupID,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	response, err := s.makeConversationResponse(r, entry)
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationDetailResponse{Conversation: response})
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}
	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	entry, err := s.chatService.GetConversation(r.Context(), chatsvc.GetConversationInput{
		UserID:         principal.UserID,
		ConversationID: conversationID,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	response, err := s.makeConversationResponse(r, entry)
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationDetailResponse{Conversation: response})
}

func (s *Server) handleUpdateConversationSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}
	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	var req updateConversationSettingsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	participant, err := s.chatService.UpdateConversationSettings(r.Context(), chatsvc.UpdateConversationSettingsInput{
		UserID:         principal.UserID,
		ConversationID: conversationID,
		IsPinned:       req.IsPinned,
		IsDND:          req.IsDND,
		Hidden:         req.Hidden,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	response := conversationSettingsResponse{SyncState: makeConversationSyncStateResponse(participant)}
	s.publishConversationSyncState(r, participant, "settings.updated")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleMarkConversationRead(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}
	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	var req markConversationReadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.LastReadMessageID != nil {
		if err := validateUUID(*req.LastReadMessageID, "lastReadMessageId"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}
	participant, err := s.chatService.MarkConversationRead(r.Context(), chatsvc.MarkConversationReadInput{
		UserID:            principal.UserID,
		ConversationID:    conversationID,
		LastReadMessageID: req.LastReadMessageID,
		LastReadAt:        req.LastReadAt,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	response := conversationSettingsResponse{SyncState: makeConversationSyncStateResponse(participant)}
	s.publishConversationSyncState(r, participant, "read")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleMarkConversationUnread(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}
	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	var req markConversationUnreadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.LastReadMessageID != nil {
		if err := validateUUID(*req.LastReadMessageID, "lastReadMessageId"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}
	if req.UnreadCount != nil && *req.UnreadCount < 0 {
		writeRequestError(w, r, &requestError{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR", Message: "unreadCount must be non-negative"})
		return
	}
	participant, err := s.chatService.MarkConversationUnread(r.Context(), chatsvc.MarkConversationUnreadInput{
		UserID:            principal.UserID,
		ConversationID:    conversationID,
		LastReadMessageID: req.LastReadMessageID,
		UnreadCount:       req.UnreadCount,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	response := conversationSettingsResponse{SyncState: makeConversationSyncStateResponse(participant)}
	s.publishConversationSyncState(r, participant, "unread")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleRecallMessage(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	messageID := r.PathValue("messageId")
	if err := validateUUID(messageID, "messageId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	message, err := s.chatService.RecallMessage(r.Context(), chatsvc.RecallMessageInput{
		UserID:         principal.UserID,
		ConversationID: conversationID,
		MessageID:      messageID,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	response, err := s.makeMessageResponse(r, message)
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	s.publishConversationMessage(r, conversationID, "chat.message.recalled", response)
	writeJSON(w, http.StatusOK, messageDetailResponse{Message: response})
}

func (s *Server) handleHideConversationHistory(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	participant, err := s.chatService.HideConversationHistory(r.Context(), chatsvc.HideConversationHistoryInput{
		UserID:         principal.UserID,
		ConversationID: conversationID,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	s.publishConversationSyncState(r, participant, "history.hidden")
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	var req sendMessageRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if err := validateSendMessageRequest(req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	attachments := make([]chatsvc.AttachmentInput, 0, len(req.Attachments))
	for _, attachment := range req.Attachments {
		attachments = append(attachments, chatsvc.AttachmentInput(attachment))
	}

	message, err := s.chatService.SendMessage(r.Context(), chatsvc.SendMessageInput{
		UserID:              principal.UserID,
		ConversationID:      conversationID,
		ClientMessageID:     req.ClientMessageID,
		ClientSentAt:        req.ClientSentAt,
		Type:                req.Type,
		Content:             req.Content,
		Attachments:         attachments,
		ReferencedMessageID: req.ReferencedMessageID,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	response, err := s.makeMessageResponse(r, message)
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	s.publishConversationMessage(r, conversationID, "chat.message.created", response)
	s.publishAllConversationSyncStates(r, conversationID, "message.created")
	writeJSON(w, http.StatusCreated, messageDetailResponse{Message: response})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.chatService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
		return
	}

	conversationID := r.PathValue("conversationId")
	if err := validateUUID(conversationID, "conversationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	limit, _, reqErr := parsePagination(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	beforeSeq, reqErr := parseOptionalInt64Query(r, "beforeMessageSeq")
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	afterSeq, reqErr := parseOptionalInt64Query(r, "afterMessageSeq")
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	messages, err := s.chatService.ListMessages(r.Context(), chatsvc.ListMessagesInput{
		UserID:         principal.UserID,
		ConversationID: conversationID,
		BeforeSeq:      beforeSeq,
		AfterSeq:       afterSeq,
		Limit:          limit,
	})
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	responses := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		response, err := s.makeMessageResponse(r, message)
		if err != nil {
			writeChatError(w, r, err)
			return
		}
		responses = append(responses, response)
	}

	writeJSON(w, http.StatusOK, messagesResponse{
		Messages:      responses,
		HasMoreBefore: beforeSeq != nil && len(responses) == limit,
		HasMoreAfter:  afterSeq != nil && len(responses) == limit,
		SyncCursor:    makeMessageSyncCursor(messages),
	})
}

func validateSendMessageRequest(req sendMessageRequest) *requestError {
	if strings.TrimSpace(req.ClientMessageID) == "" {
		return &requestError{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR", Message: "clientMessageId is required"}
	}
	if req.ReferencedMessageID != nil {
		if err := validateUUID(*req.ReferencedMessageID, "referencedMessageId"); err != nil {
			return err
		}
	}
	for _, attachment := range req.Attachments {
		if err := validateUUID(attachment.FileID, "attachments.fileId"); err != nil {
			return err
		}
	}
	return nil
}

func parseOptionalInt64Query(r *http.Request, field string) (*int64, *requestError) {
	value := strings.TrimSpace(r.URL.Query().Get(field))
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return nil, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: field + " must be a positive integer",
		}
	}
	return &parsed, nil
}

func (s *Server) makeConversationResponses(r *http.Request, entries []repository.ConversationEntry) ([]conversationResponse, error) {
	responses := make([]conversationResponse, 0, len(entries))
	for _, entry := range entries {
		response, err := s.makeConversationResponse(r, entry)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (s *Server) makeConversationResponse(r *http.Request, entry repository.ConversationEntry) (conversationResponse, error) {
	var lastMessage *messageResponse
	if entry.LastMessage != nil {
		response, err := s.makeMessageResponse(r, *entry.LastMessage)
		if err != nil {
			return conversationResponse{}, err
		}
		lastMessage = &response
	}

	return conversationResponse{
		ConversationID: entry.Conversation.ID,
		Type:           entry.Conversation.Type,
		Summary:        makeConversationSummaryResponse(entry),
		SyncState:      makeConversationSyncStateResponse(entry.Participant),
		LastMessage:    lastMessage,
		LastMessageAt:  entry.Conversation.LastMessageAt,
		Version:        entry.Conversation.Version,
		CreatedAt:      entry.Conversation.CreatedAt,
		UpdatedAt:      entry.Conversation.UpdatedAt,
	}, nil
}

func makeConversationSummaryResponse(entry repository.ConversationEntry) conversationSummaryResponse {
	response := conversationSummaryResponse{}
	if entry.PeerUser != nil {
		peer := makeConversationUserSummaryResponse(*entry.PeerUser)
		response.Title = peer.NickName
		response.AvatarVersion = peer.Version
		response.PeerUser = &peer
	}
	if entry.Group != nil {
		group := makeConversationGroupSummaryResponse(*entry.Group)
		response.Title = group.Name
		response.AvatarURL = group.AvatarURL
		response.AvatarVersion = group.AvatarVersion
		response.AvatarEtag = group.AvatarEtag
		response.AvatarContentHash = group.AvatarContentHash
		response.Group = &group
	}
	if response.Title == "" {
		response.Title = "Conversation"
	}
	return response
}

func makeConversationUserSummaryResponse(user repository.ConversationUserSummary) conversationUserSummaryResponse {
	return conversationUserSummaryResponse{
		UserUUID:     user.ID,
		UserID:       user.PublicID,
		NickName:     user.Nick,
		Gender:       user.Gender,
		GenderCustom: user.GenderCustom,
		Status:       user.Status,
		Signature:    user.Signature,
		Region:       user.Region,
		LastSeenAt:   user.LastSeenAt,
		Version:      user.Version,
		UpdatedAt:    user.UpdatedAt,
	}
}

func makeConversationGroupSummaryResponse(group repository.ConversationGroupSummary) conversationGroupSummaryResponse {
	return conversationGroupSummaryResponse{
		GroupID:           group.ID,
		Name:              group.Name,
		OwnerUUID:         group.OwnerID,
		AvatarURL:         nil,
		AvatarVersion:     group.Version,
		AvatarEtag:        nil,
		AvatarContentHash: nil,
		Introduction:      group.Introduction,
		Announcement:      group.Announcement,
		MemberCount:       group.MemberCount,
		Version:           group.Version,
		CreatedAt:         group.CreatedAt,
		UpdatedAt:         group.UpdatedAt,
	}
}

func makeConversationSyncStateResponse(participant repository.ConversationParticipant) conversationSyncStateResponse {
	return conversationSyncStateResponse{
		IsPinned:          participant.IsPinned,
		IsDND:             participant.IsDND,
		LastReadMessageID: participant.LastReadMessageID,
		LastReadAt:        participant.LastReadAt,
		UnreadCount:       participant.UnreadCount,
		HiddenAt:          participant.HiddenAt,
		UpdatedAt:         participant.UpdatedAt,
	}
}

func (s *Server) makeMessageResponse(r *http.Request, message repository.Message) (messageResponse, error) {
	attachments := make([]messageAttachmentResponse, 0, len(message.Attachments))
	if message.RecalledAt == nil {
		for _, attachment := range message.Attachments {
			url, err := s.presignMessageAttachmentURL(r, attachment.File)
			if err != nil {
				return messageResponse{}, err
			}
			attachments = append(attachments, messageAttachmentResponse{
				AttachmentID: attachment.ID,
				FileID:       attachment.File.ID,
				DisplayName:  attachment.DisplayName,
				MimeType:     attachment.File.MimeType,
				SizeBytes:    attachment.File.SizeBytes,
				URL:          url,
				ETag:         attachment.File.ETag,
				ContentHash:  attachment.File.ContentHash,
				Version:      attachment.File.Version,
				DurationMS:   attachment.DurationMS,
				Width:        attachment.Width,
				Height:       attachment.Height,
			})
		}
	}

	var content messageContentResponse
	if message.RecalledAt == nil && message.ContentText != nil {
		content.Text = *message.ContentText
	}
	if message.RecalledAt == nil && len(message.ContentJSON) > 0 && string(message.ContentJSON) != "{}" {
		content.JSON = message.ContentJSON
	}

	return messageResponse{
		MessageID:           message.ID,
		ConversationID:      message.ConversationID,
		SenderUUID:          message.SenderID,
		MessageSeq:          message.MessageSeq,
		Type:                message.Type,
		Content:             content,
		Attachments:         attachments,
		ReferencedMessageID: message.ReferencedMessageID,
		ClientMessageID:     message.ClientMessageID,
		ClientSentAt:        message.ClientSentAt,
		ServerReceivedAt:    message.ServerReceivedAt,
		CreatedAt:           message.CreatedAt,
		EditedAt:            message.EditedAt,
		RecalledAt:          message.RecalledAt,
	}, nil
}

func (s *Server) presignMessageAttachmentURL(r *http.Request, file repository.File) (*string, error) {
	if s.storage == nil {
		return nil, nil
	}
	signed, err := s.storage.PresignGetObject(r.Context(), storage.PresignObjectInput{
		Bucket:    file.Bucket,
		ObjectKey: file.ObjectKey,
		ExpiresIn: s.cfg.Storage.PresignTTL,
	})
	if err != nil {
		return nil, err
	}
	return &signed.URL, nil
}

func makeMessageSyncCursor(messages []repository.Message) *messageSyncCursor {
	if len(messages) == 0 {
		return nil
	}
	first := messages[0].MessageSeq
	last := messages[len(messages)-1].MessageSeq
	return &messageSyncCursor{
		FirstMessageSeq: &first,
		LastMessageSeq:  &last,
	}
}

func (s *Server) publishConversationMessage(r *http.Request, conversationID string, eventType string, message messageResponse) {
	if s.realtimeHub == nil || s.repos.Chat == nil {
		return
	}
	userIDs, err := s.repos.Chat.ListConversationParticipantUserIDs(r.Context(), conversationID)
	if err != nil {
		s.logger.WarnContext(r.Context(), "failed to list realtime message recipients", "error", err, "conversation_id", conversationID)
		return
	}
	publishRealtimeEvent(s.realtimeHub, userIDs, eventType, realtimeMessagePayload{Message: message})
}

func (s *Server) publishConversationSyncState(r *http.Request, participant repository.ConversationParticipant, reason string) {
	publishRealtimeEvent(s.realtimeHub, []string{participant.UserID}, "chat.conversation.sync.updated", realtimeConversationSyncPayload{
		ConversationID: participant.ConversationID,
		UserUUID:       participant.UserID,
		Reason:         reason,
		SyncState:      makeConversationSyncStateResponse(participant),
	})
}

func (s *Server) publishAllConversationSyncStates(r *http.Request, conversationID string, reason string) {
	if s.realtimeHub == nil || s.repos.Chat == nil {
		return
	}
	participants, err := s.repos.Chat.ListConversationParticipants(r.Context(), conversationID)
	if err != nil {
		s.logger.WarnContext(r.Context(), "failed to list realtime sync recipients", "error", err, "conversation_id", conversationID)
		return
	}
	for _, participant := range participants {
		s.publishConversationSyncState(r, participant, reason)
	}
}

func writeChatError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, chatsvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid chat request")
	case errors.Is(err, chatsvc.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, chatsvc.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Chat service is not configured")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Chat resource conflict")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}
