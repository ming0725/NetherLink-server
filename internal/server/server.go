package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	authsvc "nlserver/internal/auth"
	chatsvc "nlserver/internal/chat"
	"nlserver/internal/config"
	filesvc "nlserver/internal/files"
	friendsvc "nlserver/internal/friend"
	groupsvc "nlserver/internal/group"
	"nlserver/internal/realtime"
	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

type Server struct {
	cfg             config.Config
	db              *pgxpool.Pool
	redis           *redis.Client
	repos           repository.Repositories
	authService     *authsvc.Service
	chatService     *chatsvc.Service
	fileService     *filesvc.Service
	friendService   *friendsvc.Service
	groupService    *groupsvc.Service
	realtimeHub     *realtime.Hub
	realtimeFanout  *realtime.RedisFanout
	realtimeCleanup *realtime.CleanupWorker
	storage         storage.ObjectStorageProvider
	logger          *slog.Logger
	handler         http.Handler
}

type Dependencies struct {
	Config          config.Config
	DB              *pgxpool.Pool
	Redis           *redis.Client
	Repos           repository.Repositories
	AuthService     *authsvc.Service
	ChatService     *chatsvc.Service
	FileService     *filesvc.Service
	FriendService   *friendsvc.Service
	GroupService    *groupsvc.Service
	RealtimeHub     *realtime.Hub
	RealtimeFanout  *realtime.RedisFanout
	RealtimeCleanup *realtime.CleanupWorker
	Storage         storage.ObjectStorageProvider
	Logger          *slog.Logger
}

func New(cfg config.Config, db *pgxpool.Pool, redisClient *redis.Client, logger *slog.Logger) *Server {
	deps := Dependencies{
		Config: cfg,
		DB:     db,
		Redis:  redisClient,
		Logger: logger,
	}
	if db != nil {
		deps.Repos = repository.New(db)
		deps.AuthService = authsvc.NewService(cfg.Auth, deps.Repos)
	}

	return NewWithDependencies(deps)
}

func NewWithDependencies(deps Dependencies) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if !repositoriesConfigured(deps.Repos) && deps.DB != nil {
		deps.Repos = repository.New(deps.DB)
	}
	if deps.AuthService == nil && deps.Repos.Users != nil && deps.Repos.Sessions != nil {
		deps.AuthService = authsvc.NewService(deps.Config.Auth, deps.Repos)
	}
	if deps.ChatService == nil && deps.Repos.Chat != nil {
		deps.ChatService = chatsvc.NewService(deps.Repos, deps.DB)
	}
	if deps.FriendService == nil && deps.Repos.Users != nil && deps.Repos.Friends != nil {
		deps.FriendService = friendsvc.NewService(deps.Repos, deps.DB)
	}
	if deps.GroupService == nil && deps.Repos.Users != nil && deps.Repos.Groups != nil {
		deps.GroupService = groupsvc.NewService(deps.Repos, deps.DB)
	}
	if deps.FileService == nil && deps.Repos.Files != nil {
		deps.FileService = filesvc.NewService(deps.Config.Storage, deps.Repos, deps.DB, deps.Storage)
	}
	if deps.RealtimeHub == nil {
		deps.RealtimeHub = realtime.NewHub()
	}
	if deps.RealtimeFanout == nil && deps.Redis != nil && deps.RealtimeHub != nil {
		deps.RealtimeFanout = realtime.NewRedisFanout(deps.Redis, deps.RealtimeHub)
		deps.RealtimeFanout.Start(context.Background(), deps.Logger)
	}

	server := &Server{
		cfg:             deps.Config,
		db:              deps.DB,
		redis:           deps.Redis,
		repos:           deps.Repos,
		authService:     deps.AuthService,
		chatService:     deps.ChatService,
		fileService:     deps.FileService,
		friendService:   deps.FriendService,
		groupService:    deps.GroupService,
		realtimeHub:     deps.RealtimeHub,
		realtimeFanout:  deps.RealtimeFanout,
		realtimeCleanup: deps.RealtimeCleanup,
		storage:         deps.Storage,
		logger:          deps.Logger,
	}

	mux := http.NewServeMux()
	server.registerRoutes(mux)

	server.handler = withRequestID(server.withAccessLog(server.withRecover(withJSONFallbacks(mux))))
	return server
}

func repositoriesConfigured(repos repository.Repositories) bool {
	return repos.Users != nil ||
		repos.Preferences != nil ||
		repos.Sessions != nil ||
		repos.Files != nil ||
		repos.Chat != nil ||
		repos.Friends != nil ||
		repos.Groups != nil ||
		repos.RealtimeEvents != nil ||
		repos.OperationReceipts != nil
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) ListenAndServe() error {
	httpServer := &http.Server{
		Addr:              s.cfg.HTTP.Addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	err := httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)

	s.registerAPIRoutes(mux)
}

