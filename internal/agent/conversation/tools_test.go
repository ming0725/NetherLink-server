package conversation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	agentruntime "nlserver/internal/agent/runtime"
	"nlserver/internal/agent/tools"
	"nlserver/internal/config"
	"nlserver/internal/repository"
)

func TestRunWebSearchToolNormalizesTavilyParameters(t *testing.T) {
	var captured map[string]any
	var capturedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-tavily-key" {
			t.Fatalf("Authorization = %q, want Bearer test-tavily-key", got)
		}
		localCaptured := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&localCaptured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedMu.Lock()
		captured = localCaptured
		capturedMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "stock price today",
			"answer": "mock answer",
			"results": [
				{"title": "Source", "url": "https://example.com", "content": "snippet", "score": 0.9}
			],
			"response_time": "0.1"
		}`))
	}))
	defer server.Close()

	previousEndpoint := tavilySearchEndpoint
	tavilySearchEndpoint = server.URL
	t.Cleanup(func() {
		tavilySearchEndpoint = previousEndpoint
	})

	output, err := runWebSearchTool(context.Background(), config.AIConfig{
		TavilyAPIKey: "test-tavily-key",
	}, map[string]any{
		"query":           "stock price today",
		"topic":           "finance",
		"time_range":      "d",
		"start_date":      "2026-06-24",
		"end_date":        "2026-06-24",
		"target_date":     "2026-06-24",
		"include_domains": []string{"https://www.sse.com.cn/markets/", "sse.com.cn"},
		"exact_match":     true,
		"max_results":     99,
	})
	if err != nil {
		t.Fatalf("runWebSearchTool() error = %v", err)
	}

	capturedMu.Lock()
	defer capturedMu.Unlock()
	if captured["topic"] != "finance" {
		t.Fatalf("topic = %v, want finance", captured["topic"])
	}
	if captured["time_range"] != "day" {
		t.Fatalf("time_range = %v, want day", captured["time_range"])
	}
	if captured["max_results"] != float64(8) {
		t.Fatalf("max_results = %v, want 8", captured["max_results"])
	}
	if captured["search_depth"] != "basic" {
		t.Fatalf("search_depth = %v, want basic fast search", captured["search_depth"])
	}
	if captured["include_answer"] != false {
		t.Fatalf("include_answer = %v, want false", captured["include_answer"])
	}
	if _, exists := captured["include_raw_content"]; exists {
		t.Fatalf("include_raw_content = %v, search should not request raw content", captured["include_raw_content"])
	}
	if _, exists := captured["chunks_per_source"]; exists {
		t.Fatalf("chunks_per_source = %v, search should not request raw content chunks", captured["chunks_per_source"])
	}
	if captured["start_date"] != "2026-06-24" || captured["end_date"] != "2026-06-24" {
		t.Fatalf("date bounds = (%v, %v), want 2026-06-24", captured["start_date"], captured["end_date"])
	}
	domains, ok := captured["include_domains"].([]any)
	if !ok || len(domains) != 1 || domains[0] != "sse.com.cn" {
		t.Fatalf("include_domains = %#v, want deduplicated sse.com.cn", captured["include_domains"])
	}
	if output["evidence_status"] != "insufficient_target_date_not_found" {
		t.Fatalf("evidence_status = %v, want target date warning", output["evidence_status"])
	}
	if output["ok"] != true {
		t.Fatalf("ok = %v, want true", output["ok"])
	}
}

func TestRunWebSearchToolReportsTargetDateEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "2026-06-24 上证指数 收盘",
			"results": [
				{"title": "2026年6月24日市场收盘", "url": "https://example.com/one", "content": "2026年6月24日上证指数收盘数据", "score": 0.9},
				{"title": "Daily close", "url": "https://example.org/two", "content": "SSE Composite close on 2026-06-24", "score": 0.8}
			],
			"response_time": "0.1"
		}`))
	}))
	defer server.Close()

	previousEndpoint := tavilySearchEndpoint
	tavilySearchEndpoint = server.URL
	t.Cleanup(func() {
		tavilySearchEndpoint = previousEndpoint
	})

	output, err := runWebSearchTool(context.Background(), config.AIConfig{
		TavilyAPIKey: "test-tavily-key",
	}, map[string]any{
		"query":       "2026-06-24 上证指数 收盘",
		"topic":       "finance",
		"target_date": "2026-06-24",
	})
	if err != nil {
		t.Fatalf("runWebSearchTool() error = %v", err)
	}
	if output["target_date_matches"] != 2 {
		t.Fatalf("target_date_matches = %v, want 2", output["target_date_matches"])
	}
	if output["target_date_source_count"] != 2 {
		t.Fatalf("target_date_source_count = %v, want 2", output["target_date_source_count"])
	}
	if output["evidence_status"] != "multiple_date_matches" {
		t.Fatalf("evidence_status = %v, want multiple_date_matches", output["evidence_status"])
	}
	if _, exists := output["answer"]; exists {
		t.Fatal("output must not expose provider-generated answer as evidence")
	}
}

func TestRunWebSearchToolRejectsMissingTavilyKey(t *testing.T) {
	_, err := runWebSearchTool(context.Background(), config.AIConfig{}, map[string]any{
		"query": "latest news",
	})
	if err == nil {
		t.Fatal("runWebSearchTool() error = nil, want missing key error")
	}
}

func TestRunCompletionToolLogsInternalToolFailureReason(t *testing.T) {
	var logs bytes.Buffer
	service := NewService(config.AIConfig{}, repository.Repositories{}, nil, nil)
	service.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	result := service.runCompletionTool(context.Background(), tools.ToolCall{
		ID:   "call_search",
		Name: toolNameWebSearch,
		Arguments: map[string]any{
			"query": "latest news",
		},
	})
	if !strings.Contains(result, `"ok":false`) {
		t.Fatalf("result = %s, want failed tool payload", result)
	}
	logText := logs.String()
	for _, want := range []string{
		"AI tool call failed",
		"tool=web_search",
		"call_id=call_search",
		"error_kind=not_configured",
		"provider=web_search",
		"web search is not configured",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log = %q, want contains %q", logText, want)
		}
	}
}

