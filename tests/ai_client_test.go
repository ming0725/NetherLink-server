package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ai "nlserver/internal/agent/conversation"
	"nlserver/internal/agent/tools"
	"nlserver/internal/config"
)

type traceRequest struct {
	Method      string
	Path        string
	APIKey      string
	WorkspaceID string
	Body        map[string]any
}

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

func TestOpenAICompatibleClientCompleteReportsLangSmithRun(t *testing.T) {
	traceRequests := make(chan traceRequest, 2)
	langSmithServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode langsmith request: %v", err)
		}
		traceRequests <- traceRequest{
			Method:      r.Method,
			Path:        r.URL.Path,
			APIKey:      r.Header.Get("x-api-key"),
			WorkspaceID: r.Header.Get("x-tenant-id"),
			Body:        body,
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer langSmithServer.Close()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {"content": "traced assistant output"},
					"finish_reason": "stop"
				}
			],
			"usage": {"total_tokens": 23}
		}`))
	}))
	defer providerServer.Close()

	client, err := ai.NewChatClient(config.AIConfig{
		Provider:       "openai-compatible",
		APIKey:         "test-key",
		BaseURL:        providerServer.URL,
		RequestTimeout: time.Second,
		LangSmith: config.LangSmithConfig{
			Tracing:        true,
			Endpoint:       langSmithServer.URL,
			APIKey:         "test-langsmith-key",
			Project:        "NLServer",
			WorkspaceID:    "workspace-1",
			RequestTimeout: time.Second,
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	output, err := client.Complete(context.Background(), ai.CompletionRequest{
		Model: "mock-model",
		Messages: []ai.ChatMessage{
			{Role: "user", Content: "Trace this."},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if output.Content != "traced assistant output" {
		t.Fatalf("Content = %q, want traced assistant output", output.Content)
	}
	post := receiveTraceRequest(t, traceRequests)
	patch := receiveTraceRequest(t, traceRequests)

	if post.Method != http.MethodPost || post.Path != "/runs" {
		t.Fatalf("post request = %s %s, want POST /runs", post.Method, post.Path)
	}
	if post.APIKey != "test-langsmith-key" {
		t.Fatalf("langsmith api key header = %q, want test-langsmith-key", post.APIKey)
	}
	if post.WorkspaceID != "workspace-1" {
		t.Fatalf("langsmith workspace header = %q, want workspace-1", post.WorkspaceID)
	}
	if post.Body["run_type"] != "llm" {
		t.Fatalf("run_type = %v, want llm", post.Body["run_type"])
	}
	if post.Body["session_name"] != "NLServer" {
		t.Fatalf("session_name = %v, want NLServer", post.Body["session_name"])
	}
	inputs, ok := post.Body["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("inputs = %#v, want object", post.Body["inputs"])
	}
	messages, ok := inputs["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v, want one message", inputs["messages"])
	}
	firstMessage, ok := messages[0].(map[string]any)
	if !ok || firstMessage["role"] != "user" || firstMessage["content"] != "Trace this." {
		t.Fatalf("first trace message = %#v, want user message", messages[0])
	}
	extra, ok := post.Body["extra"].(map[string]any)
	if !ok {
		t.Fatalf("extra = %#v, want object", post.Body["extra"])
	}
	metadata, ok := extra["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %#v, want object", extra["metadata"])
	}
	if metadata["ls_provider"] != "openai-compatible" || metadata["ls_model_name"] != "mock-model" {
		t.Fatalf("metadata = %#v, want provider/model", metadata)
	}

	if patch.Method != http.MethodPatch || !strings.HasPrefix(patch.Path, "/runs/") {
		t.Fatalf("patch request = %s %s, want PATCH /runs/{id}", patch.Method, patch.Path)
	}
	outputs, ok := patch.Body["outputs"].(map[string]any)
	if !ok {
		t.Fatalf("outputs = %#v, want object", patch.Body["outputs"])
	}
	usage, ok := outputs["usage_metadata"].(map[string]any)
	if !ok || usage["total_tokens"] != float64(23) {
		t.Fatalf("usage_metadata = %#v, want total tokens 23", outputs["usage_metadata"])
	}
	choices, ok := outputs["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("choices = %#v, want one choice", outputs["choices"])
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		t.Fatalf("choice = %#v, want object", choices[0])
	}
	message, ok := choice["message"].(map[string]any)
	if !ok || message["content"] != "traced assistant output" {
		t.Fatalf("patch message = %#v, want traced output", choice["message"])
	}
}

func receiveTraceRequest(t *testing.T, traceRequests <-chan traceRequest) traceRequest {
	t.Helper()

	select {
	case request := <-traceRequests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for LangSmith trace request")
	}
	return traceRequest{}
}

func TestOpenAICompatibleClientCompleteSendsToolsAndParsesToolCalls(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {
						"content": "",
						"tool_calls": [
							{
								"id": "call_1",
								"type": "function",
								"function": {
									"name": "local_time",
									"arguments": "{\"timezone\":\"Asia/Shanghai\"}"
								}
							}
						]
					},
					"finish_reason": "tool_calls"
				}
			]
		}`))
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

	output, err := client.Complete(context.Background(), ai.CompletionRequest{
		Model:    "mock-model",
		Messages: []ai.ChatMessage{{Role: "user", Content: "what time is it"}},
		Tools: []tools.ToolDefinition{
			{
				Name:        "local_time",
				Description: "Get local time.",
				Schema:      map[string]any{"type": "object"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	requestTools, ok := captured["tools"].([]any)
	if !ok || len(requestTools) != 1 {
		t.Fatalf("tools = %#v, want one tool", captured["tools"])
	}
	if len(output.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(output.ToolCalls))
	}
	if output.ToolCalls[0].Name != "local_time" {
		t.Fatalf("tool call name = %q, want local_time", output.ToolCalls[0].Name)
	}
	args, ok := output.ToolCalls[0].Arguments.(map[string]any)
	if !ok || args["timezone"] != "Asia/Shanghai" {
		t.Fatalf("tool call args = %#v, want timezone Asia/Shanghai", output.ToolCalls[0].Arguments)
	}
}

func TestOpenAICompatibleClientCompleteSendsNativeToolMessages(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {"content": "final answer"},
					"finish_reason": "stop"
				}
			]
		}`))
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

	_, err = client.Complete(context.Background(), ai.CompletionRequest{
		Model: "mock-model",
		Messages: []ai.ChatMessage{
			{Role: "user", Content: "what time is it"},
			{
				Role: "assistant",
				ToolCalls: []tools.ToolCall{{
					ID:        "call_1",
					Name:      "local_time",
					Arguments: map[string]any{"timezone": "Asia/Shanghai"},
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true,"date":"2026-06-24"}`},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	messages, ok := captured["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("messages = %#v, want 3 messages", captured["messages"])
	}
	assistantMessage, ok := messages[1].(map[string]any)
	if !ok || assistantMessage["role"] != "assistant" {
		t.Fatalf("assistant message = %#v", messages[1])
	}
	toolCalls, ok := assistantMessage["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %#v, want one call", assistantMessage["tool_calls"])
	}
	toolMessage, ok := messages[2].(map[string]any)
	if !ok || toolMessage["role"] != "tool" || toolMessage["tool_call_id"] != "call_1" {
		t.Fatalf("tool message = %#v, want native tool result", messages[2])
	}
}

