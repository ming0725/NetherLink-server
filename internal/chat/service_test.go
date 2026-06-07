package chat

import (
	"errors"
	"testing"

	"nlserver/internal/repository"
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

func TestIsSendableAttachmentStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{name: "uploaded", status: "uploaded", want: true},
		{name: "processing", status: "processing", want: true},
		{name: "ready", status: "ready", want: true},
		{name: "normalizes", status: " READY ", want: true},
		{name: "pending", status: "pending", want: false},
		{name: "failed", status: "failed", want: false},
		{name: "deleted", status: "deleted", want: false},
		{name: "empty", status: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSendableAttachmentStatus(tt.status); got != tt.want {
				t.Fatalf("isSendableAttachmentStatus(%q) = %t, want %t", tt.status, got, tt.want)
			}
		})
	}
}

func TestDirectConversationPeerUserID(t *testing.T) {
	peerID := directConversationPeerUserID(repository.ConversationEntry{
		Conversation: repository.Conversation{Type: "direct"},
		PeerUser:     &repository.ConversationUserSummary{ID: "peer-user"},
	}, "current-user")
	if peerID != "peer-user" {
		t.Fatalf("peerID = %q, want peer-user", peerID)
	}
}

func TestDirectConversationPeerUserIDRejectsSelfPeer(t *testing.T) {
	peerID := directConversationPeerUserID(repository.ConversationEntry{
		Conversation: repository.Conversation{Type: "direct"},
		PeerUser:     &repository.ConversationUserSummary{ID: "current-user"},
	}, "current-user")
	if peerID != "" {
		t.Fatalf("peerID = %q, want empty", peerID)
	}
}