func TestLogCompletionToolFailureExplainsFailedPayloadWithoutError(t *testing.T) {
	var logs bytes.Buffer
	service := NewService(config.AIConfig{}, repository.Repositories{}, nil, nil)
	service.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	service.logCompletionToolFailure(context.Background(), tools.ToolCall{
		ID:   "call_empty",
		Name: toolNameWebSearch,
		Arguments: map[string]any{
			"query": "fallback query",
		},
	}, map[string]any{
		"ok":              false,
		"query":           "Trump last night on X",
		"queries":         []string{"Trump last night on X", "Trump last night site:x.com"},
		"providers":       []string{"brave"},
		"result_count":    0,
		"evidence_status": "insufficient_no_results",
		"provider_attempts": []map[string]any{
			{"providers": []string{"brave"}, "query": "Trump last night on X", "result_count": 0, "duration_ms": 118},
		},
		"provider_errors": []map[string]any{
			{"provider": "brave", "error": "Client.Timeout exceeded while awaiting headers"},
		},
	}, nil)

	logText := logs.String()
	for _, want := range []string{
		"AI tool call failed",
		"tool=web_search",
		"call_id=call_empty",
		"error_kind=timeout",
		"provider=brave",
		"query=\"Trump last night on X\"",
		"queries=\"[\\\"Trump last night on X\\\",\\\"Trump last night site:x.com\\\"]\"",
		"provider_attempts=",
		"Client.Timeout exceeded",
		"result_count=0",
		"evidence_status=insufficient_no_results",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log = %q, want contains %q", logText, want)
		}
	}
	if strings.Contains(logText, `error=""`) || strings.Contains(logText, `error_detail=""`) {
		t.Fatalf("log = %q, should not contain empty error fields", logText)
	}
}

func TestLogCompletionToolNoResultsUsesWarning(t *testing.T) {
	var logs bytes.Buffer
	service := NewService(config.AIConfig{}, repository.Repositories{}, nil, nil)
	service.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	service.logCompletionToolFailure(context.Background(), tools.ToolCall{
		ID:   "call_no_results",
		Name: toolNameWebSearch,
		Arguments: map[string]any{
			"query": "华夏全球科技先锋 基金 历史业绩 收益率",
		},
	}, map[string]any{
		"ok":              false,
		"query":           "华夏全球科技先锋 基金 历史业绩 收益率",
		"queries":         []string{"华夏全球科技先锋 基金 历史业绩 收益率"},
		"providers":       []string{"tavily"},
		"result_count":    0,
		"evidence_status": "insufficient_no_results",
		"provider_attempts": []map[string]any{
			{"providers": []string{"tavily"}, "query": "华夏全球科技先锋 基金 历史业绩 收益率", "result_count": 0},
		},
		"provider_errors": []map[string]any{},
	}, nil)

	logText := logs.String()
	for _, want := range []string{
		"level=WARN",
		"AI tool call returned no usable results",
		"tool=web_search",
		"call_id=call_no_results",
		"error_kind=no_results",
		"error_stage=retrieval",
		"evidence_status=insufficient_no_results",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log = %q, want contains %q", logText, want)
		}
	}
	if strings.Contains(logText, "level=ERROR") {
		t.Fatalf("log = %q, no-results should not be logged as ERROR", logText)
	}
}

func TestLogCompletionToolProviderUnavailableUsesWarning(t *testing.T) {
	var logs bytes.Buffer
	service := NewService(config.AIConfig{}, repository.Repositories{}, nil, nil)
	service.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	service.logCompletionToolFailure(context.Background(), tools.ToolCall{
		ID:   "call_provider_down",
		Name: toolNameWebSearch,
		Arguments: map[string]any{
			"query": "NASDAQ 100 close price June 26 2026",
		},
	}, map[string]any{
		"ok":              false,
		"query":           "NASDAQ 100 close price June 26 2026",
		"queries":         []string{"NASDAQ 100 close price June 26 2026"},
		"providers":       []string{"tavily"},
		"result_count":    0,
		"evidence_status": "provider_unavailable",
		"provider_errors": []map[string]any{
			{"provider": "tavily", "query": "NASDAQ 100 close price June 26 2026", "error": "context deadline exceeded", "duration_ms": 8001},
		},
	}, nil)

	logText := logs.String()
	for _, want := range []string{
		"level=WARN",
		"AI search provider unavailable",
		"tool=web_search",
		"call_id=call_provider_down",
		"error_kind=provider_unavailable",
		"evidence_status=provider_unavailable",
		"error=\"tool returned ok=false; evidence_status=provider_unavailable result_count=0\"",
		"provider_errors=",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log = %q, want contains %q", logText, want)
		}
	}
	if strings.Contains(logText, "level=ERROR") || strings.Contains(logText, "error=\"provider_errors=") {
		t.Fatalf("log = %q, provider_unavailable should be warning with concise error", logText)
	}
}

func TestRunCompletionToolHandlesEmptyRawJSONArgumentsWithoutEOF(t *testing.T) {
	var logs bytes.Buffer
	service := NewService(config.AIConfig{BraveSearchAPIKey: "test-brave-key"}, repository.Repositories{}, nil, nil)
	service.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	result := service.runCompletionTool(context.Background(), tools.ToolCall{
		ID:        "call_empty_raw",
		Name:      toolNameWebSearch,
		Arguments: json.RawMessage{},
	})
	if !strings.Contains(result, `"ok":false`) || !strings.Contains(result, "query is required") {
		t.Fatalf("result = %s, want query validation failure", result)
	}
	if strings.Contains(result, "unexpected end of JSON input") {
		t.Fatalf("result = %s, should not contain JSON EOF", result)
	}
	logText := logs.String()
	for _, want := range []string{
		"AI tool call failed",
		"tool=web_search",
		"call_id=call_empty_raw",
		"error_kind=invalid_arguments",
		"error_stage=tool_arguments",
		"query is required",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log = %q, want contains %q", logText, want)
		}
	}
}

func TestRunCompletionToolLogsInvalidRawJSONArgumentsAsDecodeFailure(t *testing.T) {
	var logs bytes.Buffer
	service := NewService(config.AIConfig{BraveSearchAPIKey: "test-brave-key"}, repository.Repositories{}, nil, nil)
	service.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	result := service.runCompletionTool(context.Background(), tools.ToolCall{
		ID:        "call_bad_raw",
		Name:      toolNameWebSearch,
		Arguments: json.RawMessage(`{`),
	})
	if !strings.Contains(result, `"ok":false`) || !strings.Contains(result, "invalid tool arguments JSON") {
		t.Fatalf("result = %s, want invalid JSON argument failure", result)
	}
	logText := logs.String()
	for _, want := range []string{
		"AI tool call failed",
		"tool=web_search",
		"call_id=call_bad_raw",
		"error_kind=decode",
		"error_stage=tool_arguments",
		"invalid tool arguments JSON",
		"unexpected end of JSON input",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("log = %q, want contains %q", logText, want)
		}
	}
}

