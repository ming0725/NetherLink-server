package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"nlserver/internal/config"
	"nlserver/internal/repository"
)

var (
	ErrValidation  = errors.New("ai: validation failed")
	ErrForbidden   = errors.New("ai: forbidden")
	ErrNotReady    = errors.New("ai: file not ready")
	ErrParseFailed = errors.New("ai: file parse failed")
	ErrUnavailable = errors.New("ai: unavailable")
)

const (
	maxAIMessageLength = 20000
	maxAITitleLength   = 120
	maxAIModelLength   = 128
	maxAIContextChunks = 8
	maxAIContextChars  = 12000
	temporaryFileTTL   = 24 * time.Hour
)

var aiModelPattern = regexp.MustCompile(`^[A-Za-z0-9._:/@+-]{1,128}$`)

type Service struct {
	cfg    config.AIConfig
	repos  repository.Repositories
	tx     repository.TxBeginner
	client ChatClient
}

type CreateConversationInput struct {
	UserID       string
	Title        *string
	FirstMessage *string
	Model        string
}

type UpdateConversationTitleInput struct {
	UserID         string
	ConversationID string
	Title          string
}

type SendMessageInput struct {
	UserID          string
	ConversationID  string
	Message         string
	ClientMessageID string
	AIFileIDs       []string
	Model           string
	ThinkingType    string
	ReasoningEffort string
}

type SendMessageResult struct {
	UserMessage        repository.AIMessage
	AssistantMessageID string
	StreamID           string
	UsedFiles          []repository.AIUploadedFile
	Title              *string
}

type StreamMessageInput struct {
	SendMessageInput
	OnDelta func(StreamMessageDelta) error
}

type StreamMessageDelta struct {
	Content  string
	Thinking bool
}

type StreamMessageResult struct {
	UserMessage      repository.AIMessage
	AssistantMessage repository.AIMessage
	UsedFiles        []repository.AIUploadedFile
	FinishReason     string
	Title            *string
}

type BindFileInput struct {
	UserID          string
	ConversationID  string
	FileID          string
	Purpose         string
	RetentionPolicy *string
}

func NewService(cfg config.AIConfig, repos repository.Repositories, tx repository.TxBeginner, client ChatClient) *Service {
	return &Service{cfg: cfg, repos: repos, tx: tx, client: client}
}

func (s *Service) ListConversations(ctx context.Context, userID string, limit int, offset int) ([]repository.AIConversation, error) {
	if s.repos.AI == nil {
		return nil, ErrUnavailable
	}
	return s.repos.AI.ListConversations(ctx, userID, limit, offset)
}

func (s *Service) CreateConversation(ctx context.Context, input CreateConversationInput) (repository.AIConversation, error) {
	if s.repos.AI == nil {
		return repository.AIConversation{}, ErrUnavailable
	}
	title := normalizeOptionalTitle(input.Title)
	if title != nil && titleTooLong(*title) {
		return repository.AIConversation{}, ErrValidation
	}
	firstMessage := strings.TrimSpace(valueOrEmpty(input.FirstMessage))
	if firstMessage != "" && len(firstMessage) > maxAIMessageLength {
		return repository.AIConversation{}, ErrValidation
	}
	if title == nil && firstMessage != "" {
		fallbackTitle := fallbackConversationTitle(firstMessage)
		title = &fallbackTitle
	}
	model, err := normalizeAIModel(input.Model, s.cfg.DefaultModel)
	if err != nil {
		return repository.AIConversation{}, err
	}

	var conversation repository.AIConversation
	if s.tx != nil {
		err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
			created, err := repos.AI.CreateConversation(ctx, repository.CreateAIConversationParams{
				UserID:           input.UserID,
				Title:            title,
				Model:            model,
				ContextMaxTokens: s.cfg.MaxContextTokens,
			})
			if err != nil {
				return err
			}
			if firstMessage != "" {
				if _, err := repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
					ConversationID: created.ID,
					Role:           "user",
					Text:           firstMessage,
					TokenCount:     roughTokenCount(firstMessage),
				}); err != nil {
					return err
				}
			}
			conversation = created
			return nil
		})
		return conversation, mapRepositoryError(err)
	}

	created, err := s.repos.AI.CreateConversation(ctx, repository.CreateAIConversationParams{
		UserID:           input.UserID,
		Title:            title,
		Model:            model,
		ContextMaxTokens: s.cfg.MaxContextTokens,
	})
	if err != nil {
		return repository.AIConversation{}, mapRepositoryError(err)
	}
	if firstMessage != "" {
		if _, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
			ConversationID: created.ID,
			Role:           "user",
			Text:           firstMessage,
			TokenCount:     roughTokenCount(firstMessage),
		}); err != nil {
			return repository.AIConversation{}, mapRepositoryError(err)
		}
	}
	return created, nil
}

