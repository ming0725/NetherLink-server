package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReproAISSEInternalErrorAgainstLocalServer(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("NETHERLINK_REPRO_AUTH_TOKEN"))
	if token == "" {
		t.Skip("set NETHERLINK_REPRO_AUTH_TOKEN to run the local AI SSE repro")
	}

	baseURL := getenvDefault("NETHERLINK_REPRO_BASE_URL", "http://localhost:8080")
	conversationID := getenvDefault("NETHERLINK_REPRO_CONVERSATION_ID", "0544861c-f9b8-4fb8-b87d-6ddb7b08dc0c")
	clientMessageID := getenvDefault("NETHERLINK_REPRO_CLIENT_MESSAGE_ID", "ai_msg_4689ed7c-f008-4b7f-b89b-f32a34b9c002")
	repeat := getenvIntDefault("NETHERLINK_REPRO_REPEAT", 5)
	exactClientMessageID := getenvBoolDefault("NETHERLINK_REPRO_EXACT_CLIENT_MESSAGE_ID", true)

	endpoint := fmt.Sprintf("%s/api/v1/ai/conversations/%s/messages?stream=true", strings.TrimRight(baseURL, "/"), conversationID)
	var internalErrors int

	for attempt := 1; attempt <= repeat; attempt++ {
		attemptClientMessageID := clientMessageID
		if !exactClientMessageID {
			attemptClientMessageID = fmt.Sprintf("%s-repro-%d-%d", clientMessageID, time.Now().UnixNano(), attempt)
		}

		result := sendAISSEReproRequest(t, endpoint, token, attemptClientMessageID)
		t.Logf("attempt %d/%d status=%d internalError=%v events=%d", attempt, repeat, result.status, result.internalError, len(result.events))
		for _, event := range result.events {
			t.Logf("attempt %d event=%s data=%s", attempt, event.name, event.data)
		}
		if result.rawBody != "" {
			t.Logf("attempt %d raw body:\n%s", attempt, result.rawBody)
		}
		if result.internalError {
			internalErrors++
		}
	}

	if internalErrors > 0 {
		t.Fatalf("reproduced INTERNAL_ERROR %d time(s) out of %d attempts", internalErrors, repeat)
	}
}

type sseReproResult struct {
	status        int
	events        []sseReproEvent
	rawBody       string
	internalError bool
}

type sseReproEvent struct {
	name string
	data string
}

func sendAISSEReproRequest(t *testing.T, endpoint string, token string, clientMessageID string) sseReproResult {
	t.Helper()

	payload := map[string]any{
		"aiFileIds":       []string{},
		"clientMessageId": clientMessageID,
		"message":         "你好，今天的日期是？",
		"model":           "deepseek-v4-flash",
		"reasoningEffort": "low",
		"thinkingType":    "enabled",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	t.Logf("POST %s payload=%s", endpoint, string(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	result := sseReproResult{status: resp.StatusCode}
	events, rawBody, readErr := readSSEReproBody(resp.Body)
	result.events = events
	result.rawBody = rawBody
	if readErr != nil {
		t.Logf("read response body error: %v", readErr)
	}

	for _, event := range events {
		if event.name == "client.error" && strings.Contains(event.data, "INTERNAL_ERROR") {
			result.internalError = true
			break
		}
	}
	if strings.Contains(rawBody, "INTERNAL_ERROR") {
		result.internalError = true
	}

	return result
}

func readSSEReproBody(r io.Reader) ([]sseReproEvent, string, error) {
	reader := bufio.NewReader(r)
	var events []sseReproEvent
	var raw strings.Builder
	var currentEvent string
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			raw.WriteString(line)
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if currentEvent != "" || len(dataLines) > 0 {
					events = append(events, sseReproEvent{
						name: currentEvent,
						data: strings.Join(dataLines, "\n"),
					})
					currentEvent = ""
					dataLines = nil
				}
			case strings.HasPrefix(trimmed, "event:"):
				currentEvent = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if err != nil {
			if err == io.EOF {
				if currentEvent != "" || len(dataLines) > 0 {
					events = append(events, sseReproEvent{
						name: currentEvent,
						data: strings.Join(dataLines, "\n"),
					})
				}
				return events, raw.String(), nil
			}
			return events, raw.String(), err
		}
	}
}

func getenvDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvIntDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func getenvBoolDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
