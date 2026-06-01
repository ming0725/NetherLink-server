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
	"nlserver/internal/config"
	"nlserver/internal/repository"
)

type Server struct {
	cfg         config.Config
	db          *pgxpool.Pool
	redis       *redis.Client
	repos       repository.Repositories
	authService *authsvc.Service
	logger      *slog.Logger
	handler     http.Handler
}

func New(cfg config.Config, db *pgxpool.Pool, redisClient *redis.Client, logger *slog.Logger) *Server {
	server := &Server{
		cfg:    cfg,
		db:     db,
		redis:  redisClient,
		logger: logger,
	}
	if db != nil {
		server.repos = repository.New(db)
		server.authService = authsvc.NewService(cfg.Auth, server.repos)
	}

	mux := http.NewServeMux()
	server.registerRoutes(mux)

	server.handler = withRequestID(server.withAccessLog(server.withRecover(withJSONFallbacks(mux))))
	return server
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
	mux.Handle("GET /api/v1/friend-groups", s.withAuthRequired(http.HandlerFunc(s.handleFriendGroups)))
	mux.Handle("GET /api/v1/groups", s.withAuthRequired(http.HandlerFunc(s.handleGroups)))
	mux.Handle("GET /api/v1/groups/{groupId}", s.withAuthRequired(http.HandlerFunc(s.handleGroupDetail)))
	mux.Handle("GET /api/v1/groups/{groupId}/members", s.withAuthRequired(http.HandlerFunc(s.handleGroupMembers)))
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