func (s *Service) UpdateConversationTitle(ctx context.Context, input UpdateConversationTitleInput) (repository.AIConversation, error) {
	if s.repos.AI == nil {
		return repository.AIConversation{}, ErrUnavailable
	}
	title := strings.TrimSpace(input.Title)
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" || title == "" || titleTooLong(title) {
		return repository.AIConversation{}, ErrValidation
	}
	conversation, err := s.repos.AI.UpdateConversationTitle(ctx, input.UserID, input.ConversationID, title)
	if err != nil {
		return repository.AIConversation{}, mapRepositoryError(err)
	}
	return conversation, nil
}

func (s *Service) ListMessages(ctx context.Context, userID string, conversationID string) ([]repository.AIMessage, error) {
	if s.repos.AI == nil {
		return nil, ErrUnavailable
	}
	if _, err := s.repos.AI.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, mapRepositoryError(err)
	}
	return s.repos.AI.ListMessages(ctx, userID, conversationID)
}

func (s *Service) DeleteConversation(ctx context.Context, userID string, conversationID string) error {
	if s.repos.AI == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(conversationID) == "" {
		return ErrValidation
	}
	return mapRepositoryError(s.repos.AI.DeleteConversation(ctx, userID, conversationID))
}

func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (SendMessageResult, error) {
	prepared, err := s.prepareMessageCompletion(ctx, input)
	if err != nil {
		return SendMessageResult{}, err
	}
	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	completion, err := s.client.Complete(providerCtx, CompletionRequest{
		Model:           prepared.model,
		Messages:        prepared.messages,
		ThinkingType:    prepared.thinkingType,
		ReasoningEffort: prepared.reasoningEffort,
	})
	if err != nil {
		return SendMessageResult{}, err
	}
	assistantMessage, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
		ConversationID: prepared.conversation.ID,
		Role:           "assistant",
		Text:           strings.TrimSpace(completion.Content),
		TokenCount:     completion.TokenCount,
	})
	if err != nil {
		return SendMessageResult{}, mapRepositoryError(err)
	}
	title := s.finishFirstRoundTitle(ctx, prepared, assistantMessage)

	return SendMessageResult{
		UserMessage:        prepared.userMessage,
		AssistantMessageID: assistantMessage.ID,
		StreamID:           "",
		UsedFiles:          prepared.usedFiles,
		Title:              title,
	}, nil
}

