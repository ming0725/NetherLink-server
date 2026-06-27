package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nlserver/internal/agent/mcp"
	"nlserver/internal/agent/mcp/mcp_utility"
	"nlserver/internal/agent/mcp/mcp_websearch"
	agentprompts "nlserver/internal/agent/prompts"
	"nlserver/internal/agent/tools"
	agenttoolbuiltins "nlserver/internal/agent/tools/builtins"
	"nlserver/internal/config"
)

const (
	toolNameWebSearch = agenttoolbuiltins.ModelToolWebSearch
	toolNameWebRead   = agenttoolbuiltins.ModelToolWebRead
	toolNameLocalTime = agenttoolbuiltins.ModelToolLocalTime
	toolNameCalculate = agenttoolbuiltins.ModelToolCalculate
	toolNameCalendar  = agenttoolbuiltins.ModelToolCalendar
)

var (
	tavilySearchEndpoint  = mcp_websearch.TavilySearchEndpoint
	tavilyExtractEndpoint = mcp_websearch.TavilyExtractEndpoint
	braveSearchEndpoint   = mcp_websearch.BraveSearchEndpoint
)

func completionToolDefinitions(cfg config.AIConfig) []tools.ToolDefinition {
	return agenttoolbuiltins.CompletionToolDefinitions(cfg)
}

