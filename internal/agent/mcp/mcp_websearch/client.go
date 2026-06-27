package mcp_websearch

import (
	"context"
	"encoding/json"

	"nlserver/internal/agent/mcp"
)

type SearchRunner func(context.Context, any) (map[string]any, error)
type ReadRunner func(context.Context, any) (map[string]any, error)

type LocalClient struct {
	Search SearchRunner
	Read   ReadRunner
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
	case SearchTool:
		if c.Search == nil {
			return mcp.CallResponse{}, mcp.ErrToolDisabled
		}
		payload, err = c.Search(ctx, args)
	case ReadTool:
		if c.Read == nil {
			return mcp.CallResponse{}, mcp.ErrToolDisabled
		}
		payload, err = c.Read(ctx, args)
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
