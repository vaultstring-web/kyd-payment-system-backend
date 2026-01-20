// ==============================================================================
// KYC ERROR HANDLING SERVICE - internal/kyc/errors.go
// ==============================================================================
// Comprehensive error handling with HTTP status mapping, rollback mechanisms,
// and structured error responses following Task 5.2.10 requirements
// ==============================================================================

package kyc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kyd/pkg/logger"

	"github.com/google/uuid"
)

// ==============================================================================
// ERROR TYPES AND STRUCTURES
// ==============================================================================

// ErrorCategory represents the category of error for proper handling
type ErrorCategory string

const (
	CategoryValidation      ErrorCategory = "validation"
	CategoryBusinessLogic   ErrorCategory = "business_logic"
	CategoryDatabase        ErrorCategory = "database"
	CategoryExternalService ErrorCategory = "external_service"
	CategoryAuthentication  ErrorCategory = "authentication"
	CategoryAuthorization   ErrorCategory = "authorization"
	CategoryRateLimit       ErrorCategory = "rate_limit"
	CategoryInfrastructure  ErrorCategory = "infrastructure"
	CategoryCompliance      ErrorCategory = "compliance"
)

// ErrorSeverity represents the severity level of the error
type ErrorSeverity string

const (
	SeverityLow      ErrorSeverity = "low"
	SeverityMedium   ErrorSeverity = "medium"
	SeverityHigh     ErrorSeverity = "high"
	SeverityCritical ErrorSeverity = "critical"
)

// StructuredError represents a comprehensive error with all metadata
type StructuredError struct {
	// Basic error information
	Code        string        `json:"code"`
	Message     string        `json:"message"`
	Description string        `json:"description,omitempty"`
	Category    ErrorCategory `json:"category"`
	Severity    ErrorSeverity `json:"severity"`

	// HTTP and system information
	HTTPStatusCode int            `json:"http_status_code"`
	ErrorType      string         `json:"error_type"`
	IsRetryable    bool           `json:"is_retryable"`
	RetryAfter     *time.Duration `json:"retry_after,omitempty"`

	// Context and tracing
	RequestID     string     `json:"request_id,omitempty"`
	CorrelationID string     `json:"correlation_id,omitempty"`
	UserID        *uuid.UUID `json:"user_id,omitempty"`
	ProfileID     *uuid.UUID `json:"profile_id,omitempty"`

	// Additional context
	Field         string            `json:"field,omitempty"`
	Validation    []ValidationError `json:"validation_errors,omitempty"`
	MissingFields []string          `json:"missing_fields,omitempty"`
	MissingDocs   []string          `json:"missing_documents,omitempty"`

	// Stack trace and timing
	StackTrace string    `json:"stack_trace,omitempty"`
	ErrorTime  time.Time `json:"error_time"`
	Operation  string    `json:"operation"`
	Service    string    `json:"service"`

	// Underlying error
	Cause error `json:"-"`
}

func (e *StructuredError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Category, e.Code, e.Message)
}

// Unwrap implements the error unwrapping interface
func (e *StructuredError) Unwrap() error {
	return e.Cause
}

// ToJSON converts the error to JSON for API responses
func (e *StructuredError) ToJSON() []byte {
	jsonData, _ := json.Marshal(e)
	return jsonData
}

// ==============================================================================
// ERROR HANDLER SERVICE
// ==============================================================================

// ErrorHandlerService provides comprehensive error handling capabilities
type ErrorHandlerService struct {
	logger          logger.Logger
	config          *ErrorHandlerConfig
	errorMap        map[string]ErrorMapping
	circuitBreakers map[string]*CircuitBreaker
	retryStrategies map[ErrorCategory]RetryStrategy
}

// ErrorHandlerConfig configuration for error handling
type ErrorHandlerConfig struct {
	EnableDetailedLogging bool          `json:"enable_detailed_logging"`
	LogStackTrace         bool          `json:"log_stack_trace"`
	MaxRetryAttempts      int           `json:"max_retry_attempts"`
	DefaultRetryDelay     time.Duration `json:"default_retry_delay"`
	CircuitBreakerTimeout time.Duration `json:"circuit_breaker_timeout"`
	ErrorReportingEnabled bool          `json:"error_reporting_enabled"`
	ErrorReportingURL     string        `json:"error_reporting_url,omitempty"`
}

// ErrorMapping maps error types to HTTP status codes and categories
type ErrorMapping struct {
	HTTPStatusCode int           `json:"http_status_code"`
	Category       ErrorCategory `json:"category"`
	Severity       ErrorSeverity `json:"severity"`
	IsRetryable    bool          `json:"is_retryable"`
	RetryAfter     time.Duration `json:"retry_after"`
	ErrorCode      string        `json:"error_code"`
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	Name         string
	State        CircuitBreakerState
	FailureCount int
	SuccessCount int
	Threshold    int
	Timeout      time.Duration
	LastFailure  time.Time
	HalfOpenTime *time.Time
}

