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

func setValidRequiredConfig(t *testing.T) {
	t.Helper()

	t.Setenv("NETHERLINK_AUTH_TOKEN_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("NETHERLINK_POSTGRES_PASSWORD", "0123456789abcdef")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_ACCESS_KEY", "netherlink_storage_admin")
	t.Setenv("NETHERLINK_OBJECT_STORAGE_SECRET_KEY", "0123456789abcdef")
}
