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

// slidingWindowScript implements the sliding window algorithm atomically.
// KEYS[1]: Rate limit key
// ARGV[1]: Current timestamp (ms)
// ARGV[2]: Window duration (ms)
// ARGV[3]: Max requests limit
// ARGV[4]: Unique request ID (member)
var slidingWindowScript = redis.NewScript(`
	local key = KEYS[1]
	local now = tonumber(ARGV[1])
	local window = tonumber(ARGV[2])
	local limit = tonumber(ARGV[3])
	local member = ARGV[4]

	-- Remove requests older than the window
	redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)

	-- Count requests in the current window
	local count = redis.call('ZCARD', key)

	if count >= limit then
		return -1
	end

	-- Add current request
	redis.call('ZADD', key, now, member)
	redis.call('PEXPIRE', key, window)

	return count + 1
`)

// RateLimiter applies a sliding-window rate limit backed by Redis with adaptive blocking.
type RateLimiter struct {
	cache        *redis.Client
	limit        int
	window       time.Duration
	banThreshold int
	banDuration  time.Duration
}

// NewRateLimiter constructs a RateLimiter with the given limit and window.
func NewRateLimiter(cache *redis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		cache:        cache,
		limit:        limit,
		window:       window,
		banThreshold: 5,                // Default: 5 violations triggers ban
		banDuration:  15 * time.Minute, // Default: 15 minute ban
	}
}

// WithAdaptive configures the adaptive blocking parameters.
func (rl *RateLimiter) WithAdaptive(threshold int, duration time.Duration) *RateLimiter {
	rl.banThreshold = threshold
	rl.banDuration = duration
	return rl
}

// Limit enforces the rate limit, keyed by client IP and, when available, user ID.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		}

		userID, _ := r.Context().Value(ctxUserIDKey).(uuid.UUID)
		baseKey := fmt.Sprintf("ratelimit:%s", ip)
		if userID != uuid.Nil {
			baseKey = fmt.Sprintf("ratelimit:%s:%s", ip, userID.String())
		}

		// 1. Check if user is banned
		banKey := fmt.Sprintf("%s:ban", baseKey)
		if rl.cache.Exists(r.Context(), banKey).Val() > 0 {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", rl.banDuration.Seconds()))
			http.Error(w, "Too many requests. Temporary ban active.", http.StatusTooManyRequests)
			return
		}

		// 2. Apply Sliding Window Limit
		now := time.Now().UnixMilli()
		windowMs := rl.window.Milliseconds()
		reqID := uuid.New().String()

		result, err := slidingWindowScript.Run(r.Context(), rl.cache, []string{baseKey}, now, windowMs, rl.limit, reqID).Int()
		if err != nil && err != redis.Nil {
			// Fail open to avoid blocking legitimate users on redis error
			next.ServeHTTP(w, r)
			return
		}

		// 3. Check limit result
		if result == -1 {
			// Increment violation counter
			violationKey := fmt.Sprintf("%s:violations", baseKey)
			vCount, _ := rl.cache.Incr(r.Context(), violationKey).Result()
			if vCount == 1 {
				rl.cache.Expire(r.Context(), violationKey, rl.window*10) // Keep violation history longer
			}

			// Check if we should ban
			if vCount >= int64(rl.banThreshold) {
				rl.cache.Set(r.Context(), banKey, "banned", rl.banDuration)
				rl.cache.Del(r.Context(), violationKey) // Reset violations after ban
			}

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.limit))
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", rl.limit-result))

		next.ServeHTTP(w, r)
	})
}
