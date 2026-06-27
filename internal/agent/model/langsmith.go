package model

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nlserver/internal/config"
)

const defaultLangSmithTraceHTTPTimeout = 5 * time.Second

type langSmithGateway struct {
	next   ModelGateway
	tracer *langSmithTracer
}

type langSmithTracer struct {
	httpClient  *http.Client
	endpoint    string
	apiKey      string
	project     string
	workspaceID string
	provider    string
	timeout     time.Duration
}

type langSmithRun struct {
	id             string
	firstTokenTime *time.Time
	startDone      chan struct{}
}

func wrapLangSmithGateway(gateway ModelGateway, cfg config.AIConfig) ModelGateway {
	if gateway == nil {
		return nil
	}
	tracer := newLangSmithTracer(cfg)
	if tracer == nil {
		return gateway
	}
	return &langSmithGateway{
		next:   gateway,
		tracer: tracer,
	}
}

func newLangSmithTracer(cfg config.AIConfig) *langSmithTracer {
	ls := cfg.LangSmith
	if !ls.Tracing || strings.TrimSpace(ls.APIKey) == "" {
		return nil
	}
	endpoint := strings.TrimRight(strings.TrimSpace(ls.Endpoint), "/")
	if endpoint == "" {
		endpoint = "https://api.smith.langchain.com"
	}
	timeout := ls.RequestTimeout
	if timeout <= 0 {
		timeout = defaultLangSmithTraceHTTPTimeout
	}
	return &langSmithTracer{
		httpClient:  &http.Client{Timeout: timeout},
		endpoint:    endpoint,
		apiKey:      strings.TrimSpace(ls.APIKey),
		project:     strings.TrimSpace(ls.Project),
		workspaceID: strings.TrimSpace(ls.WorkspaceID),
		provider:    inferLangSmithProvider(cfg),
		timeout:     timeout,
	}
}

func (g *langSmithGateway) Complete(ctx context.Context, req ModelRequest) (ModelResponse, error) {
	if g == nil || g.next == nil {
		return ModelResponse{}, ErrModelProviderUnavailable
	}
	run := g.tracer.startRun(req, false)
	response, err := g.next.Complete(ctx, req)
	g.tracer.finishRun(run, response, err)
	return response, err
}

func (g *langSmithGateway) Stream(ctx context.Context, req ModelRequest, onDelta func(ModelDelta) error) (ModelResponse, error) {
	if g == nil || g.next == nil {
		return ModelResponse{}, ErrModelProviderUnavailable
	}
	run := g.tracer.startRun(req, true)
	response, err := g.next.Stream(ctx, req, func(delta ModelDelta) error {
		if run != nil && run.firstTokenTime == nil && (delta.Content != "" || delta.Thinking || delta.ToolCall != nil) {
			now := time.Now().UTC()
			run.firstTokenTime = &now
		}
		if onDelta == nil {
			return nil
		}
		return onDelta(delta)
	})
	g.tracer.finishRun(run, response, err)
	return response, err
}

func (t *langSmithTracer) startRun(req ModelRequest, stream bool) *langSmithRun {
	if t == nil {
		return nil
	}
	startTime := time.Now().UTC()
	runID, err := newLangSmithUUIDv7(startTime)
	if err != nil {
		return nil
	}
	payload := map[string]any{
		"id":         runID,
		"name":       langSmithRunName(stream),
		"run_type":   "llm",
		"inputs":     langSmithInputs(req),
		"start_time": startTime.Format(time.RFC3339Nano),
		"extra":      langSmithExtra(t.provider, req, stream),
	}
	if t.project != "" {
		payload["session_name"] = t.project
	}
	run := &langSmithRun{id: runID, startDone: make(chan struct{})}
	go func() {
		defer close(run.startDone)
		if err := t.sendJSON(http.MethodPost, "/runs", payload); err != nil {
			slog.Default().Warn("langsmith trace start failed",
				"error", err,
				"endpoint", t.endpoint,
				"project", t.project,
				"timeout", t.timeout,
			)
		}
	}()
	return run
}

func (t *langSmithTracer) finishRun(run *langSmithRun, response ModelResponse, providerErr error) {
	if t == nil || run == nil || run.id == "" {
		return
	}
	payload := map[string]any{
		"end_time": time.Now().UTC().Format(time.RFC3339Nano),
	}
	outputs := langSmithOutputs(response)
	if len(outputs) > 0 {
		payload["outputs"] = outputs
	}
	if providerErr != nil {
		payload["error"] = providerErr.Error()
	}
	if run.firstTokenTime != nil {
		payload["first_token_time"] = run.firstTokenTime.Format(time.RFC3339Nano)
		payload["events"] = []map[string]any{{
			"name": "new_token",
			"time": run.firstTokenTime.Format(time.RFC3339Nano),
		}}
	}
	go func() {
		if run.startDone != nil {
			<-run.startDone
		}
		if err := t.sendJSON(http.MethodPatch, "/runs/"+run.id, payload); err != nil {
			slog.Default().Warn("langsmith trace finish failed",
				"error", err,
				"endpoint", t.endpoint,
				"project", t.project,
				"timeout", t.timeout,
			)
		}
	}()
}

