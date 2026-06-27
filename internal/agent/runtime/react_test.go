package runtime

import (
	"context"
	"strings"
	"testing"

	"nlserver/internal/agent/guardrails"
	"nlserver/internal/agent/model"
	"nlserver/internal/agent/tools"
)

func TestStreamBuffersToolBackedFinalAnswerForValidation(t *testing.T) {
	gateway := &streamingFinalGateway{}
	runtime := NewRuntime(gateway)

	var chunks []string
	var progress []Progress
	result, err := runtime.Stream(context.Background(), Request{
		Model: "mock-model",
		Messages: []model.ModelMessage{
			{Role: "user", Content: "帮我查一下最近两天的纳指100走势，然后对比一下上证指数走势"},
		},
		Tools: []tools.ToolDefinition{
			{Name: toolNameWebSearch},
			{Name: toolNameWebRead},
		},
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			switch call.Name {
			case toolNameLocalTime:
				return `{"ok":true,"timezone":"Asia/Shanghai","date":"2026-06-24","time":"19:37:31"}`
			case toolNameWebSearch:
				return `{"ok":true,"query":"纳斯达克100 2026-06-23 2026-06-24 上证指数","result_count":1,"results":[{"url":"https://example.com/market","title":"Market recap","content":"Nasdaq 100 and SSE Composite recap"}]}`
			case toolNameWebRead:
				return `{"ok":true,"result_count":1,"results":[{"url":"https://example.com/market","content":"Nasdaq 100 rose while the SSE Composite was mixed over the same period."}]}`
			default:
				return `{"ok":false,"error":"unexpected tool"}`
			}
		},
		OnProgress: func(event Progress) error {
			progress = append(progress, event)
			return nil
		},
	}, func(delta model.ModelDelta) error {
		chunks = append(chunks, delta.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	streamed := strings.Join(chunks, "")
	if !strings.Contains(streamed, "纳指100") || !strings.Contains(streamed, "上证指数") {
		t.Fatalf("streamed answer = %q, want final market comparison", streamed)
	}
	if len(chunks) != 1 || chunks[0] != streamed {
		t.Fatalf("chunks = %#v, want buffered tool-backed final answer emitted after validation", chunks)
	}
	if result.Completion.Content != streamed {
		t.Fatalf("completion = %q, streamed = %q", result.Completion.Content, streamed)
	}
	if gateway.streamCalls != 1 {
		t.Fatalf("streamCalls = %d, want one direct stream", gateway.streamCalls)
	}
	if !gateway.sawRoutingSchema {
		t.Fatal("route-only pass did not receive structured response schema")
	}
	if !gateway.sawNativeToolResult {
		t.Fatal("final stream request did not include native tool result message")
	}
	searchStart, searchEnd := pairedProgress(progress, "searching")
	if searchStart == nil || searchEnd == nil {
		t.Fatalf("progress = %#v, want paired search events", progress)
	}
	if searchStart.StepID == "" || searchStart.StepID != searchEnd.StepID ||
		searchStart.Sequence != searchEnd.Sequence ||
		searchStart.Status != "running" || searchEnd.Status != "completed" {
		t.Fatalf("search progress pair = %#v / %#v", searchStart, searchEnd)
	}
	if searchStart.Narration != "我会先核对两个市场在同一交易日的收盘表现。\n" {
		t.Fatalf("search narration = %q, want routing model commentary", searchStart.Narration)
	}
}

type streamingFinalGateway struct {
	completeCalls       int
	streamCalls         int
	sawRoutingSchema    bool
	sawNativeToolResult bool
}

func (g *streamingFinalGateway) Complete(ctx context.Context, req model.ModelRequest) (model.ModelResponse, error) {
	g.completeCalls++
	if len(req.Tools) == 0 {
		return model.ModelResponse{Content: "unused", FinishReason: "stop"}, nil
	}
	if req.ResponseSchema != nil {
		g.sawRoutingSchema = true
	}
	if !runtimeMessagesHaveNativeToolResult(req.Messages, "call_search") {
		return model.ModelResponse{
			Content: "我会先核对两个市场在同一交易日的收盘表现。",
			ToolCalls: []tools.ToolCall{{
				ID:   "call_search",
				Name: toolNameWebSearch,
				Arguments: map[string]any{
					"query":       "纳斯达克100 2026-06-23 2026-06-24 上证指数",
					"topic":       "finance",
					"target_date": "2026-06-24",
				},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	return model.ModelResponse{Content: "routing complete", FinishReason: "stop"}, nil
}

func (g *streamingFinalGateway) Stream(ctx context.Context, req model.ModelRequest, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	g.streamCalls++
	g.sawNativeToolResult = runtimeMessagesHaveNativeToolResult(req.Messages, "call_search")
	parts := []string{"纳指100近两天整体偏强，", "上证指数同期相对震荡偏弱；来源：https://example.com/market"}
	for _, part := range parts {
		if err := onDelta(model.ModelDelta{Content: part}); err != nil {
			return model.ModelResponse{}, err
		}
	}
	content := strings.Join(parts, "")
	return model.ModelResponse{Content: content, FinishReason: "stop"}, nil
}

func runtimeMessagesHaveNativeToolResult(messages []model.ModelMessage, callID string) bool {
	for _, message := range messages {
		if message.Role == "tool" && message.ToolCallID == callID && strings.TrimSpace(message.Content) != "" {
			return true
		}
	}
	return false
}

func runtimeMessagesContain(messages []model.ModelMessage, needle string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, needle) {
			return true
		}
	}
	return false
}

func progressContains(events []Progress, phase string, message string) bool {
	for _, event := range events {
		if event.Phase == phase && event.Message == message {
			return true
		}
	}
	return false
}

func pairedProgress(events []Progress, phase string) (*Progress, *Progress) {
	var start *Progress
	for index := range events {
		event := &events[index]
		if event.Phase != phase {
			continue
		}
		if event.Status == "running" && start == nil {
			start = event
			continue
		}
		if start != nil && event.StepID == start.StepID &&
			(event.Status == "completed" || event.Status == "failed") {
			return start, event
		}
	}
	return start, nil
}

func TestRuntimeInjectsUserTimezoneIntoClockAndSearch(t *testing.T) {
	gateway := &timezoneRoutingGateway{}
	runtime := NewRuntime(gateway)
	var calls []tools.ToolCall
	_, _, err := runtime.Route(context.Background(), Request{
		Model:        "mock-model",
		UserTimezone: "Asia/Shanghai",
		Messages: []model.ModelMessage{
			{Role: "user", Content: "昨晚特朗普在 X 上发了什么？"},
		},
		Tools: []tools.ToolDefinition{{Name: toolNameWebSearch}},
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			calls = append(calls, call)
			switch call.Name {
			case toolNameLocalTime:
				return `{"ok":true,"timezone":"Asia/Shanghai","date":"2026-06-27","time":"12:00:00"}`
			case toolNameWebSearch:
				return `{"ok":true,"query":"Trump X 2026-06-26","result_count":0,"results":[]}`
			default:
				return `{"ok":false,"error":"unexpected tool"}`
			}
		},
	}, false)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(calls) < 2 {
		t.Fatalf("calls = %#v, want request clock and search", calls)
	}
	clockArgs, err := toolArgsMap(calls[0].Arguments)
	if err != nil {
		t.Fatalf("clock args: %v", err)
	}
	if calls[0].Name != toolNameLocalTime || clockArgs["timezone"] != "Asia/Shanghai" {
		t.Fatalf("clock call = %#v args=%#v, want Asia/Shanghai local_time", calls[0], clockArgs)
	}
	searchArgs, err := toolArgsMap(calls[1].Arguments)
	if err != nil {
		t.Fatalf("search args: %v", err)
	}
	if calls[1].Name != toolNameWebSearch || searchArgs["timezone"] != "Asia/Shanghai" {
		t.Fatalf("search call = %#v args=%#v, want Asia/Shanghai web_search", calls[1], searchArgs)
	}
}

type timezoneRoutingGateway struct{}

func (g *timezoneRoutingGateway) Complete(ctx context.Context, req model.ModelRequest) (model.ModelResponse, error) {
	if runtimeMessagesHaveNativeToolResult(req.Messages, "call_search") {
		return model.ModelResponse{Content: "routing complete", FinishReason: "stop"}, nil
	}
	return model.ModelResponse{
		Content: "我会按北京时间昨晚的日期核对 X 上的公开来源。",
		ToolCalls: []tools.ToolCall{{
			ID:   "call_search",
			Name: toolNameWebSearch,
			Arguments: map[string]any{
				"query":       "Trump X 2026-06-26",
				"target_date": "2026-06-26",
			},
		}},
		FinishReason: "tool_calls",
	}, nil
}

func (g *timezoneRoutingGateway) Stream(ctx context.Context, req model.ModelRequest, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	return model.ModelResponse{}, nil
}

func TestUserFacingToolNarrationUsesQueryContext(t *testing.T) {
	if got := userFacingSearchStart("京能电力 2026 一季报", 0, "查一下京能电力"); got != "先核对 京能电力 2026 一季报 的公开来源。\n" {
		t.Fatalf("first search narration = %q", got)
	}
	if got := userFacingSearchStart("建投能源 2026 一季报", 1, "再看建投能源"); got != "再核对 建投能源 2026 一季报，补齐剩余信息。\n" {
		t.Fatalf("next search narration = %q", got)
	}
	if got := userFacingRoutingAction("我会先对照指数收盘数据，再检查盘后交易。"); got != "我会先对照指数收盘数据，再检查盘后交易。" {
		t.Fatalf("routing narration = %q", got)
	}
	if got := userFacingRoutingAction(`{"tool_required":false,"answer_mode":"no_tool"}`); got != "" {
		t.Fatalf("structured route decision should not become narration: %q", got)
	}
	if got := userFacingReadStart("京能电力 2026 一季报", "查一下财报"); got != "搜索结果里有几条需要直接读原文；我会打开与 京能电力 2026 一季报 最相关的来源核对。\n" {
		t.Fatalf("read narration = %q", got)
	}
}

func TestRuntimeBlocksUnadvertisedToolCalls(t *testing.T) {
	gateway := &blockedToolGateway{}
	runtime := NewRuntime(gateway)
	executorCalled := false
	var audits []ToolAuditEvent
	result, err := runtime.Complete(context.Background(), Request{
		Model:    "mock-model",
		Messages: []model.ModelMessage{{Role: "user", Content: "run the unsafe backend tool"}},
		Tools:    []tools.ToolDefinition{{Name: toolNameLocalTime}},
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			if call.Name == toolNameLocalTime {
				return `{"ok":true,"date":"2026-06-24"}`
			}
			executorCalled = true
			return `{"ok":true}`
		},
		OnToolAudit: func(event ToolAuditEvent) error {
			audits = append(audits, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if executorCalled {
		t.Fatal("executor was called for an unadvertised tool")
	}
	if !strings.Contains(result.Completion.Content, "blocked") {
		t.Fatalf("completion = %q, want blocked-tool answer", result.Completion.Content)
	}
	if !runtimeMessagesContain(result.Messages, "tool call blocked by runtime policy") {
		t.Fatalf("messages missing blocked tool evidence: %#v", result.Messages)
	}
	if len(audits) != 1 || audits[0].Status != "blocked" || audits[0].Tool != "unsafe_delete" {
		t.Fatalf("audits = %#v, want one blocked unsafe_delete audit event", audits)
	}
}

func TestRuntimeConstrainsWebSearchDomains(t *testing.T) {
	runtime := NewRuntime(nil)
	var executed tools.ToolCall
	used := 0
	result, counted, status, auditError := runtime.executeGuardedTool(context.Background(), Request{
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			executed = call
			return `{"ok":true,"result_count":1}`
		},
		MaxToolCalls: 2,
	}, tools.ToolCall{
		ID:        "call_search",
		Name:      toolNameWebSearch,
		Arguments: map[string]any{"query": "site limited search"},
	}, map[string]tools.ToolDefinition{
		toolNameWebSearch: {Name: toolNameWebSearch},
	}, guardrails.ToolPolicy{
		AllowedTools:   []string{toolNameWebSearch},
		AllowedDomains: []string{"https://example.com"},
	}, &used)
	if status != "completed" || auditError != "" || !counted || !toolResultOK(result) {
		t.Fatalf("guarded search = (%q, %t, %q, %q), want completed ok", status, counted, auditError, result)
	}
	args, ok := executed.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("executed arguments = %#v, want map", executed.Arguments)
	}
	domains, ok := args["include_domains"].([]string)
	if !ok || len(domains) != 1 || domains[0] != "example.com" {
		t.Fatalf("include_domains = %#v, want example.com", args["include_domains"])
	}
}

func TestRuntimeBlocksWebReadOutsideAllowedDomains(t *testing.T) {
	runtime := NewRuntime(nil)
	executorCalled := false
	used := 0
	result, counted, status, auditError := runtime.executeGuardedTool(context.Background(), Request{
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			executorCalled = true
			return `{"ok":true}`
		},
		MaxToolCalls: 2,
	}, tools.ToolCall{
		ID:        "call_read",
		Name:      toolNameWebRead,
		Arguments: map[string]any{"urls": []string{"https://not-example.com/page"}},
	}, map[string]tools.ToolDefinition{
		toolNameWebRead: {Name: toolNameWebRead},
	}, guardrails.ToolPolicy{
		AllowedTools:   []string{toolNameWebRead},
		AllowedDomains: []string{"example.com"},
	}, &used)
	if executorCalled {
		t.Fatal("executor was called for a disallowed web_read URL")
	}
	if status != "blocked" || counted || !strings.Contains(auditError, "allowed domain") || !strings.Contains(result, "allowed domain") {
		t.Fatalf("guarded read = (%q, %t, %q, %q), want blocked domain policy", status, counted, auditError, result)
	}
}

func TestRuntimeBlocksInvalidWebSearchArgumentsBeforeExecutor(t *testing.T) {
	runtime := NewRuntime(nil)
	for _, tc := range []struct {
		name      string
		arguments any
		wantError string
	}{
		{name: "malformed", arguments: `{"query":`, wantError: "valid JSON object"},
		{name: "missing query", arguments: map[string]any{"topic": "finance"}, wantError: "query is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executorCalled := false
			used := 0
			result, counted, status, auditError := runtime.executeGuardedTool(context.Background(), Request{
				ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
					executorCalled = true
					return `{"ok":true}`
				},
				MaxToolCalls: 2,
			}, tools.ToolCall{
				ID:        "call_search",
				Name:      toolNameWebSearch,
				Arguments: tc.arguments,
			}, map[string]tools.ToolDefinition{
				toolNameWebSearch: {Name: toolNameWebSearch},
			}, guardrails.ToolPolicy{
				AllowedTools: []string{toolNameWebSearch},
			}, &used)
			if executorCalled {
				t.Fatal("executor was called for invalid web_search arguments")
			}
			if status != "blocked" || counted || used != 0 || !strings.Contains(auditError, tc.wantError) || !strings.Contains(result, "tool call blocked") {
				t.Fatalf("guarded search = (%q, %t, used=%d, %q, %q), want blocked %q", status, counted, used, auditError, result, tc.wantError)
			}
		})
	}
}

func TestCompleteRetriesLeakedFinalToolMarkup(t *testing.T) {
	gateway := &leakingFinalGateway{}
	runtime := NewRuntime(gateway)

	result, err := runtime.Complete(context.Background(), Request{
		Model:    "mock-model",
		Messages: []model.ModelMessage{{Role: "user", Content: "查一下这些基金最近收益"}},
		Tools:    []tools.ToolDefinition{{Name: toolNameWebSearch}},
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			return `{"ok":false,"error":"web search is not configured"}`
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if containsInternalToolMarkup(result.Completion.Content) || strings.Contains(result.Completion.Content, "我需要更精确") {
		t.Fatalf("completion leaked tool markup/status text: %q", result.Completion.Content)
	}
	if result.Completion.Content != "实时检索没有返回可用结果，我无法可靠核实这些基金的收益。" {
		t.Fatalf("completion = %q, want retry answer", result.Completion.Content)
	}
	if gateway.finalCalls != 2 || !gateway.sawRetryPrompt {
		t.Fatalf("finalCalls=%d sawRetryPrompt=%t, want retry with stronger prompt", gateway.finalCalls, gateway.sawRetryPrompt)
	}
}

func TestStreamSuppressesLeakedFinalToolMarkup(t *testing.T) {
	gateway := &leakingStreamFinalGateway{}
	runtime := NewRuntime(gateway)
	var chunks []string

	result, err := runtime.Stream(context.Background(), Request{
		Model:    "mock-model",
		Messages: []model.ModelMessage{{Role: "user", Content: "查一下这些基金最近收益"}},
		Tools:    []tools.ToolDefinition{{Name: toolNameWebSearch}},
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			return `{"ok":false,"error":"web search is not configured"}`
		},
	}, func(delta model.ModelDelta) error {
		chunks = append(chunks, delta.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	streamed := strings.Join(chunks, "")
	if streamed != "实时检索没有返回可用结果，我无法可靠核实这些基金的收益。" {
		t.Fatalf("streamed = %q, want clean retry answer", streamed)
	}
	if result.Completion.Content != streamed {
		t.Fatalf("completion = %q, streamed = %q", result.Completion.Content, streamed)
	}
	if containsInternalToolMarkup(streamed) || strings.Contains(streamed, "DSML") || strings.Contains(streamed, "我需要更精确") {
		t.Fatalf("stream leaked invalid tool markup/status text: %q", streamed)
	}
	if gateway.finalStreams != 2 {
		t.Fatalf("finalStreams = %d, want retry stream", gateway.finalStreams)
	}
}

func TestRuntimeStopsSearchLoopAfterProviderUnavailable(t *testing.T) {
	gateway := &providerUnavailableGateway{}
	runtime := NewRuntime(gateway)
	executed := 0

	result, err := runtime.Complete(context.Background(), Request{
		Model:    "mock-model",
		Messages: []model.ModelMessage{{Role: "user", Content: "查最新消息"}},
		Tools:    []tools.ToolDefinition{{Name: toolNameWebSearch}},
		ToolExecutor: func(ctx context.Context, call tools.ToolCall) string {
			if call.Name != toolNameWebSearch {
				return `{"ok":true,"date":"2026-06-27"}`
			}
			executed++
			return `{"ok":false,"query":"latest news","queries":["latest news"],"providers":["tavily"],"result_count":0,"evidence_status":"provider_unavailable","provider_errors":[{"provider":"tavily","error":"context deadline exceeded"}],"results":[]}`
		},
		MaxToolCalls: 4,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if executed != 1 {
		t.Fatalf("executed web_search calls = %d, want one fail-fast provider outage", executed)
	}
	if result.Completion.Content != "实时搜索暂时不可用，无法核实最新消息。" {
		t.Fatalf("completion = %q, want provider-unavailable answer", result.Completion.Content)
	}
}

type blockedToolGateway struct{}

func (g *blockedToolGateway) Complete(ctx context.Context, req model.ModelRequest) (model.ModelResponse, error) {
	if len(req.Tools) > 0 {
		if runtimeMessagesContain(req.Messages, "tool call blocked by runtime policy") {
			return model.ModelResponse{Content: "routing complete", FinishReason: "stop"}, nil
		}
		return model.ModelResponse{
			ToolCalls: []tools.ToolCall{{
				ID:   "call_unsafe",
				Name: "unsafe_delete",
				Arguments: map[string]any{
					"target": "all",
				},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	if runtimeMessagesContain(req.Messages, "tool call blocked by runtime policy") {
		return model.ModelResponse{Content: "blocked tool was not executed", FinishReason: "stop"}, nil
	}
	return model.ModelResponse{Content: "unexpected", FinishReason: "stop"}, nil
}

func (g *blockedToolGateway) Stream(ctx context.Context, req model.ModelRequest, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	return model.ModelResponse{}, nil
}

type leakingFinalGateway struct {
	finalCalls     int
	sawRetryPrompt bool
}

func (g *leakingFinalGateway) Complete(ctx context.Context, req model.ModelRequest) (model.ModelResponse, error) {
	if len(req.Tools) > 0 {
		if runtimeMessagesHaveNativeToolResult(req.Messages, "call_search") {
			return model.ModelResponse{Content: "routing complete", FinishReason: "stop"}, nil
		}
		return model.ModelResponse{
			Content: "我先核对这些基金的公开收益数据。",
			ToolCalls: []tools.ToolCall{{
				ID:        "call_search",
				Name:      toolNameWebSearch,
				Arguments: map[string]any{"query": "基金 最近收益", "topic": "finance"},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	g.finalCalls++
	if runtimeMessagesContain(req.Messages, "previous draft was not a valid final answer") {
		g.sawRetryPrompt = true
	}
	if g.finalCalls == 1 {
		return model.ModelResponse{
			Content:      `我需要更精确地获取这些基金的数据。< | DSML | | tool_calls>< | DSML | | invoke name="web_search">{"query":"基金收益"}</ | DSML | | invoke>`,
			FinishReason: "stop",
		}, nil
	}
	return model.ModelResponse{Content: "实时检索没有返回可用结果，我无法可靠核实这些基金的收益。", FinishReason: "stop"}, nil
}

func (g *leakingFinalGateway) Stream(ctx context.Context, req model.ModelRequest, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	return model.ModelResponse{}, nil
}

type leakingStreamFinalGateway struct {
	finalStreams int
}

func (g *leakingStreamFinalGateway) Complete(ctx context.Context, req model.ModelRequest) (model.ModelResponse, error) {
	if len(req.Tools) > 0 {
		if runtimeMessagesHaveNativeToolResult(req.Messages, "call_search") {
			return model.ModelResponse{Content: "routing complete", FinishReason: "stop"}, nil
		}
		return model.ModelResponse{
			Content: "我先核对这些基金的公开收益数据。",
			ToolCalls: []tools.ToolCall{{
				ID:        "call_search",
				Name:      toolNameWebSearch,
				Arguments: map[string]any{"query": "基金 最近收益", "topic": "finance"},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	return model.ModelResponse{Content: "unexpected complete", FinishReason: "stop"}, nil
}

func (g *leakingStreamFinalGateway) Stream(ctx context.Context, req model.ModelRequest, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	g.finalStreams++
	if g.finalStreams == 1 {
		content := `我需要更精确地获取这些基金的数据。< | DSML | | tool_calls>< | DSML | | invoke name="web_search">{"query":"基金收益"}</ | DSML | | invoke>`
		if onDelta != nil {
			if err := onDelta(model.ModelDelta{Content: content}); err != nil {
				return model.ModelResponse{}, err
			}
		}
		return model.ModelResponse{Content: content, FinishReason: "stop"}, nil
	}
	content := "实时检索没有返回可用结果，我无法可靠核实这些基金的收益。"
	if onDelta != nil {
		if err := onDelta(model.ModelDelta{Content: content}); err != nil {
			return model.ModelResponse{}, err
		}
	}
	return model.ModelResponse{Content: content, FinishReason: "stop"}, nil
}

type providerUnavailableGateway struct{}

func (g *providerUnavailableGateway) Complete(ctx context.Context, req model.ModelRequest) (model.ModelResponse, error) {
	if len(req.Tools) > 0 {
		return model.ModelResponse{
			Content: "我会查询最新公开来源。",
			ToolCalls: []tools.ToolCall{
				{ID: "call_search_1", Name: toolNameWebSearch, Arguments: map[string]any{"query": "latest news"}},
				{ID: "call_search_2", Name: toolNameWebSearch, Arguments: map[string]any{"query": "latest updates"}},
			},
			FinishReason: "tool_calls",
		}, nil
	}
	if runtimeMessagesContain(req.Messages, "provider_unavailable") {
		return model.ModelResponse{Content: "实时搜索暂时不可用，无法核实最新消息。", FinishReason: "stop"}, nil
	}
	return model.ModelResponse{Content: "unexpected", FinishReason: "stop"}, nil
}

func (g *providerUnavailableGateway) Stream(ctx context.Context, req model.ModelRequest, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	return model.ModelResponse{}, nil
}
