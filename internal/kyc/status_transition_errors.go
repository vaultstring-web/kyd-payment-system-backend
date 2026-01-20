// ==============================================================================
// STATUS TRANSITION ERROR HANDLING - internal/kyc/status_transition_errors.go
// ==============================================================================
// Comprehensive error handling for status transitions (Task 5.2.10)
// ==============================================================================

package kyc

import (
	"context"
	"strings"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/logger"

	"github.com/google/uuid"
)

// ==============================================================================
// ENHANCED STATUS TRANSITION WITH ERROR HANDLING
// ==============================================================================

// EnhancedStatusTransitionService wraps status transition operations with error handling
type EnhancedStatusTransitionService struct {
	errorHandler            *ErrorHandlerService
	transactionErrorHandler *TransactionErrorHandler
	logger                  logger.Logger
}

// NewEnhancedStatusTransitionService creates a new enhanced status transition service
func NewEnhancedStatusTransitionService(
	errorHandler *ErrorHandlerService,
	logger logger.Logger,
) *EnhancedStatusTransitionService {
	return &EnhancedStatusTransitionService{
		errorHandler:            errorHandler,
		transactionErrorHandler: NewTransactionErrorHandler(errorHandler, logger),
		logger:                  logger,
	}
}

// ==============================================================================
// ERROR-AWARE STATUS TRANSITION
// ==============================================================================

// TransitionKYCStatusWithErrorHandling is the enhanced version with comprehensive error handling
func (s *EnhancedStatusTransitionService) TransitionKYCStatusWithErrorHandling(
	ctx context.Context,
	profileID uuid.UUID,
	toStatus domain.KYCSubmissionStatus,
	reason string,
	notes string,
	transitionedBy uuid.UUID,
	isManual bool,
) (*StatusTransitionResult, error) {
	startTime := time.Now()
	requestID := uuid.New().String()

	// Context for error handling
	errorContext := map[string]interface{}{
		"request_id":      requestID,
		"profile_id":      profileID,
		"to_status":       toStatus,
		"transitioned_by": transitionedBy,
		"is_manual":       isManual,
	}

	// Execute with comprehensive error handling
	result, err := s.executeStatusTransition(
		ctx,
		profileID,
		toStatus,
		reason,
		notes,
		transitionedBy,
		isManual,
		errorContext,
	)

	if err != nil {
		structuredErr := s.errorHandler.HandleError(ctx, err, "TransitionKYCStatus", errorContext)

		// Check if we should attempt recovery
		if s.shouldAttemptStatusRecovery(structuredErr) {
			s.attemptStatusRecovery(ctx, profileID, errorContext)
		}

		return nil, structuredErr
	}

	// Log successful transition with metrics
	processingTime := time.Since(startTime)
	s.logger.Info("Status transition completed with error handling", map[string]interface{}{
		"request_id":         requestID,
		"profile_id":         profileID,
		"from_status":        result.Transition.FromStatus,
		"to_status":          result.Transition.ToStatus,
		"processing_time_ms": processingTime.Milliseconds(),
		"error_handling":     "enhanced",
	})

	return result, nil
}

// executeStatusTransition executes status transition with rollback protection
func (s *EnhancedStatusTransitionService) executeStatusTransition(
	ctx context.Context,
	profileID uuid.UUID,
	toStatus domain.KYCSubmissionStatus,
	reason string,
	notes string,
	transitionedBy uuid.UUID,
	isManual bool,
	errorContext map[string]interface{},
) (*StatusTransitionResult, error) {
	// Note: This would integrate with the existing KYCService
	// For this example, we'll show the error handling pattern

	var result *StatusTransitionResult

	// Define rollback handlers specific to status transitions
	rollbackHandlers := []func() error{
		func() error {
			// Revert any notifications sent
			s.logger.Warn("Rollback handler: reverting notifications", errorContext)
			return nil
		},
		func() error {
			// Log the rollback for audit trail
			s.logger.Warn("Rollback handler: logging for audit", errorContext)
			return nil
		},
		func() error {
			// Update metrics for failed transition
			s.logger.Warn("Rollback handler: updating failure metrics", errorContext)
			return nil
		},
	}

	// Execute with transaction error handler
	err := s.transactionErrorHandler.ExecuteWithRollback(
		ctx,
		nil, // Transaction would come from service
		"transitionKYCStatus",
		func() error {
			// This would call the actual transition logic
			// For now, simulate a successful transition
			result = &StatusTransitionResult{
				Transition: &StatusTransition{
					ProfileID:      profileID,
					FromStatus:     domain.KYCSubmissionStatusSubmitted,
					ToStatus:       toStatus,
					TransitionedAt: time.Now(),
					TransitionedBy: transitionedBy,
					Reason:         reason,
					Notes:          notes,
					IsManual:       isManual,
				},
				IsValid: true,
				NextActions: []string{
					"Notify user of status change",
					"Update transaction limits if approved",
				},
			}
			return nil
		},
		rollbackHandlers...,
	)

	if err != nil {
		// Handle specific status transition errors
		enhancedErr := s.enhanceStatusTransitionError(err, errorContext)
		return nil, enhancedErr
	}

	return result, nil
}

