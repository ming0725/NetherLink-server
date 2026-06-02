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

func (s *Server) makeMessageResponse(r *http.Request, message repository.Message) (messageResponse, error) {
	attachments := make([]messageAttachmentResponse, 0, len(message.Attachments))
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

	var content messageContentResponse
	if message.ContentText != nil {
		content.Text = *message.ContentText
	}
	if len(message.ContentJSON) > 0 && string(message.ContentJSON) != "{}" {
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
