package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"nlserver/internal/repository"
)

var (
	ErrValidation  = errors.New("chat: validation failed")
	ErrForbidden   = errors.New("chat: forbidden")
	ErrUnavailable = errors.New("chat: unavailable")
)

const (
	maxClientMessageIDLength = 128
	maxMessageTextLength     = 4000
	maxAttachmentsPerMessage = 20
)

type Service struct {
	repos repository.Repositories
	tx    repository.TxBeginner
}

type SendMessageInput struct {
	UserID              string
	ConversationID      string
	ClientMessageID     string
	ClientSentAt        *time.Time
	Type                string
	Content             MessageContentInput
	Attachments         []AttachmentInput
	ReferencedMessageID *string
}

type MessageContentInput struct {
	Text string          `json:"text,omitempty"`
	JSON json.RawMessage `json:"json,omitempty"`
}

type AttachmentInput struct {
	FileID      string  `json:"fileId"`
	DisplayName *string `json:"displayName"`
	DurationMS  *int    `json:"durationMs"`
	Width       *int    `json:"width"`
	Height      *int    `json:"height"`
}

type ListMessagesInput struct {
	UserID         string
	ConversationID string
	BeforeSeq      *int64
	AfterSeq       *int64
	Limit          int
}

type ListConversationsInput struct {
	UserID  string
	Keyword string
	Limit   int
	Offset  int
}

type OpenDirectConversationInput struct {
	UserID     string
	PeerUserID string
}

type OpenGroupConversationInput struct {
	UserID  string
	GroupID string
}

type GetConversationInput struct {
	UserID         string
	ConversationID string
}

type UpdateConversationSettingsInput struct {
	UserID         string
	ConversationID string
	IsPinned       *bool
	IsDND          *bool
	Hidden         *bool
}

type MarkConversationReadInput struct {
	UserID            string
	ConversationID    string
	LastReadMessageID *string
	LastReadAt        *time.Time
}

type MarkConversationUnreadInput struct {
	UserID            string
	ConversationID    string
	LastReadMessageID *string
	UnreadCount       *int
}

type RecallMessageInput struct {
	UserID         string
	ConversationID string
	MessageID      string
}

type HideConversationHistoryInput struct {
	UserID         string
	ConversationID string
}

func NewService(repos repository.Repositories, tx repository.TxBeginner) *Service {
	return &Service{repos: repos, tx: tx}
}

func (s *Service) ListConversations(ctx context.Context, input ListConversationsInput) ([]repository.ConversationEntry, error) {
	if s.repos.Chat == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || input.Limit <= 0 || input.Offset < 0 {
		return nil, ErrValidation
	}
	conversations, err := s.repos.Chat.ListConversationsForUser(ctx, input.UserID, input.Keyword, input.Limit, input.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	return conversations, nil
}

func (s *Service) OpenDirectConversation(ctx context.Context, input OpenDirectConversationInput) (repository.ConversationEntry, error) {
	if s.repos.Chat == nil || s.repos.Users == nil || s.tx == nil {
		return repository.ConversationEntry{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.PeerUserID) == "" || input.UserID == input.PeerUserID {
		return repository.ConversationEntry{}, ErrValidation
	}

	var conversationID string
	err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		peer, err := repos.Users.GetByID(ctx, input.PeerUserID)
		if err != nil {
			return err
		}
		if peer.Status != "active" {
			return ErrForbidden
		}

		foundID, found, err := repos.Chat.FindDirectConversation(ctx, input.UserID, input.PeerUserID)
		if err != nil {
			return err
		}
		if found {
			conversationID = foundID
			return nil
		}

		conversation, err := repos.Chat.CreateConversation(ctx, repository.CreateConversationParams{
			Type:       "direct",
			PeerUserID: &input.PeerUserID,
		})
		if err != nil {
			return err
		}
		if err := repos.Chat.AddConversationParticipants(ctx, conversation.ID, []string{input.UserID, input.PeerUserID}); err != nil {
			return err
		}
		conversationID = conversation.ID
		return nil
	})
	if err != nil {
		return repository.ConversationEntry{}, mapError(err)
	}

	return s.GetConversation(ctx, GetConversationInput{UserID: input.UserID, ConversationID: conversationID})
}

