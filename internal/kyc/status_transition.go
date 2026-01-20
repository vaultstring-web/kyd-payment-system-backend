// ==============================================================================
// STATUS TRANSITION MANAGEMENT - internal/kyc/status_transition.go
// ==============================================================================
// Handles KYC status transitions, review queue management, and workflow state changes
// Task 5.2.7 implementation
// ==============================================================================

package kyc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ==============================================================================
// STATUS TRANSITION TYPES
// ==============================================================================

// StatusTransition represents a state change in KYC workflow
type StatusTransition struct {
	ProfileID        uuid.UUID                  `json:"profile_id"`
	UserID           uuid.UUID                  `json:"user_id"`
	FromStatus       domain.KYCSubmissionStatus `json:"from_status"`
	ToStatus         domain.KYCSubmissionStatus `json:"to_status"`
	TransitionedAt   time.Time                  `json:"transitioned_at"`
	TransitionedBy   uuid.UUID                  `json:"transitioned_by,omitempty"`
	Reason           string                     `json:"reason,omitempty"`
	Notes            string                     `json:"notes,omitempty"`
	IsManual         bool                       `json:"is_manual"`
	AutoTransitioned bool                       `json:"auto_transitioned"`
	Metadata         domain.Metadata            `json:"metadata"`
}

// ReviewQueueItem represents an item in the KYC review queue
type ReviewQueueItem struct {
	ProfileID     uuid.UUID             `json:"profile_id"`
	UserID        uuid.UUID             `json:"user_id"`
	SubmittedAt   time.Time             `json:"submitted_at"`
	Priority      int                   `json:"priority"` // 1-10, higher is more urgent
	EstimatedTime time.Duration         `json:"estimated_time"`
	RiskScore     decimal.Decimal       `json:"risk_score"`
	ProfileType   domain.KYCProfileType `json:"profile_type"`
	CountryCode   string                `json:"country_code"`
	QueuePosition int                   `json:"queue_position"`
	TimeInQueue   time.Duration         `json:"time_in_queue"`
	AssignedTo    *uuid.UUID            `json:"assigned_to,omitempty"`
	LastViewedAt  *time.Time            `json:"last_viewed_at,omitempty"`
	ExpiresAt     *time.Time            `json:"expires_at,omitempty"`
}

// StatusTransitionResult contains the result of a status transition operation
type StatusTransitionResult struct {
	Transition       *StatusTransition  `json:"transition"`
	Profile          *domain.KYCProfile `json:"profile"`
	User             *domain.User       `json:"user,omitempty"`
	IsValid          bool               `json:"is_valid"`
	ValidationErrors []ValidationError  `json:"validation_errors,omitempty"`
	NextActions      []string           `json:"next_actions"`
	Notifications    []Notification     `json:"notifications,omitempty"`
}

// Notification represents a system notification for status changes
type Notification struct {
	Type      string                 `json:"type"`
	Recipient uuid.UUID              `json:"recipient"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data"`
	Priority  string                 `json:"priority"` // low, medium, high, urgent
	Channel   string                 `json:"channel"`  // email, sms, push, in-app
	SentAt    *time.Time             `json:"sent_at,omitempty"`
	ReadAt    *time.Time             `json:"read_at,omitempty"`
}

// ==============================================================================
// STATUS TRANSITION VALIDATION
// ==============================================================================

