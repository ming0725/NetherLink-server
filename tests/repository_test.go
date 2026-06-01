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
}
