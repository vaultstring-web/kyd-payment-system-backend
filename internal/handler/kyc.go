// ==============================================================================
// KYC HTTP HANDLER - internal/handler/kyc.go
// ==============================================================================
// Handles KYC-related HTTP endpoints with validation, error handling, and logging
// Task 6.1 implementation
// ==============================================================================

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"kyd/internal/domain"
	"kyd/internal/kyc"
	"kyd/internal/middleware"
	"kyd/pkg/logger"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ==============================================================================
// KYC HANDLER STRUCT
// ==============================================================================

// KYCHandler handles KYC-related HTTP endpoints following auth/payment patterns
type KYCHandler struct {
	service   *kyc.KYCService
	validator *validator.Validator
	logger    logger.Logger
}

// NewKYCHandler creates a new KYCHandler with required dependencies
func NewKYCHandler(service *kyc.KYCService, val *validator.Validator, log logger.Logger) *KYCHandler {
	return &KYCHandler{
		service:   service,
		validator: val,
		logger:    log,
	}
}

// ==============================================================================
// HELPER METHODS (following auth handler pattern)
// ==============================================================================

// respondJSON sends a JSON response with proper content type and status code
func (h *KYCHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", map[string]interface{}{
			"error":   err.Error(),
			"status":  status,
			"handler": "kyc",
		})
		http.Error(w, `{"error":"response encoding failed"}`, http.StatusInternalServerError)
	}
}

// respondError sends a standardized error response
func (h *KYCHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}

// parseAndValidateRequest parses and validates JSON request body
func (h *KYCHandler) parseAndValidateRequest(w http.ResponseWriter, r *http.Request, req interface{}) bool {
	// Limit request body size to 2MB (KYC requests can be larger than auth)
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20) // 2MB limit

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(req); err != nil {
		if err == io.EOF {
			h.respondError(w, http.StatusBadRequest, "Request body is required")
			return false
		}
		h.logger.Warn("Invalid request body", map[string]interface{}{
			"error":    err.Error(),
			"handler":  "kyc",
			"endpoint": r.URL.Path,
		})
		h.respondError(w, http.StatusBadRequest, "Invalid request body format")
		return false
	}

	if err := h.validator.Validate(req); err != nil {
		h.logger.Warn("Request validation failed", map[string]interface{}{
			"error":    err.Error(),
			"handler":  "kyc",
			"endpoint": r.URL.Path,
		})
		h.respondError(w, http.StatusBadRequest, err.Error())
		return false
	}

	return true
}

// getUserIDFromContext extracts user ID from request context (JWT)
func (h *KYCHandler) getUserIDFromContext(r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.logger.Warn("Missing user ID in context", map[string]interface{}{
			"handler":  "kyc",
			"endpoint": r.URL.Path,
			"ip":       r.RemoteAddr,
		})
		return uuid.Nil, false
	}
	return userID, true
}

// ==============================================================================
// ENDPOINT 1: SUBMIT KYC DATA
// ==============================================================================

// SubmitKYC handles submission of KYC profile data
// POST /kyc/submit
func (h *KYCHandler) SubmitKYC(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from JWT context
	userID, ok := h.getUserIDFromContext(r)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized: missing user context")
		return
	}

	// Parse and validate request
	var req domain.SubmitKYCRequest
	if !h.parseAndValidateRequest(w, r, &req) {
		return
	}

	// Check if draft mode (query parameter)
	isDraft := r.URL.Query().Get("draft") == "true"

	// Log the submission attempt
	h.logger.Info("KYC submission attempt", map[string]interface{}{
		"event":        "kyc_submission_started",
		"user_id":      userID.String(),
		"profile_type": req.ProfileType,
		"is_draft":     isDraft,
		"ip":           r.RemoteAddr,
		"user_agent":   r.UserAgent(),
	})

	// Call KYC service
	response, err := h.service.SubmitKYCData(r.Context(), &req, userID, isDraft)
	if err != nil {
		h.handleKYCError(w, err, "SubmitKYCData", userID)
		return
	}

	// Log successful submission
	h.logger.Info("KYC submission completed", map[string]interface{}{
		"event":             "kyc_submission_completed",
		"user_id":           userID.String(),
		"profile_id":        response.ProfileID.String(),
		"submission_status": string(response.SubmissionStatus),
		"aml_status":        string(response.AMLStatus),
		"is_draft":          isDraft,
		"ip":                r.RemoteAddr,
	})

	h.respondJSON(w, http.StatusAccepted, response)
}