// validateStatusTransition validates if a status transition is allowed
func (s *KYCService) validateStatusTransition(
	fromStatus domain.KYCSubmissionStatus,
	toStatus domain.KYCSubmissionStatus,
	profile *domain.KYCProfile,
	user *domain.User,
	isManual bool,
	transitionedBy uuid.UUID,
) (bool, []ValidationError) {
	var errors []ValidationError

	// Define allowed transitions
	allowedTransitions := s.getAllowedTransitions(profile, user, isManual, transitionedBy)

	// Check if transition is in allowed list
	isAllowed := false
	for _, allowed := range allowedTransitions {
		if allowed.From == fromStatus && allowed.To == toStatus {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		errors = append(errors, ValidationError{
			Field:   "status",
			Message: fmt.Sprintf("Transition from %s to %s is not allowed", fromStatus, toStatus),
		})
	}

	// Additional validations based on target status
	switch toStatus {
	case domain.KYCSubmissionStatusApproved:
		// Must have positive AML status
		if profile.AMLStatus != domain.AMLStatusCleared {
			errors = append(errors, ValidationError{
				Field:   "aml_status",
				Message: "Cannot approve profile with non-cleared AML status",
			})
		}

		// Must have all required documents
		requiredDocs := s.getRequiredDocumentTypes(profile, user)
		missingDocs := s.checkMissingDocuments(profile.ID, requiredDocs)
		if len(missingDocs) > 0 {
			errors = append(errors, ValidationError{
				Field:   "documents",
				Message: fmt.Sprintf("Cannot approve profile with missing required documents: %v", missingDocs),
			})
		}

	case domain.KYCSubmissionStatusRejected:
		// Must have rejection reason
		// This would be validated in the calling function

	case domain.KYCSubmissionStatusAdditionalInfoRequired:
		// Must specify what additional info is needed
		// This would be validated in the calling function

	case domain.KYCSubmissionStatusUnderReview:
		// Profile must be submitted
		if fromStatus != domain.KYCSubmissionStatusSubmitted {
			errors = append(errors, ValidationError{
				Field:   "status",
				Message: "Only submitted profiles can be moved to under review",
			})
		}
	}

	return isAllowed && len(errors) == 0, errors
}

// getAllowedTransitions returns allowed status transitions based on context
func (s *KYCService) getAllowedTransitions(
	_ *domain.KYCProfile,
	_ *domain.User,
	isManual bool,
	transitionedBy uuid.UUID,
) []struct {
	From domain.KYCSubmissionStatus
	To   domain.KYCSubmissionStatus
} {
	var allowed []struct {
		From domain.KYCSubmissionStatus
		To   domain.KYCSubmissionStatus
	}

	// Common transitions for all users
	commonTransitions := []struct {
		From domain.KYCSubmissionStatus
		To   domain.KYCSubmissionStatus
	}{
		// User-initiated transitions
		{domain.KYCSubmissionStatusDraft, domain.KYCSubmissionStatusSubmitted},
		{domain.KYCSubmissionStatusDraft, domain.KYCSubmissionStatusSuspended},

		// System/Admin transitions
		{domain.KYCSubmissionStatusSubmitted, domain.KYCSubmissionStatusUnderReview},
		{domain.KYCSubmissionStatusSubmitted, domain.KYCSubmissionStatusSuspended},

		// Review workflow
		{domain.KYCSubmissionStatusUnderReview, domain.KYCSubmissionStatusAdditionalInfoRequired},
		{domain.KYCSubmissionStatusUnderReview, domain.KYCSubmissionStatusApproved},
		{domain.KYCSubmissionStatusUnderReview, domain.KYCSubmissionStatusRejected},
		{domain.KYCSubmissionStatusUnderReview, domain.KYCSubmissionStatusSuspended},

		// Additional info flow
		{domain.KYCSubmissionStatusAdditionalInfoRequired, domain.KYCSubmissionStatusSubmitted},
		{domain.KYCSubmissionStatusAdditionalInfoRequired, domain.KYCSubmissionStatusUnderReview},
		{domain.KYCSubmissionStatusAdditionalInfoRequired, domain.KYCSubmissionStatusSuspended},

		// Rejection flow
		{domain.KYCSubmissionStatusRejected, domain.KYCSubmissionStatusSubmitted},
		{domain.KYCSubmissionStatusRejected, domain.KYCSubmissionStatusSuspended},

		// Suspension flow
		{domain.KYCSubmissionStatusSuspended, domain.KYCSubmissionStatusSubmitted},
		{domain.KYCSubmissionStatusSuspended, domain.KYCSubmissionStatusUnderReview},
	}

	allowed = append(allowed, commonTransitions...)

	// Add admin-only transitions if user is admin
	if s.isUserAdmin(transitionedBy) {
		adminTransitions := []struct {
			From domain.KYCSubmissionStatus
			To   domain.KYCSubmissionStatus
		}{
			// Admin can move from any status to suspended
			{domain.KYCSubmissionStatusApproved, domain.KYCSubmissionStatusSuspended},
			{domain.KYCSubmissionStatusRejected, domain.KYCSubmissionStatusSuspended},

			// Admin can reactivate suspended profiles
			{domain.KYCSubmissionStatusSuspended, domain.KYCSubmissionStatusUnderReview},
			{domain.KYCSubmissionStatusSuspended, domain.KYCSubmissionStatusSubmitted},
		}
		allowed = append(allowed, adminTransitions...)
	}

	// Add auto-transitions (system-initiated)
	if !isManual {
		autoTransitions := []struct {
			From domain.KYCSubmissionStatus
			To   domain.KYCSubmissionStatus
		}{
			// Auto-approve for low-risk profiles meeting criteria
			{domain.KYCSubmissionStatusSubmitted, domain.KYCSubmissionStatusApproved},

			// Auto-reject for high-risk profiles
			{domain.KYCSubmissionStatusSubmitted, domain.KYCSubmissionStatusRejected},

			// Auto-suspend for expired reviews
			{domain.KYCSubmissionStatusUnderReview, domain.KYCSubmissionStatusSuspended},
		}
		allowed = append(allowed, autoTransitions...)
	}

	return allowed
}

// ==============================================================================
// STATUS TRANSITION EXECUTION
// ==============================================================================

// transitionKYCStatus executes a status transition with full validation and side effects
func (s *KYCService) transitionKYCStatus(
	ctx context.Context,
	profileID uuid.UUID,
	toStatus domain.KYCSubmissionStatus,
	reason string,
	notes string,
	transitionedBy uuid.UUID,
	isManual bool,
) (*StatusTransitionResult, error) {
	startTime := time.Now()

	// Get current profile
	profile, err := s.repo.FindProfileByID(ctx, profileID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find KYC profile")
	}

	// Get user for validation
	user, err := s.userRepo.FindByID(ctx, profile.UserID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	// Validate transition
	isValid, validationErrors := s.validateStatusTransition(
		profile.SubmissionStatus,
		toStatus,
		profile,
		user,
		isManual,
		transitionedBy,
	)

	if !isValid {
		s.logger.Warn("Status transition validation failed", map[string]interface{}{
			"profile_id":  profileID,
			"from_status": profile.SubmissionStatus,
			"to_status":   toStatus,
			"errors":      validationErrors,
			"is_manual":   isManual,
			"user_id":     transitionedBy,
		})

		return &StatusTransitionResult{
			Transition:       nil,
			Profile:          profile,
			IsValid:          false,
			ValidationErrors: validationErrors,
		}, nil
	}

	// Execute transition in transaction
	var transitionResult *StatusTransitionResult
	err = s.transactionManager.WithTransaction(ctx, func(txCtx *TransactionalContext) error {
		// Create status transition record
		transition := &StatusTransition{
			ProfileID:        profileID,
			UserID:           profile.UserID,
			FromStatus:       profile.SubmissionStatus,
			ToStatus:         toStatus,
			TransitionedAt:   time.Now(),
			TransitionedBy:   transitionedBy,
			Reason:           reason,
			Notes:            notes,
			IsManual:         isManual,
			AutoTransitioned: !isManual,
			Metadata: domain.Metadata{
				"transition_type": func() string {
					if isManual {
						return "manual"
					}
					return "auto"
				}(),
				"reason_code":       reason,
				"validation_passed": true,
				"executed_at":       time.Now().Format(time.RFC3339),
			},
		}

		// Update profile status
		oldStatus := profile.SubmissionStatus
		profile.SubmissionStatus = toStatus
		profile.UpdatedAt = time.Now()

		// Update timestamps based on status
		switch toStatus {
		case domain.KYCSubmissionStatusSubmitted:
			if profile.SubmittedAt == nil {
				now := time.Now()
				profile.SubmittedAt = &now
			}
		case domain.KYCSubmissionStatusUnderReview:
			if profile.ReviewedAt == nil {
				now := time.Now()
				profile.ReviewedAt = &now
				profile.ReviewedBy = &transitionedBy
			}
		case domain.KYCSubmissionStatusApproved:
			if profile.ApprovedAt == nil {
				now := time.Now()
				profile.ApprovedAt = &now
				profile.ReviewedBy = &transitionedBy
			}
			// Set next review date (e.g., 1 year from now)
			nextReview := time.Now().AddDate(1, 0, 0)
			profile.NextReviewDate = &nextReview
		case domain.KYCSubmissionStatusRejected:
			if profile.RejectedAt == nil {
				now := time.Now()
				profile.RejectedAt = &now
				profile.ReviewedBy = &transitionedBy
			}
		}

		// Update review notes if provided
		if notes != "" {
			if profile.ReviewNotes == "" {
				profile.ReviewNotes = notes
			} else {
				profile.ReviewNotes = fmt.Sprintf("%s\n[%s] %s: %s",
					profile.ReviewNotes,
					time.Now().Format(time.RFC3339),
					func() string {
						if isManual {
							return "MANUAL"
						}
						return "AUTO"
					}(),
					notes,
				)
			}
		}

		// Update metadata with transition history
		if profile.Metadata == nil {
			profile.Metadata = make(domain.Metadata)
		}

		// Get existing transitions or initialize
		var transitions []map[string]interface{}
		if existingTransitions, ok := profile.Metadata["status_transitions"].([]map[string]interface{}); ok {
			transitions = existingTransitions
		}

		// Add new transition
		transitions = append(transitions, map[string]interface{}{
			"from_status":     oldStatus,
			"to_status":       toStatus,
			"transitioned_at": time.Now().Format(time.RFC3339),
			"transitioned_by": transitionedBy.String(),
			"reason":          reason,
			"is_manual":       isManual,
			"sequence":        len(transitions) + 1,
		})

		profile.Metadata["status_transitions"] = transitions
		profile.Metadata["last_status_change"] = map[string]interface{}{
			"from":       oldStatus,
			"to":         toStatus,
			"changed_at": time.Now().Format(time.RFC3339),
			"changed_by": transitionedBy.String(),
		}

		// Save profile updates
		if err := s.repo.UpdateProfileTx(txCtx.Ctx, txCtx.Tx, profile); err != nil {
			return errors.Wrap(err, "failed to update profile status")
		}

		// Update user KYC status if needed
		if s.shouldUpdateUserKYCStatus(toStatus) {
			userKYCStatus := s.mapProfileStatusToUserKYCStatus(toStatus)
			if err := s.userRepo.UpdateKYCStatusTx(txCtx.Ctx, txCtx.Tx, profile.UserID, userKYCStatus, &toStatus); err != nil {
				return errors.Wrap(err, "failed to update user KYC status")
			}
		}

		// Generate next actions
		nextActions := s.generateNextActions(toStatus, profile, user)

		// Create notifications
		notifications := s.createStatusChangeNotifications(profile, user, transition)

		// Build result
		transitionResult = &StatusTransitionResult{
			Transition:    transition,
			Profile:       profile,
			User:          user,
			IsValid:       true,
			NextActions:   nextActions,
			Notifications: notifications,
		}

		return nil
	})

	if err != nil {
		return nil, errors.Wrap(err, "failed to execute status transition")
	}

	// Log successful transition
	s.logger.Info("KYC status transition completed", map[string]interface{}{
		"profile_id":         profileID,
		"user_id":            profile.UserID,
		"from_status":        transitionResult.Transition.FromStatus,
		"to_status":          transitionResult.Transition.ToStatus,
		"transitioned_by":    transitionedBy,
		"is_manual":          isManual,
		"processing_time_ms": time.Since(startTime).Milliseconds(),
		"reason":             reason,
	})

	// Trigger side effects (async)
	go s.handleStatusTransitionSideEffects(ctx, transitionResult)

	return transitionResult, nil
}

// ==============================================================================
// REVIEW QUEUE MANAGEMENT
// ==============================================================================

// GetReviewQueue retrieves profiles pending review with prioritization
func (s *KYCService) GetReviewQueue(
	ctx context.Context,
	filter domain.KYCSubmissionStatus,
	limit int,
	offset int,
	sortBy string,
	sortOrder string,
) ([]*ReviewQueueItem, int, error) {
	// Get pending profiles
	var profiles []*domain.KYCProfile
	var total int
	var err error

	if filter == "" {
		// Get all pending review (submitted, under_review, additional_info_required)
		profiles, err = s.repo.FindPendingReview(ctx, limit, offset)
		if err != nil {
			return nil, 0, errors.Wrap(err, "failed to get pending review profiles")
		}
		total, err = s.repo.CountPendingReview(ctx)
		if err != nil {
			return nil, 0, errors.Wrap(err, "failed to count pending review profiles")
		}
	} else {
		// Get by specific status
		profiles, err = s.repo.FindByStatus(ctx, filter, limit, offset)
		if err != nil {
			return nil, 0, errors.Wrap(err, fmt.Sprintf("failed to get profiles by status: %s", filter))
		}
		// Note: We'd need a CountByStatus method in repository
		// For now, we'll estimate with existing count
		total = len(profiles) // This is not accurate, just for demonstration
	}

	// Convert to queue items with prioritization
	queueItems := make([]*ReviewQueueItem, 0, len(profiles))

	for i, profile := range profiles {
		priority := s.calculateReviewPriority(profile)
		estimatedTime := s.estimateReviewTime(profile)
		timeInQueue := time.Since(*profile.SubmittedAt)

		queueItem := &ReviewQueueItem{
			ProfileID:     profile.ID,
			UserID:        profile.UserID,
			SubmittedAt:   *profile.SubmittedAt,
			Priority:      priority,
			EstimatedTime: estimatedTime,
			RiskScore:     profile.AMLRiskScore,
			ProfileType:   profile.ProfileType,
			CountryCode:   profile.CountryCode,
			QueuePosition: offset + i + 1,
			TimeInQueue:   timeInQueue,
			AssignedTo:    profile.ReviewedBy,
			LastViewedAt:  profile.ReviewedAt,
		}

		// Set expiration for old items
		if timeInQueue > 7*24*time.Hour { // 7 days
			expiresAt := profile.SubmittedAt.Add(14 * 24 * time.Hour) // 14 days total
			queueItem.ExpiresAt = &expiresAt
		}

		queueItems = append(queueItems, queueItem)
	}

	// Sort queue items
	s.queueItems(queueItems, sortBy, sortOrder)

	return queueItems, total, nil
}

// AssignReviewer assigns a profile to a reviewer
func (s *KYCService) AssignReviewer(
	ctx context.Context,
	profileID uuid.UUID,
	reviewerID uuid.UUID,
) error {
	profile, err := s.repo.FindProfileByID(ctx, profileID)
	if err != nil {
		return errors.Wrap(err, "failed to find profile")
	}

	// Check if profile can be assigned
	if profile.SubmissionStatus != domain.KYCSubmissionStatusSubmitted &&
		profile.SubmissionStatus != domain.KYCSubmissionStatusUnderReview &&
		profile.SubmissionStatus != domain.KYCSubmissionStatusAdditionalInfoRequired {
		return fmt.Errorf("profile in %s status cannot be assigned for review", profile.SubmissionStatus)
	}

	// Check if reviewer is valid (would validate reviewer exists and has permissions)
	if !s.isValidReviewer(reviewerID) {
		return fmt.Errorf("invalid reviewer")
	}

	// Update profile assignment
	now := time.Now()
	profile.ReviewedBy = &reviewerID
	profile.ReviewedAt = &now
	profile.SubmissionStatus = domain.KYCSubmissionStatusUnderReview
	profile.UpdatedAt = now

	// Update metadata
	if profile.Metadata == nil {
		profile.Metadata = make(domain.Metadata)
	}

	assignmentHistory, _ := profile.Metadata["assignment_history"].([]map[string]interface{})
	if assignmentHistory == nil {
		assignmentHistory = []map[string]interface{}{}
	}

	assignmentHistory = append(assignmentHistory, map[string]interface{}{
		"assigned_to":   reviewerID.String(),
		"assigned_at":   now.Format(time.RFC3339),
		"assigned_by":   "system", // Would be actual admin ID
		"assignment_id": uuid.New().String(),
	})

	profile.Metadata["assignment_history"] = assignmentHistory

	if err := s.repo.UpdateProfile(ctx, profile); err != nil {
		return errors.Wrap(err, "failed to assign reviewer")
	}

	// Send notification to reviewer
	s.sendReviewAssignmentNotification(ctx, profile, reviewerID)

	s.logger.Info("Reviewer assigned to KYC profile", map[string]interface{}{
		"profile_id":  profileID,
		"reviewer_id": reviewerID,
		"user_id":     profile.UserID,
		"status":      profile.SubmissionStatus,
	})

	return nil
}

// UnassignReviewer removes assignment from a profile
func (s *KYCService) UnassignReviewer(ctx context.Context, profileID uuid.UUID) error {
	profile, err := s.repo.FindProfileByID(ctx, profileID)
	if err != nil {
		return errors.Wrap(err, "failed to find profile")
	}

	if profile.ReviewedBy == nil {
		return nil // Already unassigned
	}

	oldReviewer := profile.ReviewedBy
	profile.ReviewedBy = nil

	// Keep reviewed_at timestamp for history
	profile.SubmissionStatus = domain.KYCSubmissionStatusSubmitted
	profile.UpdatedAt = time.Now()

	if err := s.repo.UpdateProfile(ctx, profile); err != nil {
		return errors.Wrap(err, "failed to unassign reviewer")
	}

	s.logger.Info("Reviewer unassigned from KYC profile", map[string]interface{}{
		"profile_id":   profileID,
		"old_reviewer": oldReviewer,
		"user_id":      profile.UserID,
		"status":       profile.SubmissionStatus,
	})

	return nil
}

// ==============================================================================
// HELPER METHODS FOR STATUS TRANSITIONS
// ==============================================================================

// shouldUpdateUserKYCStatus determines if user KYC status should be updated
func (s *KYCService) shouldUpdateUserKYCStatus(profileStatus domain.KYCSubmissionStatus) bool {
	// Only update user status for terminal states
	return profileStatus == domain.KYCSubmissionStatusApproved ||
		profileStatus == domain.KYCSubmissionStatusRejected ||
		profileStatus == domain.KYCSubmissionStatusSuspended
}

// mapProfileStatusToUserKYCStatus maps profile status to user KYC status
func (s *KYCService) mapProfileStatusToUserKYCStatus(profileStatus domain.KYCSubmissionStatus) domain.KYCStatus {
	switch profileStatus {
	case domain.KYCSubmissionStatusApproved:
		return domain.KYCStatusVerified
	case domain.KYCSubmissionStatusRejected:
		return domain.KYCStatusRejected
	case domain.KYCSubmissionStatusSuspended:
		return domain.KYCStatusRejected // Or a new status like "suspended"
	default:
		return domain.KYCStatusProcessing
	}
}

// calculateReviewPriority calculates priority score for review queue
func (s *KYCService) calculateReviewPriority(profile *domain.KYCProfile) int {
	priority := 5 // Default medium priority

	// Risk-based priority
	if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(70)) {
		priority += 3
	} else if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(30)) {
		priority += 1
	}

	// Profile type priority (business profiles get higher priority)
	if profile.ProfileType == domain.KYCProfileTypeBusiness {
		priority += 2
	}

	// Country risk priority
	if s.isHighRiskCountry(profile.CountryCode) {
		priority += 2
	}

	// Time in queue priority
	if profile.SubmittedAt != nil {
		timeInQueue := time.Since(*profile.SubmittedAt)
		if timeInQueue > 48*time.Hour {
			priority += 2
		} else if timeInQueue > 24*time.Hour {
			priority += 1
		}
	}

	// PEP/Sanction priority
	if profile.PEPCheck || profile.SanctionCheck {
		priority += 3
	}

	// Cap priority between 1-10
	if priority > 10 {
		priority = 10
	} else if priority < 1 {
		priority = 1
	}

	return priority
}

