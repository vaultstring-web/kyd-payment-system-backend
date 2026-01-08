// Package domain defines the core business entities for the KYD system.
package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ==============================================================================
// ENUMS & STATUS TYPES
// ==============================================================================

// KYCProfileType represents the type of KYC profile (individual or business).
type KYCProfileType string

const (
	KYCProfileTypeIndividual KYCProfileType = "individual"
	KYCProfileTypeBusiness   KYCProfileType = "business"
)

// KYCSubmissionStatus represents the submission workflow status.
type KYCSubmissionStatus string

const (
	KYCSubmissionStatusDraft                  KYCSubmissionStatus = "draft"
	KYCSubmissionStatusSubmitted              KYCSubmissionStatus = "submitted"
	KYCSubmissionStatusUnderReview            KYCSubmissionStatus = "under_review"
	KYCSubmissionStatusAdditionalInfoRequired KYCSubmissionStatus = "additional_info_required"
	KYCSubmissionStatusApproved               KYCSubmissionStatus = "approved"
	KYCSubmissionStatusRejected               KYCSubmissionStatus = "rejected"
	KYCSubmissionStatusSuspended              KYCSubmissionStatus = "suspended"
)

// AMLStatus represents the status of AML (Anti-Money Laundering) checks.
type AMLStatus string

const (
	AMLStatusPending    AMLStatus = "pending"
	AMLStatusProcessing AMLStatus = "processing"
	AMLStatusCleared    AMLStatus = "cleared"
	AMLStatusFlagged    AMLStatus = "flagged"
	AMLStatusEscalated  AMLStatus = "escalated"
	AMLStatusRejected   AMLStatus = "rejected"
)

// DocumentType represents types of KYC documents.
type DocumentType string

const (
	DocumentTypeNationalID           DocumentType = "national_id"
	DocumentTypePassport             DocumentType = "passport"
	DocumentTypeDriversLicense       DocumentType = "drivers_license"
	DocumentTypeBusinessRegistration DocumentType = "business_registration"
	DocumentTypeTaxCertificate       DocumentType = "tax_certificate"
	DocumentTypeUtilityBill          DocumentType = "utility_bill"
	DocumentTypeBankStatement        DocumentType = "bank_statement"
	DocumentTypeProofOfIncome        DocumentType = "proof_of_income"
	DocumentTypeSelfieWithID         DocumentType = "selfie_with_id"
	DocumentTypeBusinessLicense      DocumentType = "business_license"
	DocumentTypeAgentLicense         DocumentType = "agent_license"
)

// DocumentVerificationStatus represents document verification status.
type DocumentVerificationStatus string

const (
	DocumentStatusPending    DocumentVerificationStatus = "pending"
	DocumentStatusProcessing DocumentVerificationStatus = "processing"
	DocumentStatusVerified   DocumentVerificationStatus = "verified"
	DocumentStatusRejected   DocumentVerificationStatus = "rejected"
)

// VirusScanStatus represents the status of virus/malware scanning.
type VirusScanStatus string

const (
	VirusScanStatusPending     VirusScanStatus = "pending"
	VirusScanStatusScanning    VirusScanStatus = "scanning"
	VirusScanStatusClean       VirusScanStatus = "clean"
	VirusScanStatusInfected    VirusScanStatus = "infected"
	VirusScanStatusError       VirusScanStatus = "error"
	VirusScanStatusQuarantined VirusScanStatus = "quarantined"
	VirusScanStatusSkipped     VirusScanStatus = "skipped"
)

// DocumentValidationStatus represents document validation status.
type DocumentValidationStatus string

const (
	ValidationStatusPending    DocumentValidationStatus = "pending"
	ValidationStatusValidating DocumentValidationStatus = "validating"
	ValidationStatusValid      DocumentValidationStatus = "valid"
	ValidationStatusInvalid    DocumentValidationStatus = "invalid"
	ValidationStatusWarning    DocumentValidationStatus = "warning"
)

