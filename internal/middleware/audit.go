package middleware

import (
	"context"
	"net/http"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/logger"

	"github.com/google/uuid"
)

// AuditRepository defines the interface for persisting audit logs.
type AuditRepository interface {
	Create(ctx context.Context, log *domain.AuditLog) error
}

// AuditMiddleware provides request auditing.
type AuditMiddleware struct {
	repo   AuditRepository
	logger logger.Logger
}

// NewAuditMiddleware creates a new AuditMiddleware.
func NewAuditMiddleware(repo AuditRepository, log logger.Logger) *AuditMiddleware {
	return &AuditMiddleware{repo: repo, logger: log}
}

// Audit records the request in the audit log.
func (m *AuditMiddleware) Audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use the shared responseWriter from logging.go if available, or define locally if needed.
		// Since we are in the same package, we can try to use it.
		// However, safe practice: check if it's already wrapped?
		// Usually middleware wrapping order matters.
		// We'll assume we can use the one from logging.go if it's in the same package.
		
		wrapped, ok := w.(*responseWriter)
		if !ok {
			wrapped = &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		}

		next.ServeHTTP(wrapped, r)

		// Async audit logging
		go func(req *http.Request, status int, ctxUserID interface{}) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var userID *uuid.UUID
			if ctxUserID != nil {
				if id, ok := ctxUserID.(uuid.UUID); ok {
					userID = &id
				}
			}

			ip := req.RemoteAddr
			ua := req.UserAgent()
			method := req.Method
			path := req.URL.Path

			// Ignore health checks
			if path == "/health" || path == "/metrics" {
				return
			}

			logEntry := &domain.AuditLog{
				ID:         uuid.New(),
				UserID:     userID,
				Action:     method + " " + path,
				IPAddress:  &ip,
				UserAgent:  &ua,
				StatusCode: &status,
				CreatedAt:  time.Now(),
			}

			if err := m.repo.Create(ctx, logEntry); err != nil {
				m.logger.Error("Failed to create audit log", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}(r, wrapped.statusCode, r.Context().Value(ctxUserIDKey))
	})
}
