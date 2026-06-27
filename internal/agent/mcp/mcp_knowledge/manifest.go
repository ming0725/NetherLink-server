package mcp_knowledge

import (
	"encoding/json"

	"nlserver/internal/agent/mcp"
)

func Manifest() mcp.ServerManifest {
	return mcp.ServerManifest{
		Name:        "knowledge",
		DisplayName: "Knowledge Base",
		Transport:   mcp.TransportHTTP,
		Status:      "disabled",
		Tools: []mcp.ToolManifest{
			{
				Name:                 "search",
				Description:          "Search a configured first-party knowledge base.",
				InputSchema:          json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["query"]}`),
				OutputSchema:         json.RawMessage(`{"type":"object","properties":{"sources":{"type":"array"}}}`),
				SideEffect:           false,
				RequiresConfirmation: false,
				Status:               "active",
			},
		},
	}
}
