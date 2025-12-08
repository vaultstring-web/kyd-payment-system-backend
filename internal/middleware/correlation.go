// Package middleware provides shared HTTP middleware utilities.
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type correlationKey string

const ctxRequestIDKey correlationKey = "request_id"

// CorrelationID ensures every request has an X-Request-ID for tracing.
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}

		ctx := context.WithValue(r.Context(), ctxRequestIDKey, reqID)
		w.Header().Set("X-Request-ID", reqID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
