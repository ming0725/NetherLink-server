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
	ModelToolLocalTime        = "local_time"
	ModelToolCalculate        = mcp_utility.ModelCalculateTool
	ModelToolCalendar         = mcp_utility.ModelCalendarTool
	ModelToolWebSearch        = mcp_websearch.ModelSearchTool
	ModelToolWebRead          = mcp_websearch.ModelReadTool
	ModelToolSubAgentResearch = "subagent_research"
	ModelToolGroupChatSearch  = "group_chat_search"
)

func CompletionToolDefinitions(cfg config.AIConfig) []agenttools.ToolDefinition {
	return CompletionToolDefinitionsWithExtraTools(cfg, nil)
}

func CompletionToolDefinitionsWithExtraTools(cfg config.AIConfig, extraTools []agenttools.ToolDefinition) []agenttools.ToolDefinition {
	definitions := []agenttools.ToolDefinition{
		LocalTimeDefinition(),
		CalculateDefinition(),
		CalendarDefinition(),
	}
	if searchConfigured(cfg) {
		definitions = append(definitions, WebSearchDefinition())
	}
	definitions = append(definitions, WebReadDefinition())
	definitions = append(definitions, extraTools...)
	definitions = append(definitions, SubAgentResearchDefinition(cfg, toolNamesFromDefinitions(extraTools)...))
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

func GroupChatSearchDefinition() agenttools.ToolDefinition {
	return agenttools.ToolDefinition{
		Name:        ModelToolGroupChatSearch,
		Description: "Search older messages in the current group chat, then optionally expand context around a selected hit. Use mode=search first for sparse hits, and mode=context with message_id or message_seq only for the most relevant hit(s). Results are private group conversation records, not external factual evidence.",
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"search", "context"},
					"description": "Use search to return sparse matching messages. Use context to expand nearby chat around a selected message_id or message_seq from a prior search result. Defaults to search.",
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Natural-language query describing the group-chat content to retrieve. Required for mode=search; optional note for mode=context.",
				},
				"keywords": map[string]any{
					"type":        "array",
					"maxItems":    8,
					"items":       map[string]any{"type": "string"},
					"description": "Optional exact keywords or names to match in message text for mode=search.",
				},
				"message_id": map[string]any{
					"type":        "string",
					"description": "Message ID from a prior search result to expand when mode=context.",
				},
				"message_seq": map[string]any{
					"type":        "integer",
					"description": "Message sequence from a prior search result to expand when mode=context. Use message_id when available.",
				},
				"since": map[string]any{
					"type":        "string",
					"description": "Optional RFC3339 timestamp or YYYY-MM-DD local date lower bound.",
				},
				"until": map[string]any{
					"type":        "string",
					"description": "Optional RFC3339 timestamp or YYYY-MM-DD local date upper bound.",
				},
				"before_message_seq": map[string]any{
					"type":        "integer",
					"description": "Optional message sequence upper bound. The runtime will not allow searching after the triggering group message.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     20,
					"description": "Maximum sparse matches to return for mode=search.",
				},
				"context_before": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"maximum":     20,
					"description": "For mode=context, number of messages before the selected message. Defaults to 8.",
				},
				"context_after": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"maximum":     20,
					"description": "For mode=context, number of messages after the selected message. Defaults to 8.",
				},
			},
		},
	}
}

func SubAgentResearchDefinition(cfg config.AIConfig, extraToolNames ...string) agenttools.ToolDefinition {
	toolNames := []any{ModelToolLocalTime, ModelToolCalculate, ModelToolCalendar, ModelToolWebRead}
	if searchConfigured(cfg) {
		toolNames = append(toolNames, ModelToolWebSearch)
	}
	for _, name := range normalizeToolNames(extraToolNames) {
		toolNames = append(toolNames, name)
	}
	return agenttools.ToolDefinition{
		Name:        ModelToolSubAgentResearch,
		Description: "Delegate independent information-gathering subtasks to parallel backend subagents. Use this when a request can be split into multiple searches, reads, date/time checks, calculations, calendar computations, or group-chat lookups. Each subtask must declare input, expected output, available tools, necessity, selected tool, arguments, and validation criteria before execution. The backend enforces a maximum number of subtasks and returns structured evidence plus validation status.",
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"tasks"},
			"properties": map[string]any{
				"objective": map[string]any{
					"type":        "string",
					"description": "Optional concise objective for the overall delegated research.",
				},
				"tasks": map[string]any{
					"type":        "array",
					"minItems":    1,
					"maxItems":    6,
					"description": "Independent subtasks to execute in parallel. Each item is one bounded step; the backend rejects more than 6 steps.",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"id", "input", "expected_output", "available_tools", "need_tool", "tool_necessity", "tool", "arguments", "validation"},
						"properties": map[string]any{
							"id": map[string]any{
								"type":        "string",
								"description": "Short stable identifier for this subtask.",
							},
							"title": map[string]any{
								"type":        "string",
								"description": "Human-readable subtask title.",
							},
							"input": map[string]any{
								"type":        "string",
								"description": "The concrete input this subtask will operate on.",
							},
							"expected_output": map[string]any{
								"type":        "string",
								"description": "The exact output/evidence this subtask is expected to produce.",
							},
							"available_tools": map[string]any{
								"type":        "array",
								"minItems":    1,
								"items":       map[string]any{"type": "string", "enum": toolNames},
								"description": "Tools considered for this subtask. Must include the selected tool.",
							},
							"need_tool": map[string]any{
								"type":        "boolean",
								"description": "Whether a backend tool call is necessary for this subtask. False means the subtask is skipped and returned as not executed.",
							},
							"tool_necessity": map[string]any{
								"type":        "string",
								"description": "One-sentence justification for why the selected tool is necessary before calling it.",
							},
							"tool": map[string]any{
								"type":        "string",
								"enum":        toolNames,
								"description": "Built-in tool the subagent should call.",
							},
							"arguments": map[string]any{
								"type":                 "object",
								"additionalProperties": true,
								"description":          "Arguments for the selected tool.",
							},
							"validation": map[string]any{
								"type":        "string",
								"description": "How to check whether the tool result satisfies expected_output.",
							},
						},
					},
				},
			},
		},
	}
}

func toolNamesFromDefinitions(definitions []agenttools.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return normalizeToolNames(names)
}

func normalizeToolNames(values []string) []string {
	seen := map[string]struct{}{}
	names := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		names = append(names, value)
	}
	return names
}

func searchConfigured(cfg config.AIConfig) bool {
	return strings.TrimSpace(cfg.TavilyAPIKey) != "" ||
		strings.TrimSpace(cfg.BraveSearchAPIKey) != "" ||
		strings.TrimSpace(cfg.BaiduSearchAPIKey) != ""
}

func rawSchemaAsMap(raw json.RawMessage) any {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return decoded
}
