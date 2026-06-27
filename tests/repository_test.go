package tests

import (
	"testing"

	"nlserver/internal/repository"
)

func TestRepositoryAggregateConstructor(t *testing.T) {
	repositories := repository.New(nil)

	if repositories.Users == nil {
		t.Fatal("Users repository is nil")
	}
	if repositories.Preferences == nil {
		t.Fatal("Preferences repository is nil")
	}
	if repositories.Sessions == nil {
		t.Fatal("Sessions repository is nil")
	}
	if repositories.Files == nil {
		t.Fatal("Files repository is nil")
	}
	if repositories.Chat == nil {
		t.Fatal("Chat repository is nil")
	}
	if repositories.Friends == nil {
		t.Fatal("Friends repository is nil")
	}
	if repositories.Groups == nil {
		t.Fatal("Groups repository is nil")
	}
	if repositories.AI == nil {
		t.Fatal("AI repository is nil")
	}
	if repositories.Agents == nil {
		t.Fatal("Agents repository is nil")
	}
	if repositories.MCP == nil {
		t.Fatal("MCP repository is nil")
	}
	if repositories.RealtimeEvents == nil {
		t.Fatal("RealtimeEvents repository is nil")
	}
	if repositories.OperationReceipts == nil {
		t.Fatal("OperationReceipts repository is nil")
	}
}