type CircuitBreakerState string

const (
	CircuitBreakerClosed   CircuitBreakerState = "closed"
	CircuitBreakerOpen     CircuitBreakerState = "open"
	CircuitBreakerHalfOpen CircuitBreakerState = "half_open"
)

// RetryStrategy defines retry behavior for different error categories
type RetryStrategy struct {
	MaxAttempts       int           `json:"max_attempts"`
	InitialDelay      time.Duration `json:"initial_delay"`
	MaxDelay          time.Duration `json:"max_delay"`
	BackoffMultiplier float64       `json:"backoff_multiplier"`
	Jitter            bool          `json:"jitter"`
}

// ==============================================================================
// CONSTRUCTOR AND INITIALIZATION
// ==============================================================================

// NewErrorHandlerService creates a new error handler service
func NewErrorHandlerService(logger logger.Logger, config *ErrorHandlerConfig) *ErrorHandlerService {
	handler := &ErrorHandlerService{
		logger:          logger,
		config:          config,
		errorMap:        make(map[string]ErrorMapping),
		circuitBreakers: make(map[string]*CircuitBreaker),
		retryStrategies: make(map[ErrorCategory]RetryStrategy),
	}

	// Initialize error mappings
	handler.initializeErrorMappings()

	// Initialize retry strategies
	handler.initializeRetryStrategies()

	// Initialize circuit breakers for critical services
	handler.initializeCircuitBreakers()

	return handler
}

