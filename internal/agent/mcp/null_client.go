package mcp

import "context"

type NullClient struct{}

func (NullClient) ListTools(ctx context.Context, server ServerManifest) ([]ToolManifest, error) {
	return append([]ToolManifest(nil), server.Tools...), nil
}

func (NullClient) CallTool(ctx context.Context, server ServerManifest, req CallRequest) (CallResponse, error) {
	return CallResponse{}, ErrToolDisabled
}