func (s *Service) OpenGroupConversation(ctx context.Context, input OpenGroupConversationInput) (repository.ConversationEntry, error) {
	if s.repos.Chat == nil || s.repos.Groups == nil || s.tx == nil {
		return repository.ConversationEntry{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" {
		return repository.ConversationEntry{}, ErrValidation
	}

	var conversationID string
	err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		if _, err := repos.Groups.GetForUser(ctx, input.UserID, input.GroupID); err != nil {
			return err
		}

		foundID, found, err := repos.Chat.FindGroupConversation(ctx, input.GroupID)
		if err != nil {
			return err
		}
		if found {
			conversationID = foundID
			if err := repos.Chat.AddConversationParticipants(ctx, conversationID, []string{input.UserID}); err != nil {
				return err
			}
			return nil
		}

		groupID := input.GroupID
		conversation, err := repos.Chat.CreateConversation(ctx, repository.CreateConversationParams{
			Type:    "group",
			GroupID: &groupID,
		})
		if err != nil {
			return err
		}
		memberIDs, err := repos.Chat.ListGroupMemberUserIDs(ctx, input.GroupID)
		if err != nil {
			return err
		}
		if len(memberIDs) == 0 {
			return ErrForbidden
		}
		if err := repos.Chat.AddConversationParticipants(ctx, conversation.ID, memberIDs); err != nil {
			return err
		}
		conversationID = conversation.ID
		return nil
	})
	if err != nil {
		return repository.ConversationEntry{}, mapError(err)
	}

	return s.GetConversation(ctx, GetConversationInput{UserID: input.UserID, ConversationID: conversationID})
}