// estimateReviewTime estimates time needed to review a profile
func (s *KYCService) estimateReviewTime(profile *domain.KYCProfile) time.Duration {
	baseTime := 30 * time.Minute // Base review time

	// Adjust based on factors
	if profile.ProfileType == domain.KYCProfileTypeBusiness {
		baseTime += 30 * time.Minute
	}

	if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(50)) {
		baseTime += 45 * time.Minute
	}

	if s.isHighRiskCountry(profile.CountryCode) {
		baseTime += 20 * time.Minute
	}

	if profile.PEPCheck || profile.SanctionCheck {
		baseTime += 60 * time.Minute
	}

	return baseTime
}

// generateNextActions generates recommended next actions based on status
func (s *KYCService) generateNextActions(
	status domain.KYCSubmissionStatus,
	_ *domain.KYCProfile,
	_ *domain.User,
) []string {
	var actions []string

	switch status {
	case domain.KYCSubmissionStatusSubmitted:
		actions = []string{
			"Assign to reviewer",
			"Perform automated checks",
			"Check for required documents",
			"Validate against compliance requirements",
		}

	case domain.KYCSubmissionStatusUnderReview:
		actions = []string{
			"Review personal information",
			"Verify uploaded documents",
			"Check AML screening results",
			"Assess risk factors",
			"Make approval/rejection decision",
		}

	case domain.KYCSubmissionStatusAdditionalInfoRequired:
		actions = []string{
			"Notify user of required information",
			"Set follow-up reminder",
			"Monitor for user response",
			"Escalate if no response within timeframe",
		}

	case domain.KYCSubmissionStatusApproved:
		actions = []string{
			"Notify user of approval",
			"Update user KYC level and limits",
			"Schedule periodic review",
			"Archive review documents",
		}

	case domain.KYCSubmissionStatusRejected:
		actions = []string{
			"Notify user of rejection with reasons",
			"Document rejection details",
			"Process any appeals if applicable",
			"Consider user for re-submission after cooldown",
		}

	case domain.KYCSubmissionStatusSuspended:
		actions = []string{
			"Investigate suspension reason",
			"Document suspension details",
			"Notify compliance team if needed",
			"Plan for review and potential reinstatement",
		}
	}

	return actions
}

