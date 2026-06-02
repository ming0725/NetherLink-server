package server

import (
	"errors"
	"net/http"
	"time"

	aisvc "nlserver/internal/ai"
	"nlserver/internal/repository"
)

type aiCreateConversationRequest struct {
	Title        *string `json:"title"`
	FirstMessage *string `json:"firstMessage"`
}

type aiSendMessageRequest struct {
	Message         string   `json:"message"`
	ClientMessageID string   `json:"clientMessageId"`
	AIFileIDs       []string `json:"aiFileIds"`
}

type aiBindFileRequest struct {
	FileID          string  `json:"fileId"`
	Purpose         string  `json:"purpose"`
	RetentionPolicy *string `json:"retentionPolicy"`
}

type aiFeedbackRequest struct {
	Feedback string `json:"feedback"`
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
	conversation, err := s.aiService.CreateConversation(r.Context(), aisvc.CreateConversationInput{
		UserID:       principal.UserID,
		Title:        req.Title,
		FirstMessage: req.FirstMessage,
	})
	if err != nil {
		writeAIServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, aiConversationDetailResponse{Conversation: makeAIConversationResponse(conversation)})
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
	result, err := s.aiService.SendMessage(r.Context(), aisvc.SendMessageInput{
		UserID:          principal.UserID,
		ConversationID:  conversationID,
		Message:         req.Message,
		ClientMessageID: req.ClientMessageID,
		AIFileIDs:       req.AIFileIDs,
	})
	if err != nil {
		writeAIServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, aiSendMessageResponse{
		UserMessage:        makeAIMessageResponse(result.UserMessage),
		AssistantMessageID: result.AssistantMessageID,
		StreamID:           result.StreamID,
		UsedFiles:          makeAIUploadedFileResponses(result.UsedFiles),
	})
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
	file, err := s.aiService.BindFile(r.Context(), aisvc.BindFileInput{
		UserID:          principal.UserID,
		ConversationID:  conversationID,
		FileID:          req.FileID,
		Purpose:         req.Purpose,
		RetentionPolicy: req.RetentionPolicy,
	})
	if err != nil {
		writeAIServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, aiFileDetailResponse{
		AIFile:      makeAIUploadedFileResponse(file),
		ParseStatus: file.ParseStatus,
		TokenCount:  file.TokenCount,
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
	if err := s.repos.AI.DeleteUploadedFile(r.Context(), principal.UserID, aiFileID); err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
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
	message, err := s.repos.AI.SetMessageFeedback(r.Context(), principal.UserID, messageID, req.Feedback)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, aiMessageDetailResponse{Message: makeAIMessageResponse(message)})
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
