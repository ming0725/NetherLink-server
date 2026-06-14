package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	aisvc "nlserver/internal/ai"
	"nlserver/internal/repository"
)

type aiCreateConversationRequest struct {
	Title             *string `json:"title"`
	FirstMessage      *string `json:"firstMessage"`
	Model             string  `json:"model"`
	ClientOperationID string  `json:"clientOperationId"`
}

type aiUpdateConversationRequest struct {
	Title             string `json:"title"`
	ClientOperationID string `json:"clientOperationId"`
}

type aiSendMessageRequest struct {
	Message         string   `json:"message"`
	ClientMessageID string   `json:"clientMessageId"`
	AIFileIDs       []string `json:"aiFileIds"`
	Model           string   `json:"model"`
	ThinkingType    string   `json:"thinkingType"`
	ReasoningEffort string   `json:"reasoningEffort"`
}

type aiBindFileRequest struct {
	FileID            string  `json:"fileId"`
	Purpose           string  `json:"purpose"`
	RetentionPolicy   *string `json:"retentionPolicy"`
	ClientOperationID string  `json:"clientOperationId"`
}

type aiFeedbackRequest struct {
	Feedback          string `json:"feedback"`
	ClientOperationID string `json:"clientOperationId"`
}

type aiTitleRequest struct {
	FirstUserMessage string `json:"firstUserMessage"`
}

type aiConversationsResponse struct {
	Entries []aiConversationResponse `json:"entries"`
}

type aiConversationDetailResponse struct {
	Conversation aiConversationResponse `json:"conversation"`
}

type aiMessagesResponse struct {
	Messages []aiMessageResponse `json:"messages"`
}

type aiSendMessageResponse struct {
	UserMessage        aiMessageResponse        `json:"userMessage"`
	AssistantMessageID string                   `json:"assistantMessageId"`
	StreamID           string                   `json:"streamId"`
	UsedFiles          []aiUploadedFileResponse `json:"usedFiles"`
	Title              *string                  `json:"title,omitempty"`
}

type aiFilesResponse struct {
	Files []aiUploadedFileResponse `json:"files"`
}

type aiFileDetailResponse struct {
	AIFile      aiUploadedFileResponse `json:"aiFile"`
	ParseStatus string                 `json:"parseStatus"`
	TokenCount  int                    `json:"tokenCount"`
	PreviewText *string                `json:"previewText,omitempty"`
}

type aiContextResponse struct {
	UsedTokens int `json:"usedTokens"`
	MaxTokens  int `json:"maxTokens"`
	Available  int `json:"available"`
}

type aiMessageDetailResponse struct {
	Message aiMessageResponse `json:"message"`
}

type aiTitleResponse struct {
	Title string `json:"title"`
}

type aiStreamStartedResponse struct {
	StreamID        string `json:"streamId"`
	ConversationID  string `json:"conversationId"`
	ClientMessageID string `json:"clientMessageId,omitempty"`
}

type aiStreamChunkResponse struct {
	StreamID string `json:"streamId"`
	Delta    string `json:"delta"`
}

type aiStreamThinkingResponse struct {
	StreamID string `json:"streamId"`
	Active   bool   `json:"active"`
}

type aiStreamDoneResponse struct {
	StreamID         string                   `json:"streamId"`
	UserMessage      aiMessageResponse        `json:"userMessage"`
	AssistantMessage aiMessageResponse        `json:"assistantMessage"`
	UsedFiles        []aiUploadedFileResponse `json:"usedFiles"`
	FinishReason     string                   `json:"finishReason,omitempty"`
	Title            *string                  `json:"title,omitempty"`
}

type aiStreamCancelledResponse struct {
	StreamID         string                   `json:"streamId"`
	UserMessage      *aiMessageResponse       `json:"userMessage,omitempty"`
	AssistantMessage *aiMessageResponse       `json:"assistantMessage,omitempty"`
	UsedFiles        []aiUploadedFileResponse `json:"usedFiles,omitempty"`
	FinishReason     string                   `json:"finishReason,omitempty"`
	Title            *string                  `json:"title,omitempty"`
}

