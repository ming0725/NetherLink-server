package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	toolNameWebSearch       = agenttoolbuiltins.ModelToolWebSearch
	toolNameWebRead         = agenttoolbuiltins.ModelToolWebRead
	toolNameLocalTime       = agenttoolbuiltins.ModelToolLocalTime
	toolNameCalculate       = agenttoolbuiltins.ModelToolCalculate
	toolNameCalendar        = agenttoolbuiltins.ModelToolCalendar
	toolNameSubAgent        = agenttoolbuiltins.ModelToolSubAgentResearch
	toolNameGroupChatSearch = agenttoolbuiltins.ModelToolGroupChatSearch
)

var (
	tavilySearchEndpoint   = mcp_websearch.TavilySearchEndpoint
	tavilyExtractEndpoint  = mcp_websearch.TavilyExtractEndpoint
	braveSearchEndpoint    = mcp_websearch.BraveSearchEndpoint
	baiduSearchMCPEndpoint = mcp_websearch.BaiduSearchMCPEndpoint
)

func completionToolDefinitions(cfg config.AIConfig) []tools.ToolDefinition {
	return agenttoolbuiltins.CompletionToolDefinitions(cfg)
}

func groupBotCompletionToolDefinitions(cfg config.AIConfig) []tools.ToolDefinition {
	return agenttoolbuiltins.CompletionToolDefinitionsWithExtraTools(cfg, []tools.ToolDefinition{
		agenttoolbuiltins.GroupChatSearchDefinition(),
	})
}

func (s *Service) runCompletionTool(ctx context.Context, call tools.ToolCall) string {
	return s.runCompletionToolWithExecutor(ctx, call, s.runBaseCompletionTool)
}

func (s *Service) runCompletionToolWithExecutor(ctx context.Context, call tools.ToolCall, executor func(context.Context, tools.ToolCall) string) string {
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
	case toolNameSubAgent:
		payload, err = s.runSubAgentResearchTool(ctx, call.Arguments, executor)
	case toolNameGroupChatSearch:
		err = fmt.Errorf("group_chat_search requires group bot runtime context")
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

func (s *Service) runBaseCompletionTool(ctx context.Context, call tools.ToolCall) string {
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
	case toolNameGroupChatSearch:
		err = fmt.Errorf("group_chat_search requires group bot runtime context")
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

type subAgentResearchInput struct {
	Objective string              `json:"objective"`
	Tasks     []subAgentTaskInput `json:"tasks"`
}

type subAgentTaskInput struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Input          string         `json:"input"`
	ExpectedOutput string         `json:"expected_output"`
	AvailableTools []string       `json:"available_tools"`
	NeedTool       *bool          `json:"need_tool"`
	ToolNecessity  string         `json:"tool_necessity"`
	Tool           string         `json:"tool"`
	Arguments      map[string]any `json:"arguments"`
	Validation     string         `json:"validation"`
}

type subAgentTaskResult struct {
	ID               string         `json:"id"`
	Title            string         `json:"title,omitempty"`
	Input            string         `json:"input"`
	ExpectedOutput   string         `json:"expected_output"`
	AvailableTools   []string       `json:"available_tools"`
	NeedTool         bool           `json:"need_tool"`
	ToolNecessity    string         `json:"tool_necessity"`
	Tool             string         `json:"tool"`
	Validation       string         `json:"validation"`
	ValidationStatus string         `json:"validation_status"`
	Executed         bool           `json:"executed"`
	OK               bool           `json:"ok"`
	Result           map[string]any `json:"result,omitempty"`
	Error            string         `json:"error,omitempty"`
	ElapsedMS        int            `json:"elapsed_ms"`
}

func (s *Service) runSubAgentResearchTool(ctx context.Context, args any, executor func(context.Context, tools.ToolCall) string) (map[string]any, error) {
	var input subAgentResearchInput
	if err := mcp_websearch.MapToolArgs(args, &input); err != nil {
		return nil, err
	}
	input.Objective = strings.TrimSpace(input.Objective)
	tasks, err := normalizeSubAgentTasks(input.Tasks)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("subagent_research tasks are required")
	}
	if executor == nil {
		executor = s.runBaseCompletionTool
	}
	results := make([]subAgentTaskResult, len(tasks))
	var wg sync.WaitGroup
	for index, task := range tasks {
		index := index
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index] = s.runSubAgentTask(ctx, task, executor)
		}()
	}
	wg.Wait()

	okCount := 0
	executedCount := 0
	for _, result := range results {
		if result.Executed {
			executedCount++
		}
		if result.OK && result.Executed {
			okCount++
		}
	}
	return map[string]any{
		"ok":             okCount > 0,
		"tool":           toolNameSubAgent,
		"objective":      input.Objective,
		"result_count":   okCount,
		"task_count":     len(results),
		"executed_count": executedCount,
		"max_steps":      6,
		"results":        results,
	}, nil
}

