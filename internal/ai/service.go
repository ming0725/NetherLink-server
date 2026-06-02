package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	maxAIContextChunks = 8
	maxAIContextChars  = 12000
)

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
}

type SendMessageInput struct {
	UserID          string
	ConversationID  string
	Message         string
	ClientMessageID string
	AIFileIDs       []string
}

type SendMessageResult struct {
	UserMessage        repository.AIMessage
	AssistantMessageID string
	StreamID           string
	UsedFiles          []repository.AIUploadedFile
}

type StreamMessageInput struct {
	SendMessageInput
	OnDelta func(StreamMessageDelta) error
}

type StreamMessageDelta struct {
	Content string
}

type StreamMessageResult struct {
	UserMessage      repository.AIMessage
	AssistantMessage repository.AIMessage
	UsedFiles        []repository.AIUploadedFile
	FinishReason     string
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
	if title != nil && len(*title) > maxAITitleLength {
		return repository.AIConversation{}, ErrValidation
	}
	firstMessage := strings.TrimSpace(valueOrEmpty(input.FirstMessage))
	if firstMessage != "" && len(firstMessage) > maxAIMessageLength {
		return repository.AIConversation{}, ErrValidation
	}

	var conversation repository.AIConversation
	if s.tx != nil {
		err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
			created, err := repos.AI.CreateConversation(ctx, repository.CreateAIConversationParams{
				UserID:           input.UserID,
				Title:            title,
				Model:            s.cfg.DefaultModel,
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
		Model:            s.cfg.DefaultModel,
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

func (s *Service) ListMessages(ctx context.Context, userID string, conversationID string) ([]repository.AIMessage, error) {
	if s.repos.AI == nil {
		return nil, ErrUnavailable
	}
	if _, err := s.repos.AI.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, mapRepositoryError(err)
	}
	return s.repos.AI.ListMessages(ctx, userID, conversationID)
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
		ThinkingType:    s.cfg.ThinkingType,
		ReasoningEffort: s.cfg.ReasoningEffort,
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

	return SendMessageResult{
		UserMessage:        prepared.userMessage,
		AssistantMessageID: assistantMessage.ID,
		StreamID:           "",
		UsedFiles:          prepared.usedFiles,
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
		ThinkingType:    s.cfg.ThinkingType,
		ReasoningEffort: s.cfg.ReasoningEffort,
	}, func(delta CompletionStreamDelta) error {
		if input.OnDelta == nil || delta.Content == "" {
			return nil
		}
		return input.OnDelta(StreamMessageDelta{Content: delta.Content})
	})
	if err != nil {
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
	return StreamMessageResult{
		UserMessage:      prepared.userMessage,
		AssistantMessage: assistantMessage,
		UsedFiles:        prepared.usedFiles,
		FinishReason:     completion.FinishReason,
	}, nil
}

type preparedMessageCompletion struct {
	conversation repository.AIConversation
	userMessage  repository.AIMessage
	usedFiles    []repository.AIUploadedFile
	model        string
	messages     []ChatMessage
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

	history, err := s.repos.AI.ListMessages(ctx, input.UserID, conversation.ID)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}
	model := s.cfg.DefaultModel
	if conversation.Model != nil && strings.TrimSpace(*conversation.Model) != "" {
		model = strings.TrimSpace(*conversation.Model)
	}
	return preparedMessageCompletion{
		conversation: conversation,
		userMessage:  userMessage,
		usedFiles:    usedFiles,
		model:        model,
		messages:     makeCompletionMessages(history, contextChunks),
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
	if s.tx != nil {
		var uploadedFile repository.AIUploadedFile
		err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
			created, err := repos.AI.CreateUploadedFile(ctx, repository.CreateAIUploadedFileParams{
				ConversationID:  input.ConversationID,
				FileID:          input.FileID,
				Purpose:         purpose,
				RetentionPolicy: retentionPolicy,
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
	return selectRelevantChunks(userMessage, chunks), nil
}

func (s *Service) GenerateTitle(ctx context.Context, firstUserMessage string) (string, error) {
	if s.client == nil {
		return "", ErrUnavailable
	}
	text := strings.TrimSpace(firstUserMessage)
	if text == "" || len(text) > maxAIMessageLength {
		return "", ErrValidation
	}
	model := s.cfg.TitleModel
	if strings.TrimSpace(model) == "" {
		model = s.cfg.DefaultModel
	}
	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	completion, err := s.client.Complete(providerCtx, CompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: "Create a concise conversation title. Return only the title."},
			{Role: "user", Content: text},
		},
		ThinkingType:    s.cfg.ThinkingType,
		ReasoningEffort: s.cfg.ReasoningEffort,
	})
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(completion.Content)
	if len(title) > maxAITitleLength {
		title = title[:maxAITitleLength]
	}
	return title, nil
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

func selectRelevantChunks(query string, chunks []repository.AIFileChunk) []repository.AIFileChunk {
	if len(chunks) <= maxAIContextChunks {
		return trimChunksToContext(chunks)
	}
	keywords := queryKeywords(query)
	type scoredChunk struct {
		chunk repository.AIFileChunk
		score int
		index int
	}
	scored := make([]scoredChunk, 0, len(chunks))
	for index, chunk := range chunks {
		content := strings.ToLower(chunk.Content)
		score := 0
		for _, keyword := range keywords {
			if strings.Contains(content, keyword) {
				score++
			}
		}
		scored = append(scored, scoredChunk{chunk: chunk, score: score, index: index})
	}
	for i := 1; i < len(scored); i++ {
		current := scored[i]
		j := i - 1
		for ; j >= 0 && (scored[j].score < current.score || (scored[j].score == current.score && scored[j].index > current.index)); j-- {
			scored[j+1] = scored[j]
		}
		scored[j+1] = current
	}
	selected := make([]repository.AIFileChunk, 0, maxAIContextChunks)
	for _, item := range scored {
		if len(selected) >= maxAIContextChunks {
			break
		}
		selected = append(selected, item.chunk)
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

func queryKeywords(query string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, part := range strings.Fields(strings.ToLower(query)) {
		keyword := strings.Trim(part, " \t\r\n.,;:!?()[]{}\"'")
		if len([]rune(keyword)) < 2 || seen[keyword] {
			continue
		}
		seen[keyword] = true
		result = append(result, keyword)
	}
	return result
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
