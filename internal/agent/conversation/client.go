package conversation

import (
	"context"
	"errors"
	"time"

	agentmodel "nlserver/internal/agent/model"
	"nlserver/internal/agent/tools"
	"nlserver/internal/config"
)

var ErrProviderUnavailable = errors.New("ai: provider unavailable")

type ChatClient interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error)
}

type ChatMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []tools.ToolCall
}

type CompletionRequest struct {
	Model           string
	Messages        []ChatMessage
	Tools           []tools.ToolDefinition
	ResponseSchema  any
	ThinkingType    string
	ReasoningEffort string
	MaxOutputTokens int
}

type CompletionResponse struct {
	Content      string
	ToolCalls    []tools.ToolCall
	TokenCount   int
	FinishReason string
}

type CompletionStreamDelta struct {
	Content      string
	Thinking     bool
	FinishReason string
}

type OpenAICompatibleClient struct {
	gateway agentmodel.ModelGateway
}

func NewChatClient(cfg config.AIConfig) (ChatClient, error) {
	gateway, err := agentmodel.NewModelGateway(cfg)
	if err != nil {
		return nil, mapModelGatewayError(err)
	}
	return NewChatClientFromModelGateway(gateway), nil
}

func NewChatClientFromModelGateway(gateway agentmodel.ModelGateway) ChatClient {
	if gateway == nil {
		return nil
	}
	return &OpenAICompatibleClient{gateway: gateway}
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if c == nil || c.gateway == nil {
		return CompletionResponse{}, ErrProviderUnavailable
	}
	response, err := c.gateway.Complete(ctx, agentmodel.ModelRequest{
		Model:           req.Model,
		Messages:        toModelMessages(req.Messages),
		Tools:           req.Tools,
		ResponseSchema:  req.ResponseSchema,
		ThinkingType:    req.ThinkingType,
		ReasoningEffort: req.ReasoningEffort,
		MaxOutputTokens: req.MaxOutputTokens,
	})
	if err != nil {
		return CompletionResponse{}, err
	}
	return CompletionResponse{
		Content:      response.Content,
		ToolCalls:    response.ToolCalls,
		TokenCount:   response.TokenCount,
		FinishReason: response.FinishReason,
	}, nil
}

func (c *OpenAICompatibleClient) Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error) {
	if c == nil || c.gateway == nil {
		return CompletionResponse{}, ErrProviderUnavailable
	}
	response, err := c.gateway.Stream(ctx, agentmodel.ModelRequest{
		Model:           req.Model,
		Messages:        toModelMessages(req.Messages),
		Tools:           req.Tools,
		ResponseSchema:  req.ResponseSchema,
		ThinkingType:    req.ThinkingType,
		ReasoningEffort: req.ReasoningEffort,
		MaxOutputTokens: req.MaxOutputTokens,
	}, func(delta agentmodel.ModelDelta) error {
		if onDelta == nil || (!delta.Thinking && delta.Content == "") {
			return nil
		}
		return onDelta(CompletionStreamDelta{
			Content:      delta.Content,
			Thinking:     delta.Thinking,
			FinishReason: delta.FinishReason,
		})
	})
	completion := CompletionResponse{
		Content:      response.Content,
		ToolCalls:    response.ToolCalls,
		TokenCount:   response.TokenCount,
		FinishReason: response.FinishReason,
	}
	return completion, err
}

func toModelMessages(messages []ChatMessage) []agentmodel.ModelMessage {
	result := make([]agentmodel.ModelMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, agentmodel.ModelMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  message.ToolCalls,
		})
	}
	return result
}

func mapModelGatewayError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, agentmodel.ErrModelProviderUnavailable),
		errors.Is(err, agentmodel.ErrModelProviderUnsupported):
		return ErrProviderUnavailable
	default:
		return err
	}
}

func timeoutContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
