package main

import (
	"log/slog"
	"os"

	"nlserver/internal/config"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.Info("nlserver configuration loaded",
		"env", cfg.Env,
		"addr", cfg.HTTP.Addr,
		"postgres_db", cfg.Postgres.Database,
		"minio_endpoint", cfg.Storage.Endpoint,
	)
}