// initializeErrorMappings sets up all error mappings
func (h *ErrorHandlerService) initializeErrorMappings() {
	// Validation errors (400 Bad Request)
	h.errorMap["validation_error"] = ErrorMapping{
		HTTPStatusCode: http.StatusBadRequest,
		Category:       CategoryValidation,
		Severity:       SeverityLow,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "VALIDATION_ERROR",
	}

	h.errorMap["invalid_country_code"] = ErrorMapping{
		HTTPStatusCode: http.StatusBadRequest,
		Category:       CategoryValidation,
		Severity:       SeverityLow,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "INVALID_COUNTRY_CODE",
	}

	h.errorMap["underage_user"] = ErrorMapping{
		HTTPStatusCode: http.StatusBadRequest,
		Category:       CategoryValidation,
		Severity:       SeverityMedium,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "UNDERAGE_USER",
	}

	// Business logic errors
	h.errorMap["kyc_profile_exists"] = ErrorMapping{
		HTTPStatusCode: http.StatusConflict,
		Category:       CategoryBusinessLogic,
		Severity:       SeverityMedium,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "KYC_PROFILE_EXISTS",
	}

	h.errorMap["profile_in_review"] = ErrorMapping{
		HTTPStatusCode: http.StatusConflict,
		Category:       CategoryBusinessLogic,
		Severity:       SeverityMedium,
		IsRetryable:    false,
		RetryAfter:     24 * time.Hour,
		ErrorCode:      "PROFILE_IN_REVIEW",
	}

	h.errorMap["insufficient_kyc_level"] = ErrorMapping{
		HTTPStatusCode: http.StatusForbidden,
		Category:       CategoryBusinessLogic,
		Severity:       SeverityHigh,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "INSUFFICIENT_KYC_LEVEL",
	}

	// Authentication and authorization errors
	h.errorMap["unauthorized"] = ErrorMapping{
		HTTPStatusCode: http.StatusUnauthorized,
		Category:       CategoryAuthentication,
		Severity:       SeverityHigh,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "UNAUTHORIZED",
	}

	h.errorMap["forbidden"] = ErrorMapping{
		HTTPStatusCode: http.StatusForbidden,
		Category:       CategoryAuthorization,
		Severity:       SeverityHigh,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "FORBIDDEN",
	}

	// Database errors
	h.errorMap["database_connection_error"] = ErrorMapping{
		HTTPStatusCode: http.StatusServiceUnavailable,
		Category:       CategoryDatabase,
		Severity:       SeverityHigh,
		IsRetryable:    true,
		RetryAfter:     5 * time.Second,
		ErrorCode:      "DATABASE_CONNECTION_ERROR",
	}

	h.errorMap["unique_constraint_violation"] = ErrorMapping{
		HTTPStatusCode: http.StatusConflict,
		Category:       CategoryDatabase,
		Severity:       SeverityMedium,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "UNIQUE_CONSTRAINT_VIOLATION",
	}

	h.errorMap["foreign_key_violation"] = ErrorMapping{
		HTTPStatusCode: http.StatusBadRequest,
		Category:       CategoryDatabase,
		Severity:       SeverityMedium,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "FOREIGN_KEY_VIOLATION",
	}

	h.errorMap["deadlock_detected"] = ErrorMapping{
		HTTPStatusCode: http.StatusConflict,
		Category:       CategoryDatabase,
		Severity:       SeverityHigh,
		IsRetryable:    true,
		RetryAfter:     1 * time.Second,
		ErrorCode:      "DEADLOCK_DETECTED",
	}

	// Transaction errors
	h.errorMap["transaction_failed"] = ErrorMapping{
		HTTPStatusCode: http.StatusInternalServerError,
		Category:       CategoryDatabase,
		Severity:       SeverityHigh,
		IsRetryable:    true,
		RetryAfter:     2 * time.Second,
		ErrorCode:      "TRANSACTION_FAILED",
	}

	h.errorMap["rollback_failed"] = ErrorMapping{
		HTTPStatusCode: http.StatusInternalServerError,
		Category:       CategoryDatabase,
		Severity:       SeverityCritical,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "ROLLBACK_FAILED",
	}

	// External service errors
	h.errorMap["aml_service_unavailable"] = ErrorMapping{
		HTTPStatusCode: http.StatusServiceUnavailable,
		Category:       CategoryExternalService,
		Severity:       SeverityHigh,
		IsRetryable:    true,
		RetryAfter:     10 * time.Second,
		ErrorCode:      "AML_SERVICE_UNAVAILABLE",
	}

	h.errorMap["virus_scan_service_unavailable"] = ErrorMapping{
		HTTPStatusCode: http.StatusServiceUnavailable,
		Category:       CategoryExternalService,
		Severity:       SeverityHigh,
		IsRetryable:    true,
		RetryAfter:     10 * time.Second,
		ErrorCode:      "VIRUS_SCAN_SERVICE_UNAVAILABLE",
	}

	h.errorMap["file_upload_service_error"] = ErrorMapping{
		HTTPStatusCode: http.StatusServiceUnavailable,
		Category:       CategoryExternalService,
		Severity:       SeverityHigh,
		IsRetryable:    true,
		RetryAfter:     5 * time.Second,
		ErrorCode:      "FILE_UPLOAD_SERVICE_ERROR",
	}

	// Not found errors
	h.errorMap["user_not_found"] = ErrorMapping{
		HTTPStatusCode: http.StatusNotFound,
		Category:       CategoryBusinessLogic,
		Severity:       SeverityLow,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "USER_NOT_FOUND",
	}

	h.errorMap["kyc_profile_not_found"] = ErrorMapping{
		HTTPStatusCode: http.StatusNotFound,
		Category:       CategoryBusinessLogic,
		Severity:       SeverityLow,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "KYC_PROFILE_NOT_FOUND",
	}

	h.errorMap["document_not_found"] = ErrorMapping{
		HTTPStatusCode: http.StatusNotFound,
		Category:       CategoryBusinessLogic,
		Severity:       SeverityLow,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "DOCUMENT_NOT_FOUND",
	}

	// Compliance errors
	h.errorMap["aml_check_failed"] = ErrorMapping{
		HTTPStatusCode: http.StatusForbidden,
		Category:       CategoryCompliance,
		Severity:       SeverityCritical,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "AML_CHECK_FAILED",
	}

	h.errorMap["sanction_match"] = ErrorMapping{
		HTTPStatusCode: http.StatusForbidden,
		Category:       CategoryCompliance,
		Severity:       SeverityCritical,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "SANCTION_MATCH",
	}

	h.errorMap["pep_identified"] = ErrorMapping{
		HTTPStatusCode: http.StatusForbidden,
		Category:       CategoryCompliance,
		Severity:       SeverityCritical,
		IsRetryable:    false,
		RetryAfter:     0,
		ErrorCode:      "PEP_IDENTIFIED",
	}

	// Rate limiting
	h.errorMap["rate_limit_exceeded"] = ErrorMapping{
		HTTPStatusCode: http.StatusTooManyRequests,
		Category:       CategoryRateLimit,
		Severity:       SeverityMedium,
		IsRetryable:    true,
		RetryAfter:     60 * time.Second,
		ErrorCode:      "RATE_LIMIT_EXCEEDED",
	}

	// Infrastructure errors
	h.errorMap["internal_server_error"] = ErrorMapping{
		HTTPStatusCode: http.StatusInternalServerError,
		Category:       CategoryInfrastructure,
		Severity:       SeverityCritical,
		IsRetryable:    true,
		RetryAfter:     30 * time.Second,
		ErrorCode:      "INTERNAL_SERVER_ERROR",
	}
}