func TestRunWebSearchToolUsesBraveWithoutTavily(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"web": {
				"results": [
					{
						"title": "Current report",
						"url": "https://example.com/current",
						"description": "Published on 2026-06-24",
						"extra_snippets": ["The exact current value is available."],
						"page_age": "2026-06-24"
					}
				]
			}
		}`))
	}))
	defer server.Close()

	previousEndpoint := braveSearchEndpoint
	braveSearchEndpoint = server.URL
	t.Cleanup(func() {
		braveSearchEndpoint = previousEndpoint
	})

	output, err := runWebSearchTool(context.Background(), config.AIConfig{
		BraveSearchAPIKey: "test-brave-key",
	}, map[string]any{
		"query":       "current report",
		"target_date": "2026-06-24",
		"max_results": 5,
	})
	if err != nil {
		t.Fatalf("runWebSearchTool() error = %v", err)
	}
	if captured == nil || captured.Header.Get("X-Subscription-Token") != "test-brave-key" {
		t.Fatalf("Brave request missing subscription token: %#v", captured)
	}
	if got := captured.URL.Query().Get("q"); !strings.Contains(got, "current report") {
		t.Fatalf("query = %q, want contains current report", got)
	}
	if output["result_count"] != 1 || output["target_date_matches"] != 1 {
		t.Fatalf("output = %#v, want one date-matching result", output)
	}
}

func TestRunWebSearchToolExpandsAndRanksRecentSocialSources(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		queries = append(queries, query)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(query, "site:x.com") {
			_, _ = w.Write([]byte(`{
				"web": {
					"results": [
						{
							"title": "Donald J. Trump on X",
							"url": "https://x.com/realDonaldTrump/status/123",
							"description": "Donald J. Trump posted on Jun 24, 2026: policy update.",
							"extra_snippets": ["Posted June 24, 2026"],
							"page_age": "2026-06-24"
						}
					]
				}
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"web": {
				"results": [
					{
						"title": "News recap",
						"url": "https://example.com/news",
						"description": "A broad recap mentions Trump and X without the post URL.",
						"page_age": "2026-06-24"
					}
				]
			}
		}`))
	}))
	defer server.Close()

	previousEndpoint := braveSearchEndpoint
	braveSearchEndpoint = server.URL
	t.Cleanup(func() {
		braveSearchEndpoint = previousEndpoint
	})

	output, err := runWebSearchTool(context.Background(), config.AIConfig{
		BraveSearchAPIKey: "test-brave-key",
	}, map[string]any{
		"query":       "Trump last night on X",
		"topic":       "news",
		"target_date": "2026-06-24",
		"max_results": 5,
	})
	if err != nil {
		t.Fatalf("runWebSearchTool() error = %v", err)
	}
	joinedQueries := strings.Join(queries, "\n")
	if !strings.Contains(joinedQueries, "site:x.com") {
		t.Fatalf("queries = %#v, want x.com variant", queries)
	}
	results, ok := output["results"].([]map[string]any)
	if !ok || len(results) == 0 {
		t.Fatalf("results = %#v, want ranked results", output["results"])
	}
	if results[0]["source_kind"] != "social_post" || results[0]["url"] != "https://x.com/realDonaldTrump/status/123" {
		t.Fatalf("top result = %#v, want social post first", results[0])
	}
	if output["evidence_status"] != "primary_source_date_match" {
		t.Fatalf("evidence_status = %v, want primary source date match", output["evidence_status"])
	}
}

func TestRunWebReadToolExtractsArbitraryPublicPages(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"url": "https://example.com/report", "raw_content": "# Report\nThe exact value is 42."}
			],
			"failed_results": [],
			"response_time": 0.2,
			"usage": {"credits": 1}
		}`))
	}))
	defer server.Close()

	previousEndpoint := tavilyExtractEndpoint
	tavilyExtractEndpoint = server.URL
	t.Cleanup(func() {
		tavilyExtractEndpoint = previousEndpoint
	})

	output, err := runWebReadTool(context.Background(), config.AIConfig{
		TavilyAPIKey: "test-tavily-key",
	}, map[string]any{
		"urls": []string{
			"https://example.com/report#section",
			"http://127.0.0.1/private",
			"file:///etc/passwd",
		},
		"query":             "exact value",
		"extract_depth":     "advanced",
		"format":            "markdown",
		"chunks_per_source": 5,
	})
	if err != nil {
		t.Fatalf("runWebReadTool() error = %v", err)
	}
	urls, ok := captured["urls"].([]any)
	if !ok || len(urls) != 1 || urls[0] != "https://example.com/report" {
		t.Fatalf("urls = %#v, want one normalized public URL", captured["urls"])
	}
	if captured["query"] != "exact value" || captured["extract_depth"] != "advanced" {
		t.Fatalf("captured request = %#v", captured)
	}
	results, ok := output["results"].([]map[string]any)
	if !ok || len(results) != 1 || !strings.Contains(results[0]["content"].(string), "exact value is 42") {
		t.Fatalf("results = %#v, want extracted page content", output["results"])
	}
}

