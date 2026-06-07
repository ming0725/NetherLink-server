package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}

	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}

	written, err := r.ResponseWriter.Write(data)
	r.bytes += written
	return written, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *responseRecorder) Flush() {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijack")
	}
	if !r.wroteHeader {
		r.status = http.StatusSwitchingProtocols
		r.wroteHeader = true
	}
	return hijacker.Hijack()
}

type accessLogContextKey struct{}

type accessLogState struct {
	principal *AuthPrincipal
	err       *accessLogError
}

type accessLogEntry struct {
	StartedAt time.Time
	Duration  time.Duration
	Area      string
	Operation string
	Method    string
	Path      string
	Query     []accessLogQueryParam
	Status    int
	Bytes     int
	RequestID string
	RemoteIP  string
	UserID    string
	Error     *accessLogError
}

type accessLogQueryParam struct {
	Key    string
	Values []string
}

type accessLogError struct {
	Code    string
	Message string
	Cause   string
}

func setAccessLogPrincipal(ctx context.Context, principal AuthPrincipal) {
	state, ok := ctx.Value(accessLogContextKey{}).(*accessLogState)
	if !ok || state == nil {
		return
	}
	state.principal = &principal
}

func setAccessLogError(ctx context.Context, code string, message string, cause error) {
	state, ok := ctx.Value(accessLogContextKey{}).(*accessLogState)
	if !ok || state == nil {
		return
	}
	entry := &accessLogError{
		Code:    strings.TrimSpace(code),
		Message: strings.TrimSpace(message),
	}
	if cause != nil {
		entry.Cause = strings.TrimSpace(cause.Error())
	}
	state.err = entry
}

func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		state := &accessLogState{}
		r = r.WithContext(context.WithValue(r.Context(), accessLogContextKey{}, state))

		next.ServeHTTP(recorder, r)

		area, operation := accessLogOperation(r.Method, r.URL.Path)
		entry := accessLogEntry{
			StartedAt: startedAt,
			Duration:  time.Since(startedAt),
			Area:      area,
			Operation: operation,
			Method:    r.Method,
			Path:      r.URL.Path,
			Query:     accessLogQueryParams(r.URL.RawQuery),
			Status:    recorder.status,
			Bytes:     recorder.bytes,
			RequestID: requestIDFromContext(r.Context()),
			RemoteIP:  remoteIP(r.RemoteAddr),
			Error:     state.err,
		}
		if state.principal != nil {
			entry.UserID = state.principal.UserID
		}
		fmt.Fprint(os.Stdout, formatAccessLog(entry, colorsEnabled()))
	})
}

