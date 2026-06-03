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
	t.Setenv("NETHERLINK_AI_REASONING_EFFORT", "high")
	t.Setenv("NETHERLINK_AI_MAX_CONTEXT_TOKENS", "8192")
	t.Setenv("NETHERLINK_AI_REQUEST_TIMEOUT", "45s")

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
	if cfg.AI.ReasoningEffort != "high" {
		t.Fatalf("ai reasoning effort = %q, want high", cfg.AI.ReasoningEffort)
	}
	if cfg.AI.MaxContextTokens != 8192 {
		t.Fatalf("ai max context tokens = %d, want 8192", cfg.AI.MaxContextTokens)
	}
	if cfg.AI.RequestTimeout != 45*time.Second {
		t.Fatalf("ai request timeout = %v, want 45s", cfg.AI.RequestTimeout)
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
	if !strings.Contains(err.Error(), "NETHERLINK_AI_REASONING_EFFORT must be low, medium, or high") {
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

func setValidRequiredConfig(t *testing.T) {
	t.Helper()

	t.Setenv("NETHERLINK_AUTH_TOKEN_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("NETHERLINK_POSTGRES_PASSWORD", "0123456789abcdef")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_ACCESS_KEY", "netherlink_storage_admin")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_SECRET_KEY", "0123456789abcdef")
}
