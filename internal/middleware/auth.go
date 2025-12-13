// Package middleware hosts authentication, logging, and rate limiting middleware.
package middleware

import (
    "context"
    "fmt"
    "net/http"
    "strings"

    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
)

// contextKey avoids collisions when storing values in request contexts.
type contextKey string

const (
	ctxUserIDKey   contextKey = "user_id"
	ctxEmailKey    contextKey = "email"
	ctxUserTypeKey contextKey = "user_type"
)

// AuthMiddleware validates bearer JWTs and injects user identity into the context.
type AuthMiddleware struct {
	jwtSecret string
}

// NewAuthMiddleware constructs an AuthMiddleware with the given secret.
func NewAuthMiddleware(secret string) *AuthMiddleware {
	return &AuthMiddleware{jwtSecret: secret}
}

// Authenticate enforces bearer auth and populates user details on the request context.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        if strings.TrimSpace(authHeader) == "" {
            jsonError(w, http.StatusUnauthorized, "Authorization header required")
            return
        }

        parts := strings.Fields(authHeader)
        if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
            jsonError(w, http.StatusUnauthorized, "Invalid authorization format")
            return
        }
        tokenString := parts[1]

        token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
            if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                return nil, jwt.ErrSignatureInvalid
            }
            return []byte(m.jwtSecret), nil
        })

        if err != nil || !token.Valid {
            jsonError(w, http.StatusUnauthorized, "Invalid token")
            return
        }

        claims, ok := token.Claims.(jwt.MapClaims)
        if !ok {
            jsonError(w, http.StatusUnauthorized, "Invalid token claims")
            return
        }

        userIDStr, ok := claims["user_id"].(string)
        if !ok {
            jsonError(w, http.StatusUnauthorized, "Invalid user ID in token")
            return
        }

        userID, err := uuid.Parse(userIDStr)
        if err != nil {
            jsonError(w, http.StatusUnauthorized, "Invalid user ID format")
            return
        }

		ctx := context.WithValue(r.Context(), ctxUserIDKey, userID)
		if email, ok := claims["email"].(string); ok {
			ctx = context.WithValue(ctx, ctxEmailKey, email)
		}
		if userType, ok := claims["user_type"].(string); ok {
			ctx = context.WithValue(ctx, ctxUserTypeKey, userType)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserIDFromContext returns the authenticated user's UUID from context.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
    v := ctx.Value(ctxUserIDKey)
    id, ok := v.(uuid.UUID)
    return id, ok
}

// EmailFromContext returns the authenticated user's email from context.
func EmailFromContext(ctx context.Context) (string, bool) {
    v := ctx.Value(ctxEmailKey)
    s, ok := v.(string)
    return s, ok
}

// UserTypeFromContext returns the authenticated user's type from context.
func UserTypeFromContext(ctx context.Context) (string, bool) {
    v := ctx.Value(ctxUserTypeKey)
    s, ok := v.(string)
    return s, ok
}

func jsonError(w http.ResponseWriter, status int, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _, _ = w.Write([]byte(fmt.Sprintf(`{"error":"%s"}`, message)))
}

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
