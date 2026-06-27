package mcp_websearch

import (
	"encoding/json"

	"nlserver/internal/agent/mcp"
)

const (
	ServerName      = "web_search"
	SearchTool      = "search"
	ReadTool        = "read"
	ModelSearchTool = "web_search"
	ModelReadTool   = "web_read"
)

func Manifest() mcp.ServerManifest {
	return mcp.ServerManifest{
		Name:        ServerName,
		DisplayName: "Web Search",
		Transport:   mcp.TransportHTTP,
		Status:      "disabled",
		Tools: []mcp.ToolManifest{
			{
				Name:                 SearchTool,
				Description:          SearchDescription(),
				InputSchema:          SearchInputSchema(),
				OutputSchema:         json.RawMessage(`{"type":"object","properties":{"results":{"type":"array"}}}`),
				SideEffect:           false,
				RequiresConfirmation: false,
				Status:               "active",
			},
			{
				Name:                 ReadTool,
				Description:          ReadDescription(),
				InputSchema:          ReadInputSchema(),
				OutputSchema:         json.RawMessage(`{"type":"object","properties":{"results":{"type":"array"}}}`),
				SideEffect:           false,
				RequiresConfirmation: false,
				Status:               "active",
			},
		},
	}
}

func SearchDescription() string {
	return "Search the live web for current, recent, social, factual, or news information and return ranked source snippets/URLs. Resolve user-relative dates in the user's timezone first, then include explicit dates plus timezone/target_date when relevant. Supports query variants for the same intent. Search results are discovery evidence; use web_read to inspect source pages before making precise or disputed claims."
}

func ReadDescription() string {
	return "Open and extract readable content from one or more public web URLs. Use after web_search, or directly when the user provides a URL. This is the preferred evidence for exact facts, page-specific questions, tables, and claims challenged by the user."
}

func SearchInputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["query"],
		"properties": {
			"query": {
				"type": "string",
				"description": "The web search query."
			},
			"query_variants": {
				"type": "array",
				"maxItems": 3,
				"items": {"type": "string"},
				"description": "Optional alternate precise queries for the same user intent. Useful for very recent facts, social posts, exact quotes, aliases, or official-source searches."
			},
			"topic": {
				"type": "string",
				"enum": ["general", "news", "finance"],
				"description": "Optional search topic. Use finance for market/price/financial data, news for current events, and general otherwise."
			},
			"time_range": {
				"type": "string",
				"description": "Optional recency filter such as day, week, month, or year. Use only when a recency window is part of the user's request."
			},
			"start_date": {
				"type": "string",
				"description": "Optional YYYY-MM-DD lower date boundary using the search provider's 'after this date' semantics."
			},
			"end_date": {
				"type": "string",
				"description": "Optional YYYY-MM-DD upper date boundary using the search provider's 'before this date' semantics."
			},
			"target_date": {
				"type": "string",
				"description": "The exact YYYY-MM-DD date whose facts must be supported. This is used to assess whether results actually match the requested date."
			},
			"timezone": {
				"type": "string",
				"description": "Optional IANA timezone for resolving user-relative dates before searching, for example Asia/Shanghai. Use the user's timezone, not the search provider or source timezone."
			},
			"include_domains": {
				"type": "array",
				"maxItems": 8,
				"items": {"type": "string"},
				"description": "Optional authoritative domains to prioritize or require, without URL schemes or paths."
			},
			"exact_match": {
				"type": "boolean",
				"description": "Require exact quoted phrases from the query when entity ambiguity would otherwise be likely."
			},
			"max_results": {
				"type": "integer",
				"minimum": 1,
				"maximum": 8,
				"description": "Maximum number of search results to return."
			}
		}
	}`)
}

func ReadInputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["urls"],
		"properties": {
			"urls": {
				"type": "array",
				"minItems": 1,
				"maxItems": 5,
				"items": {"type": "string"},
				"description": "Public HTTP or HTTPS URLs to read."
			},
			"query": {
				"type": "string",
				"description": "Optional user intent used to rerank the most relevant extracted chunks."
			},
			"extract_depth": {
				"type": "string",
				"enum": ["basic", "advanced"],
				"description": "Use advanced for tables, embedded content, difficult pages, or exact verification; otherwise use basic."
			},
			"format": {
				"type": "string",
				"enum": ["markdown", "text"],
				"description": "Extracted content format. Markdown is the default."
			},
			"chunks_per_source": {
				"type": "integer",
				"minimum": 1,
				"maximum": 5,
				"description": "Relevant chunks per URL when query is provided."
			}
		}
	}`)
}