func (s *Service) StreamMessage(ctx context.Context, input StreamMessageInput) (StreamMessageResult, error) {
	prepared, err := s.prepareMessageCompletion(ctx, input.SendMessageInput)
	if err != nil {
		return StreamMessageResult{}, err
	}
	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	completion, err := s.client.Stream(providerCtx, CompletionRequest{
		Model:           prepared.model,
		Messages:        prepared.messages,
		ThinkingType:    prepared.thinkingType,
		ReasoningEffort: prepared.reasoningEffort,
	}, func(delta CompletionStreamDelta) error {
		if input.OnDelta == nil || (!delta.Thinking && delta.Content == "") {
			return nil
		}
		if err := input.OnDelta(StreamMessageDelta{Content: delta.Content, Thinking: delta.Thinking}); err != nil {
			return streamDeliveryError{err: err}
		}
		return nil
	})
	if err != nil {
		if isStreamCancellation(ctx, err) {
			result, saveErr := s.saveCancelledStreamMessage(ctx, prepared, completion)
			if saveErr != nil {
				return StreamMessageResult{}, saveErr
			}
			return result, context.Canceled
		}
		return StreamMessageResult{}, err
	}
	content := strings.TrimSpace(completion.Content)
	tokenCount := completion.TokenCount
	if tokenCount == 0 {
		tokenCount = roughTokenCount(content)
	}
	assistantMessage, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
		ConversationID: prepared.conversation.ID,
		Role:           "assistant",
		Text:           content,
		TokenCount:     tokenCount,
	})
	if err != nil {
		return StreamMessageResult{}, mapRepositoryError(err)
	}
	title := s.finishFirstRoundTitle(ctx, prepared, assistantMessage)
	return StreamMessageResult{
		UserMessage:      prepared.userMessage,
		AssistantMessage: assistantMessage,
		UsedFiles:        prepared.usedFiles,
		FinishReason:     completion.FinishReason,
		Title:            title,
	}, nil
}

func (s *Service) saveCancelledStreamMessage(ctx context.Context, prepared preparedMessageCompletion, completion CompletionResponse) (StreamMessageResult, error) {
	result := StreamMessageResult{
		UserMessage:  prepared.userMessage,
		UsedFiles:    prepared.usedFiles,
		FinishReason: "cancelled",
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	content := strings.TrimSpace(completion.Content)
	if content == "" {
		result.Title = s.finishFirstRoundTitleFromText(persistCtx, prepared, "")
		return result, nil
	}
	tokenCount := completion.TokenCount
	if tokenCount == 0 {
		tokenCount = roughTokenCount(content)
	}
	assistantMessage, err := s.repos.AI.CreateMessage(persistCtx, repository.CreateAIMessageParams{
		ConversationID: prepared.conversation.ID,
		Role:           "assistant",
		Text:           content,
		TokenCount:     tokenCount,
	})
	if err != nil {
		return StreamMessageResult{}, mapRepositoryError(err)
	}
	result.AssistantMessage = assistantMessage
	result.Title = s.finishFirstRoundTitle(persistCtx, prepared, assistantMessage)
	return result, nil
}

func (s *Service) finishFirstRoundTitleFromText(ctx context.Context, prepared preparedMessageCompletion, assistantText string) *string {
	if !prepared.firstRound || !prepared.autoTitle || strings.TrimSpace(prepared.titleInput) == "" {
		return nil
	}
	title, err := s.GenerateConversationTitle(ctx, prepared.titleInput, assistantText)
	if err != nil || strings.TrimSpace(title) == "" {
		title = fallbackConversationTitle(prepared.titleInput)
	}
	title = truncateTitle(title)
	if strings.TrimSpace(title) == "" {
		return nil
	}
	if _, err := s.repos.AI.UpdateConversationTitle(ctx, prepared.conversation.UserID, prepared.conversation.ID, title); err != nil {
		return nil
	}
	return &title
}

func isStreamCancellation(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) || isStreamDeliveryError(err)
}

type streamDeliveryError struct {
	err error
}

func (e streamDeliveryError) Error() string {
	return e.err.Error()
}

func (e streamDeliveryError) Unwrap() error {
	return e.err
}

func isStreamDeliveryError(err error) bool {
	var deliveryErr streamDeliveryError
	return errors.As(err, &deliveryErr)
}

type preparedMessageCompletion struct {
	conversation    repository.AIConversation
	userMessage     repository.AIMessage
	usedFiles       []repository.AIUploadedFile
	model           string
	thinkingType    string
	reasoningEffort string
	messages        []ChatMessage
	firstRound      bool
	autoTitle       bool
	titleInput      string
}

