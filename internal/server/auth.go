package server

import (
	"context"
	"net/http"
	"strings"
)

type authPrincipalContextKey struct{}

type AuthPrincipal struct {
	AccessToken string
}

func withAuthRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing bearer token")
			return
		}

		ctx := context.WithValue(r.Context(), authPrincipalContextKey{}, AuthPrincipal{
			AccessToken: token,
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