func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1", s.handleAPIRoot)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleAuthRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.handleAuthRefresh)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleAuthLogout)
	mux.Handle("GET /api/v1/auth/sessions", s.withAuthRequired(http.HandlerFunc(s.handleAuthSessions)))
	mux.Handle("POST /api/v1/auth/sessions/{sessionId}/revoke", s.withAuthRequired(http.HandlerFunc(s.handleAuthRevokeSession)))
	mux.HandleFunc("GET /api/v1/ws", s.handleWebSocket)
	mux.Handle("GET /api/v1/me", s.withAuthRequired(http.HandlerFunc(s.handleMe)))
	mux.Handle("PATCH /api/v1/me", s.withAuthRequired(http.HandlerFunc(s.handlePatchMe)))
	mux.Handle("GET /api/v1/me/user-id/check", s.withAuthRequired(http.HandlerFunc(s.handleCheckMeUserID)))
	mux.Handle("PATCH /api/v1/me/user-id", s.withAuthRequired(http.HandlerFunc(s.handlePatchMeUserID)))
	mux.Handle("GET /api/v1/me/preferences", s.withAuthRequired(http.HandlerFunc(s.handleGetMePreferences)))
	mux.Handle("PATCH /api/v1/me/preferences", s.withAuthRequired(http.HandlerFunc(s.handlePatchMePreferences)))
	mux.Handle("GET /api/v1/users", s.withAuthRequired(http.HandlerFunc(s.handleUsersSearch)))
	mux.Handle("GET /api/v1/users/by-id/{userId}", s.withAuthRequired(http.HandlerFunc(s.handleUserByPublicID)))
	mux.Handle("GET /api/v1/users/{userUuid}", s.withAuthRequired(http.HandlerFunc(s.handleUserByUUID)))
	mux.Handle("POST /api/v1/users/batch", s.withAuthRequired(http.HandlerFunc(s.handleUsersBatch)))
	mux.Handle("GET /api/v1/friends", s.withAuthRequired(http.HandlerFunc(s.handleFriends)))
	mux.Handle("PATCH /api/v1/friends/{friendUserUuid}", s.withAuthRequired(http.HandlerFunc(s.handlePatchFriend)))
	mux.Handle("DELETE /api/v1/friends/{friendUserUuid}", s.withAuthRequired(http.HandlerFunc(s.handleDeleteFriend)))
	mux.Handle("GET /api/v1/friend-groups", s.withAuthRequired(http.HandlerFunc(s.handleFriendGroups)))
	mux.Handle("POST /api/v1/friend-groups", s.withAuthRequired(http.HandlerFunc(s.handleCreateFriendGroup)))
	mux.Handle("POST /api/v1/friend-requests", s.withAuthRequired(http.HandlerFunc(s.handleCreateFriendRequest)))
	mux.Handle("GET /api/v1/friend-requests", s.withAuthRequired(http.HandlerFunc(s.handleFriendRequests)))
	mux.Handle("POST /api/v1/friend-requests/{requestId}/accept", s.withAuthRequired(http.HandlerFunc(s.handleAcceptFriendRequest)))
	mux.Handle("POST /api/v1/friend-requests/{requestId}/reject", s.withAuthRequired(http.HandlerFunc(s.handleRejectFriendRequest)))
	mux.Handle("GET /api/v1/groups", s.withAuthRequired(http.HandlerFunc(s.handleGroups)))
	mux.Handle("POST /api/v1/groups", s.withAuthRequired(http.HandlerFunc(s.handleCreateGroup)))
	mux.Handle("GET /api/v1/groups/{groupId}", s.withAuthRequired(http.HandlerFunc(s.handleGroupDetail)))
	mux.Handle("PATCH /api/v1/groups/{groupId}", s.withAuthRequired(http.HandlerFunc(s.handlePatchGroup)))
	mux.Handle("DELETE /api/v1/groups/{groupId}", s.withAuthRequired(http.HandlerFunc(s.handleDeleteGroup)))
	mux.Handle("GET /api/v1/groups/{groupId}/members", s.withAuthRequired(http.HandlerFunc(s.handleGroupMembers)))
	mux.Handle("POST /api/v1/groups/{groupId}/members", s.withAuthRequired(http.HandlerFunc(s.handleAddGroupMembers)))
	mux.Handle("DELETE /api/v1/groups/{groupId}/members/{userUuid}", s.withAuthRequired(http.HandlerFunc(s.handleRemoveGroupMember)))
	mux.Handle("PATCH /api/v1/groups/{groupId}/members/{userUuid}", s.withAuthRequired(http.HandlerFunc(s.handlePatchGroupMember)))
	mux.Handle("POST /api/v1/groups/{groupId}/transfer-owner", s.withAuthRequired(http.HandlerFunc(s.handleTransferGroupOwner)))
	mux.Handle("PATCH /api/v1/groups/{groupId}/my-settings", s.withAuthRequired(http.HandlerFunc(s.handlePatchGroupMySettings)))
	mux.Handle("POST /api/v1/group-join-requests", s.withAuthRequired(http.HandlerFunc(s.handleCreateGroupJoinRequest)))
	mux.Handle("GET /api/v1/group-notifications", s.withAuthRequired(http.HandlerFunc(s.handleGroupNotifications)))
	mux.Handle("POST /api/v1/group-join-requests/{requestId}/accept", s.withAuthRequired(http.HandlerFunc(s.handleAcceptGroupJoinRequest)))
	mux.Handle("POST /api/v1/group-join-requests/{requestId}/reject", s.withAuthRequired(http.HandlerFunc(s.handleRejectGroupJoinRequest)))
	mux.Handle("POST /api/v1/files/upload-session", s.withAuthRequired(http.HandlerFunc(s.handleCreateUploadSession)))
	mux.Handle("GET /api/v1/files/upload-session/{uploadSessionId}", s.withAuthRequired(http.HandlerFunc(s.handleGetUploadSession)))
	mux.Handle("POST /api/v1/files/upload-session/{uploadSessionId}/parts", s.withAuthRequired(http.HandlerFunc(s.handlePresignUploadParts)))
	mux.Handle("POST /api/v1/files/upload-session/{uploadSessionId}/complete", s.withAuthRequired(http.HandlerFunc(s.handleCompleteUploadSession)))
	mux.Handle("POST /api/v1/files/upload-session/{uploadSessionId}/abort", s.withAuthRequired(http.HandlerFunc(s.handleAbortUploadSession)))
	mux.Handle("GET /api/v1/conversations", s.withAuthRequired(http.HandlerFunc(s.handleListConversations)))
	mux.Handle("POST /api/v1/conversations/direct", s.withAuthRequired(http.HandlerFunc(s.handleOpenDirectConversation)))
	mux.Handle("POST /api/v1/conversations/group", s.withAuthRequired(http.HandlerFunc(s.handleOpenGroupConversation)))
	mux.Handle("GET /api/v1/conversations/{conversationId}", s.withAuthRequired(http.HandlerFunc(s.handleGetConversation)))
	mux.Handle("PATCH /api/v1/conversations/{conversationId}/settings", s.withAuthRequired(http.HandlerFunc(s.handleUpdateConversationSettings)))
	mux.Handle("POST /api/v1/conversations/{conversationId}/read", s.withAuthRequired(http.HandlerFunc(s.handleMarkConversationRead)))
	mux.Handle("POST /api/v1/conversations/{conversationId}/unread", s.withAuthRequired(http.HandlerFunc(s.handleMarkConversationUnread)))
	mux.Handle("GET /api/v1/conversations/{conversationId}/messages", s.withAuthRequired(http.HandlerFunc(s.handleListMessages)))
	mux.Handle("POST /api/v1/conversations/{conversationId}/messages", s.withAuthRequired(http.HandlerFunc(s.handleSendMessage)))
	mux.Handle("POST /api/v1/conversations/{conversationId}/messages/{messageId}/recall", s.withAuthRequired(http.HandlerFunc(s.handleRecallMessage)))
	mux.Handle("DELETE /api/v1/conversations/{conversationId}/messages", s.withAuthRequired(http.HandlerFunc(s.handleHideConversationHistory)))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "nlserver",
		"env":     s.cfg.Env,
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "PostgreSQL is not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		s.logger.WarnContext(ctx, "postgres readiness check failed", "error", err)
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "PostgreSQL is not ready")
		return
	}

	if s.redis == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Redis is not configured")
		return
	}

	if err := s.redis.Ping(ctx).Err(); err != nil {
		s.logger.WarnContext(ctx, "redis readiness check failed", "error", err)
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Redis is not ready")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"postgres": "ok",
		"redis":    "ok",
		"realtime": map[string]any{
			"hub":     s.realtimeHub.Snapshot(),
			"fanout":  s.realtimeFanout.Snapshot(),
			"cleanup": s.realtimeCleanup.Snapshot(),
		},
	})
}

func (s *Server) handleAPIRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"service":    "nlserver",
		"apiVersion": "v1",
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(r.Context(), "http request panic", "panic", recovered)
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
