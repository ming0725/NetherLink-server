package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	authsvc "nlserver/internal/auth"
	"nlserver/internal/repository"
)

type authRegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	UserID      string `json:"userId"`
	Gender      string `json:"gender"`
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
}

type authLoginRequest struct {
	Account    string `json:"account"`
	Password   string `json:"password"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
}

type authRefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
	DeviceID     string `json:"deviceId"`
}

type authLogoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type patchPreferencesRequest struct {
	ThemeColor   *string         `json:"themeColor"`
	FontMode     *string         `json:"fontMode"`
	InputEffects json.RawMessage `json:"inputEffects"`
	Settings     json.RawMessage `json:"settings"`
}

type authResponse struct {
	User         userResponse         `json:"user"`
	Preferences  *preferencesResponse `json:"preferences,omitempty"`
	AccessToken  string               `json:"accessToken"`
	RefreshToken string               `json:"refreshToken"`
	ExpiresIn    int                  `json:"expiresIn"`
}

type refreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

type userResponse struct {
	UserUUID          string     `json:"userUuid"`
	UserID            string     `json:"userId"`
	Email             string     `json:"email,omitempty"`
	NickName          string     `json:"nickName"`
	Gender            string     `json:"gender"`
	GenderCustom      *string    `json:"genderCustom"`
	AvatarURL         *string    `json:"avatarUrl"`
	AvatarVersion     int        `json:"avatarVersion"`
	AvatarEtag        *string    `json:"avatarEtag"`
	AvatarContentHash *string    `json:"avatarContentHash"`
	Status            string     `json:"status"`
	Signature         *string    `json:"signature"`
	Region            *string    `json:"region"`
	LastSeenAt        *time.Time `json:"lastSeenAt"`
	Version           int        `json:"version"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type preferencesResponse struct {
	ThemeColor   *string         `json:"themeColor"`
	FontMode     *string         `json:"fontMode"`
	InputEffects json.RawMessage `json:"inputEffects"`
	Settings     json.RawMessage `json:"settings"`
	Version      int             `json:"version"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthService(w, r) {
		return
	}
	if err := s.authRateLimit(r.Context(), "register", clientAddr(r), 10, 10*time.Minute); err != nil {
		writeRequestError(w, r, err)
		return
	}

	var req authRegisterRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	result, err := s.authService.Register(r.Context(), authsvc.RegisterInput{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
		PublicID:    req.UserID,
		Gender:      req.Gender,
		DeviceID:    req.DeviceID,
		DeviceName:  req.DeviceName,
	})
	if err != nil {
		writeAuthError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		User:         makeUserResponse(result.User),
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthService(w, r) {
		return
	}

	var req authLoginRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	rateKey := clientAddr(r) + ":" + stableKey(strings.ToLower(strings.TrimSpace(req.Account)))
	if err := s.authRateLimit(r.Context(), "login", rateKey, 10, 10*time.Minute); err != nil {
		writeRequestError(w, r, err)
		return
	}

	result, err := s.authService.Login(r.Context(), authsvc.LoginInput{
		Account:    req.Account,
		Password:   req.Password,
		DeviceID:   req.DeviceID,
		DeviceName: req.DeviceName,
	})
	if err != nil {
		writeAuthError(w, r, err)
		return
	}

	preferences := makePreferencesResponse(result.Preferences)
	writeJSON(w, http.StatusOK, authResponse{
		User:         makeUserResponse(result.User),
		Preferences:  &preferences,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	})
}

func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthService(w, r) {
		return
	}

	var req authRefreshRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	result, err := s.authService.Refresh(r.Context(), authsvc.RefreshInput{
		RefreshToken: req.RefreshToken,
		DeviceID:     req.DeviceID,
	})
	if err != nil {
		writeAuthError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, refreshResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthService(w, r) {
		return
	}

	var req authLogoutRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	if err := s.authService.Logout(r.Context(), req.RefreshToken); err != nil {
		writeAuthError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	user, err := s.repos.Users.GetByID(r.Context(), principal.UserID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, makeUserResponse(user))
}

func (s *Server) handleGetMePreferences(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	preferences, err := s.repos.Preferences.EnsureDefault(r.Context(), principal.UserID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, makePreferencesResponse(preferences))
}

func (s *Server) handlePatchMePreferences(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	var req patchPreferencesRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	preferences, err := s.repos.Preferences.Update(r.Context(), repository.UpdateUserPreferenceParams{
		UserID:       principal.UserID,
		ThemeColor:   req.ThemeColor,
		FontMode:     req.FontMode,
		InputEffects: req.InputEffects,
		Settings:     req.Settings,
	})
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, makePreferencesResponse(preferences))
}

func (s *Server) requireAuthService(w http.ResponseWriter, r *http.Request) bool {
	if s.authService != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Auth service is not configured")
	return false
}

func (s *Server) authRateLimit(ctx context.Context, bucket string, key string, limit int64, window time.Duration) *requestError {
	if s.redis == nil {
		return nil
	}

	redisKey := "nl:rate:auth:" + bucket + ":" + stableKey(key)
	count, err := s.redis.Incr(ctx, redisKey).Result()
	if err != nil {
		s.logger.WarnContext(ctx, "auth rate limit skipped", "key", redisKey, "error", err)
		return nil
	}
	if count == 1 {
		_ = s.redis.Expire(ctx, redisKey, window).Err()
	}
	if count > limit {
		return &requestError{
			Status:  http.StatusTooManyRequests,
			Code:    "RATE_LIMITED",
			Message: "Too many auth attempts",
		}
	}

	return nil
}

func makeUserResponse(user repository.User) userResponse {
	return userResponse{
		UserUUID:          user.ID,
		UserID:            user.PublicID,
		Email:             user.Email,
		NickName:          user.Nick,
		Gender:            user.Gender,
		GenderCustom:      user.GenderCustom,
		AvatarURL:         nil,
		AvatarVersion:     user.Version,
		AvatarEtag:        nil,
		AvatarContentHash: nil,
		Status:            user.Status,
		Signature:         user.Signature,
		Region:            user.Region,
		LastSeenAt:        user.LastSeenAt,
		Version:           user.Version,
		UpdatedAt:         user.UpdatedAt,
	}
}

func makePreferencesResponse(preferences repository.UserPreference) preferencesResponse {
	return preferencesResponse{
		ThemeColor:   preferences.ThemeColor,
		FontMode:     preferences.FontMode,
		InputEffects: preferences.InputEffects,
		Settings:     preferences.Settings,
		Version:      preferences.Version,
		UpdatedAt:    preferences.UpdatedAt,
	}
}

func writeRequestError(w http.ResponseWriter, r *http.Request, err *requestError) {
	writeError(w, r, err.Status, err.Code, err.Message)
}

func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, authsvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid auth request")
	case errors.Is(err, authsvc.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid account or password")
	case errors.Is(err, authsvc.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Auth resource already exists")
	case errors.Is(err, authsvc.ErrTokenExpired):
		writeError(w, r, http.StatusUnauthorized, "TOKEN_EXPIRED", "Token expired")
	case errors.Is(err, authsvc.ErrTokenInvalid):
		writeError(w, r, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "Refresh token invalid")
	case errors.Is(err, authsvc.ErrInactiveUser):
		writeError(w, r, http.StatusForbidden, "USER_INACTIVE", "User is inactive")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

func writeRepositoryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Resource conflict")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func stableKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
