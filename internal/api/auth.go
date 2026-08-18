package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuthMiddleware requires a valid JWT token.
func JWTAuthMiddleware(secretKey []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing authorization header")
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid bearer token format")
				return
			}

			tokenString := parts[1]
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				return secretKey, nil
			})

			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
				return
			}

			// Add the user ID to context
			if claims, ok := token.Claims.(jwt.MapClaims); ok {
				ctx := context.WithValue(r.Context(), "userID", claims["sub"])
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token claims")
			}
		})
	}
}
