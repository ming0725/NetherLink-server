package tests

import (
	"net/url"
	"testing"

	"nlserver/internal/config"
	"nlserver/internal/database"
)

func TestPostgresDSN(t *testing.T) {
	dsn := database.PostgresDSN(config.PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "netherlink",
		User:     "netherlink_app",
		Password: "secret",
		SSLMode:  "disable",
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}

	if parsed.Scheme != "postgres" {
		t.Fatalf("scheme = %q, want postgres", parsed.Scheme)
	}
	if parsed.Host != "localhost:5432" {
		t.Fatalf("host = %q, want localhost:5432", parsed.Host)
	}
	if parsed.Path != "/netherlink" {
		t.Fatalf("path = %q, want /netherlink", parsed.Path)
	}
	if parsed.User.Username() != "netherlink_app" {
		t.Fatalf("user = %q, want netherlink_app", parsed.User.Username())
	}
	if password, _ := parsed.User.Password(); password != "secret" {
		t.Fatalf("password = %q, want secret", password)
	}
	if parsed.Query().Get("sslmode") != "disable" {
		t.Fatalf("sslmode = %q, want disable", parsed.Query().Get("sslmode"))
	}
}