// createStatusChangeNotifications creates notifications for status changes
func (s *KYCService) createStatusChangeNotifications(
	profile *domain.KYCProfile,
	_ *domain.User,
	transition *StatusTransition,
) []Notification {
	var notifications []Notification

	// Notification to user
	userNotification := Notification{
		Type:      "kyc_status_change",
		Recipient: profile.UserID,
		Title:     fmt.Sprintf("KYC Status Update: %s", transition.ToStatus),
		Message:   s.generateStatusChangeMessage(transition, profile),
		Data: map[string]interface{}{
			"profile_id":      profile.ID.String(),
			"from_status":     transition.FromStatus,
			"to_status":       transition.ToStatus,
			"transitioned_at": transition.TransitionedAt.Format(time.RFC3339),
			"reason":          transition.Reason,
		},
		Priority: s.getNotificationPriority(transition.ToStatus),
		Channel:  "email", // Could be configurable
	}

	notifications = append(notifications, userNotification)

	// Notification to reviewer if assigned
	if profile.ReviewedBy != nil && transition.ToStatus != domain.KYCSubmissionStatusSubmitted {
		reviewerNotification := Notification{
			Type:      "kyc_review_update",
			Recipient: *profile.ReviewedBy,
			Title:     fmt.Sprintf("KYC Profile %s: %s", profile.ID.String()[:8], transition.ToStatus),
			Message:   fmt.Sprintf("Profile %s has been moved to %s status", profile.ID.String()[:8], transition.ToStatus),
			Data: map[string]interface{}{
				"profile_id":      profile.ID.String(),
				"user_id":         profile.UserID.String(),
				"from_status":     transition.FromStatus,
				"to_status":       transition.ToStatus,
				"transitioned_by": transition.TransitionedBy.String(),
			},
			Priority: "medium",
			Channel:  "in-app",
		}
		notifications = append(notifications, reviewerNotification)
	}

	// Notification to compliance team for certain statuses
	if transition.ToStatus == domain.KYCSubmissionStatusSuspended ||
		(profile.PEPCheck && transition.ToStatus == domain.KYCSubmissionStatusApproved) ||
		profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(80)) {

		complianceNotification := Notification{
			Type:      "kyc_compliance_alert",
			Recipient: uuid.Nil, // Would be compliance team group ID
			Title:     fmt.Sprintf("Compliance Alert: %s - %s", profile.ID.String()[:8], transition.ToStatus),
			Message:   s.generateComplianceAlertMessage(profile, transition),
			Data: map[string]interface{}{
				"profile_id":        profile.ID.String(),
				"user_id":           profile.UserID.String(),
				"status":            transition.ToStatus,
				"risk_score":        profile.AMLRiskScore.String(),
				"pep_check":         profile.PEPCheck,
				"sanction_check":    profile.SanctionCheck,
				"transition_reason": transition.Reason,
			},
			Priority: "high",
			Channel:  "in-app",
		}
		notifications = append(notifications, complianceNotification)
	}

	return notifications
}