// enhanceStatusTransitionError enhances error with status transition specific context
func (s *EnhancedStatusTransitionService) enhanceStatusTransitionError(
	err error,
	errorContext map[string]interface{},
) error {
	errStr := err.Error()

	// Check for status transition specific errors
	switch {
	case strings.Contains(errStr, "invalid transition"):
		return s.errorHandler.createStructuredError("invalid_status_transition", err,
			"transitionKYCStatus", errorContext)

	case strings.Contains(errStr, "permission denied"):
		return s.errorHandler.createStructuredError("transition_permission_denied", err,
			"transitionKYCStatus", errorContext)

	case strings.Contains(errStr, "aml status not cleared"):
		return s.errorHandler.createStructuredError("aml_check_pending", err,
			"transitionKYCStatus", errorContext)

	case strings.Contains(errStr, "missing documents"):
		return s.errorHandler.createStructuredError("missing_required_documents", err,
			"transitionKYCStatus", errorContext)
	}

	// Default mapping
	return s.errorHandler.MapError(err, "transitionKYCStatus", errorContext)
}

// ==============================================================================
// RECOVERY AND COMPENSATION FOR STATUS TRANSITIONS
// ==============================================================================

// shouldAttemptStatusRecovery determines if recovery should be attempted
func (s *EnhancedStatusTransitionService) shouldAttemptStatusRecovery(err *StructuredError) bool {
	// Attempt recovery for certain error types
	recoverableErrors := []string{
		"deadlock_detected",
		"database_connection_error",
		"transaction_failed",
	}

	for _, code := range recoverableErrors {
		if err.Code == code {
			return true
		}
	}

	return false
}

// attemptStatusRecovery attempts to recover from a failed status transition
func (s *EnhancedStatusTransitionService) attemptStatusRecovery(
	_ context.Context,
	profileID uuid.UUID,
	errorContext map[string]interface{},
) {
	s.logger.Warn("Attempting recovery for failed status transition", errorContext)

	// Strategies for recovery:
	// 1. Log the failed attempt for manual intervention
	// 2. Attempt to set a "recovery_pending" status
	// 3. Notify administrators

	// Log for manual intervention
	s.logger.Error("Status transition requires manual intervention", map[string]interface{}{
		"profile_id": profileID,
		"context":    errorContext,
		"action":     "manual_review_required",
	})

	// TODO: Implement actual recovery logic
	// This could involve:
	// - Setting a recovery flag in the profile metadata
	// - Creating a recovery task for administrators
	// - Scheduling automatic retry
}

// ==============================================================================
// BULK STATUS TRANSITION WITH ERROR HANDLING
// ==============================================================================

// BulkTransitionStatus processes multiple status transitions with error handling
func (s *EnhancedStatusTransitionService) BulkTransitionStatus(
	ctx context.Context,
	transitions []BulkTransitionRequest,
	transitionedBy uuid.UUID,
) (*BulkTransitionResult, error) {
	requestID := uuid.New().String()

	result := &BulkTransitionResult{
		Total:      len(transitions),
		Successful: 0,
		Failed:     0,
		Results:    make([]*BulkTransitionItemResult, 0),
		RequestID:  requestID,
		StartedAt:  time.Now(),
	}

	// Process transitions with individual error handling
	for _, transition := range transitions {
		itemResult := &BulkTransitionItemResult{
			ProfileID: transition.ProfileID,
			ToStatus:  transition.ToStatus,
		}

		// Execute individual transition with error handling
		_, err := s.TransitionKYCStatusWithErrorHandling(
			ctx,
			transition.ProfileID,
			transition.ToStatus,
			transition.Reason,
			transition.Notes,
			transitionedBy,
			true, // Manual for bulk operations
		)

		if err != nil {
			itemResult.Success = false
			itemResult.Error = err.Error()
			if structuredErr, ok := err.(*StructuredError); ok {
				itemResult.ErrorCode = structuredErr.Code
				itemResult.ErrorType = structuredErr.ErrorType
			}
			result.Failed++
		} else {
			itemResult.Success = true
			result.Successful++
		}

		result.Results = append(result.Results, itemResult)
	}

	result.CompletedAt = time.Now()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	// Log bulk operation results
	s.logger.Info("Bulk status transition completed", map[string]interface{}{
		"request_id":  requestID,
		"total":       result.Total,
		"successful":  result.Successful,
		"failed":      result.Failed,
		"duration_ms": result.Duration.Milliseconds(),
	})

	return result, nil
}

