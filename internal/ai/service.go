package ai

import (
	"context"
	"errors"
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
	if s.repos.AI == nil {
		return SendMessageResult{}, ErrUnavailable
	}
	if s.client == nil {
		return SendMessageResult{}, ErrUnavailable
	}
	text := strings.TrimSpace(input.Message)
	if input.UserID == "" || input.ConversationID == "" || text == "" || len(text) > maxAIMessageLength {
		return SendMessageResult{}, ErrValidation
	}
	conversation, err := s.repos.AI.GetConversation(ctx, input.UserID, input.ConversationID)
	if err != nil {
		return SendMessageResult{}, mapRepositoryError(err)
	}
	usedFiles, err := s.repos.AI.ListUploadedFilesByIDs(ctx, input.UserID, input.ConversationID, input.AIFileIDs)
	if err != nil {
		return SendMessageResult{}, mapRepositoryError(err)
	}
	if len(usedFiles) != len(uniqueStrings(input.AIFileIDs)) {
		return SendMessageResult{}, repository.ErrNotFound
	}
	for _, file := range usedFiles {
		switch file.ParseStatus {
		case "ready":
		case "failed":
			return SendMessageResult{}, ErrParseFailed
		default:
			return SendMessageResult{}, ErrNotReady
		}
	}

	userMessage, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
		ConversationID: conversation.ID,
		Role:           "user",
		Text:           text,
		TokenCount:     roughTokenCount(text),
	})
	if err != nil {
		return SendMessageResult{}, mapRepositoryError(err)
	}

	history, err := s.repos.AI.ListMessages(ctx, input.UserID, conversation.ID)
	if err != nil {
		return SendMessageResult{}, mapRepositoryError(err)
	}
	model := s.cfg.DefaultModel
	if conversation.Model != nil && strings.TrimSpace(*conversation.Model) != "" {
		model = strings.TrimSpace(*conversation.Model)
	}
	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	completion, err := s.client.Complete(providerCtx, CompletionRequest{
		Model:           model,
		Messages:        makeCompletionMessages(history),
		ThinkingType:    s.cfg.ThinkingType,
		ReasoningEffort: s.cfg.ReasoningEffort,
	})
	if err != nil {
		return SendMessageResult{}, err
	}
	assistantMessage, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
		ConversationID: conversation.ID,
		Role:           "assistant",
		Text:           strings.TrimSpace(completion.Content),
		TokenCount:     completion.TokenCount,
	})
	if err != nil {
		return SendMessageResult{}, mapRepositoryError(err)
	}

	return SendMessageResult{
		UserMessage:        userMessage,
		AssistantMessageID: assistantMessage.ID,
		StreamID:           "",
		UsedFiles:          usedFiles,
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

func makeCompletionMessages(messages []repository.AIMessage) []ChatMessage {
	result := []ChatMessage{{Role: "system", Content: "You are a helpful assistant."}}
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" && message.Role != "system" {
			continue
		}
		result = append(result, ChatMessage{Role: message.Role, Content: message.Text})
	}
	return result
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
