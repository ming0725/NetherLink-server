package server

import (
	"encoding/json"
	"testing"
	"time"

	"nlserver/internal/repository"
)

func TestUserResponseSeparatesAccountAndPresenceStatus(t *testing.T) {
	now := time.Date(2026, 6, 11, 13, 27, 35, 0, time.UTC)
	response := makeUserResponse(repository.User{
		ID:        "7c0a16c2-b972-424f-9ee2-65b731d913ef",
		PublicID:  "123456",
		Email:     "123456@qq.com",
		Nick:      "567",
		Gender:    "unspecified",
		Status:    "active",
		Version:   10,
		UpdatedAt: now,
	})
	response.Presence = &presenceResponse{
		Status:     "online",
		LastSeenAt: &now,
		UpdatedAt:  &now,
	}

	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal user response: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if got["accountStatus"] != "active" {
		t.Fatalf("accountStatus = %v, want active; body = %s", got["accountStatus"], body)
	}
	if got["status"] != "active" {
		t.Fatalf("status = %v, want active for backward compatibility; body = %s", got["status"], body)
	}
	presence, ok := got["presence"].(map[string]any)
	if !ok {
		t.Fatalf("presence = %T, want object; body = %s", got["presence"], body)
	}
	if presence["status"] != "online" {
		t.Fatalf("presence.status = %v, want online; body = %s", presence["status"], body)
	}
}