type aiStreamErrorResponse struct {
	RequestType string `json:"requestType"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	RequestID   string `json:"requestId"`
}

type aiConversationResponse struct {
	ConversationID    string     `json:"conversationId"`
	Title             *string    `json:"title,omitempty"`
	LastMessageAt     *time.Time `json:"lastMessageAt,omitempty"`
	HasUnreadDot      bool       `json:"hasUnreadDot"`
	Model             *string    `json:"model,omitempty"`
	ContextUsedTokens int        `json:"contextUsedTokens"`
	ContextMaxTokens  int        `json:"contextMaxTokens"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type aiMessageResponse struct {
	MessageID      string    `json:"messageId"`
	ConversationID string    `json:"conversationId"`
	Role           string    `json:"role"`
	Text           string    `json:"text"`
	TokenCount     int       `json:"tokenCount"`
	Feedback       *string   `json:"feedback,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type aiUploadedFileResponse struct {
	AIFileID            string     `json:"aiFileId"`
	ConversationID      string     `json:"conversationId"`
	MessageID           *string    `json:"messageId,omitempty"`
	FileID              string     `json:"fileId"`
	Purpose             string     `json:"purpose"`
	ParseStatus         string     `json:"parseStatus"`
	ExtractedTextFileID *string    `json:"extractedTextFileId,omitempty"`
	TokenCount          int        `json:"tokenCount"`
	RetentionPolicy     *string    `json:"retentionPolicy,omitempty"`
	ExpiresAt           *time.Time `json:"expiresAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

func (s *Server) handleAIListConversations(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	limit, offset, reqErr := parsePagination(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	conversations, err := s.aiService.ListConversations(r.Context(), principal.UserID, limit, offset)
	if err != nil {
		writeAIServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, aiConversationsResponse{Entries: makeAIConversationResponses(conversations)})
}

func (s *Server) handleAICreateConversation(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	var req aiCreateConversationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "ai.conversation.create", idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeAIServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		conversation, err := s.aiService.CreateConversation(r.Context(), aisvc.CreateConversationInput{
			UserID:       principal.UserID,
			Title:        req.Title,
			FirstMessage: req.FirstMessage,
			Model:        req.Model,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body:   aiConversationDetailResponse{Conversation: makeAIConversationResponse(conversation)},
		}, nil
	})
}

func (s *Server) handleAIUpdateConversation(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	conversationID := r.PathValue("conversationId")
	if reqErr := validateUUID(conversationID, "conversationId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req aiUpdateConversationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "ai.conversation.update:"+conversationID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeAIServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		conversation, err := s.aiService.UpdateConversationTitle(r.Context(), aisvc.UpdateConversationTitleInput{
			UserID:         principal.UserID,
			ConversationID: conversationID,
			Title:          req.Title,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   aiConversationDetailResponse{Conversation: makeAIConversationResponse(conversation)},
		}, nil
	})
}

func (s *Server) handleAIListMessages(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	conversationID := r.PathValue("conversationId")
	if reqErr := validateUUID(conversationID, "conversationId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	messages, err := s.aiService.ListMessages(r.Context(), principal.UserID, conversationID)
	if err != nil {
		writeAIServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, aiMessagesResponse{Messages: makeAIMessageResponses(messages)})
}

func (s *Server) handleAIDeleteConversation(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	conversationID := r.PathValue("conversationId")
	if reqErr := validateUUID(conversationID, "conversationId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "ai.conversation.delete:"+conversationID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeAIServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		if err := s.aiService.DeleteConversation(r.Context(), principal.UserID, conversationID); err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   okResponse{OK: true},
		}, nil
	})
}

