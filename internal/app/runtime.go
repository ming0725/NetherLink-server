package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	aisvc "nlserver/internal/agent/conversation"
	agentmodel "nlserver/internal/agent/model"
	agentprofile "nlserver/internal/agent/profile"
	authsvc "nlserver/internal/auth"
	"nlserver/internal/cache"
	chatsvc "nlserver/internal/chat"
	"nlserver/internal/config"
	"nlserver/internal/database"
	filesvc "nlserver/internal/files"
	friendsvc "nlserver/internal/friend"
	groupsvc "nlserver/internal/group"
	postsvc "nlserver/internal/post"
	"nlserver/internal/realtime"
	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

type Runtime struct {
	Config              config.Config
	DB                  *pgxpool.Pool
	Redis               *redis.Client
	Repos               repository.Repositories
	AuthService         *authsvc.Service
	AIService           *aisvc.Service
	AgentService        *agentprofile.Service
	ModelGateway        agentmodel.ModelGateway
	ChatService         *chatsvc.Service
	FileService         *filesvc.Service
	FileWorker          *filesvc.ProcessingWorker
	FileCleanup         *filesvc.CleanupWorker
	FriendService       *friendsvc.Service
	GroupService        *groupsvc.Service
	PostService         *postsvc.Service
	RealtimeHub         *realtime.Hub
	RealtimeFanout      *realtime.RedisFanout
	RealtimeCleanup     *realtime.CleanupWorker
	StorageProvider     storage.ObjectStorageProvider
	Logger              *slog.Logger
	cancel              context.CancelFunc
	fileWorkerDone      <-chan struct{}
	fileCleanupDone     <-chan struct{}
	realtimeCleanupDone <-chan struct{}
}

func NewRuntime(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Runtime, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := database.OpenPostgres(ctx, cfg.Postgres)
	if err != nil {
		return nil, err
	}
	if err := database.EnsureRuntimeSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	redisClient := cache.NewRedisClient(cfg.Redis)
	repos := repository.New(db)
	runtimeCtx, cancel := context.WithCancel(ctx)
	var modelGateway agentmodel.ModelGateway
	if gateway, err := agentmodel.NewModelGateway(cfg.AI); err == nil {
		modelGateway = gateway
	} else if !errors.Is(err, agentmodel.ErrModelProviderUnavailable) {
		redisClient.Close()
		db.Close()
		return nil, err
	}

	runtime := &Runtime{
		Config:        cfg,
		DB:            db,
		Redis:         redisClient,
		Repos:         repos,
		AuthService:   authsvc.NewServiceWithTx(cfg.Auth, repos, db),
		AgentService:  agentprofile.NewServiceWithModelGateway(repos, db, modelGateway, cfg.AI),
		ModelGateway:  modelGateway,
		ChatService:   chatsvc.NewService(repos, db),
		FileService:   filesvc.NewService(cfg.Storage, repos, db, nil),
		FriendService: friendsvc.NewService(repos, db),
		GroupService:  groupsvc.NewService(repos, db),
		PostService:   postsvc.NewService(repos, db),
		RealtimeHub:   realtime.NewHub(),
		Logger:        logger,
		cancel:        cancel,
	}
	if err := runtime.AgentService.SyncDefaultMCPManifests(ctx); err != nil {
		runtime.Close()
		return nil, err
	}
	if aiClient := aisvc.NewChatClientFromModelGateway(modelGateway); aiClient != nil {
		runtime.AIService = aisvc.NewService(cfg.AI, repos, db, aiClient)
	} else {
		runtime.AIService = aisvc.NewService(cfg.AI, repos, db, nil)
	}
	runtime.AIService.SetLogger(logger)
	runtime.RealtimeFanout = realtime.NewRedisFanout(redisClient, runtime.RealtimeHub)
	runtime.RealtimeFanout.Start(runtimeCtx, logger)
	runtime.RealtimeCleanup = realtime.NewCleanupWorker(
		repos.RealtimeEvents,
		cfg.Realtime.EventRetention,
		cfg.Realtime.EventCleanupInterval,
		cfg.Realtime.EventCleanupBatch,
		logger,
	)
	runtime.realtimeCleanupDone = runtime.RealtimeCleanup.Start(runtimeCtx)

	storageProvider, err := storage.NewProvider(cfg.Storage)
	switch {
	case err == nil:
		runtime.StorageProvider = storageProvider
		runtime.FileService = filesvc.NewService(cfg.Storage, repos, db, storageProvider)
		runtime.FileWorker = filesvc.NewProcessingWorker(cfg.Storage, repos, db, storageProvider, logger)
		runtime.FileCleanup = filesvc.NewCleanupWorker(repos, storageProvider, logger)
		runtime.fileWorkerDone = runtime.FileWorker.Start(runtimeCtx)
		runtime.fileCleanupDone = runtime.FileCleanup.Start(runtimeCtx)
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
	if r.cancel != nil {
		r.cancel()
	}
	if r.fileWorkerDone != nil {
		select {
		case <-r.fileWorkerDone:
		case <-time.After(2 * time.Second):
			r.Logger.Warn("file processing worker did not stop before shutdown timeout")
		}
	}
	if r.fileCleanupDone != nil {
		select {
		case <-r.fileCleanupDone:
		case <-time.After(2 * time.Second):
			r.Logger.Warn("file cleanup worker did not stop before shutdown timeout")
		}
	}
	if r.realtimeCleanupDone != nil {
		select {
		case <-r.realtimeCleanupDone:
		case <-time.After(2 * time.Second):
			r.Logger.Warn("realtime cleanup worker did not stop before shutdown timeout")
		}
	}
	if r.Redis != nil {
		_ = r.Redis.Close()
	}
	if r.DB != nil {
		r.DB.Close()
	}
}