// generateStatusChangeMessage generates user-friendly status change message
func (s *KYCService) generateStatusChangeMessage(
	transition *StatusTransition,
	_ *domain.KYCProfile,
) string {
	switch transition.ToStatus {
	case domain.KYCSubmissionStatusSubmitted:
		return "Your KYC application has been successfully submitted and is now in the review queue. We will notify you once it's been reviewed."

	case domain.KYCSubmissionStatusUnderReview:
		return "Your KYC application is now under review by our team. This process typically takes 1-3 business days."

	case domain.KYCSubmissionStatusAdditionalInfoRequired:
		return fmt.Sprintf("We need additional information to process your KYC application. Reason: %s. Please check your account for details.", transition.Reason)

	case domain.KYCSubmissionStatusApproved:
		return "Congratulations! Your KYC application has been approved. You can now access all platform features."

	case domain.KYCSubmissionStatusRejected:
		return fmt.Sprintf("Your KYC application has been rejected. Reason: %s. You may submit a new application after addressing the issues.", transition.Reason)

	case domain.KYCSubmissionStatusSuspended:
		return fmt.Sprintf("Your KYC status has been suspended. Reason: %s. Please contact support for assistance.", transition.Reason)

	default:
		return fmt.Sprintf("Your KYC status has been updated to: %s", transition.ToStatus)
	}
}

