package main

import (
	"context"
	"log/slog"
	"os"

	"nlserver/internal/app"
	"nlserver/internal/cache"
	"nlserver/internal/config"
	"nlserver/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	runtime, err := app.NewRuntime(context.Background(), cfg, logger)
	if err != nil {
		logger.Error("failed to initialize runtime", "error", err)
		os.Exit(1)
	}
	defer runtime.Close()

	logger.Info("nlserver starting",
		"env", cfg.Env,
		"addr", cfg.HTTP.Addr,
		"postgres_db", cfg.Postgres.Database,
		"redis_addr", cache.RedisAddr(cfg.Redis),
		"minio_endpoint", cfg.Storage.Endpoint,
	)

	if err := server.NewWithDependencies(server.Dependencies{
		Config:          runtime.Config,
		DB:              runtime.DB,
		Redis:           runtime.Redis,
		Repos:           runtime.Repos,
		AuthService:     runtime.AuthService,
		ChatService:     runtime.ChatService,
		FileService:     runtime.FileService,
		FriendService:   runtime.FriendService,
		GroupService:    runtime.GroupService,
		PostService:     runtime.PostService,
		RealtimeHub:     runtime.RealtimeHub,
		RealtimeFanout:  runtime.RealtimeFanout,
		RealtimeCleanup: runtime.RealtimeCleanup,
		Storage:         runtime.StorageProvider,
		Logger:          runtime.Logger,
	}).ListenAndServe(); err != nil {
		logger.Error("nlserver stopped with error", "error", err)
		os.Exit(1)
	}
}
