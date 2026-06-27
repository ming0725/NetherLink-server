package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nlserver/internal/agent/tools"
	"nlserver/internal/config"
)

type OpenAICompatibleModelProvider struct{}

func (OpenAICompatibleModelProvider) Name() string {
	return "openai-compatible"
}

func (OpenAICompatibleModelProvider) Supports(provider string) bool {
	switch normalizeModelProvider(provider) {
	case "openai", "openai-compatible":
		return true
	default:
		return false
	}
}

func (OpenAICompatibleModelProvider) NewGateway(cfg config.AIConfig) (ModelGateway, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, ErrModelProviderUnavailable
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAICompatibleGateway{
		httpClient: &http.Client{Timeout: cfg.RequestTimeout},
		baseURL:    baseURL,
		apiKey:     apiKey,
	}, nil
}

type OpenAICompatibleGateway struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

func (g *OpenAICompatibleGateway) Complete(ctx context.Context, req ModelRequest) (ModelResponse, error) {
	if g == nil || g.httpClient == nil || g.apiKey == "" {
		return ModelResponse{}, ErrModelProviderUnavailable
	}
	payload := openAICompatiblePayload(req, false, g.baseURL)
	body, err := json.Marshal(payload)
	if err != nil {
		return ModelResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, err
	}
	setOpenAICompatibleHeaders(httpReq, g.apiKey, false)

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return ModelResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ModelResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ModelResponse{}, fmt.Errorf("model provider returned %d: %s", resp.StatusCode, truncateProviderErrorBody(string(respBody), 500))
	}

	decoded, err := decodeOpenAICompatibleResponse(respBody)
	if err != nil {
		return ModelResponse{}, err
	}
	if len(decoded.Choices) == 0 {
		return ModelResponse{}, errors.New("model provider returned no choices")
	}
	choice := decoded.Choices[0]
	return ModelResponse{
		Content:      choice.Message.Content,
		ToolCalls:    convertOpenAIToolCalls(choice.Message.ToolCalls),
		TokenCount:   decoded.Usage.TotalTokens,
		FinishReason: choice.FinishReason,
	}, nil
}

func (g *OpenAICompatibleGateway) Stream(ctx context.Context, req ModelRequest, onDelta func(ModelDelta) error) (ModelResponse, error) {
	if g == nil || g.httpClient == nil || g.apiKey == "" {
		return ModelResponse{}, ErrModelProviderUnavailable
	}
	payload := openAICompatiblePayload(req, true, g.baseURL)
	body, err := json.Marshal(payload)
	if err != nil {
		return ModelResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, err
	}
	setOpenAICompatibleHeaders(httpReq, g.apiKey, true)

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return ModelResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return ModelResponse{}, fmt.Errorf("model provider returned %d: %s", resp.StatusCode, truncateProviderErrorBody(string(respBody), 500))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var content strings.Builder
	var toolCalls []tools.ToolCall
	var finishReason string
	totalTokens := 0
	streamResponse := func() ModelResponse {
		return ModelResponse{
			Content:      content.String(),
			ToolCalls:    toolCalls,
			TokenCount:   totalTokens,
			FinishReason: finishReason,
		}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		delta, usageTokens, err := parseOpenAICompatibleStreamData(data)
		if err != nil {
			return streamResponse(), err
		}
		if usageTokens > 0 {
			totalTokens = usageTokens
		}
		if delta.FinishReason != "" {
			finishReason = delta.FinishReason
		}
		if delta.Content != "" {
			content.WriteString(delta.Content)
		}
		if delta.ToolCall != nil {
			toolCalls = append(toolCalls, *delta.ToolCall)
		}
		if onDelta != nil && (delta.Thinking || delta.Content != "" || delta.ToolCall != nil) {
			if err := onDelta(delta); err != nil {
				return streamResponse(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return streamResponse(), err
	}
	return streamResponse(), nil
}

func openAICompatiblePayload(req ModelRequest, stream bool, baseURL string) map[string]any {
	payload := map[string]any{
		"model":    strings.TrimSpace(req.Model),
		"messages": make([]map[string]any, 0, len(req.Messages)),
		"stream":   stream,
	}
	if req.ThinkingType != "" {
		payload["thinking"] = map[string]string{"type": req.ThinkingType}
	}
	if req.ReasoningEffort != "" {
		payload["reasoning_effort"] = req.ReasoningEffort
	}
	if req.MaxOutputTokens > 0 {
		payload["max_tokens"] = req.MaxOutputTokens
	}
	for _, message := range req.Messages {
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
		payload["messages"] = append(payload["messages"].([]map[string]any), encoded)
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			if strings.TrimSpace(tool.Name) == "" {
				continue
			}
			schema := tool.Schema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  schema,
				},
			})
		}
		if len(tools) > 0 {
			payload["tools"] = tools
		}
	}
	if req.ResponseSchema != nil && supportsJSONSchemaResponseFormat(req.Model, baseURL) {
		payload["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": req.ResponseSchema,
		}
	}
	return payload
}

