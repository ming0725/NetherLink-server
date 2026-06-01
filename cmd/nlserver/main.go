package main

import (
	"context"
	"log/slog"
	"os"

	"nlserver/internal/config"
	"nlserver/internal/database"
	"nlserver/internal/server"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	db, err := database.OpenPostgres(context.Background(), cfg.Postgres)
	if err != nil {
		logger.Error("failed to configure postgres connection pool", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("nlserver starting",
		"env", cfg.Env,
		"addr", cfg.HTTP.Addr,
		"postgres_db", cfg.Postgres.Database,
		"minio_endpoint", cfg.Storage.Endpoint,
	)

	if err := server.New(cfg, db, logger).ListenAndServe(); err != nil {
		logger.Error("nlserver stopped with error", "error", err)
		os.Exit(1)
	}
}