func TestRunWebReadToolUsesBrowserlessForDynamicPages(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token"); got != "browser-token" {
			t.Fatalf("token = %q, want browser-token", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"title": "Dynamic report",
				"final_url": "https://example.com/dynamic",
				"text": "JavaScript rendered exact value: 42",
				"network_responses": [
					{
						"url": "https://api.example.com/current",
						"status": 200,
						"content_type": "application/json",
						"resource_type": "xhr",
						"body": "{\"value\":42,\"date\":\"2026-06-24\"}"
					}
				]
			},
			"type": "application/json"
		}`))
	}))
	defer server.Close()

	output, err := runWebReadTool(context.Background(), config.AIConfig{
		BrowserlessAPIKey:  "browser-token",
		BrowserlessBaseURL: server.URL,
	}, map[string]any{
		"urls": []string{"https://example.com/dynamic"},
	})
	if err != nil {
		t.Fatalf("runWebReadTool() error = %v", err)
	}
	contextValue, ok := captured["context"].(map[string]any)
	if !ok || contextValue["url"] != "https://example.com/dynamic" {
		t.Fatalf("Browserless context = %#v", captured["context"])
	}
	results, ok := output["results"].([]map[string]any)
	if !ok || len(results) != 1 || results[0]["reader"] != "browserless" {
		t.Fatalf("results = %#v, want Browserless result", output["results"])
	}
	if !strings.Contains(results[0]["content"].(string), "exact value: 42") {
		t.Fatalf("content = %q, want rendered value", results[0]["content"])
	}
	networkResponses, ok := results[0]["network_responses"].([]map[string]any)
	if !ok || len(networkResponses) != 1 || !strings.Contains(networkResponses[0]["body"].(string), `"value":42`) {
		t.Fatalf("network responses = %#v, want captured XHR data", results[0]["network_responses"])
	}
}

func TestRunWebReadToolUsesTavilyBeforeBrowserlessWhenConfigured(t *testing.T) {
	var browserlessCalled bool
	extractServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"url": "https://example.com/source", "raw_content": "Fresh exact source content."}
			],
			"failed_results": [],
			"response_time": 0.1
		}`))
	}))
	defer extractServer.Close()
	browserlessServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		browserlessCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer browserlessServer.Close()

	previousEndpoint := tavilyExtractEndpoint
	tavilyExtractEndpoint = extractServer.URL
	t.Cleanup(func() {
		tavilyExtractEndpoint = previousEndpoint
	})

	output, err := runWebReadTool(context.Background(), config.AIConfig{
		TavilyAPIKey:       "test-tavily-key",
		BrowserlessAPIKey:  "browser-token",
		BrowserlessBaseURL: browserlessServer.URL,
	}, map[string]any{
		"urls": []string{"https://example.com/source"},
	})
	if err != nil {
		t.Fatalf("runWebReadTool() error = %v", err)
	}
	if browserlessCalled {
		t.Fatal("Browserless was called despite Tavily extract returning content")
	}
	results, ok := output["results"].([]map[string]any)
	if !ok || len(results) != 1 || results[0]["reader"] != "tavily_extract" {
		t.Fatalf("results = %#v, want Tavily extract result", output["results"])
	}
}

func TestRunWebReadToolFallsBackWhenTavilyIsSlow(t *testing.T) {
	extractServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"failed_results":[],"response_time":5}`))
	}))
	defer extractServer.Close()
	browserlessServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"title": "Dynamic source",
				"final_url": "https://example.invalid/dynamic",
				"text": "Fast Browserless fallback content",
				"network_responses": []
			},
			"type": "application/json"
		}`))
	}))
	defer browserlessServer.Close()

	previousEndpoint := tavilyExtractEndpoint
	tavilyExtractEndpoint = extractServer.URL
	t.Cleanup(func() {
		tavilyExtractEndpoint = previousEndpoint
	})

	started := time.Now()
	output, err := runWebReadTool(context.Background(), config.AIConfig{
		TavilyAPIKey:       "test-tavily-key",
		BrowserlessAPIKey:  "browser-token",
		BrowserlessBaseURL: browserlessServer.URL,
	}, map[string]any{
		"urls": []string{"https://example.invalid/dynamic"},
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("runWebReadTool() error = %v", err)
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("runWebReadTool() took %s, want fast fallback before Tavily timeout", elapsed)
	}
	results, ok := output["results"].([]map[string]any)
	if !ok || len(results) != 1 || results[0]["reader"] != "browserless" {
		t.Fatalf("results = %#v, want Browserless fallback result", output["results"])
	}
	attempts, ok := output["reader_attempts"].([]map[string]any)
	if !ok || len(attempts) == 0 {
		t.Fatalf("reader_attempts = %#v, want timing diagnostics", output["reader_attempts"])
	}
}

func TestCompletionToolDefinitionsExposeSearchAndRead(t *testing.T) {
	definitions := completionToolDefinitions(config.AIConfig{TavilyAPIKey: "configured"})
	names := map[string]bool{}
	for _, definition := range definitions {
		names[definition.Name] = true
	}
	for _, name := range []string{toolNameLocalTime, toolNameCalculate, toolNameCalendar, toolNameWebSearch, toolNameWebRead, toolNameSubAgent} {
		if !names[name] {
			t.Fatalf("tool definitions missing %q: %#v", name, definitions)
		}
	}
}

func TestCompletionToolDefinitionsExposeDirectReadWithoutSearchKey(t *testing.T) {
	definitions := completionToolDefinitions(config.AIConfig{})
	names := map[string]bool{}
	for _, definition := range definitions {
		names[definition.Name] = true
	}
	if !names[toolNameWebRead] {
		t.Fatalf("tool definitions missing direct %q: %#v", toolNameWebRead, definitions)
	}
	if names[toolNameWebSearch] {
		t.Fatalf("tool definitions unexpectedly include %q without key", toolNameWebSearch)
	}
	if !names[toolNameCalculate] || !names[toolNameCalendar] || !names[toolNameSubAgent] {
		t.Fatalf("tool definitions missing utility tools without search key: %#v", definitions)
	}
}

func TestCompletionToolDefinitionsExposeSearchWithBaiduOnly(t *testing.T) {
	definitions := completionToolDefinitions(config.AIConfig{BaiduSearchAPIKey: "configured"})
	names := map[string]bool{}
	for _, definition := range definitions {
		names[definition.Name] = true
	}
	if !names[toolNameWebSearch] {
		t.Fatalf("tool definitions missing %q with Baidu configured: %#v", toolNameWebSearch, definitions)
	}
}

func TestGroupBotCompletionToolDefinitionsExposeTwoStepGroupSearch(t *testing.T) {
	definitions := groupBotCompletionToolDefinitions(config.AIConfig{})
	var groupSearch *tools.ToolDefinition
	for index := range definitions {
		if definitions[index].Name == toolNameGroupChatSearch {
			groupSearch = &definitions[index]
			break
		}
	}
	if groupSearch == nil {
		t.Fatalf("group bot tool definitions missing %q: %#v", toolNameGroupChatSearch, definitions)
	}
	properties, ok := groupSearch.Schema.(map[string]any)["properties"].(map[string]any)
	if !ok {
		t.Fatalf("group search schema = %#v, want properties", groupSearch.Schema)
	}
	for _, name := range []string{"mode", "message_id", "message_seq", "context_before", "context_after"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("group search schema missing %q: %#v", name, properties)
		}
	}
}

func TestRunCompletionToolExecutesUtilityTools(t *testing.T) {
	service := &Service{}
	calcResult := service.runCompletionTool(context.Background(), tools.ToolCall{
		ID:        "call_calc",
		Name:      toolNameCalculate,
		Arguments: map[string]any{"expression": "(2 + 3) * 4"},
	})
	if !strings.Contains(calcResult, `"value":"20"`) || !strings.Contains(calcResult, `"ok":true`) {
		t.Fatalf("calculate result = %s, want ok value 20", calcResult)
	}

	calendarResult := service.runCompletionTool(context.Background(), tools.ToolCall{
		ID:   "call_calendar",
		Name: toolNameCalendar,
		Arguments: map[string]any{
			"operation": "business_add",
			"date":      "2026-06-26",
			"amount":    1,
		},
	})
	if !strings.Contains(calendarResult, `"date":"2026-06-29"`) || !strings.Contains(calendarResult, `"ok":true`) {
		t.Fatalf("calendar result = %s, want next business day", calendarResult)
	}
}

