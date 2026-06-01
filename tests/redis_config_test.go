package tests

import (
	"testing"

	"nlserver/internal/cache"
	"nlserver/internal/config"
)

func TestRedisAddr(t *testing.T) {
	addr := cache.RedisAddr(config.RedisConfig{
		Host: "localhost",
		Port: 6379,
	})

	if addr != "localhost:6379" {
		t.Fatalf("addr = %q, want localhost:6379", addr)
	}
}
