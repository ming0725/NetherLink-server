package chat

import (
	"errors"
	"testing"
)

func TestValidateImageMessageRequiresAttachmentDimensions(t *testing.T) {
	err := validateSendMessageInput(SendMessageInput{
		UserID:          "user-1",
		ConversationID:  "conversation-1",
		ClientMessageID: "client-1",
		Type:            "image",
		Attachments: []AttachmentInput{
			{FileID: "file-1"},
		},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestValidateImageMessageAcceptsAttachmentDimensions(t *testing.T) {
	width := 1280
	height := 720
	err := validateSendMessageInput(SendMessageInput{
		UserID:          "user-1",
		ConversationID:  "conversation-1",
		ClientMessageID: "client-1",
		Type:            "image",
		Attachments: []AttachmentInput{
			{FileID: "file-1", Width: &width, Height: &height},
		},
	})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}