func TestRunCompletionToolExecutesSubAgentResearch(t *testing.T) {
	service := &Service{}
	result := service.runCompletionTool(context.Background(), tools.ToolCall{
		ID:   "call_subagent",
		Name: toolNameSubAgent,
		Arguments: map[string]any{
			"objective": "check utility subtasks",
			"tasks": []map[string]any{
				{
					"id":              "calc",
					"title":           "calculate expression",
					"input":           "7 * 6",
					"expected_output": "numeric value 42",
					"available_tools": []string{toolNameCalculate},
					"need_tool":       true,
					"tool_necessity":  "A calculator avoids arithmetic mistakes.",
					"tool":            toolNameCalculate,
					"arguments":       map[string]any{"expression": "7 * 6"},
					"validation":      "Result value must equal 42.",
				},
				{
					"id":              "calendar",
					"title":           "business day",
					"input":           "add one business day to 2026-06-26",
					"expected_output": "2026-06-29",
					"available_tools": []string{toolNameCalendar},
					"need_tool":       true,
					"tool_necessity":  "Business-day logic depends on calendar rules.",
					"tool":            toolNameCalendar,
					"arguments": map[string]any{
						"operation": "business_add",
						"date":      "2026-06-26",
						"amount":    1,
					},
					"validation": "Result date must be the next business day.",
				},
			},
		},
	})
	var decoded struct {
		OK          bool `json:"ok"`
		ResultCount int  `json:"result_count"`
		TaskCount   int  `json:"task_count"`
		Results     []struct {
			ID     string         `json:"id"`
			Tool   string         `json:"tool"`
			OK     bool           `json:"ok"`
			Result map[string]any `json:"result"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode subagent result: %v\n%s", err, result)
	}
	if !decoded.OK || decoded.ResultCount != 2 || decoded.TaskCount != 2 || len(decoded.Results) != 2 {
		t.Fatalf("subagent result = %#v, raw=%s", decoded, result)
	}
	if decoded.Results[0].ID != "calc" || decoded.Results[0].Tool != toolNameCalculate || !decoded.Results[0].OK || decoded.Results[0].Result["value"] != "42" {
		t.Fatalf("calc subtask = %#v", decoded.Results[0])
	}
	calendarPayload, _ := decoded.Results[1].Result["result"].(map[string]any)
	if decoded.Results[1].ID != "calendar" || decoded.Results[1].Tool != toolNameCalendar || !decoded.Results[1].OK || calendarPayload["date"] != "2026-06-29" {
		t.Fatalf("calendar subtask = %#v", decoded.Results[1])
	}
}

func TestRunAgentStreamReportsSearchProgressOutsideContent(t *testing.T) {
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "2026-06-24 source check",
			"results": [
				{"title": "Source", "url": "https://example.com/source", "content": "source snippet", "score": 0.9}
			],
			"response_time": "0.1"
		}`))
	}))
	defer searchServer.Close()
	extractServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"url": "https://example.com/source", "raw_content": "verified source body"}
			],
			"failed_results": [],
			"response_time": 0.1
		}`))
	}))
	defer extractServer.Close()

	previousSearchEndpoint := tavilySearchEndpoint
	previousExtractEndpoint := tavilyExtractEndpoint
	tavilySearchEndpoint = searchServer.URL
	tavilyExtractEndpoint = extractServer.URL
	t.Cleanup(func() {
		tavilySearchEndpoint = previousSearchEndpoint
		tavilyExtractEndpoint = previousExtractEndpoint
	})

	service := &Service{
		cfg: config.AIConfig{
			TavilyAPIKey: "test-tavily-key",
		},
		client: &streamSearchProgressClient{},
	}
	var chunks []string
	var transientChunks []string
	var progressEvents []*StreamProgress
	var eventOrder []string
	result, err := service.runAgentStream(context.Background(), preparedMessageCompletion{
		model:       "mock-model",
		userMessage: repository.AIMessage{Text: "check current source"},
		messages: []ChatMessage{
			{Role: "system", Content: aiConversationSystemPrompt("mock-model")},
			{Role: "user", Content: "check current source"},
		},
		thinkingType:    "enabled",
		reasoningEffort: "high",
	}, func(delta StreamMessageDelta) error {
		if delta.Progress != nil {
			progressEvents = append(progressEvents, delta.Progress)
			eventOrder = append(eventOrder, "status:"+delta.Progress.Phase+":"+delta.Progress.Status)
			if delta.Content != "" {
				t.Fatalf("progress delta leaked content: %#v", delta)
			}
			return nil
		}
		if delta.Transient {
			transientChunks = append(transientChunks, delta.Content)
			if delta.SegmentStart {
				eventOrder = append(eventOrder, "narration")
			}
			return nil
		}
		chunks = append(chunks, delta.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("runAgentStream() error = %v", err)
	}
	if result.Completion.Content != "final streamed answer" || strings.Join(chunks, "") != "final streamed answer" {
		t.Fatalf("streamed content = %q, completion = %q", strings.Join(chunks, ""), result.Completion.Content)
	}
	if !progressHasURL(progressEvents, "searching", "completed", "https://example.com/source") {
		t.Fatalf("progress events missing completed search URL: %#v", progressEvents)
	}
	if !progressHasURL(progressEvents, "reading", "running", "https://example.com/source") {
		t.Fatalf("progress events missing running read URL: %#v", progressEvents)
	}
	transientText := strings.Join(transientChunks, "")
	if !strings.Contains(transientText, "I’ll verify 2026-06-24 source check against current public sources.") {
		t.Fatalf("transient progress text missing search narration: %q", transientText)
	}
	if !strings.Contains(transientText, "A few results need primary-page support; I’ll open the sources most relevant to 2026-06-24 source check.") {
		t.Fatalf("transient progress text missing read narration: %q", transientText)
	}
	narrationIndex := slices.Index(eventOrder, "narration")
	searchStartIndex := slices.Index(eventOrder, "status:searching:running")
	if narrationIndex < 0 || searchStartIndex < 0 || narrationIndex >= searchStartIndex {
		t.Fatalf("event order = %#v, want narration before search running status", eventOrder)
	}
	for _, unwanted := range []string{
		"我先判断这个问题需要哪些资料",
		"这个问题不需要外部查询",
		"资料已整理，开始生成最终答复",
	} {
		if strings.Contains(transientText, unwanted) {
			t.Fatalf("transient progress contains generic narration %q: %q", unwanted, transientText)
		}
	}
}

func TestEmitTransientNarrationStreamsSmallDeltasWithBoundaries(t *testing.T) {
	var deltas []StreamMessageDelta
	err := emitTransientNarration(func(delta StreamMessageDelta) error {
		deltas = append(deltas, delta)
		return nil
	}, "我先查询京能电力。\n", "step-1", "progress-1")
	if err != nil {
		t.Fatalf("emitTransientNarration() error = %v", err)
	}
	if len(deltas) < 2 {
		t.Fatalf("deltas = %#v, want multiple chunks", deltas)
	}
	var text strings.Builder
	for index, delta := range deltas {
		text.WriteString(delta.Content)
		if delta.Kind != "progress" || !delta.Transient || delta.SegmentID != "progress-1" {
			t.Fatalf("delta[%d] contract = %#v", index, delta)
		}
		if delta.StepID != "step-1" {
			t.Fatalf("delta[%d].StepID = %q", index, delta.StepID)
		}
		if len([]rune(delta.Content)) > 3 {
			t.Fatalf("delta[%d] = %q, want at most 3 runes", index, delta.Content)
		}
		if delta.SegmentStart != (index == 0) {
			t.Fatalf("delta[%d].SegmentStart = %t", index, delta.SegmentStart)
		}
		if delta.SegmentEnd != (index == len(deltas)-1) {
			t.Fatalf("delta[%d].SegmentEnd = %t", index, delta.SegmentEnd)
		}
	}
	if text.String() != "我先查询京能电力。\n" {
		t.Fatalf("reconstructed text = %q", text.String())
	}
}

func TestConversationTracePersistsDisplayStepsAndUsesOnlySummaryForContext(t *testing.T) {
	collector := newConversationTraceCollector()
	collector.Add(agentruntime.Progress{
		StepID:    "step-1",
		Sequence:  1,
		Phase:     "searching",
		Status:    "running",
		Narration: "先查京能电力财报来源。",
		Tool:      toolNameWebSearch,
		Query:     "京能电力 2026 一季报",
	})
	collector.Add(agentruntime.Progress{
		StepID:   "step-1",
		Sequence: 1,
		Phase:    "searching",
		Status:   "completed",
		Tool:     toolNameWebSearch,
		Query:    "京能电力 2026 一季报",
		URLs:     []string{"https://example.com/report"},
	})
	collector.Add(agentruntime.Progress{
		StepID:    "step-2",
		Sequence:  2,
		Phase:     "reading",
		Status:    "completed",
		Narration: "已读取京能电力财报。",
		Tool:      toolNameWebRead,
		Query:     "京能电力 2026 一季报",
		URLs:      []string{"https://example.com/report"},
	})
	trace := collector.Trace("completed")
	if trace == nil || len(trace.Steps) != 2 {
		t.Fatalf("trace = %#v, want two steps", trace)
	}
	if trace.Steps[0].StepID != "step-1" || trace.Steps[0].Status != "completed" {
		t.Fatalf("trace first step = %#v, want paired completed search", trace.Steps[0])
	}
	if !strings.Contains(trace.Summary, "检索了：京能电力 2026 一季报") ||
		!strings.Contains(trace.Summary, "读取并核对了：京能电力 2026 一季报") {
		t.Fatalf("trace summary = %q", trace.Summary)
	}
	if len(trace.SourceRefs) != 1 || trace.SourceRefs[0] != "https://example.com/report" {
		t.Fatalf("trace source refs = %#v", trace.SourceRefs)
	}

	steps, err := json.Marshal(trace.Steps)
	if err != nil {
		t.Fatalf("marshal trace steps: %v", err)
	}
	contextText := formatConversationTraceContext([]repository.AIMessage{{
		ID:   "assistant-1",
		Role: "assistant",
		Text: "最终回答",
		Trace: &repository.AIMessageTrace{
			MessageID:  "assistant-1",
			Summary:    trace.Summary,
			Steps:      steps,
			SourceRefs: json.RawMessage(`["https://example.com/report"]`),
		},
	}})
	if !strings.Contains(contextText, trace.Summary) {
		t.Fatalf("trace context missing summary: %q", contextText)
	}
	if strings.Contains(contextText, "先查京能电力财报来源") ||
		strings.Contains(contextText, "已读取京能电力财报") {
		t.Fatalf("trace context leaked display steps: %q", contextText)
	}
}

func TestDiscoverLinkedDataURLsFindsPublicJSONAndJSONPResources(t *testing.T) {
	document := `
		<script src="https://counter.example.net/track?data=1"></script>
		<script src="//hq.example.com/list=ABC,DEF"></script>
		<script>
			fetch("https://api.example.org/v1/current.json?symbol=ABC");
			const image = "https://cdn.example.org/logo.png";
		</script>`
	urls := discoverLinkedDataURLs("https://finance.example.com/quote/ABC", document)
	joined := strings.Join(urls, "\n")
	for _, expected := range []string{
		"https://hq.example.com/list=ABC,DEF",
		"https://api.example.org/v1/current.json?symbol=ABC",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("discovered URLs = %#v, want %q", urls, expected)
		}
	}
	if strings.Contains(joined, "logo.png") {
		t.Fatalf("discovered URLs unexpectedly include image: %#v", urls)
	}
	if len(urls) > 0 && strings.Contains(urls[0], "counter") {
		t.Fatalf("tracking URL should not outrank quote data: %#v", urls)
	}
}

func TestAIConversationSystemPromptGroundsModelIdentity(t *testing.T) {
	prompt := aiConversationSystemPrompt("deepseek-v4-pro")
	for _, expected := range []string{
		"Runtime model identifier for this request: deepseek-v4-pro.",
		"report the runtime model identifier exactly",
		"Do not claim that the model is the latest version",
		"Distinguish NetherLink platform capabilities",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("system prompt missing %q:\n%s", expected, prompt)
		}
	}
}

func TestPrepareToolAugmentedMessagesFollowsModelToolLoop(t *testing.T) {
	var tavilyCalled bool
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tavilyCalled = true
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "2026-06-23 Nasdaq 100走势",
			"answer": "Nasdaq 100 moved higher in the prior session.",
			"results": [
				{"title": "Market recap", "url": "https://example.com/market", "content": "Nasdaq 100 recap", "score": 0.9}
			],
			"response_time": "0.1"
		}`))
	}))
	defer server.Close()

	previousEndpoint := tavilySearchEndpoint
	previousExtractEndpoint := tavilyExtractEndpoint
	tavilySearchEndpoint = server.URL
	tavilyExtractEndpoint = server.URL
	t.Cleanup(func() {
		tavilySearchEndpoint = previousEndpoint
		tavilyExtractEndpoint = previousExtractEndpoint
	})

	service := &Service{
		cfg: config.AIConfig{
			TavilyAPIKey:   "test-tavily-key",
			RequestTimeout: 0,
		},
		client: &scriptedToolClient{responses: []CompletionResponse{
			{ToolCalls: []tools.ToolCall{
				{
					ID:        "call_time",
					Name:      toolNameLocalTime,
					Arguments: map[string]any{"timezone": "Asia/Shanghai"},
				},
			}, FinishReason: "tool_calls"},
			{ToolCalls: []tools.ToolCall{
				{
					ID:   "call_search",
					Name: toolNameWebSearch,
					Arguments: map[string]any{
						"query":       "2026-06-23 Nasdaq 100走势",
						"time_range":  "day",
						"max_results": 5,
					},
				},
			}, FinishReason: "tool_calls"},
			{Content: "工具上下文已足够", FinishReason: "stop"},
		}},
	}
	messages, usedTools, completion, err := service.prepareToolAugmentedMessages(context.Background(), preparedMessageCompletion{
		model: "mock-model",
		messages: []ChatMessage{
			{Role: "system", Content: aiConversationSystemPrompt()},
			{Role: "user", Content: "昨天晚上纳斯达克100的走势"},
		},
	})
	if err != nil {
		t.Fatalf("prepareToolAugmentedMessages() error = %v", err)
	}
	if !usedTools {
		t.Fatal("usedTools = false, want true")
	}
	if completion != nil {
		t.Fatalf("completion = %#v, want nil", completion)
	}
	if !tavilyCalled {
		t.Fatal("Tavily test server was not called")
	}
	if captured["query"] != "2026-06-23 Nasdaq 100走势" {
		t.Fatalf("query = %v, want model-provided explicit date query", captured["query"])
	}
	if !messagesContainToolEvidence(messages, toolNameLocalTime) ||
		!messagesContainToolEvidence(messages, toolNameWebSearch) {
		t.Fatalf("messages did not include both tool results: %#v", messages)
	}
}

