// Package middleware provides shared HTTP middleware utilities.
// internal/middleware/idempotency.go
package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// IdempotencyMiddleware enforces Idempotency-Key usage for unsafe methods.
type IdempotencyMiddleware struct {
	cache *redis.Client
	ttl   time.Duration
}

// NewIdempotencyMiddleware constructs an IdempotencyMiddleware with a TTL.
func NewIdempotencyMiddleware(cache *redis.Client, ttl time.Duration) *IdempotencyMiddleware {
	return &IdempotencyMiddleware{
		cache: cache,
		ttl:   ttl,
	}
}

// Require blocks duplicate POST/PUT/PATCH/DELETE requests with the same key.
// It expects the header: Idempotency-Key.
func (m *IdempotencyMiddleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut &&
			r.Method != http.MethodPatch && r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			http.Error(w, "Idempotency-Key header required", http.StatusBadRequest)
			return
		}

		dataKey := fmt.Sprintf("idempotency:data:%s:%s", r.Method, key)
		lockKey := fmt.Sprintf("idempotency:lock:%s:%s", r.Method, key)

		// Fast path: cached response exists
		if handled := m.replayCached(w, r, dataKey); handled {
			return
		}

		// Acquire lock to process the first request
		ok, err := m.cache.SetNX(r.Context(), lockKey, "1", m.ttl).Result()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if !ok {
			// Another request in-flight; check if cached now, otherwise signal conflict
			if handled := m.replayCached(w, r, dataKey); handled {
				return
			}
			http.Error(w, "Duplicate request", http.StatusConflict)
			return
		}
		defer m.cache.Del(r.Context(), lockKey)

		// Capture response
		cw := newCaptureWriter(w, 1<<20) // 1MB cap
		next.ServeHTTP(cw, r)

		// Cache response for future identical requests (ignore cache errors)
		_ = m.cacheResponse(r, dataKey, cw)
	})
}

type capturedResponse struct {
	Status  int               `json:"status"`
	Body    []byte            `json:"body"`
	Headers map[string]string `json:"headers"`
}

func (m *IdempotencyMiddleware) replayCached(w http.ResponseWriter, r *http.Request, dataKey string) bool {
	payload, err := m.cache.Get(r.Context(), dataKey).Bytes()
	if err != nil {
		return false
	}

	var cr capturedResponse
	if err := json.Unmarshal(payload, &cr); err != nil {
		return false
	}

	for k, v := range cr.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(cr.Status)
	_, _ = w.Write(cr.Body)
	return true
}

func (m *IdempotencyMiddleware) cacheResponse(r *http.Request, dataKey string, cw *captureWriter) error {
	// Do not cache empty or oversized responses
	if cw.status == 0 || len(cw.buf) == 0 {
		return nil
	}

	resp := capturedResponse{
		Status:  cw.status,
		Body:    cw.buf,
		Headers: cw.headers,
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	// Set with TTL
	return m.cache.Set(r.Context(), dataKey, payload, m.ttl).Err()
}

type captureWriter struct {
	http.ResponseWriter
	buf     []byte
	limit   int
	status  int
	headers map[string]string
}

func newCaptureWriter(w http.ResponseWriter, limit int) *captureWriter {
	return &captureWriter{
		ResponseWriter: w,
		buf:            make([]byte, 0, 1024),
		limit:          limit,
		headers:        make(map[string]string),
	}
}

func (w *captureWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *captureWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	for k, v := range w.ResponseWriter.Header() {
		if len(v) > 0 {
			w.headers[k] = v[0]
		}
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if len(w.buf) < w.limit {
		space := w.limit - len(w.buf)
		if space > 0 {
			toCopy := len(p)
			if toCopy > space {
				toCopy = space
			}
			w.buf = append(w.buf, p[:toCopy]...)
		}
	}
	return w.ResponseWriter.Write(p)
}
