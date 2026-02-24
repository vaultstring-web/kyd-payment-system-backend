// Package middleware hosts authentication, logging, and rate limiting middleware.
package middleware

import (
	"context"
	"encoding/json"
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

// TokenBlacklist defines the interface for checking revoked tokens.
type TokenBlacklist interface {
	IsBlacklisted(ctx context.Context, token string) (bool, error)
}

type UserStatusChecker interface {
	IsUserActive(ctx context.Context, id uuid.UUID) (bool, error)
}

// AuthMiddleware validates bearer JWTs and injects user identity into the context.
type AuthMiddleware struct {
	jwtSecret     string
	jwtSecrets    []string
	blacklist     TokenBlacklist
	statusChecker UserStatusChecker
}

// NewAuthMiddleware constructs an AuthMiddleware with the given secret and optional blacklist.
func NewAuthMiddleware(secret string, blacklist TokenBlacklist) *AuthMiddleware {
	return NewAuthMiddlewareWithUserStatus(secret, blacklist, nil)
}

func NewAuthMiddlewareWithUserStatus(secret string, blacklist TokenBlacklist, statusChecker UserStatusChecker) *AuthMiddleware {
	return &AuthMiddleware{
		jwtSecret:     secret,
		statusChecker: statusChecker,
		blacklist:     blacklist,
	}
}

// Authenticate enforces bearer auth and populates user details on the request context.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if strings.TrimSpace(authHeader) == "" {
			respondJSONError(w, http.StatusUnauthorized, "Authorization header required")
			return
		}

		parts := strings.Fields(authHeader)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			respondJSONError(w, http.StatusUnauthorized, "Invalid authorization format")
			return
		}
		tokenString := parts[1]

		// Check blacklist if configured
		if m.blacklist != nil {
			revoked, err := m.blacklist.IsBlacklisted(r.Context(), tokenString)
			if err != nil {
				// Fail secure
				respondJSONError(w, http.StatusServiceUnavailable, "Authentication service unavailable")
				return
			}
			if revoked {
				respondJSONError(w, http.StatusUnauthorized, "Token revoked")
				return
			}
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			if m.jwtSecret != "" {
				return []byte(m.jwtSecret), nil
			}
			if len(m.jwtSecrets) > 0 {
				return []byte(m.jwtSecrets[0]), nil
			}
			return nil, jwt.ErrSignatureInvalid
		})

		if err != nil || !token.Valid {
			fmt.Printf("AuthMiddleware: Invalid token: %v\n", err)
			respondJSONError(w, http.StatusUnauthorized, "Invalid token")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			fmt.Printf("AuthMiddleware: Failed to parse token claims\n")
			respondJSONError(w, http.StatusUnauthorized, "Invalid token claims")
			return
		}

		userIDStr, ok := claims["user_id"].(string)
		if !ok {
			fmt.Printf("AuthMiddleware: user_id claim missing or not string: %v\n", claims["user_id"])
			respondJSONError(w, http.StatusUnauthorized, "Invalid user ID in token")
			return
		}

		fmt.Printf("AuthMiddleware: Extracted user_id from token: %s\n", userIDStr)

		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			fmt.Printf("AuthMiddleware: Failed to parse user_id as UUID: %s, error: %v\n", userIDStr, err)
			respondJSONError(w, http.StatusUnauthorized, "Invalid user ID format")
			return
		}

		fmt.Printf("AuthMiddleware: Successfully parsed user ID: %s\n", userID.String())

		if m.statusChecker != nil {
			active, err := m.statusChecker.IsUserActive(r.Context(), userID)
			if err != nil {
				fmt.Printf("AuthMiddleware: Failed to check user status: %v\n", err)
				respondJSONError(w, http.StatusInternalServerError, "Failed to verify account status")
				return
			}
			if !active {
				fmt.Printf("AuthMiddleware: User account is not active: %s\n", userID.String())
				respondJSONError(w, http.StatusForbidden, "Account is blocked")
				return
			}
		}

		ctx := context.WithValue(r.Context(), ctxUserIDKey, userID)
		fmt.Printf("AuthMiddleware: Injected user ID into context: %s\n", userID.String())
		if email, ok := claims["email"].(string); ok {
			ctx = context.WithValue(ctx, ctxEmailKey, email)
			fmt.Printf("AuthMiddleware: Injected email into context: %s\n", email)
		}
		if utRaw, ok := claims["user_type"]; ok {
			ctx = context.WithValue(ctx, ctxUserTypeKey, fmt.Sprintf("%v", utRaw))
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserIDFromContext extracts the user ID from the request context.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(ctxUserIDKey).(uuid.UUID)
	return userID, ok
}

// UserTypeFromContext extracts the user type from the request context.
func UserTypeFromContext(ctx context.Context) (string, bool) {
	ut, ok := ctx.Value(ctxUserTypeKey).(string)
	return ut, ok
}

func respondJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func CORS(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// Allow localhost for development and specific production domains
		if origin != "" && (strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "http://127.0.0.1") ||
			strings.HasSuffix(origin, ".kyd.com")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Correlation-ID")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