func normalizeSubAgentTasks(tasks []subAgentTaskInput) ([]subAgentTaskInput, error) {
	normalized := make([]subAgentTaskInput, 0, len(tasks))
	for index, task := range tasks {
		task.ID = strings.TrimSpace(task.ID)
		if task.ID == "" {
			task.ID = fmt.Sprintf("task-%d", index+1)
		}
		task.Title = strings.TrimSpace(task.Title)
		task.Input = strings.TrimSpace(task.Input)
		task.ExpectedOutput = strings.TrimSpace(task.ExpectedOutput)
		task.AvailableTools = uniqueStrings(task.AvailableTools)
		task.ToolNecessity = strings.TrimSpace(task.ToolNecessity)
		task.Tool = strings.TrimSpace(task.Tool)
		task.Validation = strings.TrimSpace(task.Validation)
		if task.Arguments == nil {
			task.Arguments = map[string]any{}
		}
		if task.Input == "" {
			return nil, fmt.Errorf("subagent_research task %d input is required", index+1)
		}
		if task.ExpectedOutput == "" {
			return nil, fmt.Errorf("subagent_research task %d expected_output is required", index+1)
		}
		if len(task.AvailableTools) == 0 {
			return nil, fmt.Errorf("subagent_research task %d available_tools is required", index+1)
		}
		if task.NeedTool == nil {
			return nil, fmt.Errorf("subagent_research task %d need_tool is required", index+1)
		}
		if task.ToolNecessity == "" {
			return nil, fmt.Errorf("subagent_research task %d tool_necessity is required", index+1)
		}
		if task.Tool == "" {
			return nil, fmt.Errorf("subagent_research task %d tool is required", index+1)
		}
		if task.Tool == toolNameSubAgent {
			return nil, fmt.Errorf("subagent_research tasks cannot call subagent_research recursively")
		}
		if !stringListContains(task.AvailableTools, task.Tool) {
			return nil, fmt.Errorf("subagent_research task %d available_tools must include selected tool %q", index+1, task.Tool)
		}
		if task.Validation == "" {
			return nil, fmt.Errorf("subagent_research task %d validation is required", index+1)
		}
		normalized = append(normalized, task)
		if len(normalized) >= 6 {
			break
		}
	}
	return normalized, nil
}

func (s *Service) runSubAgentTask(ctx context.Context, task subAgentTaskInput, executor func(context.Context, tools.ToolCall) string) subAgentTaskResult {
	started := time.Now()
	needTool := true
	if task.NeedTool != nil {
		needTool = *task.NeedTool
	}
	result := subAgentTaskResult{
		ID:             task.ID,
		Title:          task.Title,
		Input:          task.Input,
		ExpectedOutput: task.ExpectedOutput,
		AvailableTools: task.AvailableTools,
		NeedTool:       needTool,
		ToolNecessity:  task.ToolNecessity,
		Tool:           task.Tool,
		Validation:     task.Validation,
	}
	if !needTool {
		result.OK = true
		result.Executed = false
		result.ValidationStatus = "skipped_tool_not_necessary"
		result.ElapsedMS = int(time.Since(started).Milliseconds())
		return result
	}
	if executor == nil {
		executor = s.runBaseCompletionTool
	}
	payload := executor(ctx, tools.ToolCall{
		ID:        "subagent:" + task.ID,
		Name:      task.Tool,
		Arguments: task.Arguments,
	})
	result.Executed = true
	result.ElapsedMS = int(time.Since(started).Milliseconds())
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		result.Error = "failed to decode subagent tool result: " + err.Error()
		result.ValidationStatus = "failed_invalid_tool_result"
		return result
	}
	result.OK = toolPayloadOK(decoded)
	if !result.OK {
		result.Error = toolPayloadError(decoded)
		if result.Error == "" {
			result.Error = "subagent tool returned ok=false"
		}
		result.ValidationStatus = "failed_tool_result_not_ok"
	} else if toolPayloadInt(decoded, "result_count") == 0 && (task.Tool == toolNameWebSearch || task.Tool == toolNameWebRead || task.Tool == toolNameGroupChatSearch) {
		result.OK = false
		result.Error = "subagent tool returned no matching results"
		result.ValidationStatus = "failed_no_matching_results"
	} else {
		result.ValidationStatus = "passed_basic_result_available"
	}
	result.Result = decoded
	return result
}

type groupChatSearchInput struct {
	Mode             string   `json:"mode"`
	Query            string   `json:"query"`
	Keywords         []string `json:"keywords"`
	MessageID        string   `json:"message_id"`
	MessageSeq       int64    `json:"message_seq"`
	Since            string   `json:"since"`
	Until            string   `json:"until"`
	BeforeMessageSeq int64    `json:"before_message_seq"`
	ContextBefore    int      `json:"context_before"`
	ContextAfter     int      `json:"context_after"`
	Limit            int      `json:"limit"`
}

func stringListContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
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
	mcp_websearch.BaiduSearchMCPEndpoint = baiduSearchMCPEndpoint
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

func toolPayloadOK(payload any) bool {
	values, ok := payload.(map[string]any)
	if !ok {
		return false
	}
	okValue, exists := values["ok"].(bool)
	if !exists {
		return true
	}
	return okValue
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
