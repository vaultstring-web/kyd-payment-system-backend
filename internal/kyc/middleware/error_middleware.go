// ==============================================================================
// ERROR HANDLING MIDDLEWARE - internal/kyc/middleware/error_middleware.go
// ==============================================================================
// HTTP middleware for error handling
// ==============================================================================

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"kyd/internal/kyc"
	"kyd/pkg/logger"

	"github.com/google/uuid"
)

// ErrorHandlingMiddleware provides HTTP error handling
type ErrorHandlingMiddleware struct {
	errorHandler *kyc.ErrorHandlerService
	logger       logger.Logger
}

// NewErrorHandlingMiddleware creates new error handling middleware
func NewErrorHandlingMiddleware(
	errorHandler *kyc.ErrorHandlerService,
	logger logger.Logger,
) *ErrorHandlingMiddleware {
	return &ErrorHandlingMiddleware{
		errorHandler: errorHandler,
		logger:       logger,
	}
}

// Handler wraps HTTP handlers with error handling
func (m *ErrorHandlingMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		requestID := uuid.New().String()

		// Create context with request ID
		ctx := context.WithValue(r.Context(), "request_id", requestID)
		r = r.WithContext(ctx)

		// Add request ID to response header
		w.Header().Set("X-Request-ID", requestID)

		// Create a custom response writer to capture status code
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Recover from panics
		defer func() {
			if rec := recover(); rec != nil {
				m.handlePanic(rw, r, rec, requestID, startTime)
			}
		}()

		// Execute the handler
		next.ServeHTTP(rw, r)

		// Log request completion
		duration := time.Since(startTime)
		m.logRequest(rw, r, requestID, duration)

		// Handle non-2xx status codes
		if rw.statusCode >= 400 {
			m.handleErrorResponse(rw, r, requestID, startTime)
		}
	})
}

// handlePanic handles panics in HTTP handlers
func (m *ErrorHandlingMiddleware) handlePanic(
	w http.ResponseWriter,
	r *http.Request,
	rec interface{},
	requestID string,
	startTime time.Time,
) {
	// Create structured error from panic
	err := &kyc.StructuredError{
		Code:           "panic",
		Message:        "Internal server error",
		Category:       kyc.CategoryInfrastructure,
		Severity:       kyc.SeverityCritical,
		HTTPStatusCode: http.StatusInternalServerError,
		ErrorType:      "PANIC",
		RequestID:      requestID,
		Operation:      r.Method + " " + r.URL.Path,
		ErrorTime:      time.Now(),
	}

	// Log the panic
	m.logger.Error("HTTP handler panic", map[string]interface{}{
		"request_id":  requestID,
		"method":      r.Method,
		"path":        r.URL.Path,
		"panic":       rec,
		"duration_ms": time.Since(startTime).Milliseconds(),
	})

	// Send error response
	m.sendErrorResponse(w, err)
}

// handleErrorResponse handles error responses
func (m *ErrorHandlingMiddleware) handleErrorResponse(
	rw *responseWriter,
	r *http.Request,
	requestID string,
	startTime time.Time,
) {
	// Log error response
	m.logger.Warn("HTTP error response", map[string]interface{}{
		"request_id":  requestID,
		"method":      r.Method,
		"path":        r.URL.Path,
		"status_code": rw.statusCode,
		"duration_ms": time.Since(startTime).Milliseconds(),
		"user_agent":  r.UserAgent(),
		"client_ip":   r.RemoteAddr,
	})
}

// logRequest logs request completion
func (m *ErrorHandlingMiddleware) logRequest(
	rw *responseWriter,
	r *http.Request,
	requestID string,
	duration time.Duration,
) {
	logData := map[string]interface{}{
		"request_id":  requestID,
		"method":      r.Method,
		"path":        r.URL.Path,
		"status_code": rw.statusCode,
		"duration_ms": duration.Milliseconds(),
		"user_agent":  r.UserAgent(),
	}

	// Log at appropriate level based on status code and duration
	if rw.statusCode >= 500 {
		m.logger.Error("HTTP 5xx response", logData)
	} else if rw.statusCode >= 400 {
		m.logger.Warn("HTTP 4xx response", logData)
	} else if duration > 2*time.Second {
		m.logger.Warn("Slow HTTP request", logData)
	} else {
		m.logger.Info("HTTP request completed", logData)
	}
}

// sendErrorResponse sends an error response in JSON format
func (m *ErrorHandlingMiddleware) sendErrorResponse(
	w http.ResponseWriter,
	err *kyc.StructuredError,
) {
	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Set status code
	w.WriteHeader(err.HTTPStatusCode)

	// Create and send error response
	statusCode, response := m.errorHandler.CreateHTTPErrorResponse(err)

	// Override status code from response
	w.WriteHeader(statusCode)

	// Encode and send response
	json.NewEncoder(w).Encode(response)
}

// responseWriter is a custom response writer that captures status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}