func (s *Server) handleAISendMessage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	conversationID := r.PathValue("conversationId")
	if reqErr := validateUUID(conversationID, "conversationId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req aiSendMessageRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	for _, aiFileID := range req.AIFileIDs {
		if reqErr := validateUUID(aiFileID, "aiFileId"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}
	if wantsAISSE(r) {
		s.handleAISendMessageStream(w, r, principal, conversationID, req)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "ai.message.send:"+conversationID, idempotencyKeyFromRequest(r, req.ClientMessageID), func(err error) {
		writeAIServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		result, err := s.aiService.SendMessage(r.Context(), aisvc.SendMessageInput{
			UserID:          principal.UserID,
			ConversationID:  conversationID,
			Message:         req.Message,
			ClientMessageID: req.ClientMessageID,
			AIFileIDs:       req.AIFileIDs,
			Model:           req.Model,
			ThinkingType:    req.ThinkingType,
			ReasoningEffort: req.ReasoningEffort,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body: aiSendMessageResponse{
				UserMessage:        makeAIMessageResponse(result.UserMessage),
				AssistantMessageID: result.AssistantMessageID,
				StreamID:           result.StreamID,
				UsedFiles:          makeAIUploadedFileResponses(result.UsedFiles),
				Title:              result.Title,
			},
		}, nil
	})
}

func (s *Server) handleAISendMessageStream(w http.ResponseWriter, r *http.Request, principal AuthPrincipal, conversationID string, req aiSendMessageRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "Streaming response is unavailable")
		return
	}
	idempotencyKey := idempotencyKeyFromRequest(r, req.ClientMessageID)
	receiptClaim, reqErr, err := s.claimAIStreamReceipt(r.Context(), principal.UserID, aiStreamOperationType(conversationID), idempotencyKey)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	if err != nil {
		if idempotencyKey != "" && s.repos.OperationReceipts == nil {
			writeAIStreamServiceNotReady(w, r)
			return
		}
		writeRepositoryError(w, r, err)
		return
	}
	if receiptClaim.Key != "" && !receiptClaim.Claimed {
		s.replayAISSEStreamReceipt(w, flusher, r, conversationID, req.ClientMessageID, receiptClaim.Receipt)
		return
	}

	if s.aiStreams == nil {
		s.aiStreams = newAIStreamRegistry()
	}
	streamCtx, cancel := context.WithCancel(r.Context())
	streamID := s.aiStreams.Add(principal.UserID, "http", cancel)
	if streamID == "" {
		cancel()
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "AI stream registry is not configured")
		return
	}
	defer s.aiStreams.Remove(streamID)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if err := writeSSE(w, flusher, "ai.stream.started", aiStreamStartedResponse{
		StreamID:        streamID,
		ConversationID:  conversationID,
		ClientMessageID: req.ClientMessageID,
	}); err != nil {
		return
	}

	thinkingActive := false
	writeThinking := func(active bool) error {
		if thinkingActive == active {
			return nil
		}
		thinkingActive = active
		return writeSSE(w, flusher, "ai.stream.thinking", aiStreamThinkingResponse{
			StreamID: streamID,
			Active:   active,
		})
	}

	result, err := s.aiService.StreamMessage(streamCtx, aisvc.StreamMessageInput{
		SendMessageInput: aisvc.SendMessageInput{
			UserID:          principal.UserID,
			ConversationID:  conversationID,
			Message:         req.Message,
			ClientMessageID: req.ClientMessageID,
			AIFileIDs:       req.AIFileIDs,
			Model:           req.Model,
			ThinkingType:    req.ThinkingType,
			ReasoningEffort: req.ReasoningEffort,
		},
		OnDelta: func(delta aisvc.StreamMessageDelta) error {
			select {
			case <-streamCtx.Done():
				return context.Canceled
			default:
			}
			if delta.Thinking {
				if err := writeThinking(true); err != nil {
					return err
				}
			}
			if delta.Content == "" {
				return nil
			}
			if err := writeThinking(false); err != nil {
				return err
			}
			return writeSSE(w, flusher, "ai.stream.chunk", aiStreamChunkResponse{
				StreamID: streamID,
				Delta:    delta.Content,
			})
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			cancelled := makeAIStreamCancelledResponse(streamID, result)
			completeCtx, completeCancel := context.WithTimeout(context.Background(), aiStreamReceiptCleanupTimeout)
			if err := s.completeAIStreamCancelledReceipt(completeCtx, principal.UserID, receiptClaim, cancelled); err != nil {
				s.logger.WarnContext(completeCtx, "failed to complete cancelled AI stream receipt", "error", err, "operation_type", receiptClaim.OperationType)
			}
			completeCancel()
			_ = writeThinking(false)
			_ = writeSSE(w, flusher, "ai.stream.cancelled", cancelled)
			return
		}
		s.deleteAIStreamReceipt(principal.UserID, receiptClaim)
		code, message := aiErrorCodeAndMessage(err)
		_ = writeThinking(false)
		_ = writeSSE(w, flusher, "client.error", aiStreamErrorResponse{
			RequestType: "ai.message.send",
			Code:        code,
			Message:     message,
			RequestID:   requestIDFromContext(r.Context()),
		})
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
		s.logger.WarnContext(completeCtx, "failed to complete AI stream receipt", "error", err, "operation_type", receiptClaim.OperationType)
	}
	completeCancel()
	_ = writeThinking(false)
	_ = writeSSE(w, flusher, "ai.stream.done", done)
}