func (t *langSmithTracer) sendJSON(method string, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	timeout := t.timeout
	if timeout <= 0 {
		timeout = defaultLangSmithTraceHTTPTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, t.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", t.apiKey)
	if t.workspaceID != "" {
		req.Header.Set("x-tenant-id", t.workspaceID)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s returned %d: %s", method, path, resp.StatusCode, truncateProviderErrorBody(string(respBody), 500))
	}
	return nil
}

func langSmithRunName(stream bool) string {
	if stream {
		return "OpenAI-compatible Chat Completion Stream"
	}
	return "OpenAI-compatible Chat Completion"
}

func langSmithInputs(req ModelRequest) map[string]any {
	inputs := map[string]any{
		"messages": langSmithMessages(req.Messages),
	}
	if modelName := strings.TrimSpace(req.Model); modelName != "" {
		inputs["model"] = modelName
	}
	if len(req.Tools) > 0 {
		encodedTools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				continue
			}
			schema := tool.Schema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			encodedTools = append(encodedTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        name,
					"description": tool.Description,
					"parameters":  schema,
				},
			})
		}
		if len(encodedTools) > 0 {
			inputs["tools"] = encodedTools
		}
	}
	if req.ResponseSchema != nil {
		inputs["response_schema"] = req.ResponseSchema
	}
	return inputs
}

func langSmithMessages(messages []ModelMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		encoded := map[string]any{
			"role":    message.Role,
			"content": message.Content,
		}
		if message.ToolCallID != "" {
			encoded["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			encoded["tool_calls"] = encodeOpenAIToolCalls(message.ToolCalls)
		}
		result = append(result, encoded)
	}
	return result
}

func langSmithExtra(provider string, req ModelRequest, stream bool) map[string]any {
	modelName := strings.TrimSpace(req.Model)
	metadata := map[string]any{
		"ls_provider":   provider,
		"ls_model_name": modelName,
	}
	invocationParams := map[string]any{
		"model":  modelName,
		"stream": stream,
	}
	if req.ThinkingType != "" {
		invocationParams["thinking_type"] = req.ThinkingType
	}
	if req.ReasoningEffort != "" {
		invocationParams["reasoning_effort"] = req.ReasoningEffort
	}
	if req.MaxOutputTokens > 0 {
		invocationParams["max_tokens"] = req.MaxOutputTokens
	}
	return map[string]any{
		"metadata":          metadata,
		"invocation_params": invocationParams,
	}
}

func langSmithOutputs(response ModelResponse) map[string]any {
	outputs := map[string]any{}
	if response.Content != "" || len(response.ToolCalls) > 0 || response.FinishReason != "" {
		message := map[string]any{
			"role":    "assistant",
			"content": response.Content,
		}
		if len(response.ToolCalls) > 0 {
			message["tool_calls"] = encodeOpenAIToolCalls(response.ToolCalls)
		}
		choice := map[string]any{
			"message": message,
		}
		if response.FinishReason != "" {
			choice["finish_reason"] = response.FinishReason
		}
		outputs["choices"] = []map[string]any{choice}
	}
	if response.TokenCount > 0 {
		outputs["usage_metadata"] = map[string]any{
			"total_tokens": response.TokenCount,
		}
	}
	return outputs
}

func inferLangSmithProvider(cfg config.AIConfig) string {
	provider := normalizeModelProvider(cfg.Provider)
	baseURL := strings.ToLower(strings.TrimSpace(cfg.BaseURL))
	switch {
	case provider == "openai":
		return "openai"
	case strings.Contains(baseURL, "api.openai.com"):
		return "openai"
	case strings.Contains(baseURL, "deepseek"):
		return "deepseek"
	case provider != "":
		return provider
	default:
		return "openai-compatible"
	}
}

func newLangSmithUUIDv7(now time.Time) (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	millis := uint64(now.UnixNano() / int64(time.Millisecond))
	bytes[0] = byte(millis >> 40)
	bytes[1] = byte(millis >> 32)
	bytes[2] = byte(millis >> 24)
	bytes[3] = byte(millis >> 16)
	bytes[4] = byte(millis >> 8)
	bytes[5] = byte(millis)
	bytes[6] = (bytes[6] & 0x0f) | 0x70
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}
