// Package domain re-exports KYC domain types from pkg/domain.
// internal/domain/kyc.go
package domain

import pkg "kyd/pkg/domain"

// KYCProfileType represents the type of KYC profile.
type KYCProfileType = pkg.KYCProfileType

const (
	KYCProfileTypeIndividual = pkg.KYCProfileTypeIndividual
	KYCProfileTypeBusiness   = pkg.KYCProfileTypeBusiness
)

// KYCSubmissionStatus represents KYC submission workflow status.
type KYCSubmissionStatus = pkg.KYCSubmissionStatus

const (
	KYCSubmissionStatusDraft                  = pkg.KYCSubmissionStatusDraft
	KYCSubmissionStatusSubmitted              = pkg.KYCSubmissionStatusSubmitted
	KYCSubmissionStatusUnderReview            = pkg.KYCSubmissionStatusUnderReview
	KYCSubmissionStatusAdditionalInfoRequired = pkg.KYCSubmissionStatusAdditionalInfoRequired
	KYCSubmissionStatusApproved               = pkg.KYCSubmissionStatusApproved
	KYCSubmissionStatusRejected               = pkg.KYCSubmissionStatusRejected
	KYCSubmissionStatusSuspended              = pkg.KYCSubmissionStatusSuspended
)

// AMLStatus represents AML check status.
type AMLStatus = pkg.AMLStatus

const (
	AMLStatusPending    = pkg.AMLStatusPending
	AMLStatusProcessing = pkg.AMLStatusProcessing
	AMLStatusCleared    = pkg.AMLStatusCleared
	AMLStatusFlagged    = pkg.AMLStatusFlagged
	AMLStatusEscalated  = pkg.AMLStatusEscalated
	AMLStatusRejected   = pkg.AMLStatusRejected
)

// DocumentType represents types of KYC documents.
type DocumentType = pkg.DocumentType

const (
	DocumentTypeNationalID           = pkg.DocumentTypeNationalID
	DocumentTypePassport             = pkg.DocumentTypePassport
	DocumentTypeDriversLicense       = pkg.DocumentTypeDriversLicense
	DocumentTypeBusinessRegistration = pkg.DocumentTypeBusinessRegistration
	DocumentTypeTaxCertificate       = pkg.DocumentTypeTaxCertificate
	DocumentTypeUtilityBill          = pkg.DocumentTypeUtilityBill
	DocumentTypeBankStatement        = pkg.DocumentTypeBankStatement
	DocumentTypeProofOfIncome        = pkg.DocumentTypeProofOfIncome
	DocumentTypeSelfieWithID         = pkg.DocumentTypeSelfieWithID
	DocumentTypeBusinessLicense      = pkg.DocumentTypeBusinessLicense
	DocumentTypeAgentLicense         = pkg.DocumentTypeAgentLicense
)

// DocumentVerificationStatus represents document verification status.
type DocumentVerificationStatus = pkg.DocumentVerificationStatus

const (
	DocumentStatusPending    = pkg.DocumentStatusPending
	DocumentStatusProcessing = pkg.DocumentStatusProcessing
	DocumentStatusVerified   = pkg.DocumentStatusVerified
	DocumentStatusRejected   = pkg.DocumentStatusRejected
)

// VirusScanStatus represents virus scanning status.
type VirusScanStatus = pkg.VirusScanStatus

const (
	VirusScanStatusPending     = pkg.VirusScanStatusPending
	VirusScanStatusScanning    = pkg.VirusScanStatusScanning
	VirusScanStatusClean       = pkg.VirusScanStatusClean
	VirusScanStatusInfected    = pkg.VirusScanStatusInfected
	VirusScanStatusError       = pkg.VirusScanStatusError
	VirusScanStatusQuarantined = pkg.VirusScanStatusQuarantined
	VirusScanStatusSkipped     = pkg.VirusScanStatusSkipped
)

// DocumentValidationStatus represents document validation status.
type DocumentValidationStatus = pkg.DocumentValidationStatus

const (
	ValidationStatusPending    = pkg.ValidationStatusPending
	ValidationStatusValidating = pkg.ValidationStatusValidating
	ValidationStatusValid      = pkg.ValidationStatusValid
	ValidationStatusInvalid    = pkg.ValidationStatusInvalid
	ValidationStatusWarning    = pkg.ValidationStatusWarning
)

// OCRStatus represents OCR processing status.
type OCRStatus = pkg.OCRStatus

