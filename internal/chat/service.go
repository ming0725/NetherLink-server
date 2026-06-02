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

func NewService(repos repository.Repositories, tx repository.TxBeginner) *Service {
	return &Service{repos: repos, tx: tx}
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