// OCRStatus represents Optical Character Recognition status.
type OCRStatus string

const (
	OCRStatusPending    OCRStatus = "pending"
	OCRStatusProcessing OCRStatus = "processing"
	OCRStatusCompleted  OCRStatus = "completed"
	OCRStatusFailed     OCRStatus = "failed"
	OCRStatusSkipped    OCRStatus = "skipped"
)

// StorageProvider represents file storage providers.
type StorageProvider string

const (
	StorageProviderLocal StorageProvider = "local"
	StorageProviderS3    StorageProvider = "s3"
	StorageProviderMinIO StorageProvider = "minio"
	StorageProviderGCS   StorageProvider = "gcs"
)

// AccessLevel represents document access levels.
type AccessLevel string

const (
	AccessLevelPublic       AccessLevel = "public"
	AccessLevelInternal     AccessLevel = "internal"
	AccessLevelConfidential AccessLevel = "confidential"
	AccessLevelRestricted   AccessLevel = "restricted"
)

// RetentionPolicy represents document retention policies.
type RetentionPolicy string

const (
	RetentionPolicyKYCCompliance RetentionPolicy = "kyc_compliance"
	RetentionPolicyTransactional RetentionPolicy = "transactional"
	RetentionPolicyTemporary     RetentionPolicy = "temporary"
	RetentionPolicyPermanent     RetentionPolicy = "permanent"
)

// IncomeRange represents annual income ranges.
type IncomeRange string

const (
	IncomeRangeLessThan10K IncomeRange = "less_than_10k"
	IncomeRange10KTo50K    IncomeRange = "10k_50k"
	IncomeRange50KTo100K   IncomeRange = "50k_100k"
	IncomeRange100KTo250K  IncomeRange = "100k_250k"
	IncomeRange250KTo500K  IncomeRange = "250k_500k"
	IncomeRange500KTo1M    IncomeRange = "500k_1m"
	IncomeRangeOver1M      IncomeRange = "over_1m"
)

// TurnoverRange represents business annual turnover ranges.
type TurnoverRange string

const (
	TurnoverRangeLessThan50K TurnoverRange = "less_than_50k"
	TurnoverRange50KTo250K   TurnoverRange = "50k_250k"
	TurnoverRange250KTo1M    TurnoverRange = "250k_1m"
	TurnoverRange1MTo5M      TurnoverRange = "1m_5m"
	TurnoverRange5MTo10M     TurnoverRange = "5m_10m"
	TurnoverRange10MTo50M    TurnoverRange = "10m_50m"
	TurnoverRangeOver50M     TurnoverRange = "over_50m"
)

// SourceOfFunds represents sources of funds.
type SourceOfFunds string

const (
	SourceOfFundsEmployment  SourceOfFunds = "employment"
	SourceOfFundsBusiness    SourceOfFunds = "business"
	SourceOfFundsInvestments SourceOfFunds = "investments"
	SourceOfFundsInheritance SourceOfFunds = "inheritance"
	SourceOfFundsSavings     SourceOfFunds = "savings"
	SourceOfFundsOther       SourceOfFunds = "other"
)

// PEPType represents Politically Exposed Person types.
type PEPType string

const (
	PEPTypeDomestic                  PEPType = "domestic"
	PEPTypeForeign                   PEPType = "foreign"
	PEPTypeInternationalOrganization PEPType = "international_organization"
)

// ==============================================================================
// DOMAIN MODELS
// ==============================================================================