func TestPrepareToolAugmentedMessagesUsesModelLocalTimeDecision(t *testing.T) {
	service := &Service{
		cfg: config.AIConfig{RequestTimeout: 0},
		client: &scriptedToolClient{responses: []CompletionResponse{
			{ToolCalls: []tools.ToolCall{
				{
					ID:        "call_time",
					Name:      toolNameLocalTime,
					Arguments: map[string]any{},
				},
			}, FinishReason: "tool_calls"},
			{Content: "工具上下文已足够", FinishReason: "stop"},
		}},
	}
	messages, usedTools, completion, err := service.prepareToolAugmentedMessages(context.Background(), preparedMessageCompletion{
		model: "mock-model",
		messages: []ChatMessage{
			{Role: "system", Content: aiConversationSystemPrompt()},
			{Role: "user", Content: "今天的日期是？"},
		},
	})
	if err != nil {
		t.Fatalf("prepareToolAugmentedMessages() error = %v", err)
	}
	if !usedTools {
		t.Fatal("usedTools = false, want true")
	}
	if completion != nil {
		t.Fatalf("completion = %#v, want nil", completion)
	}
	if !messagesContainToolEvidence(messages, toolNameLocalTime) {
		t.Fatalf("messages did not include local_time result: %#v", messages)
	}
}

