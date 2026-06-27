package tests

import (
	"strings"
	"testing"
	"time"

	"nlserver/internal/config"
)

func TestConfigLoadRequiresSecrets(t *testing.T) {
	t.Setenv("NETHERLINK_AUTH_TOKEN_SECRET", "")
	t.Setenv("NETHERLINK_POSTGRES_PASSWORD", "")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_ACCESS_KEY", "")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_SECRET_KEY", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing secret error")
	}
	if !strings.Contains(err.Error(), "NETHERLINK_AUTH_TOKEN_SECRET is required") {
		t.Fatalf("Load() error = %q, want auth token secret error", err)
	}
}

func TestConfigLoadWithValidEnvironment(t *testing.T) {
	setValidRequiredConfig(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Auth.AccessTokenTTL != 15*time.Minute {
		t.Fatalf("access token ttl = %v, want 15m", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Postgres.Port != 5432 {
		t.Fatalf("postgres port = %d, want 5432", cfg.Postgres.Port)
	}
	if cfg.Storage.Buckets.Public != "netherlink-public" {
		t.Fatalf("public bucket = %q, want netherlink-public", cfg.Storage.Buckets.Public)
	}
	if cfg.Storage.UserQuotaBytes != 10*1024*1024*1024 {
		t.Fatalf("storage user quota bytes = %d, want 10 GiB", cfg.Storage.UserQuotaBytes)
	}
	if cfg.AI.Provider != "disabled" {
		t.Fatalf("ai provider = %q, want disabled", cfg.AI.Provider)
	}
	if cfg.AI.RequestTimeout != time.Minute {
		t.Fatalf("ai request timeout = %v, want 1m", cfg.AI.RequestTimeout)
	}
	if cfg.AI.MaxOutputTokens != 4096 {
		t.Fatalf("ai max output tokens = %d, want 4096", cfg.AI.MaxOutputTokens)
	}
	if cfg.AI.LangSmith.Tracing {
		t.Fatal("langsmith tracing = true, want false")
	}
	if cfg.AI.LangSmith.Endpoint != "https://api.smith.langchain.com" {
		t.Fatalf("langsmith endpoint = %q, want default endpoint", cfg.AI.LangSmith.Endpoint)
	}
	if cfg.AI.LangSmith.RequestTimeout != 5*time.Second {
		t.Fatalf("langsmith request timeout = %v, want 5s", cfg.AI.LangSmith.RequestTimeout)
	}
}

func TestConfigLoadRejectsMalformedValues(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("NETHERLINK_POSTGRES_PORT", "not-a-port")
	t.Setenv("NETHERLINK_AUTH_ACCESS_TOKEN_TTL", "never")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want malformed value error")
	}
	if !strings.Contains(err.Error(), "NETHERLINK_POSTGRES_PORT must be an integer") {
		t.Fatalf("Load() error = %q, want port parse error", err)
	}
	if !strings.Contains(err.Error(), "NETHERLINK_AUTH_ACCESS_TOKEN_TTL must be a duration") {
		t.Fatalf("Load() error = %q, want ttl parse error", err)
	}
}

func TestConfigLoadAIProvider(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("NETHERLINK_AI_PROVIDER", "openai")
	t.Setenv("NETHERLINK_AI_API_KEY", "test-ai-key")
	t.Setenv("NETHERLINK_AI_DEFAULT_MODEL", "test-model")
	t.Setenv("NETHERLINK_AI_TITLE_MODEL", "test-title-model")
	t.Setenv("NETHERLINK_AI_THINKING_TYPE", "enabled")
	t.Setenv("NETHERLINK_AI_REASONING_EFFORT", "max")
	t.Setenv("NETHERLINK_AI_MAX_CONTEXT_TOKENS", "8192")
	t.Setenv("NETHERLINK_AI_MAX_OUTPUT_TOKENS", "6000")
	t.Setenv("NETHERLINK_AI_REQUEST_TIMEOUT", "45s")
	t.Setenv("NETHERLINK_TAVILY_API_KEY", "test-tavily-key")
	t.Setenv("NETHERLINK_BRAVE_SEARCH_API_KEY", "test-brave-key")
	t.Setenv("NETHERLINK_BROWSERLESS_API_KEY", "test-browserless-key")
	t.Setenv("NETHERLINK_BROWSERLESS_BASE_URL", "https://browserless.example")
	t.Setenv("LANGSMITH_TRACING", "true")
	t.Setenv("LANGSMITH_ENDPOINT", "https://langsmith.example")
	t.Setenv("LANGSMITH_API_KEY", "test-langsmith-key")
	t.Setenv("LANGSMITH_PROJECT", "NLServer")
	t.Setenv("LANGSMITH_WORKSPACE_ID", "workspace-1")
	t.Setenv("LANGSMITH_REQUEST_TIMEOUT", "15s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.Provider != "openai" {
		t.Fatalf("ai provider = %q, want openai", cfg.AI.Provider)
	}
	if cfg.AI.DefaultModel != "test-model" {
		t.Fatalf("ai default model = %q, want test-model", cfg.AI.DefaultModel)
	}
	if cfg.AI.TitleModel != "test-title-model" {
		t.Fatalf("ai title model = %q, want test-title-model", cfg.AI.TitleModel)
	}
	if cfg.AI.ThinkingType != "enabled" {
		t.Fatalf("ai thinking type = %q, want enabled", cfg.AI.ThinkingType)
	}
	if cfg.AI.ReasoningEffort != "max" {
		t.Fatalf("ai reasoning effort = %q, want max", cfg.AI.ReasoningEffort)
	}
	if cfg.AI.MaxContextTokens != 8192 {
		t.Fatalf("ai max context tokens = %d, want 8192", cfg.AI.MaxContextTokens)
	}
	if cfg.AI.MaxOutputTokens != 6000 {
		t.Fatalf("ai max output tokens = %d, want 6000", cfg.AI.MaxOutputTokens)
	}
	if cfg.AI.RequestTimeout != 45*time.Second {
		t.Fatalf("ai request timeout = %v, want 45s", cfg.AI.RequestTimeout)
	}
	if cfg.AI.TavilyAPIKey != "test-tavily-key" {
		t.Fatalf("tavily api key = %q, want test-tavily-key", cfg.AI.TavilyAPIKey)
	}
	if cfg.AI.BraveSearchAPIKey != "test-brave-key" {
		t.Fatalf("brave search api key = %q, want test-brave-key", cfg.AI.BraveSearchAPIKey)
	}
	if cfg.AI.BrowserlessAPIKey != "test-browserless-key" {
		t.Fatalf("browserless api key = %q, want test-browserless-key", cfg.AI.BrowserlessAPIKey)
	}
	if cfg.AI.BrowserlessBaseURL != "https://browserless.example" {
		t.Fatalf("browserless base URL = %q, want https://browserless.example", cfg.AI.BrowserlessBaseURL)
	}
	if !cfg.AI.LangSmith.Tracing {
		t.Fatal("langsmith tracing = false, want true")
	}
	if cfg.AI.LangSmith.Endpoint != "https://langsmith.example" {
		t.Fatalf("langsmith endpoint = %q, want https://langsmith.example", cfg.AI.LangSmith.Endpoint)
	}
	if cfg.AI.LangSmith.APIKey != "test-langsmith-key" {
		t.Fatalf("langsmith api key = %q, want test-langsmith-key", cfg.AI.LangSmith.APIKey)
	}
	if cfg.AI.LangSmith.Project != "NLServer" {
		t.Fatalf("langsmith project = %q, want NLServer", cfg.AI.LangSmith.Project)
	}
	if cfg.AI.LangSmith.WorkspaceID != "workspace-1" {
		t.Fatalf("langsmith workspace id = %q, want workspace-1", cfg.AI.LangSmith.WorkspaceID)
	}
	if cfg.AI.LangSmith.RequestTimeout != 15*time.Second {
		t.Fatalf("langsmith request timeout = %v, want 15s", cfg.AI.LangSmith.RequestTimeout)
	}
}

func TestConfigLoadAIProviderUsesOpenAIAPIKeyFallback(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("NETHERLINK_AI_PROVIDER", "openai")
	t.Setenv("NETHERLINK_AI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "fallback-openai-key")
	t.Setenv("NETHERLINK_AI_DEFAULT_MODEL", "test-model")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.APIKey != "fallback-openai-key" {
		t.Fatalf("ai api key = %q, want fallback-openai-key", cfg.AI.APIKey)
	}
}

func TestConfigLoadAIProviderDoesNotUseOpenAIAPIKeyFallbackForCustomBaseURL(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("NETHERLINK_AI_PROVIDER", "openai-compatible")
	t.Setenv("NETHERLINK_AI_BASE_URL", "https://api.deepseek.com")
	t.Setenv("NETHERLINK_AI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "fallback-openai-key")
	t.Setenv("NETHERLINK_AI_DEFAULT_MODEL", "deepseek-v4-flash")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing NetherLink AI API key error")
	}
	if !strings.Contains(err.Error(), "NETHERLINK_AI_API_KEY is required") {
		t.Fatalf("Load() error = %q, want AI API key error", err)
	}
}

func TestConfigLoadStorageUserQuota(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("NETHERLINK_FILE_USER_QUOTA_BYTES", "123456")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.UserQuotaBytes != 123456 {
		t.Fatalf("storage user quota bytes = %d, want 123456", cfg.Storage.UserQuotaBytes)
	}
}

func TestConfigLoadAIProviderRejectsMalformedReasoningSettings(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("NETHERLINK_AI_PROVIDER", "openai-compatible")
	t.Setenv("NETHERLINK_AI_API_KEY", "test-ai-key")
	t.Setenv("NETHERLINK_AI_DEFAULT_MODEL", "test-model")
	t.Setenv("NETHERLINK_AI_THINKING_TYPE", "sometimes")
	t.Setenv("NETHERLINK_AI_REASONING_EFFORT", "maximum")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want AI reasoning validation error")
	}
	if !strings.Contains(err.Error(), "NETHERLINK_AI_THINKING_TYPE must be enabled or disabled") {
		t.Fatalf("Load() error = %q, want thinking type error", err)
	}
	if !strings.Contains(err.Error(), "NETHERLINK_AI_REASONING_EFFORT must be low, medium, high, or max") {
		t.Fatalf("Load() error = %q, want reasoning effort error", err)
	}
}

func TestConfigLoadAIProviderRequiresRuntimeSettings(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("NETHERLINK_AI_PROVIDER", "openai")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want AI runtime validation error")
	}
	if !strings.Contains(err.Error(), "NETHERLINK_AI_API_KEY is required") {
		t.Fatalf("Load() error = %q, want AI API key error", err)
	}
	if !strings.Contains(err.Error(), "NETHERLINK_AI_DEFAULT_MODEL is required") {
		t.Fatalf("Load() error = %q, want AI default model error", err)
	}
}

func TestConfigLoadLangSmithTracingRequiresAPIKey(t *testing.T) {
	setValidRequiredConfig(t)
	t.Setenv("LANGSMITH_TRACING", "true")
	t.Setenv("LANGSMITH_API_KEY", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want LangSmith API key validation error")
	}
	if !strings.Contains(err.Error(), "LANGSMITH_API_KEY is required when LANGSMITH_TRACING is true") {
		t.Fatalf("Load() error = %q, want LangSmith API key error", err)
	}
}

func setValidRequiredConfig(t *testing.T) {
	t.Helper()

	t.Setenv("NETHERLINK_AUTH_TOKEN_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("NETHERLINK_POSTGRES_PASSWORD", "0123456789abcdef")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_ACCESS_KEY", "netherlink_storage_admin")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_SECRET_KEY", "0123456789abcdef")
}
