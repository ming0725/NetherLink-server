package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatsvc "nlserver/internal/chat"
	friendsvc "nlserver/internal/friend"
	"nlserver/internal/repository"
)

func TestJSONFallbackPreservesHandlerNotFoundErrors(t *testing.T) {
	handler := withJSONFallbacks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/friend-requests/request-id/reject", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), `"message":"Resource not found"`) {
		t.Fatalf("body = %s, want Resource not found", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Route not found") {
		t.Fatalf("body = %s, should not rewrite handler 404 as route 404", rec.Body.String())
	}
}

func TestJSONFallbackConvertsRouterNotFoundErrors(t *testing.T) {
	handler := withJSONFallbacks(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), `"message":"Route not found"`) {
		t.Fatalf("body = %s, want Route not found", rec.Body.String())
	}
}

func TestWriteFriendServiceErrorReturnsFriendRequestStateDetails(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/friend-requests/request-id/reject", nil)
	rec := httptest.NewRecorder()

	writeFriendServiceError(rec, req, &friendsvc.RequestActionError{
		Err:       friendsvc.ErrRequestAlreadyHandled,
		Action:    "reject",
		ActorRole: "recipient",
		Request: repository.FriendRequest{
			ID:     "64e1d7be-4f22-458f-bf54-13d3f12c3f43",
			Status: "accepted",
			FromUser: repository.User{
				ID:       "from-user",
				PublicID: "from",
			},
			ToUser: repository.User{
				ID:       "to-user",
				PublicID: "to",
			},
		},
	})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	var body struct {
		Code    string `json:"code"`
		Details struct {
			RequestedAction string `json:"requestedAction"`
			ActorRole       string `json:"actorRole"`
			CurrentStatus   string `json:"currentStatus"`
			Request         struct {
				RequestID string `json:"requestId"`
				Status    string `json:"status"`
			} `json:"request"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	if body.Code != "FRIEND_REQUEST_ALREADY_HANDLED" {
		t.Fatalf("code = %q, want FRIEND_REQUEST_ALREADY_HANDLED", body.Code)
	}
	if body.Details.RequestedAction != "reject" || body.Details.ActorRole != "recipient" || body.Details.CurrentStatus != "accepted" {
		t.Fatalf("details = %+v, want action/role/status", body.Details)
	}
	if body.Details.Request.RequestID != "64e1d7be-4f22-458f-bf54-13d3f12c3f43" || body.Details.Request.Status != "accepted" {
		t.Fatalf("request details = %+v, want accepted request", body.Details.Request)
	}
}

func TestChatErrorCodeAndMessageMapsNotFriend(t *testing.T) {
	code, message := chatErrorCodeAndMessage(chatsvc.ErrNotFriend)
	if code != "NOT_FRIEND" {
		t.Fatalf("code = %q, want NOT_FRIEND", code)
	}
	if message == "" {
		t.Fatal("message is empty")
	}
}