func (s *Service) GetConversation(ctx context.Context, input GetConversationInput) (repository.ConversationEntry, error) {
	if s.repos.Chat == nil {
		return repository.ConversationEntry{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return repository.ConversationEntry{}, ErrValidation
	}
	entry, err := s.repos.Chat.GetConversationForUser(ctx, input.UserID, input.ConversationID)
	if err != nil {
		return repository.ConversationEntry{}, mapError(err)
	}
	return entry, nil
}

func (s *Service) UpdateConversationSettings(ctx context.Context, input UpdateConversationSettingsInput) (repository.ConversationParticipant, error) {
	if s.repos.Chat == nil {
		return repository.ConversationParticipant{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return repository.ConversationParticipant{}, ErrValidation
	}
	if input.IsPinned == nil && input.IsDND == nil && input.Hidden == nil {
		return repository.ConversationParticipant{}, ErrValidation
	}
	participant, err := s.repos.Chat.UpdateConversationSettings(ctx, repository.UpdateConversationSettingsParams{
		UserID:         input.UserID,
		ConversationID: input.ConversationID,
		IsPinned:       input.IsPinned,
		IsDND:          input.IsDND,
		Hidden:         input.Hidden,
	})
	if err != nil {
		return repository.ConversationParticipant{}, mapError(err)
	}
	return participant, nil
}

func (s *Service) MarkConversationRead(ctx context.Context, input MarkConversationReadInput) (repository.ConversationParticipant, error) {
	if s.repos.Chat == nil {
		return repository.ConversationParticipant{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return repository.ConversationParticipant{}, ErrValidation
	}
	participant, err := s.repos.Chat.MarkConversationRead(ctx, input.UserID, input.ConversationID, normalizedOptional(input.LastReadMessageID), input.LastReadAt)
	if err != nil {
		return repository.ConversationParticipant{}, mapError(err)
	}
	return participant, nil
}

func (s *Service) MarkConversationUnread(ctx context.Context, input MarkConversationUnreadInput) (repository.ConversationParticipant, error) {
	if s.repos.Chat == nil {
		return repository.ConversationParticipant{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return repository.ConversationParticipant{}, ErrValidation
	}
	if input.UnreadCount != nil && *input.UnreadCount < 0 {
		return repository.ConversationParticipant{}, ErrValidation
	}
	participant, err := s.repos.Chat.MarkConversationUnread(ctx, input.UserID, input.ConversationID, normalizedOptional(input.LastReadMessageID), input.UnreadCount)
	if err != nil {
		return repository.ConversationParticipant{}, mapError(err)
	}
	return participant, nil
}

func (s *Service) RecallMessage(ctx context.Context, input RecallMessageInput) (repository.Message, error) {
	if s.repos.Chat == nil {
		return repository.Message{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" ||
		strings.TrimSpace(input.ConversationID) == "" ||
		strings.TrimSpace(input.MessageID) == "" {
		return repository.Message{}, ErrValidation
	}
	message, err := s.repos.Chat.RecallMessage(ctx, input.UserID, input.ConversationID, input.MessageID)
	if err != nil {
		return repository.Message{}, mapError(err)
	}
	return message, nil
}

func (s *Service) HideConversationHistory(ctx context.Context, input HideConversationHistoryInput) (repository.ConversationParticipant, error) {
	if s.repos.Chat == nil {
		return repository.ConversationParticipant{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return repository.ConversationParticipant{}, ErrValidation
	}
	participant, err := s.repos.Chat.HideConversationHistory(ctx, input.UserID, input.ConversationID)
	if err != nil {
		return repository.ConversationParticipant{}, mapError(err)
	}
	return participant, nil
}

func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (repository.Message, error) {
	if s.repos.Chat == nil || s.repos.Files == nil || s.tx == nil {
		return repository.Message{}, ErrUnavailable
	}
	if err := validateSendMessageInput(input); err != nil {
		return repository.Message{}, err
	}

	messageType := normalizeMessageType(input.Type)
	attachments := make([]repository.CreateMessageAttachmentParams, 0, len(input.Attachments))
	contentText := normalizeContentText(input.Content.Text)
	contentJSON := input.Content.JSON

	var created repository.Message
	err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		if err := repos.Chat.LockConversationForParticipant(ctx, input.ConversationID, input.UserID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrForbidden
			}
			return err
		}

		for _, attachment := range input.Attachments {
			file, err := repos.Files.Get(ctx, attachment.FileID)
			if err != nil {
				return err
			}
			if file.OwnerUserID == nil || *file.OwnerUserID != input.UserID || file.StorageStatus != "uploaded" {
				return ErrForbidden
			}
			attachments = append(attachments, repository.CreateMessageAttachmentParams{
				FileID:      file.ID,
				DisplayName: normalizedOptional(attachment.DisplayName),
				DurationMS:  attachment.DurationMS,
				Width:       attachment.Width,
				Height:      attachment.Height,
			})
		}

		seq, err := repos.Chat.NextMessageSeq(ctx, input.ConversationID)
		if err != nil {
			return err
		}

		message, err := repos.Chat.CreateMessage(ctx, repository.CreateMessageParams{
			ConversationID:      input.ConversationID,
			SenderID:            input.UserID,
			MessageSeq:          seq,
			Type:                messageType,
			ContentText:         contentText,
			ContentJSON:         contentJSON,
			ReferencedMessageID: normalizedOptional(input.ReferencedMessageID),
			ClientMessageID:     input.ClientMessageID,
			ClientSentAt:        input.ClientSentAt,
			Attachments:         attachments,
		})
		if err != nil {
			return err
		}

		if err := repos.Chat.TouchConversationAfterMessage(ctx, input.ConversationID, message.ID, input.UserID); err != nil {
			return err
		}

		created = message
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return s.repos.Chat.GetMessageByClientMessageID(ctx, input.UserID, input.ConversationID, input.UserID, input.ClientMessageID)
		}
		return repository.Message{}, mapError(err)
	}

	return created, nil
}

func (s *Service) ListMessages(ctx context.Context, input ListMessagesInput) ([]repository.Message, error) {
	if s.repos.Chat == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" || input.Limit <= 0 {
		return nil, ErrValidation
	}
	if input.BeforeSeq != nil && input.AfterSeq != nil {
		return nil, ErrValidation
	}

	messages, err := s.repos.Chat.ListMessagesForParticipant(ctx, input.UserID, input.ConversationID, input.BeforeSeq, input.AfterSeq, input.Limit)
	if err != nil {
		return nil, mapError(err)
	}

	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}

func validateSendMessageInput(input SendMessageInput) error {
	if strings.TrimSpace(input.UserID) == "" ||
		strings.TrimSpace(input.ConversationID) == "" ||
		strings.TrimSpace(input.ClientMessageID) == "" ||
		len([]rune(strings.TrimSpace(input.ClientMessageID))) > maxClientMessageIDLength {
		return ErrValidation
	}

	messageType := normalizeMessageType(input.Type)
	switch messageType {
	case "text", "image", "file", "audio", "video":
	default:
		return ErrValidation
	}

	text := strings.TrimSpace(input.Content.Text)
	if len([]rune(text)) > maxMessageTextLength || len(input.Attachments) > maxAttachmentsPerMessage {
		return ErrValidation
	}
	if messageType == "text" && text == "" && len(input.Content.JSON) == 0 {
		return ErrValidation
	}
	if messageType != "text" && len(input.Attachments) == 0 {
		return ErrValidation
	}

	for _, attachment := range input.Attachments {
		if strings.TrimSpace(attachment.FileID) == "" {
			return ErrValidation
		}
		if attachment.DurationMS != nil && *attachment.DurationMS <= 0 {
			return ErrValidation
		}
		if attachment.Width != nil && *attachment.Width <= 0 {
			return ErrValidation
		}
		if attachment.Height != nil && *attachment.Height <= 0 {
			return ErrValidation
		}
		if messageType == "image" && (attachment.Width == nil || attachment.Height == nil) {
			return ErrValidation
		}
	}

	return nil
}

func normalizeMessageType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeContentText(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func mapError(err error) error {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, ErrForbidden), errors.Is(err, ErrUnavailable):
		return err
	case errors.Is(err, repository.ErrNotFound):
		return ErrForbidden
	default:
		return err
	}
}