// initializeRetryStrategies sets up retry strategies for different error categories
func (h *ErrorHandlerService) initializeRetryStrategies() {
	h.retryStrategies[CategoryDatabase] = RetryStrategy{
		MaxAttempts:       3,
		InitialDelay:      100 * time.Millisecond,
		MaxDelay:          5 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
	}

	h.retryStrategies[CategoryExternalService] = RetryStrategy{
		MaxAttempts:       5,
		InitialDelay:      1 * time.Second,
		MaxDelay:          30 * time.Second,
		BackoffMultiplier: 1.5,
		Jitter:            true,
	}

	h.retryStrategies[CategoryInfrastructure] = RetryStrategy{
		MaxAttempts:       2,
		InitialDelay:      5 * time.Second,
		MaxDelay:          30 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
	}
}

// initializeCircuitBreakers initializes circuit breakers for external services
func (h *ErrorHandlerService) initializeCircuitBreakers() {
	h.circuitBreakers["aml_service"] = &CircuitBreaker{
		Name:      "aml_service",
		State:     CircuitBreakerClosed,
		Threshold: 5,
		Timeout:   h.config.CircuitBreakerTimeout,
	}

	h.circuitBreakers["virus_scan_service"] = &CircuitBreaker{
		Name:      "virus_scan_service",
		State:     CircuitBreakerClosed,
		Threshold: 3,
		Timeout:   30 * time.Second,
	}

	h.circuitBreakers["file_upload_service"] = &CircuitBreaker{
		Name:      "file_upload_service",
		State:     CircuitBreakerClosed,
		Threshold: 10,
		Timeout:   60 * time.Second,
	}
}

// ==============================================================================
// ERROR MAPPING AND HANDLING
// ==============================================================================

// MapError maps any error to a structured error with HTTP status
func (h *ErrorHandlerService) MapError(
	err error,
	operation string,
	context map[string]interface{},
) *StructuredError {
	// Default structured error
	structuredErr := &StructuredError{
		Message:   "An unexpected error occurred",
		Code:      "internal_server_error",
		Category:  CategoryInfrastructure,
		Severity:  SeverityCritical,
		Operation: operation,
		Service:   "kyc",
		ErrorTime: time.Now(),
		Cause:     err,
	}

	// Extract context information
	if context != nil {
		if requestID, ok := context["request_id"].(string); ok {
			structuredErr.RequestID = requestID
		}
		if userID, ok := context["user_id"].(uuid.UUID); ok {
			structuredErr.UserID = &userID
		}
		if profileID, ok := context["profile_id"].(uuid.UUID); ok {
			structuredErr.ProfileID = &profileID
		}
	}

	// Check if it's already a StructuredError
	if se, ok := err.(*StructuredError); ok {
		return se
	}

	// Map error message to known error types
	errStr := strings.ToLower(err.Error())

	// Database errors
	if strings.Contains(errStr, "duplicate key") ||
		strings.Contains(errStr, "unique constraint") {
		return h.createStructuredError("unique_constraint_violation", err, operation, context)
	}

	if strings.Contains(errStr, "foreign key") ||
		strings.Contains(errStr, "referential integrity") {
		return h.createStructuredError("foreign_key_violation", err, operation, context)
	}

	if strings.Contains(errStr, "deadlock") {
		return h.createStructuredError("deadlock_detected", err, operation, context)
	}

	if strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "lost connection") {
		return h.createStructuredError("database_connection_error", err, operation, context)
	}

	// Transaction errors
	if strings.Contains(errStr, "transaction") ||
		strings.Contains(errStr, "rollback") ||
		strings.Contains(errStr, "commit") {
		return h.createStructuredError("transaction_failed", err, operation, context)
	}

	// Check for package-specific errors
	if err == sql.ErrNoRows {
		// Determine which entity wasn't found based on context
		if operation == "FindProfileByID" || operation == "FindProfileByUserID" {
			return h.createStructuredError("kyc_profile_not_found", err, operation, context)
		}
		if operation == "FindDocumentByID" {
			return h.createStructuredError("document_not_found", err, operation, context)
		}
		return h.createStructuredError("user_not_found", err, operation, context)
	}

	// Check for custom error types from the errors package
	switch {
	case strings.Contains(errStr, "user not found"):
		return h.createStructuredError("user_not_found", err, operation, context)
	case strings.Contains(errStr, "kyc profile not found"):
		return h.createStructuredError("kyc_profile_not_found", err, operation, context)
	case strings.Contains(errStr, "document not found"):
		return h.createStructuredError("document_not_found", err, operation, context)
	case strings.Contains(errStr, "insufficient balance"):
		// This would be mapped differently, but for KYC we might not see it
	}

	// Default to internal server error with mapping
	if mapping, ok := h.errorMap[structuredErr.Code]; ok {
		structuredErr.HTTPStatusCode = mapping.HTTPStatusCode
		structuredErr.ErrorType = mapping.ErrorCode
		structuredErr.IsRetryable = mapping.IsRetryable
		if mapping.RetryAfter > 0 {
			structuredErr.RetryAfter = &mapping.RetryAfter
		}
	}

	return structuredErr
}

