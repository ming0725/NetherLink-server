package mcp_utility

import (
	"context"
	"encoding/json"
	"strings"

	"nlserver/internal/agent/mcp"
)

type CalculateRunner func(context.Context, any) (map[string]any, error)
type CalendarRunner func(context.Context, any) (map[string]any, error)

type LocalClient struct {
	Calculate CalculateRunner
	Calendar  CalendarRunner
}

func (c LocalClient) ListTools(ctx context.Context, server mcp.ServerManifest) ([]mcp.ToolManifest, error) {
	return append([]mcp.ToolManifest(nil), server.Tools...), nil
}

func (c LocalClient) CallTool(ctx context.Context, server mcp.ServerManifest, req mcp.CallRequest) (mcp.CallResponse, error) {
	args, err := mcp.DecodeArguments(req.Arguments)
	if err != nil {
		return mcp.CallResponse{}, err
	}
	var (
		payload map[string]any
	)
	switch req.ToolName {
	case CalculateTool:
		if c.Calculate == nil {
			return mcp.CallResponse{}, mcp.ErrToolDisabled
		}
		payload, err = c.Calculate(ctx, args)
	case CalendarTool:
		if c.Calendar == nil {
			return mcp.CallResponse{}, mcp.ErrToolDisabled
		}
		payload, err = c.Calendar(ctx, args)
	default:
		return mcp.CallResponse{}, mcp.ErrToolNotAllowed
	}
	if err != nil {
		return mcp.CallResponse{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return mcp.CallResponse{}, err
	}
	return mcp.CallResponse{Content: encoded}, nil
}

func mapToolArgs(args any, target any) error {
	if args == nil {
		return nil
	}
	switch value := args.(type) {
	case json.RawMessage:
		text := strings.TrimSpace(string(value))
		if text == "" || text == "null" {
			return nil
		}
		return json.Unmarshal([]byte(text), target)
	case []byte:
		text := strings.TrimSpace(string(value))
		if text == "" || text == "null" {
			return nil
		}
		return json.Unmarshal([]byte(text), target)
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return json.Unmarshal([]byte(value), target)
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}
}
