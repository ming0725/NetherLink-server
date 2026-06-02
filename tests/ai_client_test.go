package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nlserver/internal/ai"
	"nlserver/internal/config"
)

func TestOpenAICompatibleClientCompleteSendsInputAndParsesOutput(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {"content": "mock assistant output"},
					"finish_reason": "stop"
				}
			],
			"usage": {"total_tokens": 17}
		}`))
	}))
	defer server.Close()

	client, err := ai.NewChatClient(config.AIConfig{
		Provider:        "openai-compatible",
		APIKey:          "test-key",
		BaseURL:         server.URL,
		RequestTimeout:  time.Second,
		ThinkingType:    "enabled",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	output, err := client.Complete(context.Background(), ai.CompletionRequest{
		Model: "mock-model",
		Messages: []ai.ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Summarize the uploaded file."},
		},
		ThinkingType:    "enabled",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if output.Content != "mock assistant output" {
		t.Fatalf("Content = %q, want mock assistant output", output.Content)
	}
	if output.TokenCount != 17 {
		t.Fatalf("TokenCount = %d, want 17", output.TokenCount)
	}
	if output.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want stop", output.FinishReason)
	}
	if captured["model"] != "mock-model" {
		t.Fatalf("model = %v, want mock-model", captured["model"])
	}
	if captured["stream"] != false {
		t.Fatalf("stream = %v, want false", captured["stream"])
	}
	messages, ok := captured["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want 2 messages", captured["messages"])
	}
	userMessage, ok := messages[1].(map[string]any)
	if !ok || userMessage["role"] != "user" || userMessage["content"] != "Summarize the uploaded file." {
		t.Fatalf("second message = %#v, want user prompt", messages[1])
	}
	thinking, ok := captured["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v, want enabled", captured["thinking"])
	}
	if captured["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v, want high", captured["reasoning_effort"])
	}
}

func TestOpenAICompatibleClientStreamParsesSSEChunks(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("Accept = %q, want text/event-stream", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":9}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := ai.NewChatClient(config.AIConfig{
		Provider:       "openai-compatible",
		APIKey:         "test-key",
		BaseURL:        server.URL,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	var chunks []string
	output, err := client.Stream(context.Background(), ai.CompletionRequest{
		Model:    "mock-model",
		Messages: []ai.ChatMessage{{Role: "user", Content: "stream"}},
	}, func(delta ai.CompletionStreamDelta) error {
		chunks = append(chunks, delta.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	if output.Content != "hello world" {
		t.Fatalf("Content = %q, want hello world", output.Content)
	}
	if strings.Join(chunks, "") != "hello world" {
		t.Fatalf("chunks = %#v, want hello world", chunks)
	}
	if output.TokenCount != 9 {
		t.Fatalf("TokenCount = %d, want 9", output.TokenCount)
	}
	if output.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want stop", output.FinishReason)
	}
	if captured["stream"] != true {
		t.Fatalf("stream = %v, want true", captured["stream"])
	}
}
