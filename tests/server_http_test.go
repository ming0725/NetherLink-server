package tests

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nlserver/internal/config"
	"nlserver/internal/server"
)

func newHTTPTestServer() *server.Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return server.New(config.Config{Env: "test"}, nil, nil, logger)
}

func TestHealthz(t *testing.T) {
	srv := newHTTPTestServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "req_test")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Request-Id") != "req_test" {
		t.Fatalf("request id header = %q, want req_test", rec.Header().Get("X-Request-Id"))
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %s, want status ok", rec.Body.String())
	}
}

func TestAPIV1Root(t *testing.T) {
	srv := newHTTPTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"apiVersion":"v1"`) {
		t.Fatalf("body = %s, want apiVersion v1", rec.Body.String())
	}
}

func TestAuthRouteWithoutDatabaseReturnsServiceNotReady(t *testing.T) {
	srv := newHTTPTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("X-Request-Id", "req_auth")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "req_auth")
}

func TestProtectedRouteWithoutTokenReturnsTokenMissing(t *testing.T) {
	srv := newHTTPTestServer()

	protectedRoutes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/auth/sessions"},
		{method: http.MethodPost, path: "/api/v1/auth/sessions/00000000-0000-0000-0000-000000000000/revoke"},
		{method: http.MethodGet, path: "/api/v1/ws"},
		{method: http.MethodGet, path: "/api/v1/me"},
		{method: http.MethodPatch, path: "/api/v1/me"},
		{method: http.MethodGet, path: "/api/v1/me/user-id/check?userId=netherlink"},
		{method: http.MethodPatch, path: "/api/v1/me/user-id"},
		{method: http.MethodGet, path: "/api/v1/users?keyword=ming"},
		{method: http.MethodGet, path: "/api/v1/users/by-id/netherlink"},
		{method: http.MethodGet, path: "/api/v1/users/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodPost, path: "/api/v1/users/batch"},
		{method: http.MethodGet, path: "/api/v1/friends"},
		{method: http.MethodPatch, path: "/api/v1/friends/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodDelete, path: "/api/v1/friends/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodGet, path: "/api/v1/friend-groups"},
		{method: http.MethodPost, path: "/api/v1/friend-groups"},
		{method: http.MethodPatch, path: "/api/v1/friend-groups/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodDelete, path: "/api/v1/friend-groups/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodPost, path: "/api/v1/friend-requests"},
		{method: http.MethodGet, path: "/api/v1/friend-requests"},
		{method: http.MethodPost, path: "/api/v1/friend-requests/00000000-0000-0000-0000-000000000000/accept"},
		{method: http.MethodPost, path: "/api/v1/friend-requests/00000000-0000-0000-0000-000000000000/reject"},
		{method: http.MethodGet, path: "/api/v1/groups"},
		{method: http.MethodGet, path: "/api/v1/groups/by-id?groupPublicId=1000000001"},
		{method: http.MethodGet, path: "/api/v1/groups/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodGet, path: "/api/v1/groups/00000000-0000-0000-0000-000000000000/members"},
		{method: http.MethodPost, path: "/api/v1/files/upload-session"},
		{method: http.MethodGet, path: "/api/v1/files/upload-session/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodPost, path: "/api/v1/files/upload-session/00000000-0000-0000-0000-000000000000/parts"},
		{method: http.MethodPost, path: "/api/v1/files/upload-session/00000000-0000-0000-0000-000000000000/complete"},
		{method: http.MethodPost, path: "/api/v1/files/upload-session/00000000-0000-0000-0000-000000000000/abort"},
		{method: http.MethodGet, path: "/api/v1/conversations"},
		{method: http.MethodPost, path: "/api/v1/conversations/direct"},
		{method: http.MethodPost, path: "/api/v1/conversations/group"},
		{method: http.MethodGet, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodPatch, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000/settings"},
		{method: http.MethodPost, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000/read"},
		{method: http.MethodPost, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000/unread"},
		{method: http.MethodGet, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000/messages"},
		{method: http.MethodPost, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000/messages"},
		{method: http.MethodPost, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000/messages/00000000-0000-0000-0000-000000000000/recall"},
		{method: http.MethodDelete, path: "/api/v1/conversations/00000000-0000-0000-0000-000000000000/messages"},
		{method: http.MethodGet, path: "/api/v1/ai/conversations"},
		{method: http.MethodPost, path: "/api/v1/ai/conversations"},
		{method: http.MethodPatch, path: "/api/v1/ai/conversations/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodGet, path: "/api/v1/ai/conversations/00000000-0000-0000-0000-000000000000/messages"},
		{method: http.MethodPost, path: "/api/v1/ai/conversations/00000000-0000-0000-0000-000000000000/messages"},
		{method: http.MethodPost, path: "/api/v1/ai/conversations/00000000-0000-0000-0000-000000000000/files"},
		{method: http.MethodGet, path: "/api/v1/ai/conversations/00000000-0000-0000-0000-000000000000/files"},
		{method: http.MethodGet, path: "/api/v1/ai/files/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodDelete, path: "/api/v1/ai/files/00000000-0000-0000-0000-000000000000"},
		{method: http.MethodGet, path: "/api/v1/ai/conversations/00000000-0000-0000-0000-000000000000/context"},
		{method: http.MethodPost, path: "/api/v1/ai/messages/00000000-0000-0000-0000-000000000000/feedback"},
		{method: http.MethodPost, path: "/api/v1/ai/title"},
	}

	for _, route := range protectedRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Header.Set("X-Request-Id", "req_me")
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		assertErrorResponse(t, rec, http.StatusUnauthorized, "TOKEN_MISSING", "req_me")
	}
}

func TestJSONNotFound(t *testing.T) {
	srv := newHTTPTestServer()

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	req.Header.Set("X-Request-Id", "req_missing")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND", "req_missing")
}

func TestJSONMethodNotAllowed(t *testing.T) {
	srv := newHTTPTestServer()

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	req.Header.Set("X-Request-Id", "req_method")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "req_method")
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Fatalf("allow header = %q, want it to include GET", allow)
	}
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, code string, requestID string) {
	t.Helper()

	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, status, rec.Body.String())
	}

	var body struct {
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v; body = %s", err, rec.Body.String())
	}
	if body.Code != code {
		t.Fatalf("code = %q, want %q", body.Code, code)
	}
	if body.RequestID != requestID {
		t.Fatalf("requestId = %q, want %q", body.RequestID, requestID)
	}
}