// ==============================================================================
// ENDPOINT 2: GET KYC STATUS
// ==============================================================================

// GetKYCStatus returns the current KYC status for the authenticated user
// GET /kyc/status
func (h *KYCHandler) GetKYCStatus(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from JWT context
	userID, ok := h.getUserIDFromContext(r)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized: missing user context")
		return
	}

	// Log status request
	h.logger.Info("KYC status request", map[string]interface{}{
		"event":   "kyc_status_request",
		"user_id": userID.String(),
		"ip":      r.RemoteAddr,
	})

	// Get user from user repository via service (placeholder - would need actual implementation)
	// For now, we'll get the KYC profile directly
	profile, err := h.service.GetUserKYCProfile(r.Context(), userID)
	if err != nil {
		h.handleKYCError(w, err, "GetUserKYCProfile", userID)
		return
	}

	// Build response based on profile
	response, err := h.buildKYCStatusResponse(r.Context(), profile, userID)
	if err != nil {
		h.handleKYCError(w, err, "buildKYCStatusResponse", userID)
		return
	}

	h.respondJSON(w, http.StatusOK, response)
}

// buildKYCStatusResponse builds a KYC status response from profile data
func (h *KYCHandler) buildKYCStatusResponse(ctx context.Context, profile *domain.KYCProfile, userID uuid.UUID) (*domain.KYCStatusResponse, error) {
	// Get user for additional info (like KYC level)
	user, err := h.service.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Get document completeness
	documentsRequired := []domain.DocumentType{
		domain.DocumentTypeNationalID,
		domain.DocumentTypeUtilityBill,
	}
	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		documentsRequired = append(documentsRequired, domain.DocumentTypeProofOfIncome)
	} else {
		documentsRequired = append(documentsRequired,
			domain.DocumentTypeBusinessRegistration,
			domain.DocumentTypeTaxCertificate,
		)
	}

	// Calculate completeness (simplified - in real implementation, check actual documents)
	documentsSubmitted := []domain.DocumentType{}
	documentsVerified := []domain.DocumentType{}
	documentsPending := documentsRequired
	missingDocuments := documentsRequired

	completenessPercentage := 0.0
	switch profile.SubmissionStatus {
	case domain.KYCSubmissionStatusApproved:
		completenessPercentage = 100.0
	case domain.KYCSubmissionStatusSubmitted:
		completenessPercentage = 50.0
	}

	// Determine next steps based on status
	var nextSteps []string
	switch profile.SubmissionStatus {
	case domain.KYCSubmissionStatusDraft:
		nextSteps = []string{
			"Complete all required fields",
			"Upload required documents",
			"Submit for review",
		}
	case domain.KYCSubmissionStatusSubmitted:
		nextSteps = []string{
			"Wait for review (typically 1-3 business days)",
			"Check email for updates",
			"Be ready to provide additional information if requested",
		}
	case domain.KYCSubmissionStatusUnderReview:
		nextSteps = []string{
			"Your application is being reviewed",
			"Monitor your email for updates",
			"Check back in 24-48 hours",
		}
	case domain.KYCSubmissionStatusAdditionalInfoRequired:
		nextSteps = []string{
			"Provide the requested additional information",
			"Check the review notes for details",
			"Resubmit your application",
		}
	case domain.KYCSubmissionStatusApproved:
		nextSteps = []string{
			"You're all set!",
			"Review your transaction limits",
			"Start using the platform",
		}
	case domain.KYCSubmissionStatusRejected:
		nextSteps = []string{
			"Review the rejection reason",
			"Address the issues mentioned",
			"Resubmit your application",
		}
	}

	// Get transaction limits based on KYC level
	dailyLimit, monthlyLimit, singleLimit := h.getTransactionLimits(user.KYCLevel)

	return &domain.KYCStatusResponse{
		UserID:                 userID,
		KYCStatus:              profile.SubmissionStatus, // This is KYCSubmissionStatus
		KYCLevel:               user.KYCLevel,
		AMLStatus:              profile.AMLStatus,
		AMLRiskScore:           profile.AMLRiskScore,
		ProfileType:            profile.ProfileType,
		SubmittedAt:            profile.SubmittedAt,
		ReviewedAt:             profile.ReviewedAt,
		NextReviewDate:         profile.NextReviewDate,
		DocumentsRequired:      documentsRequired,
		DocumentsSubmitted:     documentsSubmitted,
		DocumentsVerified:      documentsVerified,
		DocumentsPending:       documentsPending,
		MissingDocuments:       missingDocuments,
		MissingFields:          []string{}, // Would need to check against requirements
		CompletenessPercentage: completenessPercentage,
		NextSteps:              nextSteps,
		EstimatedReviewTime:    "1-3 business days",
		DailyLimit:             &dailyLimit,
		MonthlyLimit:           &monthlyLimit,
		MaxSingleTransaction:   &singleLimit,
	}, nil
}