const (
	OCRStatusPending    = pkg.OCRStatusPending
	OCRStatusProcessing = pkg.OCRStatusProcessing
	OCRStatusCompleted  = pkg.OCRStatusCompleted
	OCRStatusFailed     = pkg.OCRStatusFailed
	OCRStatusSkipped    = pkg.OCRStatusSkipped
)

// StorageProvider represents file storage providers.
type StorageProvider = pkg.StorageProvider

const (
	StorageProviderLocal = pkg.StorageProviderLocal
	StorageProviderS3    = pkg.StorageProviderS3
	StorageProviderMinIO = pkg.StorageProviderMinIO
	StorageProviderGCS   = pkg.StorageProviderGCS
)

// AccessLevel represents document access levels.
type AccessLevel = pkg.AccessLevel

const (
	AccessLevelPublic       = pkg.AccessLevelPublic
	AccessLevelInternal     = pkg.AccessLevelInternal
	AccessLevelConfidential = pkg.AccessLevelConfidential
	AccessLevelRestricted   = pkg.AccessLevelRestricted
)

// RetentionPolicy represents document retention policies.
type RetentionPolicy = pkg.RetentionPolicy

const (
	RetentionPolicyKYCCompliance = pkg.RetentionPolicyKYCCompliance
	RetentionPolicyTransactional = pkg.RetentionPolicyTransactional
	RetentionPolicyTemporary     = pkg.RetentionPolicyTemporary
	RetentionPolicyPermanent     = pkg.RetentionPolicyPermanent
)

// IncomeRange represents annual income ranges.
type IncomeRange = pkg.IncomeRange

const (
	IncomeRangeLessThan10K = pkg.IncomeRangeLessThan10K
	IncomeRange10KTo50K    = pkg.IncomeRange10KTo50K
	IncomeRange50KTo100K   = pkg.IncomeRange50KTo100K
	IncomeRange100KTo250K  = pkg.IncomeRange100KTo250K
	IncomeRange250KTo500K  = pkg.IncomeRange250KTo500K
	IncomeRange500KTo1M    = pkg.IncomeRange500KTo1M
	IncomeRangeOver1M      = pkg.IncomeRangeOver1M
)

// TurnoverRange represents business turnover ranges.
type TurnoverRange = pkg.TurnoverRange

const (
	TurnoverRangeLessThan50K = pkg.TurnoverRangeLessThan50K
	TurnoverRange50KTo250K   = pkg.TurnoverRange50KTo250K
	TurnoverRange250KTo1M    = pkg.TurnoverRange250KTo1M
	TurnoverRange1MTo5M      = pkg.TurnoverRange1MTo5M
	TurnoverRange5MTo10M     = pkg.TurnoverRange5MTo10M
	TurnoverRange10MTo50M    = pkg.TurnoverRange10MTo50M
	TurnoverRangeOver50M     = pkg.TurnoverRangeOver50M
)

// SourceOfFunds represents sources of funds.
type SourceOfFunds = pkg.SourceOfFunds

const (
	SourceOfFundsEmployment  = pkg.SourceOfFundsEmployment
	SourceOfFundsBusiness    = pkg.SourceOfFundsBusiness
	SourceOfFundsInvestments = pkg.SourceOfFundsInvestments
	SourceOfFundsInheritance = pkg.SourceOfFundsInheritance
	SourceOfFundsSavings     = pkg.SourceOfFundsSavings
	SourceOfFundsOther       = pkg.SourceOfFundsOther
)

// PEPType represents Politically Exposed Person types.
type PEPType = pkg.PEPType

const (
	PEPTypeDomestic                  = pkg.PEPTypeDomestic
	PEPTypeForeign                   = pkg.PEPTypeForeign
	PEPTypeInternationalOrganization = pkg.PEPTypeInternationalOrganization
)

// Re-export domain models
type KYCProfile = pkg.KYCProfile
type KYCRequirements = pkg.KYCRequirements
type KYCDocument = pkg.KYCDocument

// Re-export request/response structs
type KYCStatusResponse = pkg.KYCStatusResponse
type KYCRequirementsResponse = pkg.KYCRequirementsResponse
type SubmitKYCRequest = pkg.SubmitKYCRequest
type SubmitKYCResponse = pkg.SubmitKYCResponse
type UploadDocumentRequest = pkg.UploadDocumentRequest
type UploadDocumentResponse = pkg.UploadDocumentResponse
type DocumentVerificationRequest = pkg.DocumentVerificationRequest
type DocumentVerificationResponse = pkg.DocumentVerificationResponse