// createStructuredError creates a structured error from an error code
func (h *ErrorHandlerService) createStructuredError(
	errorCode string,
	cause error,
	operation string,
	context map[string]interface{},
) *StructuredError {
	mapping, exists := h.errorMap[errorCode]
	if !exists {
		mapping = h.errorMap["internal_server_error"]
	}

	err := &StructuredError{
		Code:           errorCode,
		Message:        cause.Error(),
		Category:       mapping.Category,
		Severity:       mapping.Severity,
		HTTPStatusCode: mapping.HTTPStatusCode,
		ErrorType:      mapping.ErrorCode,
		IsRetryable:    mapping.IsRetryable,
		Operation:      operation,
		Service:        "kyc",
		ErrorTime:      time.Now(),
		Cause:          cause,
	}

	if mapping.RetryAfter > 0 {
		err.RetryAfter = &mapping.RetryAfter
	}

	// Add context
	if context != nil {
		if requestID, ok := context["request_id"].(string); ok {
			err.RequestID = requestID
		}
		if userID, ok := context["user_id"].(uuid.UUID); ok {
			err.UserID = &userID
		}
		if profileID, ok := context["profile_id"].(uuid.UUID); ok {
			err.ProfileID = &profileID
		}
	}

	return err
}

// HandleError processes an error with appropriate logging and metrics
func (h *ErrorHandlerService) HandleError(
	ctx context.Context,
	err error,
	operation string,
	context map[string]interface{},
) *StructuredError {
	structuredErr := h.MapError(err, operation, context)

	// Log the error based on severity
	h.logError(ctx, structuredErr)

	// Update metrics
	h.updateErrorMetrics(structuredErr)

	// Report to external monitoring if enabled
	if h.config.ErrorReportingEnabled {
		h.reportToMonitoring(structuredErr)
	}

	return structuredErr
}

// ==============================================================================
// TRANSACTION ERROR HANDLING WITH ROLLBACK
// ==============================================================================

// TransactionErrorHandler handles transaction errors with rollback support
type TransactionErrorHandler struct {
	errorHandler *ErrorHandlerService
	logger       logger.Logger
}

// NewTransactionErrorHandler creates a new transaction error handler
func NewTransactionErrorHandler(errorHandler *ErrorHandlerService, logger logger.Logger) *TransactionErrorHandler {
	return &TransactionErrorHandler{
		errorHandler: errorHandler,
		logger:       logger,
	}
}

// ExecuteWithRollback executes a function within a transaction with automatic rollback on error
func (h *TransactionErrorHandler) ExecuteWithRollback(
	ctx context.Context,
	tx Transaction,
	operation string,
	fn func() error,
	rollbackHandlers ...func() error,
) (rollbackErr error) {
	// Execute the function
	err := fn()

	// If no error, return nil
	if err == nil {
		return nil
	}

	// Rollback the transaction
	if tx != nil {
		rollbackErr = tx.Rollback()
		if rollbackErr != nil {
			// Log rollback failure
			h.logger.Error("Failed to rollback transaction", map[string]interface{}{
				"operation":      operation,
				"error":          err.Error(),
				"rollback_error": rollbackErr.Error(),
				"transaction_id": tx.GetID(),
			})
		} else {
			h.logger.Warn("Transaction rolled back due to error", map[string]interface{}{
				"operation":      operation,
				"error":          err.Error(),
				"transaction_id": tx.GetID(),
			})
		}
	}

	// Execute rollback handlers (cleanup functions)
	for _, handler := range rollbackHandlers {
		if handlerErr := handler(); handlerErr != nil {
			h.logger.Warn("Rollback handler failed", map[string]interface{}{
				"operation":     operation,
				"handler_error": handlerErr.Error(),
			})
		}
	}

	return err
}

// ExecuteWithCompensation executes operations with compensation actions on failure
func (h *TransactionErrorHandler) ExecuteWithCompensation(
	ctx context.Context,
	operations []func() error,
	compensations []func() error,
) error {
	if len(operations) != len(compensations) {
		return fmt.Errorf("operations and compensations must have same length")
	}

	var completedOperations []int
	var operationErr error

	// Execute operations in sequence
	for i, operation := range operations {
		if err := operation(); err != nil {
			operationErr = err
			break
		}
		completedOperations = append(completedOperations, i)
	}

	// If error occurred, execute compensations in reverse order
	if operationErr != nil {
		// Execute compensations for completed operations in reverse order
		for j := len(completedOperations) - 1; j >= 0; j-- {
			opIndex := completedOperations[j]
			if compErr := compensations[opIndex](); compErr != nil {
				h.logger.Error("Compensation failed", map[string]interface{}{
					"operation_index": opIndex,
					"error":           compErr.Error(),
					"original_error":  operationErr.Error(),
				})
			}
		}
		return operationErr
	}

	return nil
}