func accessLogQueryParams(rawQuery string) []accessLogQueryParam {
	if rawQuery == "" {
		return nil
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return []accessLogQueryParam{{Key: "invalid query", Values: []string{rawQuery}}}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if !isSensitiveQueryParam(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	params := make([]accessLogQueryParam, 0, len(keys))
	for _, key := range keys {
		params = append(params, accessLogQueryParam{Key: key, Values: values[key]})
	}
	return params
}

func sanitizeAccessLogQuery(rawQuery string) string {
	params := accessLogQueryParams(rawQuery)
	if len(params) == 0 {
		return ""
	}
	values := url.Values{}
	for _, param := range params {
		values[param.Key] = param.Values
	}
	return values.Encode()
}

func isSensitiveQueryParam(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", ""))
	switch normalized {
	case "accesstoken", "refreshtoken", "idtoken", "token", "authorization", "apikey":
		return true
	default:
		return false
	}
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
	ansiPurple = "\x1b[35m"
)

func colorsEnabled() bool {
	return os.Getenv("NO_COLOR") == ""
}

func colorize(enabled bool, color string, value string) string {
	if !enabled || value == "" {
		return value
	}
	return color + value + ansiReset
}

func formatAccessLog(entry accessLogEntry, color bool) string {
	var builder strings.Builder
	level, levelColor := accessLogLevel(entry.Status)
	areaLabel, areaColor := accessLogAreaLabel(entry.Area)
	status := fmt.Sprintf("%d %s", entry.Status, http.StatusText(entry.Status))
	if strings.TrimSpace(http.StatusText(entry.Status)) == "" {
		status = fmt.Sprintf("%d", entry.Status)
	}

	fmt.Fprintf(&builder, "%s  %s  %s  %s\n",
		colorize(color, ansiDim, entry.StartedAt.Format("2006-01-02 15:04:05.000")),
		colorize(color, levelColor, level),
		colorize(color, areaColor, areaLabel),
		colorize(color, ansiBold, entry.Operation),
	)
	fmt.Fprintf(&builder, "  %s %s  ->  %s  %s  %s\n",
		colorize(color, methodColor(entry.Method), entry.Method),
		entry.Path,
		colorize(color, statusColor(entry.Status), status),
		formatDuration(entry.Duration),
		formatBytes(entry.Bytes),
	)
	if entry.UserID != "" {
		fmt.Fprintf(&builder, "  User: %s\n", entry.UserID)
	}
	if entry.Error != nil && (entry.Error.Code != "" || entry.Error.Message != "" || entry.Error.Cause != "") {
		builder.WriteString("  Error:")
		if entry.Error.Code != "" {
			fmt.Fprintf(&builder, " %s", entry.Error.Code)
		}
		if entry.Error.Message != "" {
			if entry.Error.Code != "" {
				builder.WriteString(" -")
			}
			fmt.Fprintf(&builder, " %s", entry.Error.Message)
		}
		if entry.Error.Cause != "" && entry.Error.Cause != entry.Error.Message {
			fmt.Fprintf(&builder, " (cause: %s)", entry.Error.Cause)
		}
		builder.WriteString("\n")
	}
	if len(entry.Query) > 0 {
		builder.WriteString("  Query:\n")
		for _, param := range entry.Query {
			fmt.Fprintf(&builder, "    %s: %s\n", param.Key, strings.Join(param.Values, ", "))
		}
	}
	if entry.RequestID != "" || entry.RemoteIP != "" {
		builder.WriteString("  Trace:")
		if entry.RequestID != "" {
			fmt.Fprintf(&builder, " request %s", entry.RequestID)
		}
		if entry.RemoteIP != "" {
			fmt.Fprintf(&builder, " from %s", entry.RemoteIP)
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	return builder.String()
}

func accessLogLevel(status int) (string, string) {
	if status >= 500 {
		return "ERROR", ansiRed
	}
	if status >= 400 {
		return "WARN ", ansiYellow
	}
	return "INFO ", ansiGreen
}

func accessLogAreaLabel(area string) (string, string) {
	switch area {
	case "ai":
		return "AI", ansiPurple
	case "chat":
		return "CHAT", ansiCyan
	case "realtime":
		return "WS", ansiBlue
	case "file":
		return "FILE", ansiBlue
	case "auth":
		return "AUTH", ansiYellow
	case "group":
		return "GROUP", ansiCyan
	case "friend":
		return "FRIEND", ansiCyan
	case "post":
		return "POST", ansiGreen
	case "notification":
		return "NOTICE", ansiGreen
	case "system":
		return "SYS", ansiDim
	default:
		return strings.ToUpper(area), ansiDim
	}
}

func statusColor(status int) string {
	if status >= 500 {
		return ansiRed
	}
	if status >= 400 {
		return ansiYellow
	}
	if status >= 300 {
		return ansiBlue
	}
	return ansiGreen
}

func methodColor(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet:
		return ansiBlue
	case http.MethodPost:
		return ansiGreen
	case http.MethodPatch:
		return ansiYellow
	case http.MethodDelete:
		return ansiRed
	default:
		return ansiDim
	}
}

func formatDuration(duration time.Duration) string {
	if duration < time.Second {
		return fmt.Sprintf("%dms", duration.Milliseconds())
	}
	return fmt.Sprintf("%.3fs", duration.Seconds())
}

func formatBytes(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1024*1024))
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func accessLogOperation(method string, requestPath string) (string, string) {
	method = strings.ToUpper(strings.TrimSpace(method))
	path := strings.TrimSpace(requestPath)

	switch {
	case path == "/healthz":
		return "system", "health.check"
	case path == "/readyz":
		return "system", "ready.check"
	case path == "/api/v1":
		return "system", "api.root"
	case path == "/api/v1/ws":
		return "realtime", "websocket.connect"
	case strings.HasPrefix(path, "/api/v1/auth/"):
		return "auth", operationFromMethod(method, "auth")
	case strings.HasPrefix(path, "/api/v1/me"):
		return "profile", operationFromMethod(method, "profile")
	case strings.HasPrefix(path, "/api/v1/avatar-files/"):
		return "file", "avatar.download"
	case strings.HasPrefix(path, "/api/v1/files/") || path == "/api/v1/files":
		return "file", operationFromMethod(method, "file")
	case strings.HasPrefix(path, "/api/v1/ai/"):
		return "ai", aiAccessLogOperation(method, path)
	case strings.HasPrefix(path, "/api/v1/conversations/") || path == "/api/v1/conversations":
		return "chat", chatAccessLogOperation(method, path)
	case strings.HasPrefix(path, "/api/v1/friend-") || strings.HasPrefix(path, "/api/v1/friends"):
		return "friend", friendAccessLogOperation(method, path)
	case strings.HasPrefix(path, "/api/v1/group-") || strings.HasPrefix(path, "/api/v1/groups"):
		return "group", operationFromMethod(method, "group")
	case strings.HasPrefix(path, "/api/v1/posts") || strings.HasPrefix(path, "/api/v1/comments") || strings.HasPrefix(path, "/api/v1/replies"):
		return "post", operationFromMethod(method, "post")
	case strings.HasPrefix(path, "/api/v1/notifications"):
		return "notification", operationFromMethod(method, "notification")
	case strings.HasPrefix(path, "/api/v1/operations"):
		return "operation", operationFromMethod(method, "operation")
	case strings.HasPrefix(path, "/api/v1/users"):
		return "directory", operationFromMethod(method, "user")
	default:
		return "http", operationFromMethod(method, "request")
	}
}

func friendAccessLogOperation(method string, path string) string {
	switch {
	case path == "/api/v1/friend-requests":
		return operationFromMethod(method, "friend.request")
	case strings.Contains(path, "/friend-requests/") && strings.HasSuffix(path, "/accept"):
		return "friend.request.accept"
	case strings.Contains(path, "/friend-requests/") && strings.HasSuffix(path, "/reject"):
		return "friend.request.reject"
	case path == "/api/v1/friend-groups" || strings.Contains(path, "/friend-groups/"):
		return operationFromMethod(method, "friend.group")
	case strings.Contains(path, "/friends/") || path == "/api/v1/friends":
		return operationFromMethod(method, "friend")
	default:
		return operationFromMethod(method, "friend")
	}
}

func aiAccessLogOperation(method string, path string) string {
	switch {
	case path == "/api/v1/ai/conversations":
		return operationFromMethod(method, "ai.conversation")
	case strings.Contains(path, "/conversations/") && !strings.Contains(path, "/messages") && !strings.Contains(path, "/files") && !strings.HasSuffix(path, "/context"):
		return operationFromMethod(method, "ai.conversation")
	case strings.Contains(path, "/messages/") && strings.HasSuffix(path, "/feedback"):
		return "ai.message.feedback"
	case strings.Contains(path, "/messages"):
		if method == http.MethodPost {
			return "ai.message.send"
		}
		return operationFromMethod(method, "ai.message")
	case strings.Contains(path, "/files"):
		return operationFromMethod(method, "ai.file")
	case strings.HasSuffix(path, "/context"):
		return "ai.context.get"
	case strings.HasSuffix(path, "/title"):
		return "ai.title.generate"
	default:
		return operationFromMethod(method, "ai")
	}
}

func chatAccessLogOperation(method string, path string) string {
	if strings.Contains(path, "/messages") {
		if method == http.MethodPost {
			return "chat.message.send"
		}
		return operationFromMethod(method, "chat.message")
	}
	if strings.HasSuffix(path, "/read") || strings.HasSuffix(path, "/unread") {
		return operationFromMethod(method, "chat.read")
	}
	return operationFromMethod(method, "chat.conversation")
}

func operationFromMethod(method string, resource string) string {
	action := strings.ToLower(method)
	switch method {
	case http.MethodGet:
		action = "get"
	case http.MethodPost:
		action = "create"
	case http.MethodPatch:
		action = "update"
	case http.MethodDelete:
		action = "delete"
	}
	return resource + "." + action
}