func (s *Service) prepareMessageCompletion(ctx context.Context, input SendMessageInput) (preparedMessageCompletion, error) {
	if s.repos.AI == nil {
		return preparedMessageCompletion{}, ErrUnavailable
	}
	if s.client == nil {
		return preparedMessageCompletion{}, ErrUnavailable
	}
	text := strings.TrimSpace(input.Message)
	if input.UserID == "" || input.ConversationID == "" || text == "" || len(text) > maxAIMessageLength {
		return preparedMessageCompletion{}, ErrValidation
	}
	conversation, err := s.repos.AI.GetConversation(ctx, input.UserID, input.ConversationID)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}
	existingMessages, err := s.repos.AI.ListMessages(ctx, input.UserID, conversation.ID)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}
	usedFiles, err := s.repos.AI.ListUploadedFilesByIDs(ctx, input.UserID, input.ConversationID, input.AIFileIDs)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}
	if len(usedFiles) != len(uniqueStrings(input.AIFileIDs)) {
		return preparedMessageCompletion{}, repository.ErrNotFound
	}
	for _, file := range usedFiles {
		switch file.ParseStatus {
		case "ready":
		case "failed":
			return preparedMessageCompletion{}, ErrParseFailed
		default:
			return preparedMessageCompletion{}, ErrNotReady
		}
	}
	contextChunks, err := s.selectFileContext(ctx, input.UserID, conversation.ID, usedFiles, text)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}

	userMessage, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
		ConversationID: conversation.ID,
		Role:           "user",
		Text:           text,
		TokenCount:     roughTokenCount(text),
	})
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}

	history := make([]repository.AIMessage, 0, len(existingMessages)+1)
	history = append(history, existingMessages...)
	history = append(history, userMessage)
	firstRound := !hasRole(existingMessages, "assistant")
	titleInput := initialUserMessagesForTitle(history)
	autoTitle := isBlankStringPtr(conversation.Title) || (firstRound && titleInput != "" && strings.TrimSpace(*conversation.Title) == fallbackConversationTitle(titleInput))
	if isBlankStringPtr(conversation.Title) && titleInput != "" {
		fallbackTitle := fallbackConversationTitle(titleInput)
		updated, err := s.repos.AI.UpdateConversationTitle(ctx, input.UserID, conversation.ID, fallbackTitle)
		if err != nil {
			return preparedMessageCompletion{}, mapRepositoryError(err)
		}
		conversation = updated
	}
	model := s.cfg.DefaultModel
	if conversation.Model != nil && strings.TrimSpace(*conversation.Model) != "" {
		model = strings.TrimSpace(*conversation.Model)
	}
	model, err = normalizeAIModel(input.Model, model)
	if err != nil {
		return preparedMessageCompletion{}, err
	}
	thinkingType, err := normalizeThinkingType(input.ThinkingType, s.cfg.ThinkingType)
	if err != nil {
		return preparedMessageCompletion{}, err
	}
	reasoningEffort, err := normalizeReasoningEffort(input.ReasoningEffort, s.cfg.ReasoningEffort)
	if err != nil {
		return preparedMessageCompletion{}, err
	}
	return preparedMessageCompletion{
		conversation:    conversation,
		userMessage:     userMessage,
		usedFiles:       usedFiles,
		model:           model,
		thinkingType:    thinkingType,
		reasoningEffort: reasoningEffort,
		messages:        makeCompletionMessages(history, contextChunks),
		firstRound:      firstRound,
		autoTitle:       autoTitle,
		titleInput:      titleInput,
	}, nil
}