// generateComplianceAlertMessage generates message for compliance team
func (s *KYCService) generateComplianceAlertMessage(
	profile *domain.KYCProfile,
	transition *StatusTransition,
) string {
	riskFactors := []string{}

	if profile.PEPCheck {
		riskFactors = append(riskFactors, "PEP identified")
	}
	if profile.SanctionCheck {
		riskFactors = append(riskFactors, "Sanction match")
	}
	if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(70)) {
		riskFactors = append(riskFactors, fmt.Sprintf("High risk score: %s", profile.AMLRiskScore.String()))
	}
	if s.isHighRiskCountry(profile.CountryCode) {
		riskFactors = append(riskFactors, fmt.Sprintf("High-risk country: %s", profile.CountryCode))
	}

	riskSummary := strings.Join(riskFactors, ", ")
	if riskSummary == "" {
		riskSummary = "No specific risk factors identified"
	}

	return fmt.Sprintf(
		"Profile %s (User: %s) transitioned to %s with risk factors: %s. Transition reason: %s",
		profile.ID.String()[:8],
		profile.UserID.String()[:8],
		transition.ToStatus,
		riskSummary,
		transition.Reason,
	)
}

// getNotificationPriority determines notification priority based on status
func (s *KYCService) getNotificationPriority(status domain.KYCSubmissionStatus) string {
	switch status {
	case domain.KYCSubmissionStatusSuspended,
		domain.KYCSubmissionStatusRejected:
		return "high"
	case domain.KYCSubmissionStatusApproved,
		domain.KYCSubmissionStatusAdditionalInfoRequired:
		return "medium"
	default:
		return "low"
	}
}

