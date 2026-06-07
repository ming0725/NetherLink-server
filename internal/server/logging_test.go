package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFormatAccessLogUsesEnglishLabelsAndErrorReason(t *testing.T) {
	output := formatAccessLog(accessLogEntry{
		StartedAt: time.Date(2026, 6, 6, 14, 59, 20, 52*int(time.Millisecond), time.Local),
		Duration:  8 * time.Millisecond,
		Area:      "friend",
		Operation: "friend.request.reject",
		Method:    http.MethodPost,
		Path:      "/api/v1/friend-requests/64e1d7be-4f22-458f-bf54-13d3f12c3f43/reject",
		Status:    http.StatusNotFound,
		Bytes:     100,
		RequestID: "req_c150d0b43ab0cc8da10fbd631dbcece6",
		RemoteIP:  "::1",
		UserID:    "9413c14d-aa45-42c2-a72b-123d541aaba4",
		Error: &accessLogError{
			Code:    "NOT_FOUND",
			Message: "Resource not found",
			Cause:   "repository: not found",
		},
	}, false)

	for _, want := range []string{
		"FRIEND  friend.request.reject",
		"User: 9413c14d-aa45-42c2-a72b-123d541aaba4",
		"Error: NOT_FOUND - Resource not found (cause: repository: not found)",
		"Trace: request req_c150d0b43ab0cc8da10fbd631dbcece6 from ::1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("formatted log missing %q:\n%s", want, output)
		}
	}
	for _, unexpected := range []string{"用户", "参数", "追踪", "请求", "来源"} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("formatted log contains Chinese label %q:\n%s", unexpected, output)
		}
	}
}

func TestFriendAccessLogOperationNamesRequestDecisions(t *testing.T) {
	_, accept := accessLogOperation(http.MethodPost, "/api/v1/friend-requests/00000000-0000-0000-0000-000000000000/accept")
	if accept != "friend.request.accept" {
		t.Fatalf("accept operation = %q, want friend.request.accept", accept)
	}

	_, reject := accessLogOperation(http.MethodPost, "/api/v1/friend-requests/00000000-0000-0000-0000-000000000000/reject")
	if reject != "friend.request.reject" {
		t.Fatalf("reject operation = %q, want friend.request.reject", reject)
	}
}