// KYCProfile represents a user's detailed KYC information.
type KYCProfile struct {
	// Core identifiers
	ID          uuid.UUID      `json:"id" db:"id"`
	UserID      uuid.UUID      `json:"user_id" db:"user_id"`
	ProfileType KYCProfileType `json:"profile_type" db:"profile_type"`

	// Individual fields
	DateOfBirth       *time.Time    `json:"date_of_birth,omitempty" db:"date_of_birth"`
	PlaceOfBirth      string        `json:"place_of_birth,omitempty" db:"place_of_birth"`
	Nationality       string        `json:"nationality,omitempty" db:"nationality"`
	Occupation        string        `json:"occupation,omitempty" db:"occupation"`
	EmployerName      string        `json:"employer_name,omitempty" db:"employer_name"`
	AnnualIncomeRange IncomeRange   `json:"annual_income_range,omitempty" db:"annual_income_range"`
	SourceOfFunds     SourceOfFunds `json:"source_of_funds,omitempty" db:"source_of_funds"`

	// Business fields
	CompanyName               string        `json:"company_name,omitempty" db:"company_name"`
	CompanyRegistrationNumber string        `json:"company_registration_number,omitempty" db:"company_registration_number"`
	CompanyTaxID              string        `json:"company_tax_id,omitempty" db:"company_tax_id"`
	BusinessNature            string        `json:"business_nature,omitempty" db:"business_nature"`
	IncorporationDate         *time.Time    `json:"incorporation_date,omitempty" db:"incorporation_date"`
	AnnualTurnoverRange       TurnoverRange `json:"annual_turnover_range,omitempty" db:"annual_turnover_range"`
	NumberOfEmployees         *int          `json:"number_of_employees,omitempty" db:"number_of_employees"`
	BusinessIndustry          string        `json:"business_industry,omitempty" db:"business_industry"`

	// Address information
	AddressLine1  string `json:"address_line1" db:"address_line1"`
	AddressLine2  string `json:"address_line2,omitempty" db:"address_line2"`
	City          string `json:"city" db:"city"`
	StateProvince string `json:"state_province,omitempty" db:"state_province"`
	PostalCode    string `json:"postal_code,omitempty" db:"postal_code"`
	CountryCode   string `json:"country_code" db:"country_code"`

	// Contact information
	PhoneNumber    string `json:"phone_number,omitempty" db:"phone_number"`
	AltPhoneNumber string `json:"alt_phone_number,omitempty" db:"alt_phone_number"`

	// KYC status tracking
	SubmissionStatus KYCSubmissionStatus `json:"submission_status" db:"submission_status"`
	KYCLevel         int                 `json:"kyc_level" db:"kyc_level"`
	ReviewNotes      string              `json:"review_notes,omitempty" db:"review_notes"`
	ReviewedBy       *uuid.UUID          `json:"reviewed_by,omitempty" db:"reviewed_by"`
	ReviewedAt       *time.Time          `json:"reviewed_at,omitempty" db:"reviewed_at"`
	NextReviewDate   *time.Time          `json:"next_review_date,omitempty" db:"next_review_date"`

	// AML/CFT compliance
	AMLRiskScore  decimal.Decimal `json:"aml_risk_score" db:"aml_risk_score"`
	AMLStatus     AMLStatus       `json:"aml_check_status" db:"aml_check_status"`
	PEPCheck      bool            `json:"pep_check" db:"pep_check"`
	SanctionCheck bool            `json:"sanction_check" db:"sanction_check"`
	PEPType       *PEPType        `json:"pep_type,omitempty" db:"pep_type"`
	SanctionList  string          `json:"sanction_list,omitempty" db:"sanction_list"`

	// Enhanced due diligence
	EDDRequired   bool       `json:"edd_required" db:"edd_required"`
	EDDLevel      int        `json:"edd_level" db:"edd_level"`
	EDDReviewDate *time.Time `json:"edd_review_date,omitempty" db:"edd_review_date"`

	// Metadata and timestamps
	Metadata    Metadata   `json:"metadata,omitempty" db:"metadata"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty" db:"submitted_at"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty" db:"approved_at"`
	RejectedAt  *time.Time `json:"rejected_at,omitempty" db:"rejected_at"`
}