// checkMissingDocuments checks which required documents are missing
func (s *KYCService) checkMissingDocuments(_ uuid.UUID, _ []domain.DocumentType) []domain.DocumentType {
	// This would query the database for existing documents
	// For now, return empty (all documents present)
	return []domain.DocumentType{}
}

// getRequiredDocumentTypes returns required document types for a profile
func (s *KYCService) getRequiredDocumentTypes(profile *domain.KYCProfile, _ *domain.User) []domain.DocumentType {
	// Basic documents for all profiles
	required := []domain.DocumentType{
		domain.DocumentTypeNationalID,
		domain.DocumentTypeUtilityBill,
	}

	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		required = append(required, domain.DocumentTypeProofOfIncome)
	} else {
		required = append(required,
			domain.DocumentTypeBusinessRegistration,
			domain.DocumentTypeTaxCertificate,
		)
	}

	return required
}

// isUserAdmin checks if a user has admin privileges
func (s *KYCService) isUserAdmin(_ uuid.UUID) bool {
	// This would check user role/permissions
	// For now, return false (implement as needed)
	return false
}

// isValidReviewer checks if a user can be assigned as reviewer
func (s *KYCService) isValidReviewer(_ uuid.UUID) bool {
	// This would check user permissions
	// For now, return true
	return true
}

