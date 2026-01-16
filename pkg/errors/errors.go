// Package errors provides common, reusable error values and helpers.
package errors

import (
	"errors"
	"fmt"
)

// Common errors
var (
	ErrUserNotFound        = errors.New("user not found")
	ErrUserAlreadyExists   = errors.New("user already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrWalletAlreadyExists = errors.New("wallet already exists")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrSettlementNotFound  = errors.New("settlement not found")
	ErrRateNotAvailable    = errors.New("exchange rate not available")
	ErrCurrencyNotAllowed  = errors.New("currency not allowed for user country")
	// KYC Profile errors
	ErrKYCProfileNotFound    = errors.New("kyc profile not found")
	ErrKYCAlreadyApproved    = errors.New("kyc already approved")
	ErrKYCUnderReview        = errors.New("kyc already under review")
	ErrKYCSubmissionRequired = errors.New("kyc submission required")
	ErrKYCPendingReview      = errors.New("kyc pending review")

	// KYC Requirements errors
	ErrKYCRequirementsNotFound = errors.New("kyc requirements not found")
	ErrInvalidKYCLevel         = errors.New("invalid kyc level")

	// Document errors
	ErrDocumentNotFound         = errors.New("document not found")
	ErrDocumentAlreadyVerified  = errors.New("document already verified")
	ErrDocumentRejected         = errors.New("document rejected")
	ErrDocumentVirusInfected    = errors.New("document contains virus/malware")
	ErrDocumentValidationFailed = errors.New("document validation failed")
	ErrDocumentSizeExceeded     = errors.New("document size exceeded")
	ErrInvalidDocumentType      = errors.New("invalid document type")
	ErrInvalidDocumentFormat    = errors.New("invalid document format")

	// File upload errors
	ErrFileUploadFailed   = errors.New("file upload failed")
	ErrFileStorageFailed  = errors.New("file storage failed")
	ErrFileTooLarge       = errors.New("file too large")
	ErrFileTypeNotAllowed = errors.New("file type not allowed")

	// AML errors
	ErrAMLCheckFailed        = errors.New("aml check failed")
	ErrAMLEscalationRequired = errors.New("aml escalation required")
	ErrPEPDetected           = errors.New("politically exposed person detected")
	ErrSanctionMatch         = errors.New("sanction match detected")

	// Compliance errors
	ErrComplianceViolation      = errors.New("compliance violation")
	ErrRetentionPolicyViolation = errors.New("retention policy violation")
	ErrAccessDenied             = errors.New("access denied to document")
)

// Wrap wraps an error with additional context
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