// KYCRequirements represents country/user-type specific KYC requirements.
type KYCRequirements struct {
	ID uuid.UUID `json:"id" db:"id"`

	// Targeting criteria
	CountryCode string `json:"country_code" db:"country_code"`
	UserType    string `json:"user_type" db:"user_type"`
	KYCLevel    int    `json:"kyc_level" db:"kyc_level"`

	// Document requirements
	RequiredDocuments    []DocumentType `json:"required_documents" db:"required_documents"`
	RequiredFields       []string       `json:"required_fields" db:"required_fields"`
	ConditionalDocuments Metadata       `json:"conditional_documents,omitempty" db:"conditional_documents"`

	// Document specifications
	MaxFileSizeMB     int      `json:"max_file_size_mb" db:"max_file_size_mb"`
	MinFileSizeKB     int      `json:"min_file_size_kb,omitempty" db:"min_file_size_kb"`
	AllowedMimeTypes  []string `json:"allowed_mime_types" db:"allowed_mime_types"`
	AllowedExtensions []string `json:"allowed_extensions" db:"allowed_extensions"`

	// Image specific constraints
	MinImageWidth         int      `json:"min_image_width,omitempty" db:"min_image_width"`
	MinImageHeight        int      `json:"min_image_height,omitempty" db:"min_image_height"`
	MaxImageWidth         int      `json:"max_image_width,omitempty" db:"max_image_width"`
	MaxImageHeight        int      `json:"max_image_height,omitempty" db:"max_image_height"`
	ImageAspectRatioRange Metadata `json:"image_aspect_ratio_range,omitempty" db:"image_aspect_ratio_range"`

	// Validation rules
	MinAgeYears         *int             `json:"min_age_years,omitempty" db:"min_age_years"`
	MaxAgeYears         *int             `json:"max_age_years,omitempty" db:"max_age_years"`
	MinAnnualIncome     *decimal.Decimal `json:"min_annual_income,omitempty" db:"min_annual_income"`
	MaxAnnualIncome     *decimal.Decimal `json:"max_annual_income,omitempty" db:"max_annual_income"`
	MinBusinessTurnover *decimal.Decimal `json:"min_business_turnover,omitempty" db:"min_business_turnover"`
	MaxBusinessTurnover *decimal.Decimal `json:"max_business_turnover,omitempty" db:"max_business_turnover"`

	// Transaction limits
	DailyTransactionLimit   *decimal.Decimal `json:"daily_transaction_limit,omitempty" db:"daily_transaction_limit"`
	MonthlyTransactionLimit *decimal.Decimal `json:"monthly_transaction_limit,omitempty" db:"monthly_transaction_limit"`
	MaxSingleTransaction    *decimal.Decimal `json:"max_single_transaction,omitempty" db:"max_single_transaction"`

	// Display & localization
	DisplayName       string   `json:"display_name" db:"display_name"`
	Description       string   `json:"description,omitempty" db:"description"`
	Instructions      string   `json:"instructions,omitempty" db:"instructions"`
	FieldLabels       Metadata `json:"field_labels,omitempty" db:"field_labels"`
	FieldPlaceholders Metadata `json:"field_placeholders,omitempty" db:"field_placeholders"`
	FieldHelpText     Metadata `json:"field_help_text,omitempty" db:"field_help_text"`

	// Review & processing
	EstimatedReviewDays      int             `json:"estimated_review_days" db:"estimated_review_days"`
	ExpeditedReviewAvailable bool            `json:"expedited_review_available" db:"expedited_review_available"`
	ExpeditedReviewHours     *int            `json:"expedited_review_hours,omitempty" db:"expedited_review_hours"`
	AutoApprovalThreshold    decimal.Decimal `json:"auto_approval_threshold" db:"auto_approval_threshold"`
	ManualReviewRequired     bool            `json:"manual_review_required" db:"manual_review_required"`
	ReviewerRole             string          `json:"reviewer_role" db:"reviewer_role"`

	// Compliance & regulations
	RegulationReferences []string   `json:"regulation_references,omitempty" db:"regulation_references"`
	LegalBasis           string     `json:"legal_basis,omitempty" db:"legal_basis"`
	LastRegulatoryUpdate *time.Time `json:"last_regulatory_update,omitempty" db:"last_regulatory_update"`

	// Status & versioning
	IsActive          bool       `json:"is_active" db:"is_active"`
	EffectiveFrom     time.Time  `json:"effective_from" db:"effective_from"`
	EffectiveTo       *time.Time `json:"effective_to,omitempty" db:"effective_to"`
	Version           int        `json:"version" db:"version"`
	PreviousVersionID *uuid.UUID `json:"previous_version_id,omitempty" db:"previous_version_id"`

	// Audit
	CreatedBy *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty" db:"updated_by"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
}