func makeAIStreamCancelledResponse(streamID string, result aisvc.StreamMessageResult) aiStreamCancelledResponse {
	response := aiStreamCancelledResponse{
		StreamID:     streamID,
		UsedFiles:    makeAIUploadedFileResponses(result.UsedFiles),
		FinishReason: result.FinishReason,
		Title:        result.Title,
	}
	if response.FinishReason == "" {
		response.FinishReason = "cancelled"
	}
	if result.UserMessage.ID != "" {
		userMessage := makeAIMessageResponse(result.UserMessage)
		response.UserMessage = &userMessage
	}
	if result.AssistantMessage.ID != "" {
		assistantMessage := makeAIMessageResponse(result.AssistantMessage)
		response.AssistantMessage = &assistantMessage
	}
	return response
}

func (s *Server) replayAISSEStreamReceipt(w http.ResponseWriter, flusher http.Flusher, r *http.Request, conversationID string, clientMessageID string, receipt repository.OperationReceipt) {
	streamID := newAIStreamID()
	finalEvent, done, cancelled, code, message := aiStreamFinalFromReceipt(receipt, streamID)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if code != "" {
		_ = writeSSE(w, flusher, "client.error", aiStreamErrorResponse{
			RequestType: "ai.message.send",
			Code:        code,
			Message:     message,
			RequestID:   requestIDFromContext(r.Context()),
		})
		return
	}
	if err := writeSSE(w, flusher, "ai.stream.started", aiStreamStartedResponse{
		StreamID:        streamID,
		ConversationID:  conversationID,
		ClientMessageID: clientMessageID,
	}); err != nil {
		return
	}
	switch finalEvent {
	case "ai.stream.cancelled":
		_ = writeSSE(w, flusher, "ai.stream.cancelled", cancelled)
	default:
		_ = writeSSE(w, flusher, "ai.stream.done", done)
	}
}

func (s *Server) handleAIBindFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	conversationID := r.PathValue("conversationId")
	if reqErr := validateUUID(conversationID, "conversationId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req aiBindFileRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if reqErr := validateUUID(req.FileID, "fileId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "ai.file.bind:"+conversationID+":"+req.FileID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeAIServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		file, err := s.aiService.BindFile(r.Context(), aisvc.BindFileInput{
			UserID:          principal.UserID,
			ConversationID:  conversationID,
			FileID:          req.FileID,
			Purpose:         req.Purpose,
			RetentionPolicy: req.RetentionPolicy,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body: aiFileDetailResponse{
				AIFile:      makeAIUploadedFileResponse(file),
				ParseStatus: file.ParseStatus,
				TokenCount:  file.TokenCount,
			},
		}, nil
	})
}

func (s *Server) handleAIListFiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIRepository(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	conversationID := r.PathValue("conversationId")
	if reqErr := validateUUID(conversationID, "conversationId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	files, err := s.repos.AI.ListUploadedFiles(r.Context(), principal.UserID, conversationID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, aiFilesResponse{Files: makeAIUploadedFileResponses(files)})
}

func (s *Server) handleAIGetFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIRepository(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	aiFileID := r.PathValue("aiFileId")
	if reqErr := validateUUID(aiFileID, "aiFileId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	file, err := s.repos.AI.GetUploadedFile(r.Context(), principal.UserID, aiFileID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, aiFileDetailResponse{
		AIFile:      makeAIUploadedFileResponse(file),
		ParseStatus: file.ParseStatus,
		TokenCount:  file.TokenCount,
	})
}

func (s *Server) handleAIDeleteFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIRepository(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	aiFileID := r.PathValue("aiFileId")
	if reqErr := validateUUID(aiFileID, "aiFileId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "ai.file.delete:"+aiFileID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeRepositoryError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		if err := s.repos.AI.DeleteUploadedFile(r.Context(), principal.UserID, aiFileID); err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   okResponse{OK: true},
		}, nil
	})
}