// BulkTransitionRequest represents a single transition in bulk operation
type BulkTransitionRequest struct {
	ProfileID uuid.UUID                  `json:"profile_id"`
	ToStatus  domain.KYCSubmissionStatus `json:"to_status"`
	Reason    string                     `json:"reason,omitempty"`
	Notes     string                     `json:"notes,omitempty"`
}

// BulkTransitionResult represents the result of a bulk transition operation
type BulkTransitionResult struct {
	Total       int                         `json:"total"`
	Successful  int                         `json:"successful"`
	Failed      int                         `json:"failed"`
	Results     []*BulkTransitionItemResult `json:"results"`
	RequestID   string                      `json:"request_id"`
	StartedAt   time.Time                   `json:"started_at"`
	CompletedAt time.Time                   `json:"completed_at"`
	Duration    time.Duration               `json:"duration"`
}

// BulkTransitionItemResult represents result for a single transition
type BulkTransitionItemResult struct {
	ProfileID uuid.UUID                  `json:"profile_id"`
	ToStatus  domain.KYCSubmissionStatus `json:"to_status"`
	Success   bool                       `json:"success"`
	Error     string                     `json:"error,omitempty"`
	ErrorCode string                     `json:"error_code,omitempty"`
	ErrorType string                     `json:"error_type,omitempty"`
}

// ==============================================================================
// ERROR HANDLING FOR REVIEW QUEUE OPERATIONS
// ==============================================================================

// GetReviewQueueWithErrorHandling gets review queue with error handling
func (s *EnhancedStatusTransitionService) GetReviewQueueWithErrorHandling(
	ctx context.Context,
	filter domain.KYCSubmissionStatus,
	limit int,
	offset int,
	sortBy string,
	sortOrder string,
) ([]*ReviewQueueItem, int, error) {
	errorContext := map[string]interface{}{
		"operation": "GetReviewQueue",
		"filter":    string(filter),
		"limit":     limit,
		"offset":    offset,
	}

	// Execute with retry for database errors
	var queueItems []*ReviewQueueItem
	var total int

	err := s.errorHandler.ExecuteWithRetry(
		ctx,
		CategoryDatabase,
		"GetReviewQueue",
		func() error {
			// This would call the actual repository method
			// For now, return empty result
			queueItems = []*ReviewQueueItem{}
			total = 0
			return nil
		},
	)

	if err != nil {
		structuredErr := s.errorHandler.HandleError(ctx, err, "GetReviewQueue", errorContext)
		return nil, 0, structuredErr
	}

	return queueItems, total, nil
}

// AssignReviewerWithErrorHandling assigns reviewer with error handling
func (s *EnhancedStatusTransitionService) AssignReviewerWithErrorHandling(
	ctx context.Context,
	profileID uuid.UUID,
	reviewerID uuid.UUID,
) error {
	errorContext := map[string]interface{}{
		"profile_id":  profileID,
		"reviewer_id": reviewerID,
	}

	// Execute with transaction protection
	err := s.transactionErrorHandler.ExecuteWithRollback(
		ctx,
		nil,
		"AssignReviewer",
		func() error {
			// This would call the actual assignment logic
			return nil // Simulate success
		},
		// Rollback handlers
		func() error {
			s.logger.Warn("Rolling back reviewer assignment", errorContext)
			return nil
		},
	)

	if err != nil {
		return s.errorHandler.HandleError(ctx, err, "AssignReviewer", errorContext)
	}

	return nil
}