func (s *Service) BindFile(ctx context.Context, input BindFileInput) (repository.AIUploadedFile, error) {
	if s.repos.AI == nil || s.repos.Files == nil {
		return repository.AIUploadedFile{}, ErrUnavailable
	}
	if strings.TrimSpace(input.FileID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return repository.AIUploadedFile{}, ErrValidation
	}
	if _, err := s.repos.AI.GetConversation(ctx, input.UserID, input.ConversationID); err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	file, err := s.repos.Files.Get(ctx, input.FileID)
	if err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	if file.OwnerUserID == nil || *file.OwnerUserID != input.UserID {
		return repository.AIUploadedFile{}, ErrForbidden
	}
	if file.StorageStatus != "ready" {
		return repository.AIUploadedFile{}, ErrNotReady
	}
	purpose := strings.TrimSpace(input.Purpose)
	if purpose == "" {
		purpose = "reference"
	}
	if purpose != "reference" && purpose != "attachment" {
		return repository.AIUploadedFile{}, ErrValidation
	}
	retentionPolicy := normalizeRetentionPolicy(input.RetentionPolicy)
	if retentionPolicy != nil && *retentionPolicy == "" {
		return repository.AIUploadedFile{}, ErrValidation
	}
	expiresAt := aiFileExpiresAt(retentionPolicy)
	if s.tx != nil {
		var uploadedFile repository.AIUploadedFile
		err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
			created, err := repos.AI.CreateUploadedFile(ctx, repository.CreateAIUploadedFileParams{
				ConversationID:  input.ConversationID,
				FileID:          input.FileID,
				Purpose:         purpose,
				RetentionPolicy: retentionPolicy,
				ExpiresAt:       expiresAt,
			})
			if err != nil {
				return err
			}
			if _, err := repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
				FileID:      input.FileID,
				UsageType:   "ai_upload",
				BusinessID:  created.ID,
				OwnerUserID: input.UserID,
				Visibility:  "private",
			}); err != nil {
				return err
			}
			if _, err := repos.Files.CreateProcessingJob(ctx, repository.CreateFileProcessingJobParams{
				FileID:  input.FileID,
				JobType: "ai_parse",
				Status:  "pending",
			}); err != nil {
				return err
			}
			uploadedFile = created
			return nil
		})
		if err != nil {
			return repository.AIUploadedFile{}, mapRepositoryError(err)
		}
		return uploadedFile, nil
	}

	uploadedFile, err := s.repos.AI.CreateUploadedFile(ctx, repository.CreateAIUploadedFileParams{
		ConversationID:  input.ConversationID,
		FileID:          input.FileID,
		Purpose:         purpose,
		RetentionPolicy: retentionPolicy,
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	if _, err := s.repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
		FileID:      input.FileID,
		UsageType:   "ai_upload",
		BusinessID:  uploadedFile.ID,
		OwnerUserID: input.UserID,
		Visibility:  "private",
	}); err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	if _, err := s.repos.Files.CreateProcessingJob(ctx, repository.CreateFileProcessingJobParams{
		FileID:  input.FileID,
		JobType: "ai_parse",
		Status:  "pending",
	}); err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	return uploadedFile, nil
}

func (s *Service) selectFileContext(ctx context.Context, userID string, conversationID string, usedFiles []repository.AIUploadedFile, userMessage string) ([]repository.AIFileChunk, error) {
	if len(usedFiles) == 0 {
		return []repository.AIFileChunk{}, nil
	}
	ids := make([]string, 0, len(usedFiles))
	for _, file := range usedFiles {
		ids = append(ids, file.ID)
	}
	chunks, err := s.repos.AI.ListFileChunksByUploadedFileIDs(ctx, userID, conversationID, ids)
	if err != nil {
		return nil, err
	}
	return SelectRelevantChunks(userMessage, chunks), nil
}

func (s *Service) GenerateTitle(ctx context.Context, firstUserMessage string) (string, error) {
	return s.GenerateConversationTitle(ctx, firstUserMessage, "")
}