// KYC Document with extended metadata
type KYCDocument struct {
	ID             uuid.UUID    `json:"id" db:"id"`
	UserID         uuid.UUID    `json:"user_id" db:"user_id"`
	DocumentType   DocumentType `json:"document_type" db:"document_type"`
	DocumentNumber string       `json:"document_number,omitempty" db:"document_number"`
	IssuingCountry string       `json:"issuing_country,omitempty" db:"issuing_country"`
	IssueDate      *time.Time   `json:"issue_date,omitempty" db:"issue_date"`
	ExpiryDate     *time.Time   `json:"expiry_date,omitempty" db:"expiry_date"`

	// Legacy URL fields (for backward compatibility)
	FrontImageURL  string `json:"front_image_url,omitempty" db:"front_image_url"`
	BackImageURL   string `json:"back_image_url,omitempty" db:"back_image_url"`
	SelfieImageURL string `json:"selfie_image_url,omitempty" db:"selfie_image_url"`

	// File metadata
	FileName      string `json:"file_name,omitempty" db:"file_name"`
	FileSizeBytes *int64 `json:"file_size_bytes,omitempty" db:"file_size_bytes"`
	MimeType      string `json:"mime_type,omitempty" db:"mime_type"`
	FileExtension string `json:"file_extension,omitempty" db:"file_extension"`

	// Storage information
	StorageProvider StorageProvider `json:"storage_provider" db:"storage_provider"`
	StorageBucket   string          `json:"storage_bucket,omitempty" db:"storage_bucket"`
	StorageKey      string          `json:"storage_key,omitempty" db:"storage_key"`
	StorageRegion   string          `json:"storage_region,omitempty" db:"storage_region"`
	PublicURL       string          `json:"public_url,omitempty" db:"public_url"`
	InternalPath    string          `json:"internal_path,omitempty" db:"internal_path"`

	// File integrity
	FileHashSHA256        string     `json:"file_hash_sha256,omitempty" db:"file_hash_sha256"`
	FileHashMD5           string     `json:"file_hash_md5,omitempty" db:"file_hash_md5"`
	ChecksumAlgorithm     string     `json:"checksum_algorithm" db:"checksum_algorithm"`
	FileIntegrityVerified bool       `json:"file_integrity_verified" db:"file_integrity_verified"`
	IntegrityVerifiedAt   *time.Time `json:"integrity_verified_at,omitempty" db:"integrity_verified_at"`
	IntegrityVerifiedBy   *uuid.UUID `json:"integrity_verified_by,omitempty" db:"integrity_verified_by"`

	// Virus scanning
	VirusScanStatus  VirusScanStatus `json:"virus_scan_status" db:"virus_scan_status"`
	VirusScanResult  string          `json:"virus_scan_result,omitempty" db:"virus_scan_result"`
	VirusScanEngine  string          `json:"virus_scan_engine,omitempty" db:"virus_scan_engine"`
	VirusScanVersion string          `json:"virus_scan_version,omitempty" db:"virus_scan_version"`
	VirusSignatures  string          `json:"virus_signatures,omitempty" db:"virus_signatures"`
	ScannedAt        *time.Time      `json:"scanned_at,omitempty" db:"scanned_at"`
	ScannedBy        string          `json:"scanned_by,omitempty" db:"scanned_by"`

	// OCR processing
	OCRStatus       OCRStatus  `json:"ocr_status" db:"ocr_status"`
	OCRText         string     `json:"ocr_text,omitempty" db:"ocr_text"`
	OCRConfidence   *float64   `json:"ocr_confidence,omitempty" db:"ocr_confidence"`
	OCRProcessedAt  *time.Time `json:"ocr_processed_at,omitempty" db:"ocr_processed_at"`
	ExtractedFields Metadata   `json:"extracted_fields,omitempty" db:"extracted_fields"`

	// Document validation
	ValidationStatus   DocumentValidationStatus `json:"validation_status" db:"validation_status"`
	ValidationErrors   []string                 `json:"validation_errors,omitempty" db:"validation_errors"`
	ValidationWarnings []string                 `json:"validation_warnings,omitempty" db:"validation_warnings"`
	IsBlurred          bool                     `json:"is_blurred" db:"is_blurred"`
	IsExpired          bool                     `json:"is_expired" db:"is_expired"`
	IsTampered         bool                     `json:"is_tampered" db:"is_tampered"`
	QualityScore       *float64                 `json:"quality_score,omitempty" db:"quality_score"`
	ValidationNotes    string                   `json:"validation_notes,omitempty" db:"validation_notes"`
	ValidatedAt        *time.Time               `json:"validated_at,omitempty" db:"validated_at"`
	ValidatedBy        *uuid.UUID               `json:"validated_by,omitempty" db:"validated_by"`

	// Verification
	VerificationStatus DocumentVerificationStatus `json:"verification_status" db:"verification_status"`
	VerificationNotes  string                     `json:"verification_notes,omitempty" db:"verification_notes"`
	VerifiedBy         *uuid.UUID                 `json:"verified_by,omitempty" db:"verified_by"`
	VerifiedAt         *time.Time                 `json:"verified_at,omitempty" db:"verified_at"`

	// Document expiration and retention
	RetentionPolicy       RetentionPolicy `json:"retention_policy" db:"retention_policy"`
	RetentionDays         int             `json:"retention_days" db:"retention_days"`
	ScheduledDeletionDate *time.Time      `json:"scheduled_deletion_date,omitempty" db:"scheduled_deletion_date"`
	IsArchived            bool            `json:"is_archived" db:"is_archived"`
	ArchivedAt            *time.Time      `json:"archived_at,omitempty" db:"archived_at"`
	ArchiveReason         string          `json:"archive_reason,omitempty" db:"archive_reason"`

	// Document access and audit
	AccessLevel      AccessLevel `json:"access_level" db:"access_level"`
	DownloadCount    int         `json:"download_count" db:"download_count"`
	LastDownloadedAt *time.Time  `json:"last_downloaded_at,omitempty" db:"last_downloaded_at"`
	LastDownloadedBy *uuid.UUID  `json:"last_downloaded_by,omitempty" db:"last_downloaded_by"`
	ViewCount        int         `json:"view_count" db:"view_count"`
	LastViewedAt     *time.Time  `json:"last_viewed_at,omitempty" db:"last_viewed_at"`
	LastViewedBy     *uuid.UUID  `json:"last_viewed_by,omitempty" db:"last_viewed_by"`

	// Business process tracking
	ProcessID              *uuid.UUID `json:"process_id,omitempty" db:"process_id"`
	WorkflowStep           string     `json:"workflow_step,omitempty" db:"workflow_step"`
	ReviewCycle            int        `json:"review_cycle" db:"review_cycle"`
	IsManualReviewRequired bool       `json:"is_manual_review_required" db:"is_manual_review_required"`
	ManualReviewNotes      string     `json:"manual_review_notes,omitempty" db:"manual_review_notes"`
	ManualReviewedAt       *time.Time `json:"manual_reviewed_at,omitempty" db:"manual_reviewed_at"`
	ManualReviewedBy       *uuid.UUID `json:"manual_reviewed_by,omitempty" db:"manual_reviewed_by"`

	// Metadata
	Metadata  Metadata  `json:"metadata,omitempty" db:"metadata"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// KYCStatusResponse represents the complete KYC status for a user
type KYCStatusResponse struct {
	UserID         uuid.UUID           `json:"user_id"`
	KYCStatus      KYCSubmissionStatus `json:"kyc_status"`
	KYCLevel       int                 `json:"kyc_level"`
	AMLStatus      AMLStatus           `json:"aml_status"`
	AMLRiskScore   decimal.Decimal     `json:"aml_risk_score"`
	ProfileType    KYCProfileType      `json:"profile_type,omitempty"`
	SubmittedAt    *time.Time          `json:"submitted_at,omitempty"`
	ReviewedAt     *time.Time          `json:"reviewed_at,omitempty"`
	NextReviewDate *time.Time          `json:"next_review_date,omitempty"`

	// Document completeness
	DocumentsRequired      []DocumentType `json:"documents_required"`
	DocumentsSubmitted     []DocumentType `json:"documents_submitted"`
	DocumentsVerified      []DocumentType `json:"documents_verified"`
	DocumentsPending       []DocumentType `json:"documents_pending"`
	CompletenessPercentage float64        `json:"completeness_percentage"`

	// Missing requirements
	MissingDocuments []DocumentType `json:"missing_documents"`
	MissingFields    []string       `json:"missing_fields"`

	// Next steps
	NextSteps           []string `json:"next_steps"`
	EstimatedReviewTime string   `json:"estimated_review_time,omitempty"`

	// Restrictions
	DailyLimit           *decimal.Decimal `json:"daily_limit,omitempty"`
	MonthlyLimit         *decimal.Decimal `json:"monthly_limit,omitempty"`
	MaxSingleTransaction *decimal.Decimal `json:"max_single_transaction,omitempty"`
}

// KYCRequirementsResponse represents requirements for a specific country/user type
type KYCRequirementsResponse struct {
	CountryCode  string `json:"country_code"`
	UserType     string `json:"user_type"`
	KYCLevel     int    `json:"kyc_level"`
	DisplayName  string `json:"display_name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`

	// Requirements
	RequiredDocuments []DocumentType `json:"required_documents"`
	RequiredFields    []string       `json:"required_fields"`

	// Document specifications
	MaxFileSizeMB     int      `json:"max_file_size_mb"`
	AllowedMimeTypes  []string `json:"allowed_mime_types"`
	AllowedExtensions []string `json:"allowed_extensions"`

	// Validation rules
	MinAgeYears *int `json:"min_age_years,omitempty"`
	MaxAgeYears *int `json:"max_age_years,omitempty"`

	// Review timeline
	EstimatedReviewDays  int  `json:"estimated_review_days"`
	ExpeditedAvailable   bool `json:"expedited_available"`
	ExpeditedReviewHours *int `json:"expedited_review_hours,omitempty"`

	// Transaction limits (if approved)
	DailyLimit           *decimal.Decimal `json:"daily_limit,omitempty"`
	MonthlyLimit         *decimal.Decimal `json:"monthly_limit,omitempty"`
	MaxSingleTransaction *decimal.Decimal `json:"max_single_transaction,omitempty"`
}