// ==============================================================================
// CIRCUIT BREAKER IMPLEMENTATION
// ==============================================================================

// ExecuteWithCircuitBreaker executes a function with circuit breaker protection
func (h *ErrorHandlerService) ExecuteWithCircuitBreaker(
	circuitBreakerName string,
	fn func() error,
) error {
	cb, exists := h.circuitBreakers[circuitBreakerName]
	if !exists {
		// No circuit breaker for this service, just execute
		return fn()
	}

	// Check circuit breaker state
	switch cb.State {
	case CircuitBreakerOpen:
		// Check if timeout has passed
		if time.Since(cb.LastFailure) > cb.Timeout {
			cb.State = CircuitBreakerHalfOpen
			now := time.Now()
			cb.HalfOpenTime = &now
			h.logger.Info("Circuit breaker moved to half-open state", map[string]interface{}{
				"circuit_breaker": circuitBreakerName,
			})
		} else {
			return fmt.Errorf("circuit breaker is open for %s", circuitBreakerName)
		}

	case CircuitBreakerHalfOpen:
		// Allow one request to test if service is back
		h.logger.Debug("Circuit breaker in half-open state, testing", map[string]interface{}{
			"circuit_breaker": circuitBreakerName,
		})

	case CircuitBreakerClosed:
		// Normal operation
	}

	// Execute the function
	err := fn()

	// Update circuit breaker state
	h.updateCircuitBreaker(cb, err)

	return err
}

// updateCircuitBreaker updates the circuit breaker state based on execution result
func (h *ErrorHandlerService) updateCircuitBreaker(cb *CircuitBreaker, err error) {
	if err == nil {
		// Success
		cb.SuccessCount++
		if cb.State == CircuitBreakerHalfOpen {
			// Success in half-open state, close the circuit breaker
			cb.State = CircuitBreakerClosed
			cb.FailureCount = 0
			cb.SuccessCount = 0
			h.logger.Info("Circuit breaker closed", map[string]interface{}{
				"circuit_breaker": cb.Name,
			})
		}
	} else {
		// Failure
		cb.FailureCount++
		cb.LastFailure = time.Now()

		if cb.State == CircuitBreakerHalfOpen {
			// Failure in half-open state, open again
			cb.State = CircuitBreakerOpen
			h.logger.Warn("Circuit breaker reopened", map[string]interface{}{
				"circuit_breaker": cb.Name,
			})
		} else if cb.FailureCount >= cb.Threshold {
			// Too many failures, open the circuit breaker
			cb.State = CircuitBreakerOpen
			h.logger.Error("Circuit breaker opened", map[string]interface{}{
				"circuit_breaker": cb.Name,
				"failure_count":   cb.FailureCount,
				"threshold":       cb.Threshold,
			})
		}
	}
}

// ==============================================================================
// RETRY MECHANISM
// ==============================================================================

// ExecuteWithRetry executes a function with retry logic
func (h *ErrorHandlerService) ExecuteWithRetry(
	ctx context.Context,
	category ErrorCategory,
	operation string,
	fn func() error,
) error {
	strategy, exists := h.retryStrategies[category]
	if !exists {
		// Use default strategy
		strategy = RetryStrategy{
			MaxAttempts:       h.config.MaxRetryAttempts,
			InitialDelay:      h.config.DefaultRetryDelay,
			MaxDelay:          30 * time.Second,
			BackoffMultiplier: 2.0,
			Jitter:            true,
		}
	}

	var lastErr error
	delay := strategy.InitialDelay

	for attempt := 1; attempt <= strategy.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		structuredErr := h.MapError(err, operation, nil)
		if !structuredErr.IsRetryable {
			return err
		}

		// Log retry attempt
		h.logger.Warn("Retrying operation", map[string]interface{}{
			"operation":     operation,
			"attempt":       attempt,
			"max_attempts":  strategy.MaxAttempts,
			"error":         err.Error(),
			"next_retry_ms": delay.Milliseconds(),
			"category":      string(category),
		})

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// Continue with next attempt
		}

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * strategy.BackoffMultiplier)
		if delay > strategy.MaxDelay {
			delay = strategy.MaxDelay
		}

		// Add jitter if enabled
		if strategy.Jitter {
			delay = h.addJitter(delay)
		}
	}

	return fmt.Errorf("operation '%s' failed after %d retries: %w",
		operation, strategy.MaxAttempts, lastErr)
}