func (s *Server) handleAIContext(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIRepository(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	conversationID := r.PathValue("conversationId")
	if reqErr := validateUUID(conversationID, "conversationId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	conversation, err := s.repos.AI.GetConversation(r.Context(), principal.UserID, conversationID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	available := 0
	if conversation.ContextMaxTokens > conversation.ContextUsedTokens {
		available = conversation.ContextMaxTokens - conversation.ContextUsedTokens
	}
	writeJSON(w, http.StatusOK, aiContextResponse{
		UsedTokens: conversation.ContextUsedTokens,
		MaxTokens:  conversation.ContextMaxTokens,
		Available:  available,
	})
}

func (s *Server) handleAIMessageFeedback(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIRepository(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	messageID := r.PathValue("messageId")
	if reqErr := validateUUID(messageID, "messageId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req aiFeedbackRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.Feedback != "liked" && req.Feedback != "disliked" && req.Feedback != "none" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "feedback must be liked, disliked, or none")
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "ai.message.feedback:"+messageID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeRepositoryError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		message, err := s.repos.AI.SetMessageFeedback(r.Context(), principal.UserID, messageID, req.Feedback)
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   aiMessageDetailResponse{Message: makeAIMessageResponse(message)},
		}, nil
	})
}

func (s *Server) handleAITitle(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIService(w, r) {
		return
	}
	var req aiTitleRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	title, err := s.aiService.GenerateTitle(r.Context(), req.FirstUserMessage)
	if err != nil {
		writeAIServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, aiTitleResponse{Title: title})
}

func (s *Server) requireAIService(w http.ResponseWriter, r *http.Request) bool {
	if s.aiService != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "AI service is not configured")
	return false
}

func (s *Server) requireAIRepository(w http.ResponseWriter, r *http.Request) bool {
	if s.repos.AI != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "AI repository is not configured")
	return false
}

func writeAIServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, aisvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid AI request")
	case errors.Is(err, aisvc.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, aisvc.ErrNotReady):
		writeError(w, r, http.StatusConflict, "AI_FILE_NOT_READY", "AI file is not ready")
	case errors.Is(err, aisvc.ErrParseFailed):
		writeError(w, r, http.StatusUnprocessableEntity, "AI_FILE_PARSE_FAILED", "AI file parse failed")
	case errors.Is(err, aisvc.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "AI service is not configured")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Resource conflict")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

func wantsAISSE(r *http.Request) bool {
	return isEventStreamRequest(r)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func makeAIConversationResponses(conversations []repository.AIConversation) []aiConversationResponse {
	responses := make([]aiConversationResponse, 0, len(conversations))
	for _, conversation := range conversations {
		responses = append(responses, makeAIConversationResponse(conversation))
	}
	return responses
}

func makeAIConversationResponse(conversation repository.AIConversation) aiConversationResponse {
	return aiConversationResponse{
		ConversationID:    conversation.ID,
		Title:             conversation.Title,
		LastMessageAt:     conversation.LastMessageAt,
		HasUnreadDot:      conversation.HasUnreadDot,
		Model:             conversation.Model,
		ContextUsedTokens: conversation.ContextUsedTokens,
		ContextMaxTokens:  conversation.ContextMaxTokens,
		CreatedAt:         conversation.CreatedAt,
		UpdatedAt:         conversation.UpdatedAt,
	}
}

func makeAIMessageResponses(messages []repository.AIMessage) []aiMessageResponse {
	responses := make([]aiMessageResponse, 0, len(messages))
	for _, message := range messages {
		responses = append(responses, makeAIMessageResponse(message))
	}
	return responses
}

func makeAIMessageResponse(message repository.AIMessage) aiMessageResponse {
	return aiMessageResponse{
		MessageID:      message.ID,
		ConversationID: message.ConversationID,
		Role:           message.Role,
		Text:           message.Text,
		TokenCount:     message.TokenCount,
		Feedback:       message.Feedback,
		CreatedAt:      message.CreatedAt,
		UpdatedAt:      message.UpdatedAt,
	}
}

func makeAIUploadedFileResponses(files []repository.AIUploadedFile) []aiUploadedFileResponse {
	responses := make([]aiUploadedFileResponse, 0, len(files))
	for _, file := range files {
		responses = append(responses, makeAIUploadedFileResponse(file))
	}
	return responses
}

func makeAIUploadedFileResponse(file repository.AIUploadedFile) aiUploadedFileResponse {
	return aiUploadedFileResponse{
		AIFileID:            file.ID,
		ConversationID:      file.ConversationID,
		MessageID:           file.MessageID,
		FileID:              file.FileID,
		Purpose:             file.Purpose,
		ParseStatus:         file.ParseStatus,
		ExtractedTextFileID: file.ExtractedTextFileID,
		TokenCount:          file.TokenCount,
		RetentionPolicy:     file.RetentionPolicy,
		ExpiresAt:           file.ExpiresAt,
		CreatedAt:           file.CreatedAt,
		UpdatedAt:           file.UpdatedAt,
	}
}