// ==============================================================================
// REQUEST/RESPONSE STRUCTS FOR API ENDPOINTS
// ==============================================================================

// SubmitKYCRequest represents the request for submitting KYC data
type SubmitKYCRequest struct {
	ProfileType KYCProfileType `json:"profile_type" validate:"required"`

	// Individual fields
	DateOfBirth       *time.Time    `json:"date_of_birth,omitempty" validate:"omitempty"`
	PlaceOfBirth      string        `json:"place_of_birth,omitempty"`
	Nationality       string        `json:"nationality,omitempty"`
	Occupation        string        `json:"occupation,omitempty"`
	EmployerName      string        `json:"employer_name,omitempty"`
	AnnualIncomeRange IncomeRange   `json:"annual_income_range,omitempty"`
	SourceOfFunds     SourceOfFunds `json:"source_of_funds,omitempty"`

	// Business fields
	CompanyName               string        `json:"company_name,omitempty"`
	CompanyRegistrationNumber string        `json:"company_registration_number,omitempty"`
	CompanyTaxID              string        `json:"company_tax_id,omitempty"`
	BusinessNature            string        `json:"business_nature,omitempty"`
	IncorporationDate         *time.Time    `json:"incorporation_date,omitempty"`
	AnnualTurnoverRange       TurnoverRange `json:"annual_turnover_range,omitempty"`
	NumberOfEmployees         *int          `json:"number_of_employees,omitempty"`
	BusinessIndustry          string        `json:"business_industry,omitempty"`

	// Address information
	AddressLine1  string `json:"address_line1" validate:"required"`
	AddressLine2  string `json:"address_line2,omitempty"`
	City          string `json:"city" validate:"required"`
	StateProvince string `json:"state_province,omitempty"`
	PostalCode    string `json:"postal_code,omitempty"`
	CountryCode   string `json:"country_code" validate:"required,len=2"`

	// Contact information
	PhoneNumber    string `json:"phone_number,omitempty"`
	AltPhoneNumber string `json:"alt_phone_number,omitempty"`
}