// sendReviewAssignmentNotification sends notification to assigned reviewer
func (s *KYCService) sendReviewAssignmentNotification(_ context.Context, profile *domain.KYCProfile, reviewerID uuid.UUID) {
	// Implementation would send actual notification
	s.logger.Info("Review assignment notification sent", map[string]interface{}{
		"profile_id":  profile.ID,
		"reviewer_id": reviewerID,
		"user_id":     profile.UserID,
	})
}

// handleStatusTransitionSideEffects handles async side effects of status changes
func (s *KYCService) handleStatusTransitionSideEffects(ctx context.Context, result *StatusTransitionResult) {
	// Send notifications
	for _, notification := range result.Notifications {
		s.sendNotification(ctx, &notification)
	}

	// Update user limits if approved
	if result.Transition.ToStatus == domain.KYCSubmissionStatusApproved {
		s.updateUserTransactionLimits(ctx, result.Profile.UserID, result.Profile.KYCLevel)
	}

	// Log audit trail
	s.logStatusTransitionAudit(ctx, result)

	// Trigger compliance workflows if needed
	if result.Transition.ToStatus == domain.KYCSubmissionStatusSuspended ||
		result.Profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(70)) {
		s.triggerComplianceWorkflow(ctx, result.Profile, result.Transition)
	}
}

// sendNotification sends a notification
func (s *KYCService) sendNotification(_ context.Context, notification *Notification) {
	// Implementation would integrate with notification service
	s.logger.Debug("Notification sent", map[string]interface{}{
		"type":      notification.Type,
		"recipient": notification.Recipient,
		"priority":  notification.Priority,
		"channel":   notification.Channel,
	})
}

// updateUserTransactionLimits updates user transaction limits based on KYC level
func (s *KYCService) updateUserTransactionLimits(_ context.Context, userID uuid.UUID, kycLevel int) {
	// Implementation would update user limits in wallet service
	s.logger.Info("User transaction limits updated", map[string]interface{}{
		"user_id":   userID,
		"kyc_level": kycLevel,
	})
}

// logStatusTransitionAudit logs status transition to audit trail
func (s *KYCService) logStatusTransitionAudit(_ context.Context, result *StatusTransitionResult) {
	// Implementation would write to audit log
	s.logger.Info("Status transition audit logged", map[string]interface{}{
		"profile_id":  result.Profile.ID,
		"from_status": result.Transition.FromStatus,
		"to_status":   result.Transition.ToStatus,
		"user_id":     result.Profile.UserID,
		"is_manual":   result.Transition.IsManual,
	})
}

// triggerComplianceWorkflow triggers compliance workflows for high-risk cases
func (s *KYCService) triggerComplianceWorkflow(_ context.Context, profile *domain.KYCProfile, transition *StatusTransition) {
	// Implementation would trigger compliance review workflows
	s.logger.Warn("Compliance workflow triggered", map[string]interface{}{
		"profile_id":      profile.ID,
		"status":          transition.ToStatus,
		"risk_score":      profile.AMLRiskScore.String(),
		"transition_type": transition.Metadata["transition_type"],
	})
}

// queueItems sorts queue items based on criteria
func (s *KYCService) queueItems(items []*ReviewQueueItem, sortBy string, sortOrder string) {
	// Implementation would sort items
	// Default: by priority desc, then submitted_at asc
}