func TestPrepareToolAugmentedMessagesSkipsToolsWhenModelDeclines(t *testing.T) {
	service := &Service{
		cfg:    config.AIConfig{RequestTimeout: 0},
		client: &scriptedToolClient{responses: []CompletionResponse{{Content: "稳定事实回答", FinishReason: "stop"}}},
	}
	messages, usedTools, completion, err := service.prepareToolAugmentedMessages(context.Background(), preparedMessageCompletion{
		model: "mock-model",
		messages: []ChatMessage{
			{Role: "system", Content: aiConversationSystemPrompt()},
			{Role: "user", Content: "解释一下二分查找"},
		},
	})
	if err != nil {
		t.Fatalf("prepareToolAugmentedMessages() error = %v", err)
	}
	if usedTools {
		t.Fatal("usedTools = true, want false")
	}
	if completion == nil || completion.Content != "稳定事实回答" {
		t.Fatalf("completion = %#v, want model answer", completion)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want original messages only", len(messages))
	}
}

func TestPrepareToolAugmentedMessagesKeepsCompoundLocalTimeQuestionForModel(t *testing.T) {
	service := &Service{
		cfg: config.AIConfig{RequestTimeout: 0},
		client: &scriptedToolClient{responses: []CompletionResponse{
			{ToolCalls: []tools.ToolCall{
				{
					ID:        "call_time",
					Name:      toolNameLocalTime,
					Arguments: map[string]any{},
				},
			}, FinishReason: "tool_calls"},
			{Content: "工具上下文已足够", FinishReason: "stop"},
		}},
	}
	messages, usedTools, completion, err := service.prepareToolAugmentedMessages(context.Background(), preparedMessageCompletion{
		model: "deepseek-v4-pro",
		messages: []ChatMessage{
			{Role: "system", Content: aiConversationSystemPrompt()},
			{Role: "user", Content: "你是什么模型？能不能告诉我今天的日子？"},
		},
	})
	if err != nil {
		t.Fatalf("prepareToolAugmentedMessages() error = %v", err)
	}
	if !usedTools {
		t.Fatal("usedTools = false, want true")
	}
	if completion != nil {
		t.Fatalf("completion = %#v, want nil so final model can answer the compound question", completion)
	}
	var joined strings.Builder
	for _, message := range messages {
		joined.WriteString(message.Content)
	}
	if !strings.Contains(joined.String(), "你是什么模型？能不能告诉我今天的日子？") ||
		!messagesContainToolEvidence(messages, toolNameLocalTime) {
		t.Fatalf("messages did not preserve user question plus local_time context: %#v", messages)
	}
}