// SubmitKYCResponse represents the response after submitting KYC data
type SubmitKYCResponse struct {
	ProfileID           uuid.UUID           `json:"profile_id"`
	UserID              uuid.UUID           `json:"user_id"`
	SubmissionStatus    KYCSubmissionStatus `json:"submission_status"`
	AMLStatus           AMLStatus           `json:"aml_status"`
	Message             string              `json:"message"`
	NextSteps           []string            `json:"next_steps"`
	MissingDocuments    []DocumentType      `json:"missing_documents,omitempty"`
	EstimatedReviewTime string              `json:"estimated_review_time,omitempty"`
}

// UploadDocumentRequest represents the request for uploading a document
type UploadDocumentRequest struct {
	DocumentType   DocumentType `json:"document_type" validate:"required"`
	DocumentNumber string       `json:"document_number,omitempty"`
	IssuingCountry string       `json:"issuing_country,omitempty"`
	IssueDate      *time.Time   `json:"issue_date,omitempty"`
	ExpiryDate     *time.Time   `json:"expiry_date,omitempty"`
	FileBase64     string       `json:"file_base64,omitempty" validate:"omitempty,base64"` // Alternative to multipart
	FileName       string       `json:"file_name,omitempty"`
	MimeType       string       `json:"mime_type,omitempty"`
}

