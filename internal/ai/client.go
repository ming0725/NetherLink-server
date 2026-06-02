package ai

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
	"time"

	"nlserver/internal/config"
)

var ErrProviderUnavailable = errors.New("ai: provider unavailable")

type ChatClient interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error)
}

type ChatMessage struct {
	Role    string
	Content string
}

type CompletionRequest struct {
	Model           string
	Messages        []ChatMessage
	ThinkingType    string
	ReasoningEffort string
}

type CompletionResponse struct {
	Content      string
	TokenCount   int
	FinishReason string
}

type CompletionStreamDelta struct {
	Content      string
	FinishReason string
}

type OpenAICompatibleClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

func NewChatClient(cfg config.AIConfig) (ChatClient, error) {
	switch cfg.Provider {
	case "disabled", "":
		return nil, ErrProviderUnavailable
	case "openai", "openai-compatible":
		baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return &OpenAICompatibleClient{
			httpClient: &http.Client{Timeout: cfg.RequestTimeout},
			baseURL:    baseURL,
			apiKey:     strings.TrimSpace(cfg.APIKey),
		}, nil
	default:
		return nil, ErrProviderUnavailable
	}
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if c == nil || c.httpClient == nil || c.apiKey == "" {
		return CompletionResponse{}, ErrProviderUnavailable
	}
	payload := map[string]any{
		"model":    strings.TrimSpace(req.Model),
		"messages": make([]map[string]string, 0, len(req.Messages)),
		"stream":   false,
	}
	if req.ThinkingType != "" {
		payload["thinking"] = map[string]string{"type": req.ThinkingType}
	}
	if req.ReasoningEffort != "" {
		payload["reasoning_effort"] = req.ReasoningEffort
	}
	for _, message := range req.Messages {
		payload["messages"] = append(payload["messages"].([]map[string]string), map[string]string{
			"role":    message.Role,
			"content": message.Content,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return CompletionResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResponse{}, fmt.Errorf("ai provider returned %d", resp.StatusCode)
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return CompletionResponse{}, err
	}
	if len(decoded.Choices) == 0 {
		return CompletionResponse{}, errors.New("ai provider returned no choices")
	}
	return CompletionResponse{
		Content:      decoded.Choices[0].Message.Content,
		TokenCount:   decoded.Usage.TotalTokens,
		FinishReason: decoded.Choices[0].FinishReason,
	}, nil
}

func (c *OpenAICompatibleClient) Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error) {
	if c == nil || c.httpClient == nil || c.apiKey == "" {
		return CompletionResponse{}, ErrProviderUnavailable
	}
	payload := map[string]any{
		"model":    strings.TrimSpace(req.Model),
		"messages": make([]map[string]string, 0, len(req.Messages)),
		"stream":   true,
	}
	if req.ThinkingType != "" {
		payload["thinking"] = map[string]string{"type": req.ThinkingType}
	}
	if req.ReasoningEffort != "" {
		payload["reasoning_effort"] = req.ReasoningEffort
	}
	for _, message := range req.Messages {
		payload["messages"] = append(payload["messages"].([]map[string]string), map[string]string{
			"role":    message.Role,
			"content": message.Content,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<20))
		return CompletionResponse{}, fmt.Errorf("ai provider returned %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var content strings.Builder
	var finishReason string
	totalTokens := 0
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
		delta, usageTokens, err := parseStreamData(data)
		if err != nil {
			return CompletionResponse{}, err
		}
		if usageTokens > 0 {
			totalTokens = usageTokens
		}
		if delta.FinishReason != "" {
			finishReason = delta.FinishReason
		}
		if delta.Content == "" {
			continue
		}
		content.WriteString(delta.Content)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return CompletionResponse{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return CompletionResponse{}, err
	}
	return CompletionResponse{
		Content:      content.String(),
		TokenCount:   totalTokens,
		FinishReason: finishReason,
	}, nil
}

func parseStreamData(data string) (CompletionStreamDelta, int, error) {
	var decoded struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		return CompletionStreamDelta{}, 0, err
	}
	if len(decoded.Choices) == 0 {
		return CompletionStreamDelta{}, decoded.Usage.TotalTokens, nil
	}
	content := decoded.Choices[0].Delta.Content
	if content == "" {
		content = decoded.Choices[0].Message.Content
	}
	return CompletionStreamDelta{
		Content:      content,
		FinishReason: decoded.Choices[0].FinishReason,
	}, decoded.Usage.TotalTokens, nil
}

func timeoutContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