func TestCompleteMessageRunsVerificationSearchForRiskyDraft(t *testing.T) {
	var tavilyCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tavilyCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "Nasdaq 100 2026-06-23 close",
			"answer": "Nasdaq 100 closed far above 19,885 on 2026-06-23.",
			"results": [
				{"title": "Verified market recap", "url": "https://example.com/verified", "content": "Nasdaq 100 close was not 19,885.", "score": 0.95}
			],
			"response_time": "0.1"
		}`))
	}))
	defer server.Close()

	previousEndpoint := tavilySearchEndpoint
	previousExtractEndpoint := tavilyExtractEndpoint
	tavilySearchEndpoint = server.URL
	tavilyExtractEndpoint = server.URL
	t.Cleanup(func() {
		tavilySearchEndpoint = previousEndpoint
		tavilyExtractEndpoint = previousExtractEndpoint
	})

	service := &Service{
		cfg: config.AIConfig{
			TavilyAPIKey:   "test-tavily-key",
			RequestTimeout: 0,
		},
		client: factCheckClient{},
	}
	completion, _, err := service.completeMessageWithTools(context.Background(), preparedMessageCompletion{
		model:       "mock-model",
		userMessage: repository.AIMessage{Text: "昨天晚上纳斯达克100的走势"},
		messages: []ChatMessage{
			{Role: "system", Content: aiConversationSystemPrompt()},
			{Role: "user", Content: "昨天晚上纳斯达克100的走势"},
		},
	})
	if err != nil {
		t.Fatalf("completeMessageWithTools() error = %v", err)
	}
	if !tavilyCalled {
		t.Fatal("verification search was not called")
	}
	if !strings.Contains(completion.Content, "https://example.com/verified") {
		t.Fatalf("completion = %q, want verified source", completion.Content)
	}
}

func TestVerifyCompletionFailsClosedForSearchBackedDraftWhenPlannerIsInvalid(t *testing.T) {
	var tavilyCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tavilyCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"上证指数","results":[],"response_time":"0.1"}`))
	}))
	defer server.Close()

	previousEndpoint := tavilySearchEndpoint
	tavilySearchEndpoint = server.URL
	t.Cleanup(func() {
		tavilySearchEndpoint = previousEndpoint
	})

	service := &Service{
		cfg: config.AIConfig{
			TavilyAPIKey: "test-tavily-key",
		},
		client: invalidVerificationPlannerClient{},
	}
	verified, err := service.verifyCompletionIfNeeded(
		context.Background(),
		preparedMessageCompletion{
			model:       "mock-model",
			userMessage: repository.AIMessage{Text: "今天上证指数收盘多少？"},
		},
		[]ChatMessage{{
			Role:    "system",
			Content: `Tool web_search result: {"ok":true,"evidence_status":"insufficient_no_results"}`,
		}},
		CompletionResponse{Content: "上证指数收于4163.10点。"},
	)
	if err != nil {
		t.Fatalf("verifyCompletionIfNeeded() error = %v", err)
	}
	if !tavilyCalled {
		t.Fatal("fallback verification search was not called")
	}
	if verified.Content != "现有来源不足，无法核验该收盘点位。" {
		t.Fatalf("verified content = %q, want fail-closed revision", verified.Content)
	}
}

type scriptedToolClient struct {
	responses []CompletionResponse
	index     int
}

func (c *scriptedToolClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if c.index >= len(c.responses) {
		return CompletionResponse{Content: "no more scripted responses", FinishReason: "stop"}, nil
	}
	response := c.responses[c.index]
	c.index++
	return response, nil
}

func (c *scriptedToolClient) Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

type streamSearchProgressClient struct{}

func (c *streamSearchProgressClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if len(req.Tools) > 0 && !testMessagesHaveToolCall(req.Messages, toolNameWebSearch) {
		return CompletionResponse{ToolCalls: []tools.ToolCall{{
			ID:   "call_search",
			Name: toolNameWebSearch,
			Arguments: map[string]any{
				"query":       "2026-06-24 source check",
				"target_date": "2026-06-24",
				"max_results": 3,
			},
		}}, FinishReason: "tool_calls"}, nil
	}
	return CompletionResponse{Content: "routing complete", FinishReason: "stop"}, nil
}

func (c *streamSearchProgressClient) Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error) {
	if req.ThinkingType != "disabled" || req.ReasoningEffort != "low" {
		return CompletionResponse{}, fmt.Errorf("stream reasoning = (%q, %q), want disabled/low", req.ThinkingType, req.ReasoningEffort)
	}
	if !messagesContainToolEvidence(req.Messages, toolNameWebSearch) || !messagesContainToolEvidence(req.Messages, toolNameWebRead) {
		return CompletionResponse{}, fmt.Errorf("stream request missing tool evidence")
	}
	if onDelta != nil {
		if err := onDelta(CompletionStreamDelta{Content: "final streamed answer"}); err != nil {
			return CompletionResponse{}, err
		}
	}
	return CompletionResponse{
		Content:      "final streamed answer",
		TokenCount:   5,
		FinishReason: "stop",
	}, nil
}

func progressHasURL(events []*StreamProgress, phase string, status string, url string) bool {
	for _, event := range events {
		if event == nil || event.Phase != phase || event.Status != status {
			continue
		}
		for _, candidate := range event.URLs {
			if candidate == url {
				return true
			}
		}
	}
	return false
}

type factCheckClient struct{}

func (c factCheckClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	switch {
	case len(req.Tools) > 0:
		return CompletionResponse{Content: "纳斯达克100收于19,885.01点。", FinishReason: "stop"}, nil
	case req.ResponseSchema != nil:
		return CompletionResponse{Content: `{"need_verify":true,"query":"Nasdaq 100 2026-06-23 close","topic":"finance","target_date":"2026-06-23","reason":"The draft contains a precise market index value."}`, FinishReason: "stop"}, nil
	case messageTextContains(req.Messages, "Revise the draft"):
		return CompletionResponse{Content: "核验后应修正该点位，来源：https://example.com/verified", FinishReason: "stop"}, nil
	default:
		return CompletionResponse{Content: "unused", FinishReason: "stop"}, nil
	}
}

func (c factCheckClient) Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

type invalidVerificationPlannerClient struct{}

func (c invalidVerificationPlannerClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if req.ResponseSchema != nil {
		return CompletionResponse{Content: "not valid JSON"}, nil
	}
	if messageTextContains(req.Messages, "Revise the draft") {
		return CompletionResponse{Content: "现有来源不足，无法核验该收盘点位。"}, nil
	}
	return CompletionResponse{Content: "unused"}, nil
}

func (c invalidVerificationPlannerClient) Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

func messageTextContains(messages []ChatMessage, needle string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, needle) {
			return true
		}
	}
	return false
}