// ==============================================================================
// ENDPOINT 3: UPLOAD KYC DOCUMENT
// ==============================================================================

// UploadDocument handles uploading of KYC documents
// POST /kyc/documents
func (h *KYCHandler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from JWT context
	userID, ok := h.getUserIDFromContext(r)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized: missing user context")
		return
	}

	// Parse and validate request
	var req domain.UploadDocumentRequest
	if !h.parseAndValidateRequest(w, r, &req) {
		return
	}

	// Log document upload attempt
	h.logger.Info("KYC document upload attempt", map[string]interface{}{
		"event":         "kyc_document_upload_started",
		"user_id":       userID.String(),
		"document_type": req.DocumentType,
		"file_name":     req.FileName,
		"ip":            r.RemoteAddr,
	})

	// Check if user has KYC profile
	hasProfile, err := h.service.UserHasKYCProfile(r.Context(), userID)
	if err != nil {
		h.handleKYCError(w, err, "UserHasKYCProfile", userID)
		return
	}

	if !hasProfile {
		h.respondError(w, http.StatusBadRequest, "KYC profile required before document upload")
		return
	}

	// Process document upload
	// Note: This is a simplified implementation
	// In production, this would handle file upload, storage, and processing
	documentID := uuid.New()
	response := &domain.UploadDocumentResponse{
		DocumentID:         documentID,
		UserID:             userID,
		DocumentType:       req.DocumentType,
		VerificationStatus: domain.DocumentStatusPending,
		VirusScanStatus:    domain.VirusScanStatusPending,
		ValidationStatus:   domain.ValidationStatusPending,
		Message:            "Document uploaded successfully and queued for processing",
		NextSteps: []string{
			"Wait for virus scan to complete",
			"Document will be validated automatically",
			"Check back for verification status",
		},
	}

	// Log successful upload
	h.logger.Info("KYC document uploaded", map[string]interface{}{
		"event":         "kyc_document_uploaded",
		"user_id":       userID.String(),
		"document_id":   documentID.String(),
		"document_type": req.DocumentType,
		"ip":            r.RemoteAddr,
	})

	h.respondJSON(w, http.StatusCreated, response)
}

// ==============================================================================
// ENDPOINT 4: GET KYC REQUIREMENTS
// ==============================================================================

// GetRequirements returns KYC requirements for the user's country and type
// GET /kyc/requirements
func (h *KYCHandler) GetRequirements(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from JWT context
	userID, ok := h.getUserIDFromContext(r)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized: missing user context")
		return
	}

	// Get query parameters with defaults
	countryCode := r.URL.Query().Get("country_code")
	userType := r.URL.Query().Get("user_type")
	kycLevelStr := r.URL.Query().Get("kyc_level")

	// If parameters not provided, try to get from user profile
	if countryCode == "" || userType == "" {
		user, err := h.service.GetUser(r.Context(), userID)
		if err != nil {
			h.handleKYCError(w, err, "GetUser", userID)
			return
		}
		if countryCode == "" {
			countryCode = user.CountryCode
		}
		if userType == "" {
			userType = string(user.UserType)
		}
	}

	// Validate required parameters
	if countryCode == "" {
		h.respondError(w, http.StatusBadRequest, "country_code is required")
		return
	}
	if userType == "" {
		h.respondError(w, http.StatusBadRequest, "user_type is required")
		return
	}

	// Parse KYC level with default
	kycLevel := 1 // Default level
	if kycLevelStr != "" {
		level, err := strconv.Atoi(kycLevelStr)
		if err != nil || level < 1 || level > 5 {
			h.respondError(w, http.StatusBadRequest, "kyc_level must be between 1 and 5")
			return
		}
		kycLevel = level
	}

	// Log requirements request
	h.logger.Info("KYC requirements request", map[string]interface{}{
		"event":        "kyc_requirements_request",
		"user_id":      userID.String(),
		"country_code": countryCode,
		"user_type":    userType,
		"kyc_level":    kycLevel,
		"ip":           r.RemoteAddr,
	})

	// Get requirements from repository via service
	requirements, err := h.service.GetKYCRequirements(r.Context(), countryCode, userType, kycLevel)
	if err != nil {
		h.handleKYCError(w, err, "GetKYCRequirements", userID)
		return
	}

	// Build response
	response := h.buildRequirementsResponse(requirements, countryCode, userType, kycLevel)

	h.respondJSON(w, http.StatusOK, response)
}