// addJitter adds random jitter to a delay to prevent thundering herd
func (h *ErrorHandlerService) addJitter(delay time.Duration) time.Duration {
	// Add ±10% jitter
	// In production, you would use crypto/rand for better randomness
	// For simplicity, we'll use a pseudo-random approach
	randomFactor := 0.9 + (0.2 * (float64(time.Now().UnixNano()%100) / 100.0))
	return time.Duration(float64(delay) * randomFactor)
}

// ==============================================================================
// LOGGING AND MONITORING
// ==============================================================================

// logError logs an error with appropriate level based on severity
func (h *ErrorHandlerService) logError(_ context.Context, err *StructuredError) {
	logData := map[string]interface{}{
		"error_code":       err.Code,
		"error_type":       err.ErrorType,
		"category":         string(err.Category),
		"severity":         string(err.Severity),
		"http_status_code": err.HTTPStatusCode,
		"operation":        err.Operation,
		"is_retryable":     err.IsRetryable,
		"error_time":       err.ErrorTime.Format(time.RFC3339),
	}

	if err.RequestID != "" {
		logData["request_id"] = err.RequestID
	}
	if err.UserID != nil {
		logData["user_id"] = err.UserID.String()
	}
	if err.ProfileID != nil {
		logData["profile_id"] = err.ProfileID.String()
	}
	if err.RetryAfter != nil {
		logData["retry_after_ms"] = err.RetryAfter.Milliseconds()
	}

	// Add stack trace if enabled
	if h.config.LogStackTrace && err.Cause != nil {
		// In production, use runtime.Caller or errors.WithStack
		logData["stack_trace"] = fmt.Sprintf("%+v", err.Cause)
	}

	// Log at appropriate level based on severity
	switch err.Severity {
	case SeverityCritical, SeverityHigh:
		h.logger.Error(err.Message, logData)
	case SeverityMedium:
		h.logger.Warn(err.Message, logData)
	case SeverityLow:
		h.logger.Info(err.Message, logData)
	}

	// Detailed logging if enabled
	if h.config.EnableDetailedLogging {
		h.logger.Debug("Detailed error information", logData)
	}
}

// updateErrorMetrics updates error metrics for monitoring
func (h *ErrorHandlerService) updateErrorMetrics(err *StructuredError) {
	// In production, you would update metrics here
	// This could be Prometheus metrics, StatsD, or other monitoring systems

	metricsData := map[string]interface{}{
		"error_code":   err.Code,
		"category":     string(err.Category),
		"severity":     string(err.Severity),
		"operation":    err.Operation,
		"is_retryable": err.IsRetryable,
	}

	h.logger.Debug("Error metrics updated", metricsData)
}

// reportToMonitoring reports errors to external monitoring systems
func (h *ErrorHandlerService) reportToMonitoring(err *StructuredError) {
	// In production, this would send to Sentry, DataDog, New Relic, etc.
	if h.config.ErrorReportingURL != "" {
		// Make HTTP POST request to error reporting service
		h.logger.Debug("Error reported to monitoring system", map[string]interface{}{
			"error_code": err.Code,
			"error_type": err.ErrorType,
			"severity":   string(err.Severity),
		})
	}
}

// ==============================================================================
// HTTP ERROR RESPONSE GENERATION
// ==============================================================================

// HTTPErrorResponse represents an error response for HTTP APIs
type HTTPErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
		Details []struct {
			Field   string `json:"field,omitempty"`
			Message string `json:"message,omitempty"`
		} `json:"details,omitempty"`
		RequestID     string `json:"request_id,omitempty"`
		CorrelationID string `json:"correlation_id,omitempty"`
		Timestamp     string `json:"timestamp"`
		Documentation string `json:"documentation,omitempty"`
	} `json:"error"`
}

// CreateHTTPErrorResponse creates an HTTP error response from a structured error
func (h *ErrorHandlerService) CreateHTTPErrorResponse(err *StructuredError) (int, *HTTPErrorResponse) {
	response := &HTTPErrorResponse{}

	response.Error.Code = err.ErrorType
	response.Error.Message = err.Message
	response.Error.Type = string(err.Category)
	response.Error.Timestamp = time.Now().Format(time.RFC3339)
	response.Error.RequestID = err.RequestID
	response.Error.CorrelationID = err.CorrelationID

	// Add validation errors if present
	if len(err.Validation) > 0 {
		for _, validationErr := range err.Validation {
			response.Error.Details = append(response.Error.Details, struct {
				Field   string `json:"field,omitempty"`
				Message string `json:"message,omitempty"`
			}{
				Field:   validationErr.Field,
				Message: validationErr.Message,
			})
		}
	}

	// Add documentation link based on error code
	response.Error.Documentation = h.getErrorDocumentation(err.Code)

	return err.HTTPStatusCode, response
}

