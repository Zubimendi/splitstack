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
				// If no key is set in config, allow traffic (fail-open or disable auth for local dev if chosen).
				// For a strict MVP, this should probably still require a key, but we'll check it here.
				next.ServeHTTP(w, r)
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