func (s *Service) GenerateConversationTitle(ctx context.Context, firstUserMessage string, assistantMessage string) (string, error) {
	if s.client == nil {
		return "", ErrUnavailable
	}
	text := strings.TrimSpace(firstUserMessage)
	if text == "" || len(text) > maxAIMessageLength {
		return "", ErrValidation
	}
	assistantText := strings.TrimSpace(assistantMessage)
	model := s.cfg.TitleModel
	if strings.TrimSpace(model) == "" {
		model = s.cfg.DefaultModel
	}
	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	completion, err := s.client.Complete(providerCtx, CompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: titleGenerationSystemPrompt()},
			{Role: "user", Content: formatTitleGenerationInput(text, assistantText)},
		},
		ThinkingType:    s.cfg.ThinkingType,
		ReasoningEffort: s.cfg.ReasoningEffort,
	})
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(completion.Content)
	return truncateTitle(title), nil
}

func mapRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrNotFound):
		return repository.ErrNotFound
	case errors.Is(err, repository.ErrConflict):
		return repository.ErrConflict
	default:
		return err
	}
}

func normalizeAIModel(value string, fallback string) (string, error) {
	model := strings.TrimSpace(value)
	if model == "" {
		model = strings.TrimSpace(fallback)
	}
	if model == "" || len([]rune(model)) > maxAIModelLength || !aiModelPattern.MatchString(model) {
		return "", ErrValidation
	}
	return model, nil
}

func normalizeThinkingType(value string, fallback string) (string, error) {
	thinkingType := strings.TrimSpace(value)
	if thinkingType == "" {
		thinkingType = strings.TrimSpace(fallback)
	}
	switch thinkingType {
	case "", "enabled", "disabled":
		return thinkingType, nil
	default:
		return "", ErrValidation
	}
}

func normalizeReasoningEffort(value string, fallback string) (string, error) {
	reasoningEffort := strings.TrimSpace(value)
	if reasoningEffort == "" {
		reasoningEffort = strings.TrimSpace(fallback)
	}
	switch reasoningEffort {
	case "", "low", "medium", "high", "max":
		return reasoningEffort, nil
	default:
		return "", ErrValidation
	}
}

func makeCompletionMessages(messages []repository.AIMessage, chunks []repository.AIFileChunk) []ChatMessage {
	result := []ChatMessage{{Role: "system", Content: "You are a helpful assistant."}}
	if len(chunks) > 0 {
		result = append(result, ChatMessage{
			Role:    "system",
			Content: formatAIFileContext(chunks),
		})
	}
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" && message.Role != "system" {
			continue
		}
		result = append(result, ChatMessage{Role: message.Role, Content: message.Text})
	}
	return result
}

func (s *Service) finishFirstRoundTitle(ctx context.Context, prepared preparedMessageCompletion, assistantMessage repository.AIMessage) *string {
	return s.finishFirstRoundTitleFromText(ctx, prepared, assistantMessage.Text)
}

func hasRole(messages []repository.AIMessage, role string) bool {
	for _, message := range messages {
		if message.Role == role {
			return true
		}
	}
	return false
}

