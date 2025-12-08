// Package middleware provides shared HTTP middleware utilities.
package middleware

import (
	"net/http"
	"time"

	"kyd/pkg/logger"
)

// LoggingMiddleware records basic request metrics using the provided logger.
type LoggingMiddleware struct {
	logger logger.Logger
}

// NewLoggingMiddleware constructs a LoggingMiddleware.
func NewLoggingMiddleware(log logger.Logger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: log}
}

// Log wraps handlers with structured request/response logging.
func (m *LoggingMiddleware) Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		m.logger.Info("HTTP Request", map[string]interface{}{
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      wrapped.statusCode,
			"duration_ms": time.Since(start).Milliseconds(),
			"ip":          r.RemoteAddr,
			"user_agent":  r.UserAgent(),
		})
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}