// buildRequirementsResponse builds a requirements response from domain requirements
func (h *KYCHandler) buildRequirementsResponse(req *domain.KYCRequirements, countryCode, userType string, kycLevel int) *domain.KYCRequirementsResponse {
	if req == nil {
		// Return default requirements if none found
		return &domain.KYCRequirementsResponse{
			CountryCode:  countryCode,
			UserType:     userType,
			KYCLevel:     kycLevel,
			DisplayName:  fmt.Sprintf("Default KYC Requirements for %s - %s", countryCode, userType),
			Description:  "Standard KYC requirements",
			Instructions: "Please provide the following documents and information",
			RequiredDocuments: []domain.DocumentType{
				domain.DocumentTypeNationalID,
				domain.DocumentTypeUtilityBill,
			},
			RequiredFields: []string{
				"address_line1",
				"city",
				"country_code",
				"phone_number",
			},
			MaxFileSizeMB:        10,
			AllowedMimeTypes:     []string{"image/jpeg", "image/png", "application/pdf"},
			AllowedExtensions:    []string{".jpg", ".jpeg", ".png", ".pdf"},
			MinAgeYears:          func() *int { age := 18; return &age }(),
			EstimatedReviewDays:  3,
			ExpeditedAvailable:   true,
			ExpeditedReviewHours: func() *int { hours := 24; return &hours }(),
			DailyLimit:           h.getDefaultDailyLimit(kycLevel),
			MonthlyLimit:         h.getDefaultMonthlyLimit(kycLevel),
			MaxSingleTransaction: h.getDefaultSingleLimit(kycLevel),
		}
	}

	// Convert domain requirements to response format
	return &domain.KYCRequirementsResponse{
		CountryCode:          req.CountryCode,
		UserType:             req.UserType,
		KYCLevel:             req.KYCLevel,
		DisplayName:          req.DisplayName,
		Description:          req.Description,
		Instructions:         req.Instructions,
		RequiredDocuments:    req.RequiredDocuments,
		RequiredFields:       req.RequiredFields,
		MaxFileSizeMB:        req.MaxFileSizeMB,
		AllowedMimeTypes:     req.AllowedMimeTypes,
		AllowedExtensions:    req.AllowedExtensions,
		MinAgeYears:          req.MinAgeYears,
		MaxAgeYears:          req.MaxAgeYears,
		EstimatedReviewDays:  req.EstimatedReviewDays,
		ExpeditedAvailable:   req.ExpeditedReviewAvailable,
		ExpeditedReviewHours: req.ExpeditedReviewHours,
		DailyLimit:           req.DailyTransactionLimit,
		MonthlyLimit:         req.MonthlyTransactionLimit,
		MaxSingleTransaction: req.MaxSingleTransaction,
	}
}

// ==============================================================================
// ERROR HANDLING (following auth handler pattern)
// ==============================================================================

// handleKYCError handles KYC-specific errors with appropriate HTTP status codes
func (h *KYCHandler) handleKYCError(w http.ResponseWriter, err error, operation string, userID uuid.UUID) {
	// Extract error details
	errMsg := err.Error()

	// Map common KYC errors to HTTP status codes
	statusCode, clientMessage := h.mapKYCError(err)

	// Log the error with context
	logData := map[string]interface{}{
		"operation": operation,
		"user_id":   userID.String(),
		"error":     errMsg,
		"status":    statusCode,
	}

	if statusCode >= 500 {
		h.logger.Error("KYC system error", logData)
	} else {
		h.logger.Warn("KYC client error", logData)
	}

	// Respond to client
	h.respondError(w, statusCode, clientMessage)
}