func initialUserMessagesForTitle(messages []repository.AIMessage) string {
	parts := []string{}
	for _, message := range messages {
		if message.Role == "assistant" {
			break
		}
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(message.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func fallbackConversationTitle(text string) string {
	return truncateTitle(strings.TrimSpace(text))
}

func truncateTitle(title string) string {
	title = strings.TrimSpace(strings.Trim(title, "\"'“”‘’`"))
	runes := []rune(title)
	if len(runes) > maxAITitleLength {
		title = string(runes[:maxAITitleLength])
	}
	return strings.TrimSpace(title)
}

func titleTooLong(title string) bool {
	return len([]rune(strings.TrimSpace(title))) > maxAITitleLength
}

func isBlankStringPtr(value *string) bool {
	return value == nil || strings.TrimSpace(*value) == ""
}

func titleGenerationSystemPrompt() string {
	return strings.Join([]string{
		"你是对话标题生成器。请根据首轮用户消息和助手回复，为这段 AI 对话生成一个具体、易扫描的标题。",
		"要求：",
		"1. 标题要概括用户真正想完成的任务、问题对象或讨论主题。",
		"2. 优先使用用户使用的语言；中文标题建议 4 到 18 个汉字，英文标题不超过 8 个单词。",
		"3. 避免“新对话”“问题咨询”“技术讨论”这类空泛标题。",
		"4. 不要添加引号、句号、冒号前缀、编号、解释或多余换行。",
		"5. 只输出标题本身。",
	}, "\n")
}

func formatTitleGenerationInput(userText string, assistantText string) string {
	if strings.TrimSpace(assistantText) == "" {
		return fmt.Sprintf("用户首条消息：\n%s", strings.TrimSpace(userText))
	}
	return fmt.Sprintf("用户首条消息：\n%s\n\n助手首轮回复：\n%s", strings.TrimSpace(userText), strings.TrimSpace(assistantText))
}

// SelectRelevantChunks chooses bounded uploaded-file context for provider prompts.
func SelectRelevantChunks(query string, chunks []repository.AIFileChunk) []repository.AIFileChunk {
	if len(chunks) == 0 {
		return []repository.AIFileChunk{}
	}
	if len(chunks) <= maxAIContextChunks {
		return trimChunksToContext(chunks)
	}
	terms := querySearchTerms(query)
	if len(terms) == 0 {
		return trimChunksToContext(chunks)
	}
	phrase := normalizedSearchText(query)
	bigrams := queryBigrams(terms)

	type chunkStats struct {
		tokens []string
		tf     map[string]int
	}
	type scoredChunk struct {
		score float64
		index int
	}

	stats := make([]chunkStats, len(chunks))
	documentFrequency := map[string]int{}
	totalTokenCount := 0
	for index, chunk := range chunks {
		tokens := tokenizeSearchText(chunk.Content)
		tf := map[string]int{}
		for _, token := range tokens {
			tf[token]++
		}
		seenInChunk := map[string]bool{}
		for _, term := range terms {
			if tf[term] > 0 && !seenInChunk[term] {
				documentFrequency[term]++
				seenInChunk[term] = true
			}
		}
		stats[index] = chunkStats{tokens: tokens, tf: tf}
		totalTokenCount += len(tokens)
	}
	averageTokenCount := float64(totalTokenCount) / float64(len(chunks))
	if averageTokenCount < 1 {
		averageTokenCount = 1
	}

	scored := make([]scoredChunk, 0, len(chunks))
	for index, chunk := range chunks {
		score := bm25Score(terms, stats[index].tf, len(stats[index].tokens), averageTokenCount, documentFrequency, len(chunks))
		content := normalizedSearchText(chunk.Content)
		if phrase != "" && len(terms) > 1 && strings.Contains(content, phrase) {
			score += 3
		}
		for _, bigram := range bigrams {
			if strings.Contains(content, bigram) {
				score += 1.25
			}
		}
		scored = append(scored, scoredChunk{score: score, index: index})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})

	selectedIndexes := []int{}
	selectedSet := map[int]bool{}
	addIndex := func(index int) {
		if index < 0 || index >= len(chunks) || selectedSet[index] || len(selectedIndexes) >= maxAIContextChunks {
			return
		}
		selectedSet[index] = true
		selectedIndexes = append(selectedIndexes, index)
	}
	for _, item := range scored {
		if item.score <= 0 || len(selectedIndexes) >= maxAIContextChunks {
			break
		}
		addIndex(item.index)
		if item.index > 0 && chunks[item.index-1].AIFileID == chunks[item.index].AIFileID {
			addIndex(item.index - 1)
		}
		if item.index+1 < len(chunks) && chunks[item.index+1].AIFileID == chunks[item.index].AIFileID {
			addIndex(item.index + 1)
		}
	}
	if len(selectedIndexes) == 0 {
		for index := range chunks {
			addIndex(index)
		}
	}
	sort.Ints(selectedIndexes)
	selected := make([]repository.AIFileChunk, 0, len(selectedIndexes))
	for _, index := range selectedIndexes {
		selected = append(selected, chunks[index])
	}
	return trimChunksToContext(selected)
}

func trimChunksToContext(chunks []repository.AIFileChunk) []repository.AIFileChunk {
	selected := []repository.AIFileChunk{}
	used := 0
	for _, chunk := range chunks {
		contentLength := len([]rune(chunk.Content))
		if contentLength == 0 {
			continue
		}
		if used+contentLength > maxAIContextChars && len(selected) > 0 {
			break
		}
		selected = append(selected, chunk)
		used += contentLength
		if len(selected) >= maxAIContextChunks {
			break
		}
	}
	return selected
}

func querySearchTerms(query string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, token := range tokenizeSearchText(query) {
		if len([]rune(token)) < 2 || commonSearchStopWords[token] || seen[token] {
			continue
		}
		seen[token] = true
		result = append(result, token)
	}
	return result
}

func bm25Score(terms []string, termFrequency map[string]int, documentTokenCount int, averageTokenCount float64, documentFrequency map[string]int, documentCount int) float64 {
	if documentTokenCount == 0 || documentCount == 0 {
		return 0
	}
	const k1 = 1.2
	const b = 0.75
	score := 0.0
	for _, term := range terms {
		tf := termFrequency[term]
		if tf == 0 {
			continue
		}
		df := documentFrequency[term]
		idf := math.Log(1 + (float64(documentCount)-float64(df)+0.5)/(float64(df)+0.5))
		lengthNorm := k1 * (1 - b + b*float64(documentTokenCount)/averageTokenCount)
		score += idf * (float64(tf) * (k1 + 1)) / (float64(tf) + lengthNorm)
	}
	return score
}

func queryBigrams(terms []string) []string {
	if len(terms) < 2 {
		return nil
	}
	bigrams := make([]string, 0, len(terms)-1)
	for index := 0; index+1 < len(terms); index++ {
		bigrams = append(bigrams, terms[index]+" "+terms[index+1])
	}
	return bigrams
}

func normalizedSearchText(text string) string {
	return strings.Join(tokenizeSearchText(text), " ")
}

func tokenizeSearchText(text string) []string {
	tokens := []string{}
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		tokens = append(tokens, builder.String())
		builder.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			builder.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

var commonSearchStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "how": true, "in": true,
	"is": true, "it": true, "of": true, "on": true, "or": true, "that": true,
	"the": true, "this": true, "to": true, "was": true, "what": true, "when": true,
	"where": true, "which": true, "who": true, "why": true, "with": true,
}

func formatAIFileContext(chunks []repository.AIFileChunk) string {
	var builder strings.Builder
	builder.WriteString("Use these uploaded-file excerpts as reference context when relevant. If they do not answer the user, say so and answer from general knowledge.\n")
	for _, chunk := range chunks {
		builder.WriteString(fmt.Sprintf("\n[aiFileId=%s chunk=%d]\n%s\n", chunk.AIFileID, chunk.ChunkIndex, strings.TrimSpace(chunk.Content)))
	}
	return builder.String()
}

func normalizeOptionalTitle(title *string) *string {
	if title == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*title)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeRetentionPolicy(policy *string) *string {
	if policy == nil {
		defaultPolicy := "conversation"
		return &defaultPolicy
	}
	trimmed := strings.TrimSpace(*policy)
	switch trimmed {
	case "conversation", "temporary", "source_deleted_after_parse", "none":
		return &trimmed
	default:
		empty := ""
		return &empty
	}
}

func aiFileExpiresAt(policy *string) *time.Time {
	if policy == nil || strings.TrimSpace(*policy) != "temporary" {
		return nil
	}
	expiresAt := time.Now().UTC().Add(temporaryFileTTL)
	return &expiresAt
}

func roughTokenCount(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	count := len([]rune(trimmed)) / 4
	if count < 1 {
		count = 1
	}
	return count
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}