func supportsJSONSchemaResponseFormat(model string, baseURL string) bool {
	return !isDeepSeekTarget(model, baseURL)
}

func isDeepSeekTarget(model string, baseURL string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(model, "deepseek") || strings.Contains(baseURL, "deepseek")
}

func setOpenAICompatibleHeaders(req *http.Request, apiKey string, stream bool) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
}

func truncateProviderErrorBody(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "<empty body>"
	}
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return text
}

type openAICompatibleResponse struct {
	Choices []struct {
		Message openAICompatibleMessage `json:"message"`
		Delta   openAICompatibleMessage `json:"delta"`

		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type openAICompatibleMessage struct {
	Content          string                     `json:"content"`
	ReasoningContent string                     `json:"reasoning_content"`
	ToolCalls        []openAICompatibleToolCall `json:"tool_calls"`
}

type openAICompatibleToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func decodeOpenAICompatibleResponse(data []byte) (openAICompatibleResponse, error) {
	var decoded openAICompatibleResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return openAICompatibleResponse{}, err
	}
	return decoded, nil
}

func parseOpenAICompatibleStreamData(data string) (ModelDelta, int, error) {
	decoded, err := decodeOpenAICompatibleResponse([]byte(data))
	if err != nil {
		return ModelDelta{}, 0, err
	}
	if len(decoded.Choices) == 0 {
		return ModelDelta{}, decoded.Usage.TotalTokens, nil
	}
	choice := decoded.Choices[0]
	content := choice.Delta.Content
	if content == "" {
		content = choice.Message.Content
	}
	reasoningContent := choice.Delta.ReasoningContent
	if reasoningContent == "" {
		reasoningContent = choice.Message.ReasoningContent
	}
	var toolCall *tools.ToolCall
	if calls := convertOpenAIToolCalls(choice.Delta.ToolCalls); len(calls) > 0 {
		toolCall = &calls[0]
	} else if calls := convertOpenAIToolCalls(choice.Message.ToolCalls); len(calls) > 0 {
		toolCall = &calls[0]
	}
	return ModelDelta{
		Content:      content,
		Thinking:     reasoningContent != "",
		ToolCall:     toolCall,
		FinishReason: choice.FinishReason,
	}, decoded.Usage.TotalTokens, nil
}

func convertOpenAIToolCalls(calls []openAICompatibleToolCall) []tools.ToolCall {
	results := make([]tools.ToolCall, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.Function.Name) == "" && strings.TrimSpace(call.ID) == "" {
			continue
		}
		results = append(results, tools.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: decodeToolArguments(call.Function.Arguments),
		})
	}
	return results
}

func encodeOpenAIToolCalls(calls []tools.ToolCall) []map[string]any {
	results := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			continue
		}
		results = append(results, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": encodeToolArguments(call.Arguments),
			},
		})
	}
	return results
}

func decodeToolArguments(raw string) any {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err == nil {
		return decoded
	}
	return text
}

func encodeToolArguments(args any) string {
	if args == nil {
		return "{}"
	}
	if text, ok := args.(string); ok {
		if strings.TrimSpace(text) == "" {
			return "{}"
		}
		return text
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}