// UploadDocumentResponse represents the response after uploading a document
type UploadDocumentResponse struct {
	DocumentID         uuid.UUID                  `json:"document_id"`
	UserID             uuid.UUID                  `json:"user_id"`
	DocumentType       DocumentType               `json:"document_type"`
	VerificationStatus DocumentVerificationStatus `json:"verification_status"`
	VirusScanStatus    VirusScanStatus            `json:"virus_scan_status"`
	ValidationStatus   DocumentValidationStatus   `json:"validation_status"`
	PublicURL          string                     `json:"public_url,omitempty"`
	Message            string                     `json:"message"`
	NextSteps          []string                   `json:"next_steps,omitempty"`
}

// DocumentVerificationRequest represents a request to verify a document
type DocumentVerificationRequest struct {
	VerificationStatus DocumentVerificationStatus `json:"verification_status" validate:"required"`
	VerificationNotes  string                     `json:"verification_notes,omitempty"`
}

// DocumentVerificationResponse represents the response after document verification
type DocumentVerificationResponse struct {
	DocumentID         uuid.UUID                  `json:"document_id"`
	VerificationStatus DocumentVerificationStatus `json:"verification_status"`
	VerifiedBy         uuid.UUID                  `json:"verified_by"`
	VerifiedAt         time.Time                  `json:"verified_at"`
	Message            string                     `json:"message"`
}
