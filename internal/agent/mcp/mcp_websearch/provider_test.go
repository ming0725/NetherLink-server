package mcp_websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"nlserver/internal/config"
)

func TestExpandedSearchInputsKeepsGenericQueryVariants(t *testing.T) {
	variants := expandedSearchInputs(webSearchInput{
		Query:         "OpenAI latest model release",
		QueryVariants: []string{"site:openai.com OpenAI latest model release", "OpenAI model release June 2026"},
		Topic:         "general",
		MaxResults:    5,
	})
	queries := make([]string, 0, len(variants))
	for _, variant := range variants {
		queries = append(queries, variant.Query)
	}
	for _, want := range []string{
		"OpenAI latest model release",
		"site:openai.com OpenAI latest model release",
		"OpenAI model release June 2026",
	} {
		if !containsString(queries, want) {
			t.Fatalf("queries = %#v, want %q", queries, want)
		}
	}
}

func TestFailedSearchOutputKeepsProviderDiagnostics(t *testing.T) {
	input := webSearchInput{Query: "latest current event", Topic: "news"}
	variants := []webSearchInput{
		{Query: "latest current event", Topic: "news"},
		{Query: "latest current event site:example.com", Topic: "news"},
	}
	providerErrors := []map[string]any{{
		"provider":    "tavily",
		"query":       "latest current event",
		"error":       "context deadline exceeded",
		"duration_ms": 8000,
	}}
	output := failedSearchOutput(input, variants, []string{"tavily"}, providerErrors)
	if output["ok"] != false || output["evidence_status"] != "provider_unavailable" || output["result_count"] != 0 {
		t.Fatalf("output = %#v, want structured provider_unavailable failure", output)
	}
	if got := stringSliceFromAny(output["providers"]); len(got) != 1 || got[0] != "tavily" {
		t.Fatalf("providers = %#v, want tavily", output["providers"])
	}
	if got := stringSliceFromAny(output["queries"]); len(got) != 2 {
		t.Fatalf("queries = %#v, want two attempted queries", output["queries"])
	}
	if attempts, ok := output["provider_attempts"].([]map[string]any); !ok || len(attempts) != 1 {
		t.Fatalf("provider_attempts = %#v, want provider error details", output["provider_attempts"])
	}
}

func TestRunSearchReturnsStructuredFailureWhenProvidersFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
	}))
	defer server.Close()

	previousEndpoint := TavilySearchEndpoint
	TavilySearchEndpoint = server.URL
	t.Cleanup(func() {
		TavilySearchEndpoint = previousEndpoint
	})

	output, err := RunSearch(context.Background(), config.AIConfig{
		TavilyAPIKey: "test-tavily-key",
	}, map[string]any{
		"query":       "latest current event",
		"topic":       "news",
		"max_results": 2,
	})
	if err != nil {
		t.Fatalf("RunSearch() error = %v, want structured provider failure payload", err)
	}
	if output["ok"] != false || output["evidence_status"] != "provider_unavailable" || output["result_count"] != 0 {
		t.Fatalf("output = %#v, want provider_unavailable failure", output)
	}
	if got := stringSliceFromAny(output["providers"]); len(got) != 1 || got[0] != "tavily" {
		t.Fatalf("providers = %#v, want tavily", output["providers"])
	}
	if attempts, ok := output["provider_attempts"].([]map[string]any); !ok || len(attempts) != 1 {
		t.Fatalf("provider_attempts = %#v, want one failed Tavily attempt", output["provider_attempts"])
	}
}

func TestRunSearchAcceptsAndReturnsTimezone(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode search body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "Trump X 2026-06-26 Beijing last night",
			"results": [
				{"title": "Trump post on June 26, 2026", "url": "https://example.com/post", "content": "June 26, 2026 post from X", "score": 0.9}
			],
			"response_time": "0.1"
		}`))
	}))
	defer server.Close()

	previousEndpoint := TavilySearchEndpoint
	TavilySearchEndpoint = server.URL
	t.Cleanup(func() {
		TavilySearchEndpoint = previousEndpoint
	})

	output, err := RunSearch(context.Background(), config.AIConfig{
		TavilyAPIKey: "test-tavily-key",
	}, map[string]any{
		"query":       "Trump X 2026-06-26 Beijing last night",
		"topic":       "news",
		"target_date": "2026-06-26",
		"timezone":    "Asia/Shanghai",
		"max_results": 1,
	})
	if err != nil {
		t.Fatalf("RunSearch() error = %v", err)
	}
	if captured["max_results"] != float64(1) || captured["topic"] != "news" {
		t.Fatalf("captured provider body = %#v", captured)
	}
	if output["timezone"] != "Asia/Shanghai" || output["target_date"] != "2026-06-26" {
		t.Fatalf("output = %#v, want timezone and target_date", output)
	}
}

func TestRunSearchStopsVariantsWhenOnlyProviderFailsFirstQuery(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
	}))
	defer server.Close()

	previousEndpoint := TavilySearchEndpoint
	TavilySearchEndpoint = server.URL
	t.Cleanup(func() {
		TavilySearchEndpoint = previousEndpoint
	})

	output, err := RunSearch(context.Background(), config.AIConfig{
		TavilyAPIKey: "test-tavily-key",
	}, map[string]any{
		"query":          "NASDAQ 100 close price June 26 2026",
		"query_variants": []string{"^NDX close June 26 2026", "Nasdaq 100 index closing June 26 2026", "NASDAQ 100 close price June 26 2026 2026-06-26"},
		"topic":          "finance",
		"max_results":    5,
	})
	if err != nil {
		t.Fatalf("RunSearch() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want only the primary query when sole provider fails", requests.Load())
	}
	if output["ok"] != false || output["evidence_status"] != "provider_unavailable" {
		t.Fatalf("output = %#v, want provider_unavailable", output)
	}
	if got := stringSliceFromAny(output["queries"]); len(got) != 1 || got[0] != "NASDAQ 100 close price June 26 2026" {
		t.Fatalf("queries = %#v, want only attempted primary query", output["queries"])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
