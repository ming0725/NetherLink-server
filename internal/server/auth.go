package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	authsvc "nlserver/internal/auth"
)

type authPrincipalContextKey struct{}

type AuthPrincipal struct {
	AccessToken string
	UserID      string
	SessionID   string
	DeviceID    string
}

func (s *Server) withAuthRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			writeError(w, r, http.StatusUnauthorized, "TOKEN_MISSING", "Missing bearer token")
			return
		}

		if s.authService == nil {
			writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Auth service is not configured")
			return
		}

		principal, err := s.authService.ValidateAccessToken(r.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, authsvc.ErrTokenExpired):
				writeError(w, r, http.StatusUnauthorized, "TOKEN_EXPIRED", "Access token expired")
			default:
				writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
			}
			return
		}

		ctx := context.WithValue(r.Context(), authPrincipalContextKey{}, AuthPrincipal{
			AccessToken: token,
			UserID:      principal.UserID,
			SessionID:   principal.SessionID,
			DeviceID:    principal.DeviceID,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authPrincipalFromContext(ctx context.Context) (AuthPrincipal, bool) {
	principal, ok := ctx.Value(authPrincipalContextKey{}).(AuthPrincipal)
	return principal, ok
}

func bearerToken(header string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}

	return strings.TrimSpace(token)
}