// mapKYCError maps KYC errors to appropriate HTTP status codes and messages
func (h *KYCHandler) mapKYCError(err error) (int, string) {
	errStr := err.Error()

	// Check for specific error types or patterns
	switch {
	// Validation errors (400)
	case strings.Contains(errStr, "validation") ||
		strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "required") ||
		strings.Contains(errStr, "missing"):
		return http.StatusBadRequest, "Invalid KYC data: " + extractErrorMessage(errStr)

	// User not found (404)
	case strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "does not exist"):
		return http.StatusNotFound, "User or profile not found"

	// Already exists (409)
	case strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "duplicate") ||
		strings.Contains(errStr, "already have"):
		return http.StatusConflict, "KYC profile already exists"

	// In progress/review (409)
	case strings.Contains(errStr, "in review") ||
		strings.Contains(errStr, "in progress") ||
		strings.Contains(errStr, "cannot modify"):
		return http.StatusConflict, "KYC profile is currently being reviewed and cannot be modified"

	// Permission/authorization (403)
	case strings.Contains(errStr, "permission") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "forbidden"):
		return http.StatusForbidden, "Not authorized to perform this KYC operation"

	// Rate limiting (429)
	case strings.Contains(errStr, "too many") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "try again"):
		return http.StatusTooManyRequests, "Too many KYC requests. Please try again later"

	// Service unavailable (503)
	case strings.Contains(errStr, "unavailable") ||
		strings.Contains(errStr, "down") ||
		strings.Contains(errStr, "maintenance"):
		return http.StatusServiceUnavailable, "KYC service is temporarily unavailable"

	// Default to 500 for system errors
	default:
		return http.StatusInternalServerError, "An internal error occurred"
	}
}

// extractErrorMessage extracts a user-friendly error message
func extractErrorMessage(errStr string) string {
	// Try to extract the meaningful part after common prefixes
	prefixes := []string{
		"failed to",
		"unable to",
		"error:",
		"kyc error:",
	}

	for _, prefix := range prefixes {
		if idx := strings.Index(strings.ToLower(errStr), prefix); idx != -1 && idx+len(prefix) < len(errStr) {
			// Extract message and clean it up
			message := strings.TrimSpace(errStr[idx+len(prefix):])
			if len(message) > 0 {
				// Capitalize first letter
				message = strings.ToUpper(string(message[0])) + message[1:]
				return message
			}
		}
	}

	// Return the original error, truncated if too long
	if len(errStr) > 100 {
		return errStr[:100] + "..."
	}
	return errStr
}

// ==============================================================================
// TRANSACTION LIMIT HELPERS
// ==============================================================================

// getTransactionLimits returns limits based on KYC level
func (h *KYCHandler) getTransactionLimits(kycLevel int) (daily, monthly, single decimal.Decimal) {
	// Define limits based on KYC level
	limits := map[int]struct{ daily, monthly, single float64 }{
		1: {1000.0, 5000.0, 500.0},
		2: {5000.0, 25000.0, 2500.0},
		3: {20000.0, 100000.0, 10000.0},
		4: {50000.0, 250000.0, 25000.0},
		5: {100000.0, 500000.0, 50000.0},
	}

	if limit, exists := limits[kycLevel]; exists {
		return decimal.NewFromFloat(limit.daily),
			decimal.NewFromFloat(limit.monthly),
			decimal.NewFromFloat(limit.single)
	}

	// Default for level 0 or unknown
	return decimal.NewFromFloat(100.0),
		decimal.NewFromFloat(500.0),
		decimal.NewFromFloat(50.0)
}

// getDefaultDailyLimit returns default daily limit
func (h *KYCHandler) getDefaultDailyLimit(kycLevel int) *decimal.Decimal {
	daily, _, _ := h.getTransactionLimits(kycLevel)
	return &daily
}

// getDefaultMonthlyLimit returns default monthly limit
func (h *KYCHandler) getDefaultMonthlyLimit(kycLevel int) *decimal.Decimal {
	_, monthly, _ := h.getTransactionLimits(kycLevel)
	return &monthly
}

// getDefaultSingleLimit returns default single transaction limit
func (h *KYCHandler) getDefaultSingleLimit(kycLevel int) *decimal.Decimal {
	_, _, single := h.getTransactionLimits(kycLevel)
	return &single
}
