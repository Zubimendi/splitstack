package api

import (
	"net/http"
	"strings"
)

// AuthMiddleware requires a simple fixed token (Bearer <API_KEY>) to protect MVP routes.
func AuthMiddleware(expectedKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expectedKey == "" {
				writeError(w, http.StatusInternalServerError, "AUTH_NOT_CONFIGURED", "server is not configured with an API key")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing authorization header")
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != expectedKey {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing bearer token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
