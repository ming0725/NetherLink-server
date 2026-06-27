package model

import (
	"context"

	"nlserver/internal/agent/tools"
)

type ModelGateway interface {
	Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
	Stream(ctx context.Context, req ModelRequest, onDelta func(ModelDelta) error) (ModelResponse, error)
}

type ModelRequest struct {
	Model           string
	Messages        []ModelMessage
	Tools           []tools.ToolDefinition
	ResponseSchema  any
	ThinkingType    string
	ReasoningEffort string
	MaxOutputTokens int
}

type ModelMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []tools.ToolCall
}

type ModelResponse struct {
	Content      string
	ToolCalls    []tools.ToolCall
	TokenCount   int
	FinishReason string
}

type ModelDelta struct {
	Content      string
	Thinking     bool
	ToolCall     *tools.ToolCall
	FinishReason string
}
