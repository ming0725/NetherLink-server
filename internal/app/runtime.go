package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	authsvc "nlserver/internal/auth"
	"nlserver/internal/cache"
	chatsvc "nlserver/internal/chat"
	"nlserver/internal/config"
	"nlserver/internal/database"
	filesvc "nlserver/internal/files"
	friendsvc "nlserver/internal/friend"
	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

type Runtime struct {
	Config          config.Config
	DB              *pgxpool.Pool
	Redis           *redis.Client
	Repos           repository.Repositories
	AuthService     *authsvc.Service
	ChatService     *chatsvc.Service
	FileService     *filesvc.Service
	FriendService   *friendsvc.Service
	StorageProvider storage.ObjectStorageProvider
	Logger          *slog.Logger
}

func NewRuntime(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Runtime, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := database.OpenPostgres(ctx, cfg.Postgres)
	if err != nil {
		return nil, err
	}

	redisClient := cache.NewRedisClient(cfg.Redis)
	repos := repository.New(db)

	runtime := &Runtime{
		Config:        cfg,
		DB:            db,
		Redis:         redisClient,
		Repos:         repos,
		AuthService:   authsvc.NewService(cfg.Auth, repos),
		ChatService:   chatsvc.NewService(repos, db),
		FileService:   filesvc.NewService(cfg.Storage, repos, db, nil),
		FriendService: friendsvc.NewService(repos, db),
		Logger:        logger,
	}

	storageProvider, err := storage.NewProvider(cfg.Storage)
	switch {
	case err == nil:
		runtime.StorageProvider = storageProvider
		runtime.FileService = filesvc.NewService(cfg.Storage, repos, db, storageProvider)
	case errors.Is(err, storage.ErrProviderDisabled):
		logger.Warn("object storage provider disabled")
	case errors.Is(err, storage.ErrProviderNotImplemented):
		logger.Warn("object storage provider not ready", "provider", cfg.Storage.Provider)
	default:
		runtime.Close()
		return nil, err
	}

	return runtime, nil
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if r.Redis != nil {
		_ = r.Redis.Close()
	}
	if r.DB != nil {
		r.DB.Close()
	}
}
