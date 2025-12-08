// Package middleware provides shared HTTP middleware utilities.
package middleware

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RateLimiter applies a fixed-window rate limit backed by Redis.
type RateLimiter struct {
	cache  *redis.Client
	limit  int
	window time.Duration
}

// NewRateLimiter constructs a RateLimiter with the given limit and window.
func NewRateLimiter(cache *redis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		cache:  cache,
		limit:  limit,
		window: window,
	}
}

// Limit enforces the rate limit, keyed by client IP and, when available, user ID.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		}

		userID, _ := r.Context().Value(ctxUserIDKey).(uuid.UUID)
		key := fmt.Sprintf("ratelimit:%s", ip)
		if userID != uuid.Nil {
			key = fmt.Sprintf("ratelimit:%s:%s", ip, userID.String())
		}

		count, err := rl.cache.Incr(r.Context(), key).Result()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if count == 1 {
			if err := rl.cache.Expire(r.Context(), key, rl.window).Err(); err != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}

		if count > int64(rl.limit) {
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.limit))
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", rl.limit-int(count)))

		next.ServeHTTP(w, r)
	})
}
