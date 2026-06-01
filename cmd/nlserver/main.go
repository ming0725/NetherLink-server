package main

import (
	"context"
	"log/slog"
	"os"

	"nlserver/internal/cache"
	"nlserver/internal/config"
	"nlserver/internal/database"
	"nlserver/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	db, err := database.OpenPostgres(context.Background(), cfg.Postgres)
	if err != nil {
		logger.Error("failed to configure postgres connection pool", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	redisClient := cache.NewRedisClient(cfg.Redis)
	defer redisClient.Close()

	logger.Info("nlserver starting",
		"env", cfg.Env,
		"addr", cfg.HTTP.Addr,
		"postgres_db", cfg.Postgres.Database,
		"redis_addr", cache.RedisAddr(cfg.Redis),
		"minio_endpoint", cfg.Storage.Endpoint,
	)

	if err := server.New(cfg, db, redisClient, logger).ListenAndServe(); err != nil {
		logger.Error("nlserver stopped with error", "error", err)
		os.Exit(1)
	}
}