func (s *Service) runCompletionTool(ctx context.Context, call tools.ToolCall) string {
	var payload any
	var err error
	switch call.Name {
	case toolNameLocalTime:
		payload, err = runLocalTimeTool(call.Arguments)
	case toolNameCalculate:
		payload, err = s.runUtilityMCPTool(ctx, mcp_utility.CalculateTool, call.Arguments)
	case toolNameCalendar:
		payload, err = s.runUtilityMCPTool(ctx, mcp_utility.CalendarTool, call.Arguments)
	case toolNameWebSearch:
		payload, err = s.runWebSearchMCPTool(ctx, mcp_websearch.SearchTool, call.Arguments)
	case toolNameWebRead:
		payload, err = s.runWebSearchMCPTool(ctx, mcp_websearch.ReadTool, call.Arguments)
	default:
		err = fmt.Errorf("unknown tool %q", call.Name)
	}
	if err != nil {
		s.logCompletionToolFailure(ctx, call, payload, err)
		payload = map[string]any{
			"ok":    false,
			"error": err.Error(),
		}
	} else if toolPayloadFailed(payload) {
		s.logCompletionToolFailure(ctx, call, payload, nil)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"ok":false,"error":"failed to encode tool result"}`
	}
	return string(data)
}

func (s *Service) runWebSearchMCPTool(ctx context.Context, toolName string, args any) (map[string]any, error) {
	server := mcp_websearch.Manifest()
	server.Status = "active"
	var manifestTool mcp.ToolManifest
	for _, tool := range server.Tools {
		if tool.Name == toolName {
			manifestTool = tool
			break
		}
	}
	if manifestTool.Name == "" {
		return nil, mcp.ErrToolNotAllowed
	}
	manifestTool.Status = "active"
	client := mcp_websearch.LocalClient{
		Search: func(ctx context.Context, args any) (map[string]any, error) {
			syncWebSearchProviderEndpoints()
			return mcp_websearch.RunSearch(ctx, s.cfg, args)
		},
		Read: func(ctx context.Context, args any) (map[string]any, error) {
			syncWebSearchProviderEndpoints()
			return mcp_websearch.RunRead(ctx, s.cfg, args)
		},
	}
	adapter := mcp.NewAdapter(server, manifestTool, client)
	response, err := adapter.Call(ctx, args)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(response.Content)) == "" {
		return nil, fmt.Errorf("mcp: decode %s response: empty JSON response", toolName)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Content, &payload); err != nil {
		return nil, fmt.Errorf("mcp: decode %s response: %w", toolName, err)
	}
	return payload, nil
}

func (s *Service) runUtilityMCPTool(ctx context.Context, toolName string, args any) (map[string]any, error) {
	server := mcp_utility.Manifest()
	server.Status = "active"
	var manifestTool mcp.ToolManifest
	for _, tool := range server.Tools {
		if tool.Name == toolName {
			manifestTool = tool
			break
		}
	}
	if manifestTool.Name == "" {
		return nil, mcp.ErrToolNotAllowed
	}
	manifestTool.Status = "active"
	client := mcp_utility.LocalClient{
		Calculate: mcp_utility.RunCalculate,
		Calendar:  mcp_utility.RunCalendar,
	}
	adapter := mcp.NewAdapter(server, manifestTool, client)
	response, err := adapter.Call(ctx, args)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(response.Content)) == "" {
		return nil, fmt.Errorf("mcp: decode %s response: empty JSON response", toolName)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Content, &payload); err != nil {
		return nil, fmt.Errorf("mcp: decode %s response: %w", toolName, err)
	}
	return payload, nil
}

func runLocalTimeTool(args any) (map[string]any, error) {
	parsed := map[string]any{}
	if err := mcp_websearch.MapToolArgs(args, &parsed); err != nil {
		return nil, err
	}
	location := time.Local
	if timezone, _ := parsed["timezone"].(string); strings.TrimSpace(timezone) != "" {
		loaded, err := time.LoadLocation(strings.TrimSpace(timezone))
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q", timezone)
		}
		location = loaded
	}
	now := time.Now().In(location)
	zoneName, zoneOffset := now.Zone()
	return map[string]any{
		"ok":               true,
		"timezone":         location.String(),
		"zone_name":        zoneName,
		"utc_offset":       agentprompts.FormatUTCOffset(zoneOffset),
		"rfc3339":          now.Format(time.RFC3339),
		"date":             now.Format("2006-01-02"),
		"time":             now.Format("15:04:05"),
		"weekday":          now.Weekday().String(),
		"unix":             now.Unix(),
		"unix_millisecond": now.UnixMilli(),
	}, nil
}

func runWebReadTool(ctx context.Context, cfg config.AIConfig, args any) (map[string]any, error) {
	syncWebSearchProviderEndpoints()
	return mcp_websearch.RunRead(ctx, cfg, args)
}

func runWebSearchTool(ctx context.Context, cfg config.AIConfig, args any) (map[string]any, error) {
	syncWebSearchProviderEndpoints()
	return mcp_websearch.RunSearch(ctx, cfg, args)
}

func runCalculateTool(ctx context.Context, args any) (map[string]any, error) {
	return mcp_utility.RunCalculate(ctx, args)
}

func runCalendarTool(ctx context.Context, args any) (map[string]any, error) {
	return mcp_utility.RunCalendar(ctx, args)
}

func syncWebSearchProviderEndpoints() {
	mcp_websearch.TavilySearchEndpoint = tavilySearchEndpoint
	mcp_websearch.TavilyExtractEndpoint = tavilyExtractEndpoint
	mcp_websearch.BraveSearchEndpoint = braveSearchEndpoint
}

func normalizeTavilyTopic(topic string) string {
	return mcp_websearch.NormalizeTopic(topic)
}

func normalizeISODate(value string) (string, error) {
	return mcp_websearch.NormalizeISODate(value)
}

func normalizePublicWebURLs(values []string) []string {
	return mcp_websearch.NormalizePublicWebURLs(values)
}

func discoverLinkedDataURLs(pageURL string, document string) []string {
	return mcp_websearch.DiscoverLinkedDataURLs(pageURL, document)
}

func toolPayloadFailed(payload any) bool {
	values, ok := payload.(map[string]any)
	if !ok {
		return false
	}
	if okValue, exists := values["ok"].(bool); exists && !okValue {
		return true
	}
	if _, exists := values["error"]; !exists {
		return false
	}
	return strings.TrimSpace(toolPayloadError(payload)) != ""
}

func toolPayloadError(payload any) string {
	values, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if _, exists := values["error"]; !exists {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(values["error"]))
}

func (s *Service) logCompletionToolFailure(ctx context.Context, call tools.ToolCall, payload any, err error) {
	if s == nil || s.logger == nil {
		return
	}
	detail := toolFailureDetail(payload, err)
	if detail == "" {
		detail = "tool returned ok=false without an error field"
	}
	kind := classifyToolErrorKind(detail)
	args := []any{
		"tool", strings.TrimSpace(call.Name),
		"call_id", strings.TrimSpace(call.ID),
		"error", detail,
		"error_detail", detail,
		"error_kind", kind,
		"error_stage", classifyToolErrorStage(detail),
		"provider", inferToolErrorProvider(call.Name, detail),
		"query", firstNonEmpty(toolPayloadString(payload, "query"), toolCallQuery(call.Arguments)),
		"queries", truncateLogField(toolPayloadJSON(payload, "queries"), 1200),
		"providers", truncateLogField(toolPayloadJSON(payload, "providers"), 800),
		"result_count", toolPayloadInt(payload, "result_count"),
		"evidence_status", toolPayloadString(payload, "evidence_status"),
		"target_date", toolPayloadString(payload, "target_date"),
		"provider_attempts", truncateLogField(toolPayloadJSON(payload, "provider_attempts"), 1600),
		"reader_attempts", truncateLogField(toolPayloadJSON(payload, "reader_attempts"), 1600),
		"provider_errors", truncateLogField(toolPayloadJSON(payload, "provider_errors"), 1200),
		"failed_results", truncateLogField(toolPayloadJSON(payload, "failed_results"), 1200),
	}
	if toolPayloadString(payload, "evidence_status") == "provider_unavailable" {
		s.logger.WarnContext(ctx, "AI search provider unavailable", args...)
		return
	}
	if kind == "no_results" {
		s.logger.WarnContext(ctx, "AI tool call returned no usable results", args...)
		return
	}
	s.logger.ErrorContext(ctx, "AI tool call failed", args...)
}

func toolCallQuery(args any) string {
	var input struct {
		Query string `json:"query"`
	}
	if err := mcp_websearch.MapToolArgs(args, &input); err != nil {
		return ""
	}
	return strings.TrimSpace(input.Query)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func toolFailureDetail(payload any, err error) string {
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		return strings.TrimSpace(err.Error())
	}
	if text := toolPayloadError(payload); text != "" {
		return text
	}
	values, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if providerErrors := strings.TrimSpace(toolPayloadJSON(values, "provider_errors")); providerErrors != "" && providerErrors != "[]" {
		if toolPayloadString(values, "evidence_status") == "provider_unavailable" {
			return fmt.Sprintf("tool returned ok=false; evidence_status=provider_unavailable result_count=%d", toolPayloadInt(values, "result_count"))
		}
		return "provider_errors=" + providerErrors
	}
	evidenceStatus := toolPayloadString(values, "evidence_status")
	resultCount := toolPayloadInt(values, "result_count")
	switch {
	case evidenceStatus != "":
		return fmt.Sprintf("tool returned ok=false; evidence_status=%s result_count=%d", evidenceStatus, resultCount)
	case resultCount == 0:
		return "tool returned ok=false; result_count=0"
	default:
		return "tool returned ok=false"
	}
}

func toolPayloadString(payload any, key string) string {
	values, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	value, exists := values[key]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func toolPayloadInt(payload any, key string) int {
	values, ok := payload.(map[string]any)
	if !ok {
		return 0
	}
	value, exists := values[key]
	if !exists {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func toolPayloadJSON(payload any, key string) string {
	values, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	value, exists := values[key]
	if !exists || value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return string(data)
}

func truncateLogField(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "..."
}

func classifyToolErrorKind(detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "context deadline exceeded"),
		strings.Contains(lower, "client.timeout exceeded"),
		strings.Contains(lower, "i/o timeout"),
		strings.Contains(lower, "timeout"):
		return "timeout"
	case strings.Contains(lower, "lookup "):
		return "dns"
	case strings.Contains(lower, "dial tcp"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "connection reset"):
		return "connectivity"
	case strings.Contains(lower, "web search is not configured"),
		strings.Contains(lower, "browserless is not configured"):
		return "not_configured"
	case strings.Contains(lower, "provider_unavailable"):
		return "provider_unavailable"
	case strings.Contains(lower, "returned 401"),
		strings.Contains(lower, "returned 403"),
		strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "forbidden"):
		return "auth"
	case strings.Contains(lower, "returned 429"),
		strings.Contains(lower, "rate limit"):
		return "rate_limit"
	case strings.Contains(lower, "decode "),
		strings.Contains(lower, "unexpected end of json input"),
		strings.Contains(lower, "invalid tool arguments json"),
		strings.Contains(lower, "empty json response"),
		strings.Contains(lower, "cannot unmarshal"),
		strings.Contains(lower, "invalid character"):
		return "decode"
	case strings.Contains(lower, "query is required"),
		strings.Contains(lower, "expression is required"),
		strings.Contains(lower, "at least one valid public http or https url is required"),
		strings.Contains(lower, "must not"),
		strings.Contains(lower, "must be"),
		strings.Contains(lower, "invalid start_date"),
		strings.Contains(lower, "invalid end_date"),
		strings.Contains(lower, "invalid target_date"),
		strings.Contains(lower, "outside the allowed domain policy"):
		return "invalid_arguments"
	case strings.Contains(lower, "insufficient_no_results"),
		strings.Contains(lower, "result_count=0"),
		strings.Contains(lower, `"result_count":0`):
		return "no_results"
	default:
		return "provider_error"
	}
}

func classifyToolErrorStage(detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "tool arguments"),
		strings.Contains(lower, "query is required"),
		strings.Contains(lower, "expression is required"),
		strings.Contains(lower, "url is required"),
		strings.Contains(lower, "invalid start_date"),
		strings.Contains(lower, "invalid end_date"),
		strings.Contains(lower, "invalid target_date"),
		strings.Contains(lower, "outside the allowed domain policy"):
		return "tool_arguments"
	case strings.Contains(lower, "decode ") && strings.Contains(lower, "response"),
		strings.Contains(lower, "empty json response"):
		return "tool_response"
	case strings.Contains(lower, "insufficient_no_results"),
		strings.Contains(lower, "result_count=0"),
		strings.Contains(lower, `"result_count":0`):
		return "retrieval"
	case strings.Contains(lower, "web search is not configured"),
		strings.Contains(lower, "browserless is not configured"):
		return "configuration"
	case classifyToolErrorKind(detail) == "timeout",
		classifyToolErrorKind(detail) == "dns",
		classifyToolErrorKind(detail) == "connectivity",
		classifyToolErrorKind(detail) == "auth",
		classifyToolErrorKind(detail) == "rate_limit":
		return "provider"
	default:
		return "provider"
	}
}

func inferToolErrorProvider(toolName string, detail string) string {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "tavily"):
		return "tavily"
	case strings.Contains(lower, "brave"):
		return "brave"
	case strings.Contains(lower, "browserless"):
		return "browserless"
	case strings.Contains(lower, "web search failed across providers"):
		return "web_search"
	case toolName == toolNameWebRead:
		return "web_read"
	case toolName == toolNameWebSearch:
		return "web_search"
	default:
		return ""
	}
}