func TestOpenAICompatibleClientCompleteSendsResponseSchema(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {"content": "{\"need_verify\":false,\"query\":\"\",\"reason\":\"stable\"}"},
					"finish_reason": "stop"
				}
			]
		}`))
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

	schema := map[string]any{
		"name":   "verification_decision",
		"strict": true,
		"schema": map[string]any{
			"type": "object",
		},
	}
	_, err = client.Complete(context.Background(), ai.CompletionRequest{
		Model:          "mock-model",
		Messages:       []ai.ChatMessage{{Role: "user", Content: "check"}},
		ResponseSchema: schema,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	responseFormat, ok := captured["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_schema" {
		t.Fatalf("response_format = %#v, want json_schema", captured["response_format"])
	}
	if responseFormat["json_schema"] == nil {
		t.Fatal("response_format.json_schema is nil")
	}
}

func TestOpenAICompatibleClientCompleteOmitsResponseSchemaForDeepSeek(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {"content": "{\"tool_required\":false,\"answer_mode\":\"no_tool\"}"},
					"finish_reason": "stop"
				}
			]
		}`))
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

	_, err = client.Complete(context.Background(), ai.CompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ai.ChatMessage{{Role: "user", Content: "route"}},
		ResponseSchema: map[string]any{
			"name": "react_route_decision",
			"schema": map[string]any{
				"type": "object",
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if _, ok := captured["response_format"]; ok {
		t.Fatalf("response_format = %#v, want omitted for DeepSeek", captured["response_format"])
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
		Model:           "mock-model",
		Messages:        []ai.ChatMessage{{Role: "user", Content: "stream"}},
		MaxOutputTokens: 700,
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
	if captured["max_tokens"] != float64(700) {
		t.Fatalf("max_tokens = %v, want 700", captured["max_tokens"])
	}
}

func TestOpenAICompatibleClientStreamReportsReasoningWithoutLeakingText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private chain of thought\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":5}}\n\n"))
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

	var deltas []ai.CompletionStreamDelta
	output, err := client.Stream(context.Background(), ai.CompletionRequest{
		Model:    "mock-model",
		Messages: []ai.ChatMessage{{Role: "user", Content: "stream"}},
	}, func(delta ai.CompletionStreamDelta) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	if output.Content != "answer" {
		t.Fatalf("Content = %q, want answer", output.Content)
	}
	if len(deltas) != 2 {
		t.Fatalf("len(deltas) = %d, want 2", len(deltas))
	}
	if !deltas[0].Thinking || deltas[0].Content != "" {
		t.Fatalf("reasoning delta = %#v, want thinking-only delta", deltas[0])
	}
	if strings.Contains(deltas[0].Content, "private chain of thought") {
		t.Fatalf("reasoning text leaked in content: %#v", deltas[0])
	}
	if deltas[1].Thinking || deltas[1].Content != "answer" {
		t.Fatalf("answer delta = %#v, want content-only answer", deltas[1])
	}
}
