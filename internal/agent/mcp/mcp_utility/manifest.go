package mcp_utility

import (
	"encoding/json"

	"nlserver/internal/agent/mcp"
)

const (
	ServerName         = "utility"
	CalculateTool      = "calculate"
	CalendarTool       = "calendar"
	ModelCalculateTool = "calculate"
	ModelCalendarTool  = "calendar"
)

func Manifest() mcp.ServerManifest {
	return mcp.ServerManifest{
		Name:        ServerName,
		DisplayName: "Utility Tools",
		Transport:   mcp.TransportHTTP,
		Status:      "active",
		Tools: []mcp.ToolManifest{
			{
				Name:                 CalculateTool,
				Description:          CalculateDescription(),
				InputSchema:          CalculateInputSchema(),
				OutputSchema:         json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"},"value":{"type":"string"},"result_count":{"type":"integer"}}}`),
				SideEffect:           false,
				RequiresConfirmation: false,
				Status:               "active",
			},
			{
				Name:                 CalendarTool,
				Description:          CalendarDescription(),
				InputSchema:          CalendarInputSchema(),
				OutputSchema:         json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"},"result":{"type":"object"},"results":{"type":"array"},"result_count":{"type":"integer"}}}`),
				SideEffect:           false,
				RequiresConfirmation: false,
				Status:               "active",
			},
		},
	}
}

func CalculateDescription() string {
	return "Evaluate numeric, boolean, comparison, aggregate, and scientific expressions without executing code. Supports long expressions, variables, constants, arithmetic precedence, parentheses, factorial, percentages, modulo, powers, trigonometric/log functions, min/max/sum/avg/product, and integer gcd/lcm. Use it for exact-looking arithmetic, formula checking, fast long-expression evaluation, percentages, statistics, and deterministic calculations."
}

func CalendarDescription() string {
	return "Calculate dates and calendar facts. Supports date info, date/time add or subtract, date differences, ISO week/weekday, period boundaries, nth weekday of a month, date ranges, and business-day add/diff with configurable weekends and holidays. Use it for forward/backward date inference, weekday lookup, workday counting, month/quarter/year boundaries, and relative calendar reasoning."
}

func CalculateInputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["expression"],
		"properties": {
			"expression": {
				"type": "string",
				"description": "Expression to evaluate, for example '(12.5% + 0.3) * 4', 'sum(1,2,3)^2', or 'gcd(84, 126)'."
			},
			"variables": {
				"type": "object",
				"additionalProperties": {
					"anyOf": [
						{"type": "number"},
						{"type": "integer"},
						{"type": "string"},
						{"type": "boolean"}
					]
				},
				"description": "Optional variables referenced by name in the expression. Values may be numeric strings, numbers, or booleans."
			},
			"precision_bits": {
				"type": "integer",
				"minimum": 32,
				"maximum": 1024,
				"description": "Internal big-float precision in bits. Default 256."
			},
			"angle_unit": {
				"type": "string",
				"enum": ["radian", "degree"],
				"description": "Angle unit for trigonometric functions. Default radian."
			}
		}
	}`)
}

func CalendarInputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["info", "add", "diff", "business_add", "business_diff", "nth_weekday", "period_bounds", "range"],
				"description": "Calendar operation. Defaults to info."
			},
			"timezone": {
				"type": "string",
				"description": "Optional IANA timezone such as Asia/Shanghai or America/New_York. Defaults to the request timezone when supplied by the runtime, otherwise server local timezone."
			},
			"date": {
				"type": "string",
				"description": "Primary date or datetime. Accepts YYYY-MM-DD, RFC3339, or common local datetime formats."
			},
			"start_date": {
				"type": "string",
				"description": "Start date for diff, business_diff, or range."
			},
			"end_date": {
				"type": "string",
				"description": "End date for diff, business_diff, or range."
			},
			"amount": {
				"type": "integer",
				"description": "Signed amount for add or business_add. Negative values move backward."
			},
			"unit": {
				"type": "string",
				"enum": ["second", "minute", "hour", "day", "week", "month", "quarter", "year"],
				"description": "Unit for add, diff, or range. Default day."
			},
			"weekday": {
				"type": "string",
				"description": "Weekday name or number for nth_weekday, for example Monday, Mon, 1, Sunday, or 7."
			},
			"nth": {
				"type": "integer",
				"minimum": -5,
				"maximum": 5,
				"description": "Nth weekday in a month. Positive counts from the start; -1 means last."
			},
			"year": {
				"type": "integer",
				"description": "Year for nth_weekday."
			},
			"month": {
				"type": "integer",
				"minimum": 1,
				"maximum": 12,
				"description": "Month for nth_weekday."
			},
			"period": {
				"type": "string",
				"enum": ["week", "month", "quarter", "year"],
				"description": "Period for period_bounds."
			},
			"first_weekday": {
				"type": "string",
				"description": "First day of week for week boundaries. Default Monday."
			},
			"weekend": {
				"type": "array",
				"maxItems": 7,
				"items": {"type": "string"},
				"description": "Weekend weekdays for business-day operations. Defaults to Saturday and Sunday."
			},
			"holidays": {
				"type": "array",
				"maxItems": 128,
				"items": {"type": "string"},
				"description": "YYYY-MM-DD holidays excluded from business days."
			},
			"include_start": {
				"type": "boolean",
				"description": "For business_diff, count the start date when it is a business day. Default true."
			},
			"include_end": {
				"type": "boolean",
				"description": "For business_diff, count the end date when it is a business day. Default true."
			},
			"business_only": {
				"type": "boolean",
				"description": "For range, include only business days."
			},
			"limit": {
				"type": "integer",
				"minimum": 1,
				"maximum": 400,
				"description": "Maximum number of range results. Default 120."
			}
		}
	}`)
}