// getErrorDocumentation returns documentation URL for an error code
func (h *ErrorHandlerService) getErrorDocumentation(errorCode string) string {
	// In production, this would map to actual documentation URLs
	docBaseURL := "https://docs.example.com/errors/"

	docMap := map[string]string{
		"VALIDATION_ERROR":       docBaseURL + "validation",
		"INVALID_COUNTRY_CODE":   docBaseURL + "country-codes",
		"KYC_PROFILE_EXISTS":     docBaseURL + "kyc-profile-management",
		"AML_CHECK_FAILED":       docBaseURL + "aml-compliance",
		"SANCTION_MATCH":         docBaseURL + "sanctions-compliance",
		"PEP_IDENTIFIED":         docBaseURL + "pep-screening",
		"INSUFFICIENT_KYC_LEVEL": docBaseURL + "kyc-levels",
		"RATE_LIMIT_EXCEEDED":    docBaseURL + "rate-limiting",
		"INTERNAL_SERVER_ERROR":  docBaseURL + "server-errors",
	}

	if url, exists := docMap[errorCode]; exists {
		return url
	}

	return docBaseURL
}

// ==============================================================================
// ERROR RECOVERY AND GRACEFUL DEGRADATION
// ==============================================================================

// FallbackResult represents a fallback result when primary operation fails
type FallbackResult struct {
	Data      interface{} `json:"data"`
	FromCache bool        `json:"from_cache"`
	Degraded  bool        `json:"degraded"`
	Message   string      `json:"message,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// ExecuteWithFallback executes a primary function with fallback options
func (h *ErrorHandlerService) ExecuteWithFallback(
	ctx context.Context,
	primary func() (interface{}, error),
	fallbacks ...func() (interface{}, error),
) (interface{}, error) {
	// Try primary function
	result, err := primary()
	if err == nil {
		return result, nil
	}

	// Log primary failure
	structuredErr := h.HandleError(ctx, err, "primary_operation", nil)
	h.logger.Warn("Primary operation failed, trying fallbacks", map[string]interface{}{
		"error_code":     structuredErr.Code,
		"fallback_count": len(fallbacks),
	})

	// Try fallbacks in order
	for i, fallback := range fallbacks {
		fallbackResult, fallbackErr := fallback()
		if fallbackErr == nil {
			h.logger.Info("Fallback succeeded", map[string]interface{}{
				"fallback_index": i,
			})

			// Wrap result to indicate it's from fallback
			return &FallbackResult{
				Data:      fallbackResult,
				FromCache: i == 0, // Assuming first fallback is cache
				Degraded:  true,
				Message:   "Service operating in degraded mode",
				Error:     structuredErr.Message,
			}, nil
		}

		h.logger.Warn("Fallback failed", map[string]interface{}{
			"fallback_index": i,
			"error":          fallbackErr.Error(),
		})
	}

	// All fallbacks failed
	return nil, fmt.Errorf("all operations failed, last error: %w", err)
}

// HealthCheckResult represents the health status of a service
type HealthCheckResult struct {
	Service     string            `json:"service"`
	Status      string            `json:"status"` // "healthy", "degraded", "unhealthy"
	Message     string            `json:"message,omitempty"`
	Checks      map[string]string `json:"checks,omitempty"`
	LastChecked time.Time         `json:"last_checked"`
}

// CheckServiceHealth checks the health of a service with fallback options
func (h *ErrorHandlerService) CheckServiceHealth(
	ctx context.Context,
	serviceName string,
	primaryCheck func() error,
	fallbackChecks ...func() error,
) *HealthCheckResult {
	result := &HealthCheckResult{
		Service:     serviceName,
		LastChecked: time.Now(),
		Checks:      make(map[string]string),
	}

	// Try primary check
	if err := primaryCheck(); err != nil {
		result.Checks["primary"] = fmt.Sprintf("failed: %v", err)

		// Try fallback checks
		for i, fallbackCheck := range fallbackChecks {
			if fallbackErr := fallbackCheck(); fallbackErr != nil {
				result.Checks[fmt.Sprintf("fallback_%d", i)] = fmt.Sprintf("failed: %v", fallbackErr)
			} else {
				result.Checks[fmt.Sprintf("fallback_%d", i)] = "passed"
				result.Status = "degraded"
				result.Message = "Service is operating with reduced functionality"
				return result
			}
		}

		// All checks failed
		result.Status = "unhealthy"
		result.Message = fmt.Sprintf("Service is unavailable: %v", err)
	} else {
		result.Checks["primary"] = "passed"
		result.Status = "healthy"
	}

	return result
}
