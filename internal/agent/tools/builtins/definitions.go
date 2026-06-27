package builtins

import (
	"encoding/json"
	"strings"

	"nlserver/internal/agent/mcp/mcp_utility"
	"nlserver/internal/agent/mcp/mcp_websearch"
	agenttools "nlserver/internal/agent/tools"
	"nlserver/internal/config"
)

const (
	ModelToolLocalTime = "local_time"
	ModelToolCalculate = mcp_utility.ModelCalculateTool
	ModelToolCalendar  = mcp_utility.ModelCalendarTool
	ModelToolWebSearch = mcp_websearch.ModelSearchTool
	ModelToolWebRead   = mcp_websearch.ModelReadTool
)

func CompletionToolDefinitions(cfg config.AIConfig) []agenttools.ToolDefinition {
	definitions := []agenttools.ToolDefinition{
		LocalTimeDefinition(),
		CalculateDefinition(),
		CalendarDefinition(),
	}
	if strings.TrimSpace(cfg.TavilyAPIKey) != "" || strings.TrimSpace(cfg.BraveSearchAPIKey) != "" {
		definitions = append(definitions, WebSearchDefinition())
	}
	definitions = append(definitions, WebReadDefinition())
	return definitions
}

func LocalTimeDefinition() agenttools.ToolDefinition {
	return agenttools.ToolDefinition{
		Name:        ModelToolLocalTime,
		Description: "Get the current date and time in a requested IANA timezone. Use this for questions about the current time, date, today, tomorrow, or relative dates.",
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"timezone": map[string]any{
					"type":        "string",
					"description": "Optional IANA timezone, for example Asia/Shanghai or America/New_York. Leave empty only when the request timezone is already supplied by the runtime.",
				},
			},
		},
	}
}

func CalculateDefinition() agenttools.ToolDefinition {
	return agenttools.ToolDefinition{
		Name:        ModelToolCalculate,
		Description: mcp_utility.CalculateDescription(),
		Schema:      rawSchemaAsMap(mcp_utility.CalculateInputSchema()),
	}
}

func CalendarDefinition() agenttools.ToolDefinition {
	return agenttools.ToolDefinition{
		Name:        ModelToolCalendar,
		Description: mcp_utility.CalendarDescription(),
		Schema:      rawSchemaAsMap(mcp_utility.CalendarInputSchema()),
	}
}

func WebSearchDefinition() agenttools.ToolDefinition {
	return agenttools.ToolDefinition{
		Name:        ModelToolWebSearch,
		Description: mcp_websearch.SearchDescription(),
		Schema:      rawSchemaAsMap(mcp_websearch.SearchInputSchema()),
	}
}

func WebReadDefinition() agenttools.ToolDefinition {
	return agenttools.ToolDefinition{
		Name:        ModelToolWebRead,
		Description: mcp_websearch.ReadDescription(),
		Schema:      rawSchemaAsMap(mcp_websearch.ReadInputSchema()),
	}
}

func rawSchemaAsMap(raw json.RawMessage) any {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return decoded
}
