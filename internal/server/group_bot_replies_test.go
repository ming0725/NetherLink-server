package server

import (
	"reflect"
	"testing"

	"nlserver/internal/repository"
)

func TestMentionedAgentIDsFiltersAndDeduplicatesAIOnly(t *testing.T) {
	message := repository.Message{
		Mentions: []repository.MessageMention{
			{TargetType: "user", TargetIdentifier: "user-1"},
			{TargetType: "ai", TargetIdentifier: "agent-1"},
			{TargetType: "AI", TargetIdentifier: "agent-1"},
			{TargetType: "ai", TargetIdentifier: "agent-2"},
			{TargetType: "ai", TargetIdentifier: " "},
		},
	}
	got := mentionedAgentIDs(message)
	want := []string{"agent-1", "agent-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mentionedAgentIDs = %#v, want %#v", got, want)
	}
}

func TestMentionedAgentIDsPrefersTargetUserIDForDeduplication(t *testing.T) {
	botUserID := "00000000-0000-0000-0000-000000000001"
	message := repository.Message{
		Mentions: []repository.MessageMention{
			{TargetType: "ai", TargetIdentifier: "11111111-1111-1111-1111-111111111111", TargetUserID: &botUserID},
			{TargetType: "ai", TargetIdentifier: "bot-public-id", TargetUserID: &botUserID},
		},
	}

	got := mentionedAgentIDs(message)
	want := []string{botUserID}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mentionedAgentIDs = %#v, want %#v", got, want)
	}
}

func TestClaimGroupBotReplyDeduplicatesRecentTrigger(t *testing.T) {
	srv := &Server{}

	if !srv.claimGroupBotReply("message-1", "agent-1") {
		t.Fatal("first claim was rejected")
	}
	if srv.claimGroupBotReply("message-1", "agent-1") {
		t.Fatal("duplicate claim was accepted")
	}
	if !srv.claimGroupBotReply("message-1", "agent-2") {
		t.Fatal("different agent claim was rejected")
	}
	if !srv.claimGroupBotReply("message-2", "agent-1") {
		t.Fatal("different message claim was rejected")
	}
}
