// ==============================================================================
// KYC SERVICE - internal/kyc/service.go
// ==============================================================================
// KYC business logic with AML, document verification, and compliance workflows
// ==============================================================================

package kyc

import (
	"context"
	"errors"
	"fmt"
	"kyd/internal/aml"
	"kyd/internal/domain"
	"kyd/internal/fileupload"
	"kyd/internal/virusscan"
	"kyd/pkg/config"
	kyderrors "kyd/pkg/errors"
	"kyd/pkg/logger"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ==============================================================================
// REPOSITORY INTERFACES
// ==============================================================================

// Repository defines the data persistence interface for KYC operations
type Repository interface {
	// Profile operations
	CreateProfile(ctx context.Context, profile *domain.KYCProfile) error
	UpdateProfile(ctx context.Context, profile *domain.KYCProfile) error
	BeginTransaction(ctx context.Context) (Transaction, error)
	CreateProfileTx(ctx context.Context, tx Transaction, profile *domain.KYCProfile) error
	UpdateProfileTx(ctx context.Context, tx Transaction, profile *domain.KYCProfile) error
	UpdateSubmissionStatusTx(ctx context.Context, tx Transaction, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error
	UpdateAMLStatusTx(ctx context.Context, tx Transaction, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error
	UpdateKYCLevelTx(ctx context.Context, tx Transaction, id uuid.UUID, level int) error
	FindProfileByIDTx(ctx context.Context, tx Transaction, id uuid.UUID) (*domain.KYCProfile, error)
	FindProfileByUserIDTx(ctx context.Context, tx Transaction, userID uuid.UUID) (*domain.KYCProfile, error)
	ExistsByUserIDTx(ctx context.Context, tx Transaction, userID uuid.UUID) (bool, error)
	FindProfileByID(ctx context.Context, id uuid.UUID) (*domain.KYCProfile, error)
	FindProfileByUserID(ctx context.Context, userID uuid.UUID) (*domain.KYCProfile, error)
	ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error)
	UpdateSubmissionStatus(ctx context.Context, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error
	UpdateAMLStatus(ctx context.Context, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error
	UpdateKYCLevel(ctx context.Context, id uuid.UUID, level int) error
	FindPendingReview(ctx context.Context, limit, offset int) ([]*domain.KYCProfile, error)
	CountPendingReview(ctx context.Context) (int, error)
	FindByStatus(ctx context.Context, status domain.KYCSubmissionStatus, limit, offset int) ([]*domain.KYCProfile, error)

	// Requirements operations
	FindRequirementsByCountryAndUserType(ctx context.Context, countryCode, userType string, kycLevel int) (*domain.KYCRequirements, error)
	FindAllActiveRequirements(ctx context.Context, limit, offset int) ([]*domain.KYCRequirements, error)

	// Document operations
	CreateDocument(ctx context.Context, document *domain.KYCDocument) error
	UpdateDocument(ctx context.Context, document *domain.KYCDocument) error
	FindDocumentByID(ctx context.Context, id uuid.UUID) (*domain.KYCDocument, error)
	FindDocumentsByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.KYCDocument, error)
	FindDocumentsByUserIDAndType(ctx context.Context, userID uuid.UUID, docType domain.DocumentType) ([]*domain.KYCDocument, error)
	UpdateDocumentVerificationStatus(ctx context.Context, id uuid.UUID, status domain.DocumentVerificationStatus, notes string, verifiedBy uuid.UUID) error
	UpdateDocumentVirusScanStatus(ctx context.Context, id uuid.UUID, status domain.VirusScanStatus, resultStr, engine, version string) error
	FindPendingVirusScan(ctx context.Context, limit int) ([]*domain.KYCDocument, error)
	FindPendingVerification(ctx context.Context, limit, offset int) ([]*domain.KYCDocument, error)
	CountDocumentsByUserIDAndStatus(ctx context.Context, userID uuid.UUID, status domain.DocumentVerificationStatus) (int, error)
	DeleteDocument(ctx context.Context, id uuid.UUID) error
}

// UserRepository defines user-related operations needed for KYC
type UserRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status domain.KYCStatus, profileStatus *domain.KYCSubmissionStatus) error
	UpdateKYCLevel(ctx context.Context, userID uuid.UUID, level int, updateProfile bool) error
	UpdateKYCStatusTx(ctx context.Context, tx Transaction, userID uuid.UUID, status domain.KYCStatus, profileStatus *domain.KYCSubmissionStatus) error
	UpdateKYCLevelTx(ctx context.Context, tx Transaction, userID uuid.UUID, level int, updateProfile bool) error
	UpdateUserRiskScoreTx(ctx context.Context, tx Transaction, userID uuid.UUID, riskScore decimal.Decimal) error
	FindByIDTx(ctx context.Context, tx Transaction, id uuid.UUID) (*domain.User, error)
	GetUserKYCProfile(ctx context.Context, userID uuid.UUID) (*domain.KYCProfile, error)
	GetUserKYCRequirements(ctx context.Context, userID uuid.UUID) (*domain.KYCRequirements, error)
	UpdateUserWithKYCProfile(ctx context.Context, user *domain.User, profile *domain.KYCProfile) error
	UserHasKYCProfile(ctx context.Context, userID uuid.UUID) (bool, error)
	GetUserKYCStatus(ctx context.Context, userID uuid.UUID) (*domain.KYCStatusResponse, error)
	UpdateUserRiskScore(ctx context.Context, userID uuid.UUID, riskScore decimal.Decimal) error
}

type Transaction interface {
	Commit() error
	Rollback() error
	// Optional: Get the underlying transaction ID or context
	GetID() string
}

// ==============================================================================
// KYC SERVICE STRUCT WITH DEPENDENCIES
// ==============================================================================

// KYCService implements KYC business logic with AML checks, document verification,
// and compliance workflows following microservices patterns from auth, wallet, etc.
type KYCService struct {
	repo               Repository                   // KYC data persistence
	userRepo           UserRepository               // User operations
	transactionManager *TransactionManager          // Transaction management
	fileUploadService  fileupload.FileUploadService // Document storage
	virusScanService   virusscan.VirusScanService   // Security scanning
	amlService         aml.AMLService               // AML compliance checks
	config             *config.Config               // Application configuration
	logger             logger.Logger                // Structured logging

	// Asynchronous processing channels (following settlement service pattern)
	documentQueue     chan *documentProcessingJob
	verificationQueue chan *verificationJob
}

// ==============================================================================
// TRANSACTION TYPES
// ==============================================================================

// TransactionalContext holds the transaction and related context
type TransactionalContext struct {
	Tx  Transaction
	Ctx context.Context
}

// TransactionManager manages database transactions
type TransactionManager struct {
	repo Repository
}

// NewTransactionManager creates a new transaction manager
func NewTransactionManager(repo Repository) *TransactionManager {
	return &TransactionManager{repo: repo}
}

// WithTransaction executes a function within a transaction
func (tm *TransactionManager) WithTransaction(ctx context.Context, fn func(txCtx *TransactionalContext) error) error {
	// Start transaction
	tx, err := tm.repo.BeginTransaction(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	txCtx := &TransactionalContext{
		Tx:  tx,
		Ctx: ctx,
	}

	// Always rollback on error, commit on success
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r) // Re-panic after rollback
		}
	}()

	// Execute the function
	if err := fn(txCtx); err != nil {
		tx.Rollback()
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ==============================================================================
// COMPLIANCE REQUIREMENTS CHECK - TASK 5.2.6
// ==============================================================================

// ComplianceCheckResult contains the result of checking KYC requirements
type ComplianceCheckResult struct {
	Requirements     *domain.KYCRequirements `json:"requirements"`
	IsCompliant      bool                    `json:"is_compliant"`
	MissingFields    []string                `json:"missing_fields,omitempty"`
	MissingDocuments []domain.DocumentType   `json:"missing_documents,omitempty"`
	ValidationErrors []ValidationError       `json:"validation_errors,omitempty"`
	RequirementsID   uuid.UUID               `json:"requirements_id"`
	ComplianceScore  float64                 `json:"compliance_score"`
	MeetsMinimum     bool                    `json:"meets_minimum"`
	WarningMessages  []string                `json:"warning_messages,omitempty"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

var ErrUserInactive = errors.New("user is inactive")

func (k *KYCService) startVerificationWorker() {
	panic("unimplemented")
}

func (k *KYCService) startDocumentWorker() {
	panic("unimplemented")
}

// documentProcessingJob represents a document to be processed asynchronously
type documentProcessingJob struct {
	documentID uuid.UUID
	filePath   string
	userID     uuid.UUID
}

// verificationJob represents a KYC profile to be verified asynchronously
type verificationJob struct {
	profileID uuid.UUID
	userID    uuid.UUID
}

// ==============================================================================
// SERVICE CONSTRUCTOR
// ==============================================================================

// NewKYCService creates a new KYC service with all required dependencies
func NewKYCService(
	repo Repository,
	userRepo UserRepository,
	fileUploadService fileupload.FileUploadService,
	virusScanService virusscan.VirusScanService,
	amlService aml.AMLService,
	config *config.Config,
	logger logger.Logger,
) *KYCService {
	service := &KYCService{
		repo:               repo,
		userRepo:           userRepo,
		transactionManager: NewTransactionManager(repo),
		fileUploadService:  fileUploadService,
		virusScanService:   virusScanService,
		amlService:         amlService,
		config:             config,
		logger:             logger,
		documentQueue:      make(chan *documentProcessingJob, 100), // Buffer for async processing
		verificationQueue:  make(chan *verificationJob, 50),        // Buffer for verification workflow
	}

	// Start asynchronous workers (following settlement service pattern)
	go service.startDocumentWorker()
	go service.startVerificationWorker()

	return service
}

// SubmitKYCData validation helpers
func (s *KYCService) validateKYCSubmission(ctx context.Context, req *domain.SubmitKYCRequest, userID uuid.UUID) ([]ValidationError, error) {
	var validationErrors []ValidationError

	// Validate user exists and is active
	user, err := s.validateUser(ctx, userID)
	if err != nil {
		return nil, err // Return system error, not validation error
	}

	// Validate country_code is valid ISO 3166-1 alpha-2
	if err := s.validateCountryCode(req.CountryCode); err != nil {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "country_code",
			Message: err.Error(),
		})
	}

	// Validate profile_type matches user_type
	if err := s.validateProfileTypeMatchesUserType(req.ProfileType, user.UserType); err != nil {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "profile_type",
			Message: err.Error(),
		})
	}

	// Validate required fields based on profile_type
	profileErrors := s.validateRequiredFields(req)
	validationErrors = append(validationErrors, profileErrors...)

	// Validate age >= 18 (for individuals)
	if req.ProfileType == domain.KYCProfileTypeIndividual && req.DateOfBirth != nil {
		if err := s.validateAge(*req.DateOfBirth); err != nil {
			validationErrors = append(validationErrors, ValidationError{
				Field:   "date_of_birth",
				Message: err.Error(),
			})
		}
	}

	// Validate income/turnover ranges
	if err := s.validateIncomeTurnoverRanges(req); err != nil {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "income_turnover",
			Message: err.Error(),
		})
	}

	// Validate address information completeness
	if err := s.validateAddress(req); err != nil {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "address",
			Message: err.Error(),
		})
	}

	// Validate business-specific fields if applicable
	if req.ProfileType == domain.KYCProfileTypeBusiness {
		businessErrors := s.validateBusinessFields(req)
		validationErrors = append(validationErrors, businessErrors...)
	}

	return validationErrors, nil
}

// validateUser checks if user exists and is active
func (s *KYCService) validateUser(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		// Check if it's a "user not found" error
		if errors.Is(err, kyderrors.ErrUserNotFound) {
			return nil, kyderrors.ErrUserNotFound
		}
		return nil, kyderrors.Wrap(err, "failed to find user")
	}

	if !user.IsActive {
		return nil, ErrUserInactive
	}

	return user, nil
}

// validateCountryCode validates ISO 3166-1 alpha-2 country code
func (s *KYCService) validateCountryCode(countryCode string) error {
	// Basic format validation: 2 uppercase letters
	regex := regexp.MustCompile(`^[A-Z]{2}$`)
	if !regex.MatchString(countryCode) {
		return errors.New("invalid country code format, must be 2 uppercase letters (ISO 3166-1 alpha-2)")
	}

	// Check if country is in supported list (you can extend this list)
	supportedCountries := map[string]bool{
		"US": true, "GB": true, "DE": true, "FR": true, "IT": true,
		"ES": true, "NL": true, "BE": true, "LU": true, "CH": true,
		"CN": true, "JP": true, "KR": true, "SG": true, "HK": true,
		"AU": true, "CA": true, "IN": true, "BR": true, "MX": true,
	}

	if !supportedCountries[countryCode] {
		return errors.New("country not supported for KYC processing")
	}

	return nil
}

// validateProfileTypeMatchesUserType ensures profile_type matches user_type
func (s *KYCService) validateProfileTypeMatchesUserType(profileType domain.KYCProfileType, userType domain.UserType) error {
	// Convert both to string for comparison since they're likely string-based types
	userTypeStr := string(userType)
	profileTypeStr := string(profileType)

	// Check individual users
	if userTypeStr == "individual" {
		if profileTypeStr != "individual" {
			return errors.New("individual users must submit individual KYC profiles")
		}
	}

	// Check business/merchant/agent users
	if userTypeStr == "business" || userTypeStr == "merchant" || userTypeStr == "agent" {
		if profileTypeStr != "business" {
			return errors.New("business/merchant/agent users must submit business KYC profiles")
		}
	}

	// If we get here with an unknown user type, return error
	if userTypeStr != "individual" && userTypeStr != "business" &&
		userTypeStr != "merchant" && userTypeStr != "agent" {
		return errors.New("invalid user type")
	}

	return nil
}

// validateRequiredFields checks required fields based on profile type
func (s *KYCService) validateRequiredFields(req *domain.SubmitKYCRequest) []ValidationError {
	var errors []ValidationError

	// Common required fields for all profiles
	if req.AddressLine1 == "" {
		errors = append(errors, ValidationError{
			Field:   "address_line1",
			Message: "address line 1 is required",
		})
	}

	if req.City == "" {
		errors = append(errors, ValidationError{
			Field:   "city",
			Message: "city is required",
		})
	}

	if req.CountryCode == "" {
		errors = append(errors, ValidationError{
			Field:   "country_code",
			Message: "country code is required",
		})
	}

	// Individual-specific required fields
	if req.ProfileType == domain.KYCProfileTypeIndividual {
		if req.DateOfBirth == nil {
			errors = append(errors, ValidationError{
				Field:   "date_of_birth",
				Message: "date of birth is required for individual profiles",
			})
		}

		if req.Nationality == "" {
			errors = append(errors, ValidationError{
				Field:   "nationality",
				Message: "nationality is required for individual profiles",
			})
		}

		if req.Occupation == "" {
			errors = append(errors, ValidationError{
				Field:   "occupation",
				Message: "occupation is required for individual profiles",
			})
		}

		if req.SourceOfFunds == "" {
			errors = append(errors, ValidationError{
				Field:   "source_of_funds",
				Message: "source of funds is required for individual profiles",
			})
		}
	}

	// Business-specific required fields
	if req.ProfileType == domain.KYCProfileTypeBusiness {
		if req.CompanyName == "" {
			errors = append(errors, ValidationError{
				Field:   "company_name",
				Message: "company name is required for business profiles",
			})
		}

		if req.CompanyRegistrationNumber == "" {
			errors = append(errors, ValidationError{
				Field:   "company_registration_number",
				Message: "company registration number is required for business profiles",
			})
		}

		if req.BusinessNature == "" {
			errors = append(errors, ValidationError{
				Field:   "business_nature",
				Message: "business nature is required for business profiles",
			})
		}

		if req.IncorporationDate == nil {
			errors = append(errors, ValidationError{
				Field:   "incorporation_date",
				Message: "incorporation date is required for business profiles",
			})
		}
	}

	return errors
}

// validateAge checks if the user is at least 18 years old
func (s *KYCService) validateAge(dateOfBirth time.Time) error {
	now := time.Now()
	age := now.Year() - dateOfBirth.Year()

	// Adjust if birthday hasn't occurred this year
	if now.YearDay() < dateOfBirth.YearDay() {
		age--
	}

	if age < 18 {
		return errors.New("user must be at least 18 years old")
	}

	// Also check maximum reasonable age (optional)
	if age > 120 {
		return errors.New("invalid date of birth")
	}

	return nil
}

// validateIncomeTurnoverRanges validates income/turnover ranges
func (s *KYCService) validateIncomeTurnoverRanges(req *domain.SubmitKYCRequest) error {
	// Validate income range for individuals
	if req.ProfileType == domain.KYCProfileTypeIndividual && req.AnnualIncomeRange != "" {
		validIncomeRanges := map[string]bool{
			string(domain.IncomeRangeLessThan10K): true,
			string(domain.IncomeRange10KTo50K):    true,
			string(domain.IncomeRange50KTo100K):   true,
			string(domain.IncomeRange100KTo250K):  true,
			string(domain.IncomeRange250KTo500K):  true,
			string(domain.IncomeRange500KTo1M):    true,
			string(domain.IncomeRangeOver1M):      true,
		}

		if !validIncomeRanges[string(req.AnnualIncomeRange)] {
			return errors.New("invalid annual income range")
		}
	}

	// Validate turnover range for businesses
	if req.ProfileType == domain.KYCProfileTypeBusiness && req.AnnualTurnoverRange != "" {
		validTurnoverRanges := map[string]bool{
			string(domain.TurnoverRangeLessThan50K): true,
			string(domain.TurnoverRange50KTo250K):   true,
			string(domain.TurnoverRange250KTo1M):    true,
			string(domain.TurnoverRange1MTo5M):      true,
			string(domain.TurnoverRange5MTo10M):     true,
			string(domain.TurnoverRange10MTo50M):    true,
			string(domain.TurnoverRangeOver50M):     true,
		}

		if !validTurnoverRanges[string(req.AnnualTurnoverRange)] {
			return errors.New("invalid annual turnover range")
		}
	}

	return nil
}

// validateAddress validates address completeness
func (s *KYCService) validateAddress(req *domain.SubmitKYCRequest) error {
	// Already checked address_line1, city, country_code in validateRequiredFields
	// Additional address validations can go here

	// Validate postal code format based on country (simplified)
	if req.PostalCode != "" {
		switch req.CountryCode {
		case "US":
			// US ZIP code: 5 digits or 5+4
			regex := regexp.MustCompile(`^\d{5}(-\d{4})?$`)
			if !regex.MatchString(req.PostalCode) {
				return errors.New("invalid US ZIP code format")
			}
		case "GB":
			// UK postcode: AA1 1AA or similar
			regex := regexp.MustCompile(`^[A-Z]{1,2}\d[A-Z\d]? ?\d[A-Z]{2}$`)
			if !regex.MatchString(strings.ToUpper(req.PostalCode)) {
				return errors.New("invalid UK postcode format")
			}
		case "CA":
			// Canadian postal code: A1A 1A1
			regex := regexp.MustCompile(`^[A-Z]\d[A-Z] \d[A-Z]\d$`)
			if !regex.MatchString(strings.ToUpper(req.PostalCode)) {
				return errors.New("invalid Canadian postal code format")
			}
			// Add more country-specific validations as needed
		}
	}

	return nil
}

// validateBusinessFields validates business-specific fields
func (s *KYCService) validateBusinessFields(req *domain.SubmitKYCRequest) []ValidationError {
	var errors []ValidationError

	// Validate business registration number format
	if req.CompanyRegistrationNumber != "" {
		// Remove common separators for validation
		cleanRegNum := strings.ToUpper(strings.ReplaceAll(req.CompanyRegistrationNumber, " ", ""))

		// Basic validation - at least 5 alphanumeric characters
		if len(cleanRegNum) < 5 {
			errors = append(errors, ValidationError{
				Field:   "company_registration_number",
				Message: "registration number is too short",
			})
		}
	}

	// Validate tax ID format
	if req.CompanyTaxID != "" {
		cleanTaxID := strings.ToUpper(strings.ReplaceAll(req.CompanyTaxID, " ", ""))

		// Basic validation - at least 9 characters for most tax IDs
		if len(cleanTaxID) < 9 {
			errors = append(errors, ValidationError{
				Field:   "company_tax_id",
				Message: "tax ID appears to be invalid",
			})
		}
	}

	// Validate number of employees - FIXED VERSION
	if req.NumberOfEmployees != nil {
		// Dereference the pointer to get the actual value
		if *req.NumberOfEmployees < 0 {
			errors = append(errors, ValidationError{
				Field:   "number_of_employees",
				Message: "number of employees cannot be negative",
			})
		}

		// Additional validation: reasonable maximum
		if *req.NumberOfEmployees > 1000000 {
			errors = append(errors, ValidationError{
				Field:   "number_of_employees",
				Message: "number of employees exceeds reasonable maximum",
			})
		}
	}

	// Validate incorporation date is not in the future
	if req.IncorporationDate != nil && req.IncorporationDate.After(time.Now()) {
		errors = append(errors, ValidationError{
			Field:   "incorporation_date",
			Message: "incorporation date cannot be in the future",
		})
	}

	// Validate business industry
	if req.BusinessIndustry != "" {
		validIndustries := map[string]bool{
			"technology": true, "finance": true, "healthcare": true,
			"retail": true, "manufacturing": true, "real_estate": true,
			"transportation": true, "agriculture": true, "education": true,
			"entertainment": true, "energy": true, "construction": true,
		}

		if !validIndustries[strings.ToLower(req.BusinessIndustry)] {
			errors = append(errors, ValidationError{
				Field:   "business_industry",
				Message: "invalid business industry",
			})
		}
	}

	return errors
}

// normalizePhoneNumber validates and normalizes phone numbers
func (s *KYCService) normalizePhoneNumber(phone string, countryCode string) (string, error) {
	if phone == "" {
		return "", nil
	}

	// Remove all non-numeric characters
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(phone, "")

	// Basic validation based on country
	switch countryCode {
	case "US", "CA":
		if len(digits) != 10 {
			return "", errors.New("phone number must be 10 digits for US/Canada")
		}
	case "GB":
		if len(digits) < 10 || len(digits) > 15 {
			return "", errors.New("invalid UK phone number length")
		}
	default:
		if len(digits) < 5 || len(digits) > 15 {
			return "", errors.New("invalid phone number length")
		}
	}

	return digits, nil
}

// ==============================================================================
// EXISTING KYC CHECK - TASK 5.2.2
// ==============================================================================

// ExistingProfileAction represents the action to take based on existing KYC status
type ExistingProfileAction string

const (
	ActionCreateNew         ExistingProfileAction = "create_new"
	ActionUpdateExisting    ExistingProfileAction = "update_existing"
	ActionCreateNewVersion  ExistingProfileAction = "create_new_version"
	ActionErrorCannotModify ExistingProfileAction = "error_cannot_modify"
)

// ==============================================================================
// AML CHECK INITIATION - TASK 5.2.5
// ==============================================================================

// AMLCheckInitiationResult contains the result of AML check initiation
type AMLCheckInitiationResult struct {
	CheckID       string `json:"check_id"`
	CheckType     string `json:"check_type"`
	Status        string `json:"status"`
	EstimatedTime string `json:"estimated_time,omitempty"`
	IsAsync       bool   `json:"is_async"`
	Message       string `json:"message,omitempty"`
}

// AMLProfile represents the data needed for AML screening
type AMLProfile struct {
	UserID           uuid.UUID             `json:"user_id"`
	ProfileID        uuid.UUID             `json:"profile_id"`
	ProfileType      domain.KYCProfileType `json:"profile_type"`
	CountryCode      string                `json:"country_code"`
	Nationality      string                `json:"nationality,omitempty"`
	Occupation       string                `json:"occupation,omitempty"`
	IncomeRange      domain.IncomeRange    `json:"income_range,omitempty"`
	SourceOfFunds    domain.SourceOfFunds  `json:"source_of_funds,omitempty"`
	BusinessName     string                `json:"business_name,omitempty"`
	BusinessIndustry string                `json:"business_industry,omitempty"`
	RiskScore        decimal.Decimal       `json:"risk_score"`
	Amount           decimal.Decimal       `json:"amount"`
}

// ExistingProfileCheckResult contains the result of checking existing KYC profile
type ExistingProfileCheckResult struct {
	ExistingProfile *domain.KYCProfile    `json:"existing_profile,omitempty"`
	Action          ExistingProfileAction `json:"action"`
	Message         string                `json:"message,omitempty"`
	Allowed         bool                  `json:"allowed"`
	ErrorCode       string                `json:"error_code,omitempty"`
}

// checkExistingKYCProfile checks the existing KYC status and determines the action
func (s *KYCService) checkExistingKYCProfile(ctx context.Context, userID uuid.UUID) (*ExistingProfileCheckResult, error) {
	// Check if user already has a KYC profile
	exists, err := s.repo.ExistsByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing KYC profile: %w", err)
	}

	// No existing profile - create new
	if !exists {
		return &ExistingProfileCheckResult{
			Action:  ActionCreateNew,
			Message: "No existing KYC profile found",
			Allowed: true,
		}, nil
	}

	// Get the existing profile
	existingProfile, err := s.repo.FindProfileByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing KYC profile: %w", err)
	}

	// Determine action based on submission status
	switch existingProfile.SubmissionStatus {
	case domain.KYCSubmissionStatusDraft:
		// Draft status - can update existing
		return &ExistingProfileCheckResult{
			ExistingProfile: existingProfile,
			Action:          ActionUpdateExisting,
			Message:         "Existing draft profile can be updated",
			Allowed:         true,
		}, nil

	case domain.KYCSubmissionStatusSubmitted,
		domain.KYCSubmissionStatusUnderReview,
		domain.KYCSubmissionStatusAdditionalInfoRequired:
		// Profile is in review process - cannot modify
		return &ExistingProfileCheckResult{
			ExistingProfile: existingProfile,
			Action:          ActionErrorCannotModify,
			Message:         fmt.Sprintf("Cannot modify KYC profile while in %s status", existingProfile.SubmissionStatus),
			Allowed:         false,
			ErrorCode:       "KYC_PROFILE_IN_REVIEW",
		}, nil

	case domain.KYCSubmissionStatusApproved:
		// Profile is approved - check if new version is allowed
		return s.handleApprovedProfile(existingProfile)

	case domain.KYCSubmissionStatusRejected:
		// Profile is rejected - check if new version is allowed
		return s.handleRejectedProfile(existingProfile)

	case domain.KYCSubmissionStatusSuspended:
		// Profile is suspended - cannot modify
		return &ExistingProfileCheckResult{
			ExistingProfile: existingProfile,
			Action:          ActionErrorCannotModify,
			Message:         "Cannot modify suspended KYC profile",
			Allowed:         false,
			ErrorCode:       "KYC_PROFILE_SUSPENDED",
		}, nil

	default:
		// Unknown status
		return &ExistingProfileCheckResult{
			ExistingProfile: existingProfile,
			Action:          ActionErrorCannotModify,
			Message:         fmt.Sprintf("Unknown KYC profile status: %s", existingProfile.SubmissionStatus),
			Allowed:         false,
			ErrorCode:       "KYC_UNKNOWN_STATUS",
		}, nil
	}
}

// checkComplianceRequirements validates the KYC profile against country/user-type specific requirements
func (s *KYCService) checkComplianceRequirements(
	ctx context.Context,
	profile *domain.KYCProfile,
	user *domain.User,
) (*ComplianceCheckResult, error) {
	// Fetch requirements for the user's country, user type, and KYC level
	requirements, err := s.repo.FindRequirementsByCountryAndUserType(
		ctx,
		profile.CountryCode,
		string(user.UserType),
		profile.KYCLevel,
	)
	if err != nil {
		// If no specific requirements found, use default requirements
		s.logger.Warn("No specific KYC requirements found, using defaults", map[string]interface{}{
			"country_code": profile.CountryCode,
			"user_type":    user.UserType,
			"kyc_level":    profile.KYCLevel,
			"error":        err.Error(),
		})

		// Create default requirements
		requirements = s.createDefaultRequirements(profile.CountryCode, string(user.UserType), profile.KYCLevel)
	}

	// Validate against requirements
	missingFields, validationErrors := s.validateAgainstRequirements(profile, user, requirements)
	missingDocuments := s.checkRequiredDocuments(profile, requirements)

	// Calculate compliance score
	complianceScore := s.calculateComplianceScore(profile, requirements, missingFields, missingDocuments)

	// Determine if meets minimum requirements
	meetsMinimum := s.meetsMinimumRequirements(profile, requirements, missingFields, missingDocuments)

	// Generate warning messages
	warningMessages := s.generateComplianceWarnings(profile, requirements, missingFields, missingDocuments)

	result := &ComplianceCheckResult{
		Requirements:     requirements,
		IsCompliant:      len(missingFields) == 0 && len(missingDocuments) == 0,
		MissingFields:    missingFields,
		MissingDocuments: missingDocuments,
		ValidationErrors: validationErrors,
		RequirementsID:   requirements.ID,
		ComplianceScore:  complianceScore,
		MeetsMinimum:     meetsMinimum,
		WarningMessages:  warningMessages,
	}

	// Store requirements reference in profile metadata
	s.storeRequirementsReference(profile, result)

	return result, nil
}

// createDefaultRequirements creates default KYC requirements when none are found
func (s *KYCService) createDefaultRequirements(countryCode, userType string, kycLevel int) *domain.KYCRequirements {
	now := time.Now()

	return &domain.KYCRequirements{
		ID:          uuid.New(),
		CountryCode: countryCode,
		UserType:    userType,
		KYCLevel:    kycLevel,

		// Default document requirements
		RequiredDocuments: []domain.DocumentType{
			domain.DocumentTypeNationalID,  // Proof of identity
			domain.DocumentTypeUtilityBill, // Proof of address
		},

		// Default required fields
		RequiredFields: s.getDefaultRequiredFields(userType),

		// Document specifications
		MaxFileSizeMB:     10,
		AllowedMimeTypes:  []string{"image/jpeg", "image/png", "application/pdf"},
		AllowedExtensions: []string{".jpg", ".jpeg", ".png", ".pdf"},

		// Default age requirement
		MinAgeYears: func() *int { age := 18; return &age }(),

		// Default review timeline
		EstimatedReviewDays:      3,
		ExpeditedReviewAvailable: false,
		ManualReviewRequired:     false,
		AutoApprovalThreshold:    decimal.NewFromInt(1000),

		// Status & versioning
		IsActive:      true,
		EffectiveFrom: now,
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// getDefaultRequiredFields returns default required fields based on user type
func (s *KYCService) getDefaultRequiredFields(userType string) []string {
	commonFields := []string{
		"address_line1",
		"city",
		"country_code",
		"phone_number",
	}

	if strings.ToLower(userType) == "individual" {
		individualFields := []string{
			"date_of_birth",
			"nationality",
			"occupation",
			"source_of_funds",
		}
		return append(commonFields, individualFields...)
	}

	// Business fields
	businessFields := []string{
		"company_name",
		"company_registration_number",
		"business_nature",
		"incorporation_date",
	}
	return append(commonFields, businessFields...)
}

// validateAgainstRequirements validates profile data against KYC requirements
func (s *KYCService) validateAgainstRequirements(
	profile *domain.KYCProfile,
	_ *domain.User,
	requirements *domain.KYCRequirements,
) ([]string, []ValidationError) {
	var missingFields []string
	var validationErrors []ValidationError

	// Check required fields
	for _, field := range requirements.RequiredFields {
		if !s.isFieldPresent(profile, field) {
			missingFields = append(missingFields, field)
		}
	}

	// Validate age if specified
	if requirements.MinAgeYears != nil && profile.DateOfBirth != nil {
		age := s.calculateAge(*profile.DateOfBirth)
		if age < *requirements.MinAgeYears {
			validationErrors = append(validationErrors, ValidationError{
				Field: "date_of_birth",
				Message: fmt.Sprintf("Minimum age requirement not met: %d years (provided: %d years)",
					*requirements.MinAgeYears, age),
			})
		}
	}

	// Validate income/turnover ranges
	if profile.ProfileType == domain.KYCProfileTypeIndividual && requirements.MinAnnualIncome != nil {
		minIncome := s.mapIncomeRangeToDecimal(*requirements.MinAnnualIncome)
		if s.isIncomeBelowMinimum(profile.AnnualIncomeRange, minIncome) {
			validationErrors = append(validationErrors, ValidationError{
				Field: "annual_income_range",
				Message: fmt.Sprintf("Minimum income requirement not met: %s",
					requirements.MinAnnualIncome.String()),
			})
		}
	}

	if profile.ProfileType == domain.KYCProfileTypeBusiness && requirements.MinBusinessTurnover != nil {
		minTurnover := s.mapTurnoverRangeToDecimal(*requirements.MinBusinessTurnover)
		if s.isTurnoverBelowMinimum(profile.AnnualTurnoverRange, minTurnover) {
			validationErrors = append(validationErrors, ValidationError{
				Field: "annual_turnover_range",
				Message: fmt.Sprintf("Minimum turnover requirement not met: %s",
					requirements.MinBusinessTurnover.String()),
			})
		}
	}

	return missingFields, validationErrors
}

// checkRequiredDocuments checks which required documents are missing
func (s *KYCService) checkRequiredDocuments(
	profile *domain.KYCProfile,
	requirements *domain.KYCRequirements,
) []domain.DocumentType {
	// In a real implementation, this would check the database for existing documents
	// For now, we'll return all required documents as "missing" since document upload
	// is typically done separately from profile submission

	var missingDocs []domain.DocumentType

	// Check basic required documents
	for _, docType := range requirements.RequiredDocuments {
		missingDocs = append(missingDocs, docType)
	}

	// Add conditional documents based on profile type
	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		// Individuals need proof of income
		missingDocs = append(missingDocs, domain.DocumentTypeProofOfIncome)
	} else {
		// Businesses need additional documents
		missingDocs = append(missingDocs,
			domain.DocumentTypeBusinessRegistration,
			domain.DocumentTypeTaxCertificate,
			domain.DocumentTypeBusinessLicense,
		)
	}

	return missingDocs
}

// ==============================================================================
// ERROR HANDLING WITH TRANSACTIONS - TASK 5.2.10
// ==============================================================================

// mapTransactionError maps transaction errors to appropriate HTTP status codes
func (s *KYCService) mapTransactionError(err error) (int, string) {
	if err == nil {
		return http.StatusOK, "success"
	}

	// Check for specific transaction errors
	errStr := err.Error()

	// Database constraint violations
	if strings.Contains(errStr, "duplicate key") ||
		strings.Contains(errStr, "unique constraint") {
		return http.StatusConflict, "duplicate_record"
	}

	// Foreign key violations
	if strings.Contains(errStr, "foreign key") ||
		strings.Contains(errStr, "referential integrity") {
		return http.StatusBadRequest, "invalid_reference"
	}

	// Deadlock or timeout
	if strings.Contains(errStr, "deadlock") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "lock timeout") {
		return http.StatusConflict, "transaction_conflict"
	}

	// Connection errors
	if strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "lost connection") {
		return http.StatusServiceUnavailable, "database_unavailable"
	}

	// Generic transaction errors
	if strings.Contains(errStr, "transaction") ||
		strings.Contains(errStr, "rollback") ||
		strings.Contains(errStr, "commit") {
		return http.StatusInternalServerError, "transaction_error"
	}

	// Default
	return http.StatusInternalServerError, "internal_error"
}

// logTransactionError logs transaction errors with structured context
func (s *KYCService) logTransactionError(_ context.Context, err error, operation string, contextData map[string]interface{}) {
	statusCode, errorCode := s.mapTransactionError(err)

	logData := map[string]interface{}{
		"operation":   operation,
		"error":       err.Error(),
		"status_code": statusCode,
		"error_code":  errorCode,
		"logged_at":   time.Now().Format(time.RFC3339),
	}

	// Add context data if provided
	for key, value := range contextData {
		logData[key] = value
	}

	// Log at appropriate level based on status code
	if statusCode >= 500 {
		s.logger.Error("Transaction system error", logData)
	} else if statusCode >= 400 {
		s.logger.Warn("Transaction client error", logData)
	} else {
		s.logger.Info("Transaction completed", logData)
	}
}

// executeWithRetry executes a function with retry logic for transient failures
func (s *KYCService) executeWithRetry(ctx context.Context, maxRetries int, delay time.Duration, operation string, fn func() error) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !s.isRetryableError(err) {
			return err
		}

		// Log retry attempt
		s.logger.Warn("Retrying transaction operation", map[string]interface{}{
			"operation":     operation,
			"attempt":       attempt,
			"max_retries":   maxRetries,
			"error":         err.Error(),
			"next_retry_ms": delay.Milliseconds(),
		})

		// Wait before retry
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// Exponential backoff
				delay *= 2
			}
		}
	}

	return fmt.Errorf("operation '%s' failed after %d retries: %w", operation, maxRetries, lastErr)
}

// isRetryableError determines if an error is retryable
func (s *KYCService) isRetryableError(err error) bool {
	errStr := err.Error()

	// Retry on transient errors
	retryableErrors := []string{
		"deadlock",
		"timeout",
		"lock timeout",
		"connection",
		"network",
		"temporarily unavailable",
		"try again",
		"retry",
		"busy",
	}

	for _, retryable := range retryableErrors {
		if strings.Contains(strings.ToLower(errStr), retryable) {
			return true
		}
	}

	return false
}

// Helper methods for compliance checking
func (s *KYCService) isFieldPresent(profile *domain.KYCProfile, field string) bool {
	switch strings.ToLower(field) {
	case "address_line1":
		return profile.AddressLine1 != ""
	case "address_line2":
		return profile.AddressLine2 != ""
	case "city":
		return profile.City != ""
	case "state_province":
		return profile.StateProvince != ""
	case "postal_code":
		return profile.PostalCode != ""
	case "country_code":
		return profile.CountryCode != ""
	case "phone_number":
		return profile.PhoneNumber != ""
	case "alt_phone_number":
		return profile.AltPhoneNumber != ""
	case "date_of_birth":
		return profile.DateOfBirth != nil
	case "nationality":
		return profile.Nationality != ""
	case "occupation":
		return profile.Occupation != ""
	case "source_of_funds":
		return profile.SourceOfFunds != ""
	case "company_name":
		return profile.CompanyName != ""
	case "company_registration_number":
		return profile.CompanyRegistrationNumber != ""
	case "business_nature":
		return profile.BusinessNature != ""
	case "incorporation_date":
		return profile.IncorporationDate != nil
	default:
		return true // Unknown field, assume present
	}
}

func (s *KYCService) calculateAge(dateOfBirth time.Time) int {
	now := time.Now()
	age := now.Year() - dateOfBirth.Year()

	// Adjust if birthday hasn't occurred this year
	if now.YearDay() < dateOfBirth.YearDay() {
		age--
	}

	return age
}

func (s *KYCService) mapIncomeRangeToDecimal(minIncome decimal.Decimal) decimal.Decimal {
	// Map minimum income to a range threshold
	// This is simplified - in reality, you'd map to the appropriate range
	return minIncome
}

func (s *KYCService) mapTurnoverRangeToDecimal(minTurnover decimal.Decimal) decimal.Decimal {
	// Map minimum turnover to a range threshold
	return minTurnover
}

func (s *KYCService) isIncomeBelowMinimum(_ domain.IncomeRange, _ decimal.Decimal) bool {
	// Simplified check - in reality, you'd compare the range to the minimum
	return false // Default to passing
}

func (s *KYCService) isTurnoverBelowMinimum(_ domain.TurnoverRange, _ decimal.Decimal) bool {
	// Simplified check
	return false // Default to passing
}

func (s *KYCService) calculateComplianceScore(
	profile *domain.KYCProfile,
	requirements *domain.KYCRequirements,
	missingFields []string,
	missingDocuments []domain.DocumentType,
) float64 {
	totalRequirements := len(requirements.RequiredFields) + len(requirements.RequiredDocuments)
	if totalRequirements == 0 {
		return 100.0 // No requirements means fully compliant
	}

	metRequirements := totalRequirements - (len(missingFields) + len(missingDocuments))
	complianceScore := (float64(metRequirements) / float64(totalRequirements)) * 100

	// Adjust for high-risk profiles
	if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(50)) {
		complianceScore *= 0.8 // Reduce score by 20% for high risk
	}

	return complianceScore
}

func (s *KYCService) meetsMinimumRequirements(
	profile *domain.KYCProfile,
	requirements *domain.KYCRequirements,
	_ []string,
	_ []domain.DocumentType,
) bool {
	// Define critical fields that must be present
	criticalFields := []string{
		"address_line1",
		"city",
		"country_code",
		"phone_number",
	}

	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		criticalFields = append(criticalFields, "date_of_birth", "nationality")
	} else {
		criticalFields = append(criticalFields, "company_name", "company_registration_number")
	}

	// Check if all critical fields are present
	for _, field := range criticalFields {
		if !s.isFieldPresent(profile, field) {
			return false
		}
	}

	// Check if age requirement is met (if applicable)
	if requirements.MinAgeYears != nil && profile.DateOfBirth != nil {
		age := s.calculateAge(*profile.DateOfBirth)
		if age < *requirements.MinAgeYears {
			return false
		}
	}

	return true
}

func (s *KYCService) generateComplianceWarnings(
	profile *domain.KYCProfile,
	requirements *domain.KYCRequirements,
	missingFields []string,
	missingDocuments []domain.DocumentType,
) []string {
	var warnings []string

	// Warn about missing fields
	if len(missingFields) > 0 {
		warnings = append(warnings,
			fmt.Sprintf("Missing required fields: %s", strings.Join(missingFields, ", ")))
	}

	// Warn about missing documents
	if len(missingDocuments) > 0 {
		var docNames []string
		for _, docType := range missingDocuments {
			docNames = append(docNames, string(docType))
		}
		warnings = append(warnings,
			fmt.Sprintf("Required documents not uploaded: %s", strings.Join(docNames, ", ")))
	}

	// Warn about high-risk profiles
	if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(50)) {
		warnings = append(warnings, "High-risk profile identified - enhanced due diligence may be required")
	}

	// Warn about upcoming expiration (if applicable)
	if profile.DateOfBirth != nil && requirements.MinAgeYears != nil {
		age := s.calculateAge(*profile.DateOfBirth)
		if age >= *requirements.MinAgeYears && age < *requirements.MinAgeYears+1 {
			warnings = append(warnings, "User is close to minimum age requirement")
		}
	}

	return warnings
}

func (s *KYCService) storeRequirementsReference(profile *domain.KYCProfile, result *ComplianceCheckResult) {
	if profile.Metadata == nil {
		profile.Metadata = make(domain.Metadata)
	}

	profile.Metadata["compliance_check"] = map[string]interface{}{
		"requirements_id":   result.RequirementsID.String(),
		"checked_at":        time.Now().Format(time.RFC3339),
		"compliance_score":  result.ComplianceScore,
		"meets_minimum":     result.MeetsMinimum,
		"is_compliant":      result.IsCompliant,
		"missing_fields":    result.MissingFields,
		"missing_documents": result.MissingDocuments,
		"warning_count":     len(result.WarningMessages),
	}
}

// handleApprovedProfile determines if a new version can be created for approved profile
func (s *KYCService) handleApprovedProfile(profile *domain.KYCProfile) (*ExistingProfileCheckResult, error) {
	// Check if new version is allowed (configurable business rule)
	// For example: Allow new version after 30 days, or if KYC level needs upgrade

	// Simple rule: Check if profile was approved more than 30 days ago
	if profile.ApprovedAt != nil {
		daysSinceApproval := time.Since(*profile.ApprovedAt).Hours() / 24

		// If approved within last 30 days, don't allow new version
		if daysSinceApproval < 30 {
			return &ExistingProfileCheckResult{
				ExistingProfile: profile,
				Action:          ActionErrorCannotModify,
				Message:         "Cannot create new version: Profile was approved recently",
				Allowed:         false,
				ErrorCode:       "KYC_APPROVED_RECENTLY",
			}, nil
		}
	}

	// Allow creating new version
	return &ExistingProfileCheckResult{
		ExistingProfile: profile,
		Action:          ActionCreateNewVersion,
		Message:         "Can create new version of approved profile",
		Allowed:         true,
	}, nil
}

// handleRejectedProfile determines if a new version can be created for rejected profile
func (s *KYCService) handleRejectedProfile(profile *domain.KYCProfile) (*ExistingProfileCheckResult, error) {
	// For rejected profiles, we typically allow new submissions
	// But we might want to check the rejection reason or limit retries

	// Check if too many rejection attempts (e.g., more than 3)
	// This could be tracked in metadata or a separate counter

	// For now, always allow new version for rejected profiles
	return &ExistingProfileCheckResult{
		ExistingProfile: profile,
		Action:          ActionCreateNewVersion,
		Message:         "Can create new version of rejected profile",
		Allowed:         true,
	}, nil
}

// canCreateNewVersion checks if creating a new version is allowed based on business rules
func (s *KYCService) canCreateNewVersion(profile *domain.KYCProfile) bool {
	// Business rules for creating new versions:
	// 1. Check if user is allowed to have multiple versions (based on user tier)
	// 2. Check time since last update (e.g., must wait 30 days for major updates)
	// 3. Check if there are pending transactions that would be affected
	// 4. Check KYC level upgrade requirements

	// For now, implement simple rules:

	// Rule 1: Never allow if profile is suspended
	if profile.SubmissionStatus == domain.KYCSubmissionStatusSuspended {
		return false
	}

	// Rule 2: For approved profiles, only allow if approved more than 30 days ago
	if profile.SubmissionStatus == domain.KYCSubmissionStatusApproved {
		if profile.ApprovedAt == nil {
			return true // No approval date, allow
		}
		return time.Since(*profile.ApprovedAt).Hours() >= 24*30
	}

	// Rule 3: Always allow for rejected profiles (within limits)
	if profile.SubmissionStatus == domain.KYCSubmissionStatusRejected {
		if profile.RejectedAt == nil {
			return true
		}

		// Check if rejected too many times (e.g., more than 3 times in 90 days)
		// This would require tracking rejection count in metadata
		rejectionCount, ok := profile.Metadata["rejection_count"].(float64)
		if ok && rejectionCount > 3 {
			// Check if first rejection was within 90 days
			if firstRejection, ok := profile.Metadata["first_rejection_date"].(string); ok {
				if firstRejectionTime, err := time.Parse(time.RFC3339, firstRejection); err == nil {
					if time.Since(firstRejectionTime).Hours() < 24*90 {
						return false // Too many rejections in short time
					}
				}
			}
		}
		return true
	}

	// Default: Don't allow for other statuses
	return false
}

// archiveExistingProfile marks an existing profile as archived when creating new version
func (s *KYCService) archiveExistingProfile(ctx context.Context, profile *domain.KYCProfile) error {
	// Update the existing profile to mark it as superseded
	profile.SubmissionStatus = domain.KYCSubmissionStatusSuspended
	profile.ReviewNotes = fmt.Sprintf("Superseded by new version on %s", time.Now().Format(time.RFC3339))

	// Update metadata to track versioning
	if profile.Metadata == nil {
		profile.Metadata = make(map[string]interface{})
	}
	profile.Metadata["superseded_at"] = time.Now().Format(time.RFC3339)
	profile.Metadata["is_superseded"] = true

	// Save the archived profile
	return s.repo.UpdateProfile(ctx, profile)
}

// ==============================================================================
// BUILD KYC PROFILE - TASK 5.2.3
// ==============================================================================

// buildKYCProfile creates a new KYCProfile domain model from the request
func (s *KYCService) buildKYCProfile(req *domain.SubmitKYCRequest, userID uuid.UUID, isNewProfile, isDraft bool) (*domain.KYCProfile, error) {
	now := time.Now()

	// Determine submission status
	var submissionStatus domain.KYCSubmissionStatus
	if isDraft {
		submissionStatus = domain.KYCSubmissionStatusDraft
	} else {
		submissionStatus = domain.KYCSubmissionStatusSubmitted
	}

	// Start building the profile
	profile := &domain.KYCProfile{
		ID:          uuid.New(),
		UserID:      userID,
		ProfileType: req.ProfileType,

		// ========== INDIVIDUAL FIELDS ==========
		DateOfBirth:       req.DateOfBirth,
		PlaceOfBirth:      req.PlaceOfBirth,
		Nationality:       req.Nationality,
		Occupation:        req.Occupation,
		EmployerName:      req.EmployerName,
		AnnualIncomeRange: req.AnnualIncomeRange,
		SourceOfFunds:     req.SourceOfFunds,

		// ========== BUSINESS FIELDS ==========
		CompanyName:               req.CompanyName,
		CompanyRegistrationNumber: req.CompanyRegistrationNumber,
		CompanyTaxID:              req.CompanyTaxID,
		BusinessNature:            req.BusinessNature,
		IncorporationDate:         req.IncorporationDate,
		AnnualTurnoverRange:       req.AnnualTurnoverRange,
		NumberOfEmployees:         req.NumberOfEmployees,
		BusinessIndustry:          req.BusinessIndustry,

		// ========== ADDRESS INFORMATION ==========
		AddressLine1:  req.AddressLine1,
		AddressLine2:  req.AddressLine2,
		City:          req.City,
		StateProvince: req.StateProvince,
		PostalCode:    req.PostalCode,
		CountryCode:   req.CountryCode,

		// ========== CONTACT INFORMATION ==========
		PhoneNumber:    req.PhoneNumber,
		AltPhoneNumber: req.AltPhoneNumber,

		// ========== KYC STATUS TRACKING ==========
		SubmissionStatus: submissionStatus,
		KYCLevel:         1, // Default KYC level

		// ========== AML/CFT COMPLIANCE ==========
		AMLRiskScore:  decimal.Zero,            // Start with 0
		AMLStatus:     domain.AMLStatusPending, // Try AMLStatus instead of AMLCheckStatus
		PEPCheck:      false,                   // Will be set after AML check
		SanctionCheck: false,                   // Will be set after AML check

		// ========== AUDIT & METADATA ==========
		Metadata: s.buildKYCMetadata(req, isNewProfile),

		// ========== TIMESTAMPS ==========
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Set submitted_at if submitting (not draft)
	if !isDraft {
		currentTime := now
		profile.SubmittedAt = &currentTime
	}

	// Set conditional defaults based on profile type
	s.setProfileTypeDefaults(profile)

	// Validate that the profile has all required fields
	if err := s.validateProfileCompleteness(profile); err != nil {
		return nil, fmt.Errorf("incomplete KYC profile: %w", err)
	}

	return profile, nil
}

// buildKYCMetadata creates metadata JSON for flexible field storage
func (s *KYCService) buildKYCMetadata(req *domain.SubmitKYCRequest, isNewProfile bool) domain.Metadata {
	metadata := make(domain.Metadata)

	// Track submission context
	metadata["submission_context"] = map[string]interface{}{
		"is_new_profile": isNewProfile,
		"submitted_at":   time.Now().Format(time.RFC3339),
		"client_ip":      "", // Would be set by handler/middleware
		"user_agent":     "", // Would be set by handler/middleware
		"api_version":    "1.0",
	}

	// Track source of profile creation (API, admin portal, etc.)
	metadata["source"] = "api"

	// Store validation flags
	metadata["validation"] = map[string]interface{}{
		"auto_validated":      false,
		"validation_attempts": 0,
		"last_validated_at":   nil,
	}

	// Store workflow tracking
	metadata["workflow"] = map[string]interface{}{
		"current_step":     "profile_creation",
		"next_step":        "document_upload",
		"assigned_to":      nil,
		"escalation_level": 0,
	}

	// Store compliance info
	metadata["compliance"] = map[string]interface{}{
		"regulatory_jurisdiction": req.CountryCode,
		"data_retention_policy":   "kyc_compliance",
		"gdpr_compliant":          true,
		"consent_obtained":        false,
	}

	return metadata
}

// setProfileTypeDefaults sets conditional defaults based on profile type
func (s *KYCService) setProfileTypeDefaults(profile *domain.KYCProfile) {
	switch profile.ProfileType {
	case domain.KYCProfileTypeIndividual:
		// Individual-specific defaults
		if profile.SourceOfFunds == "" {
			profile.SourceOfFunds = domain.SourceOfFundsOther
		}

		// Calculate and set risk factors for individuals
		profile.Metadata["risk_factors"] = map[string]interface{}{
			"is_pep":                  false,
			"has_adverse_media":       false,
			"high_risk_country":       s.isHighRiskCountry(profile.CountryCode),
			"high_value_individual":   s.isHighValueIndividual(profile.AnnualIncomeRange),
			"cash_intensive_business": false,
		}

	case domain.KYCProfileTypeBusiness:
		// Business-specific defaults
		if profile.BusinessIndustry == "" {
			profile.BusinessIndustry = "other"
		}

		// Calculate and set risk factors for businesses
		profile.Metadata["risk_factors"] = map[string]interface{}{
			"business_type":      profile.BusinessNature,
			"high_risk_industry": s.isHighRiskIndustry(profile.BusinessIndustry),
			"high_risk_country":  s.isHighRiskCountry(profile.CountryCode),
			"cash_intensive":     s.isCashIntensiveBusiness(profile.BusinessIndustry),
			"non_profit": strings.Contains(strings.ToLower(profile.BusinessNature), "non-profit") ||
				strings.Contains(strings.ToLower(profile.BusinessNature), "charity"),
		}

		// Set EDD (Enhanced Due Diligence) flags
		profile.EDDRequired = s.isEDDRequired(profile)
		if profile.EDDRequired {
			profile.EDDLevel = 1
			profile.EDDReviewDate = s.calculateEDDReviewDate()
		}
	}

	// Calculate initial risk score based on profile type and factors
	profile.AMLRiskScore = s.calculateInitialRiskScore(profile)
}

// validateProfileCompleteness ensures all required fields are populated
func (s *KYCService) validateProfileCompleteness(profile *domain.KYCProfile) error {
	var missingFields []string

	// Common required fields for all profiles
	if profile.AddressLine1 == "" {
		missingFields = append(missingFields, "address_line1")
	}
	if profile.City == "" {
		missingFields = append(missingFields, "city")
	}
	if profile.CountryCode == "" {
		missingFields = append(missingFields, "country_code")
	}

	// Profile type specific fields
	switch profile.ProfileType {
	case domain.KYCProfileTypeIndividual:
		if profile.DateOfBirth == nil {
			missingFields = append(missingFields, "date_of_birth")
		}
		if profile.Nationality == "" {
			missingFields = append(missingFields, "nationality")
		}
		if profile.Occupation == "" {
			missingFields = append(missingFields, "occupation")
		}
		if profile.SourceOfFunds == "" {
			missingFields = append(missingFields, "source_of_funds")
		}

		// Validate age is at least 18
		if profile.DateOfBirth != nil {
			if !s.isAtLeast18(*profile.DateOfBirth) {
				return errors.New("individual must be at least 18 years old")
			}
		}

	case domain.KYCProfileTypeBusiness:
		if profile.CompanyName == "" {
			missingFields = append(missingFields, "company_name")
		}
		if profile.CompanyRegistrationNumber == "" {
			missingFields = append(missingFields, "company_registration_number")
		}
		if profile.BusinessNature == "" {
			missingFields = append(missingFields, "business_nature")
		}
		if profile.IncorporationDate == nil {
			missingFields = append(missingFields, "incorporation_date")
		}

		// Validate incorporation date is not in future
		if profile.IncorporationDate != nil && profile.IncorporationDate.After(time.Now()) {
			return errors.New("incorporation date cannot be in the future")
		}
	}

	if len(missingFields) > 0 {
		return fmt.Errorf("missing required fields: %v", missingFields)
	}

	return nil
}

// Helper methods for risk assessment and defaults

func (s *KYCService) isHighRiskCountry(countryCode string) bool {
	highRiskCountries := map[string]bool{
		"AF": true, // Afghanistan
		"IR": true, // Iran
		"KP": true, // North Korea
		"SD": true, // Sudan
		"SY": true, // Syria
		"YE": true, // Yemen
		"ZW": true, // Zimbabwe
	}
	return highRiskCountries[countryCode]
}

func (s *KYCService) isHighValueIndividual(incomeRange domain.IncomeRange) bool {
	highValueRanges := map[domain.IncomeRange]bool{
		domain.IncomeRange500KTo1M: true,
		domain.IncomeRangeOver1M:   true,
	}
	return highValueRanges[incomeRange]
}

func (s *KYCService) isHighRiskIndustry(industry string) bool {
	highRiskIndustries := map[string]bool{
		"casino":              true,
		"gambling":            true,
		"cryptocurrency":      true,
		"precious_metals":     true,
		"arms":                true,
		"adult_entertainment": true,
	}
	return highRiskIndustries[strings.ToLower(industry)]
}

func (s *KYCService) isCashIntensiveBusiness(industry string) bool {
	cashIntensiveIndustries := map[string]bool{
		"restaurant":        true,
		"retail":            true,
		"gas_station":       true,
		"convenience_store": true,
		"car_dealership":    true,
		"jewelry":           true,
	}
	return cashIntensiveIndustries[strings.ToLower(industry)]
}

func (s *KYCService) isEDDRequired(profile *domain.KYCProfile) bool {
	// Enhanced Due Diligence is required for:
	// 1. High risk countries
	// 2. High risk industries
	// 3. High turnover businesses
	// 4. PEPs or sanctioned entities

	if profile.ProfileType != domain.KYCProfileTypeBusiness {
		return false
	}

	// Check high risk country
	if s.isHighRiskCountry(profile.CountryCode) {
		return true
	}

	// Check high risk industry
	if s.isHighRiskIndustry(profile.BusinessIndustry) {
		return true
	}

	// Check high turnover (over 1M)
	highTurnoverRanges := map[domain.TurnoverRange]bool{
		domain.TurnoverRange1MTo5M:   true,
		domain.TurnoverRange5MTo10M:  true,
		domain.TurnoverRange10MTo50M: true,
		domain.TurnoverRangeOver50M:  true,
	}
	if highTurnoverRanges[profile.AnnualTurnoverRange] {
		return true
	}

	return false
}

func (s *KYCService) calculateEDDReviewDate() *time.Time {
	// EDD reviews typically every 12 months
	reviewDate := time.Now().AddDate(1, 0, 0)
	return &reviewDate
}

func (s *KYCService) calculateInitialRiskScore(profile *domain.KYCProfile) decimal.Decimal {
	riskScore := decimal.NewFromInt(0)

	// Base risk based on country
	if s.isHighRiskCountry(profile.CountryCode) {
		riskScore = riskScore.Add(decimal.NewFromInt(30))
	}

	// Profile type specific risks
	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		// Individual risk factors
		if s.isHighValueIndividual(profile.AnnualIncomeRange) {
			riskScore = riskScore.Add(decimal.NewFromInt(20))
		}
	} else {
		// Business risk factors
		if s.isHighRiskIndustry(profile.BusinessIndustry) {
			riskScore = riskScore.Add(decimal.NewFromInt(40))
		}
		if s.isCashIntensiveBusiness(profile.BusinessIndustry) {
			riskScore = riskScore.Add(decimal.NewFromInt(15))
		}
	}

	// Cap at 100
	if riskScore.GreaterThan(decimal.NewFromInt(100)) {
		riskScore = decimal.NewFromInt(100)
	}

	return riskScore
}

func (s *KYCService) isAtLeast18(dateOfBirth time.Time) bool {
	now := time.Now()
	age := now.Year() - dateOfBirth.Year()

	// Adjust if birthday hasn't occurred this year
	if now.YearDay() < dateOfBirth.YearDay() {
		age--
	}

	return age >= 18
}

// ==============================================================================
// TRANSACTIONAL SAVE - TASK 5.2.4
// ==============================================================================

// TransactionalSaveResult represents the result of a transactional save operation
type TransactionalSaveResult struct {
	Profile        *domain.KYCProfile `json:"profile"`
	User           *domain.User       `json:"user"`
	WasNewProfile  bool               `json:"was_new_profile"`
	WasUserUpdated bool               `json:"was_user_updated"`
}

// saveKYCProfileTransactionally saves KYC profile and updates user in a single transaction
func (s *KYCService) saveKYCProfileTransactionally(
	ctx context.Context,
	profile *domain.KYCProfile,
	user *domain.User,
	existingProfile *domain.KYCProfile,
	isNewProfile bool,
	isFirstSubmission bool,
) (*TransactionalSaveResult, error) {
	var saveResult *TransactionalSaveResult

	// Use transaction manager to handle the transaction
	err := s.transactionManager.WithTransaction(ctx, func(txCtx *TransactionalContext) error {
		var err error

		// Save KYC profile
		if isNewProfile {
			err = s.repo.CreateProfileTx(txCtx.Ctx, txCtx.Tx, profile)
		} else {
			// For updates, we need to ensure we're updating the correct profile
			if existingProfile != nil {
				profile.ID = existingProfile.ID
				profile.CreatedAt = existingProfile.CreatedAt
				// Preserve some existing timestamps if they exist
				if existingProfile.SubmittedAt != nil && profile.SubmissionStatus != domain.KYCSubmissionStatusDraft {
					profile.SubmittedAt = existingProfile.SubmittedAt
				}
				if existingProfile.ApprovedAt != nil {
					profile.ApprovedAt = existingProfile.ApprovedAt
				}
				if existingProfile.RejectedAt != nil {
					profile.RejectedAt = existingProfile.RejectedAt
				}
			}
			err = s.repo.UpdateProfileTx(txCtx.Ctx, txCtx.Tx, profile)
		}

		if err != nil {
			return fmt.Errorf("failed to save KYC profile: %w", err)
		}

		// Update user if this is first submission
		var userUpdated bool
		if isFirstSubmission {
			// Update user's KYC status and level
			user.KYCStatus = domain.KYCStatusProcessing
			user.KYCLevel = 1
			user.UpdatedAt = time.Now()

			// Update user via repository with transaction
			if err := s.userRepo.UpdateKYCStatusTx(txCtx.Ctx, txCtx.Tx, user.ID, user.KYCStatus, nil); err != nil {
				return fmt.Errorf("failed to update user KYC status: %w", err)
			}

			// Also update KYC level with transaction
			if err := s.userRepo.UpdateKYCLevelTx(txCtx.Ctx, txCtx.Tx, user.ID, user.KYCLevel, true); err != nil {
				return fmt.Errorf("failed to update user KYC level: %w", err)
			}

			userUpdated = true
		}

		// Create the result
		saveResult = &TransactionalSaveResult{
			Profile:        profile,
			User:           user,
			WasNewProfile:  isNewProfile,
			WasUserUpdated: userUpdated,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return saveResult, nil
}

// handleSaveRollback handles rollback in case of transaction failure
func (s *KYCService) handleSaveRollback(_ context.Context, profile *domain.KYCProfile, user *domain.User, err error) error {
	s.logger.Error("Transaction failed, performing rollback", map[string]interface{}{
		"user_id":     user.ID,
		"profile_id":  profile.ID,
		"error":       err.Error(),
		"rollback_at": time.Now().Format(time.RFC3339),
	})

	// Log detailed context for debugging
	rollbackContext := map[string]interface{}{
		"profile_status":     profile.SubmissionStatus,
		"profile_kyc_level":  profile.KYCLevel,
		"user_kyc_status":    user.KYCStatus,
		"user_kyc_level":     user.KYCLevel,
		"transaction_error":  err.Error(),
		"rollback_initiated": true,
	}

	s.logger.Error("Rollback context", rollbackContext)

	// In a real system, you might want to:
	// 1. Send alerts to administrators
	// 2. Log to a dedicated rollback/audit table
	// 3. Trigger manual review for failed transactions
	// 4. Update metrics for transaction failure rates

	return fmt.Errorf("transaction failed and rolled back: %w", err)
}

// updateUserKYCStatus updates user's KYC status and level
func (s *KYCService) updateUserKYCStatus(ctx context.Context, userID uuid.UUID, status domain.KYCStatus, level int) error {
	// Use the user repository to update KYC status
	// We need to pass nil for profileStatus since we're only updating user
	if err := s.userRepo.UpdateKYCStatus(ctx, userID, status, nil); err != nil {
		return fmt.Errorf("failed to update user KYC status: %w", err)
	}

	// Also update KYC level
	if err := s.userRepo.UpdateKYCLevel(ctx, userID, level, true); err != nil {
		return fmt.Errorf("failed to update user KYC level: %w", err)
	}

	return nil
}

// saveKYCProfileWithTransaction is a more complete transaction implementation
// This would require our repository to support transactions
func (s *KYCService) saveKYCProfileWithTransaction() error {
	// TODO: Implement proper transaction when repository supports it
	// This is a placeholder showing what a transaction would look like

	/*
		// Pseudo-code for transaction pattern
		tx, err := s.repo.BeginTransaction(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback()

		// Save KYC profile
		if isNewProfile {
			if err := s.repo.CreateProfileTx(ctx, tx, profile); err != nil {
				return fmt.Errorf("failed to create KYC profile: %w", err)
			}
		} else {
			if err := s.repo.UpdateProfileTx(ctx, tx, profile); err != nil {
				return fmt.Errorf("failed to update KYC profile: %w", err)
			}
		}

		// Update user if needed
		if updateUser {
			user.KYCStatus = domain.KYCStatusProcessing
			user.KYCLevel = 1
			user.UpdatedAt = time.Now()

			if err := s.userRepo.UpdateTx(ctx, tx, user); err != nil {
				return fmt.Errorf("failed to update user: %w", err)
			}
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		return nil
	*/

	// For now, use the non-transactional version
	return fmt.Errorf("transaction support not yet implemented")
}

// archiveAndCreateNewProfile archives old profile and creates new one in transaction
func (s *KYCService) archiveAndCreateNewProfile(
	ctx context.Context,
	oldProfile *domain.KYCProfile,
	newProfile *domain.KYCProfile,
	user *domain.User,
) error {
	// Use transaction manager for atomic operation
	return s.transactionManager.WithTransaction(ctx, func(txCtx *TransactionalContext) error {
		// Mark old profile as superseded
		oldProfile.SubmissionStatus = domain.KYCSubmissionStatusSuspended
		oldProfile.ReviewNotes = fmt.Sprintf("Superseded by new profile %s on %s",
			newProfile.ID, time.Now().Format(time.RFC3339))
		oldProfile.UpdatedAt = time.Now()

		// Update metadata
		if oldProfile.Metadata == nil {
			oldProfile.Metadata = make(domain.Metadata)
		}
		oldProfile.Metadata["superseded_by"] = newProfile.ID.String()
		oldProfile.Metadata["superseded_at"] = time.Now().Format(time.RFC3339)

		// Save old profile (archived) with transaction
		if err := s.repo.UpdateProfileTx(txCtx.Ctx, txCtx.Tx, oldProfile); err != nil {
			return fmt.Errorf("failed to archive old profile: %w", err)
		}

		// Create new profile with transaction
		if err := s.repo.CreateProfileTx(txCtx.Ctx, txCtx.Tx, newProfile); err != nil {
			return fmt.Errorf("failed to create new profile: %w", err)
		}

		// Update user status with transaction
		user.KYCStatus = domain.KYCStatusProcessing
		user.KYCLevel = newProfile.KYCLevel
		user.UpdatedAt = time.Now()

		if err := s.userRepo.UpdateKYCStatusTx(txCtx.Ctx, txCtx.Tx, user.ID, user.KYCStatus, nil); err != nil {
			return fmt.Errorf("failed to update user KYC status: %w", err)
		}

		if err := s.userRepo.UpdateKYCLevelTx(txCtx.Ctx, txCtx.Tx, user.ID, user.KYCLevel, true); err != nil {
			return fmt.Errorf("failed to update user KYC level: %w", err)
		}

		s.logger.Info("Successfully archived old profile and created new one", map[string]interface{}{
			"user_id":          user.ID,
			"old_profile_id":   oldProfile.ID,
			"new_profile_id":   newProfile.ID,
			"old_status":       oldProfile.SubmissionStatus,
			"new_status":       newProfile.SubmissionStatus,
			"transaction_type": "archive_and_create",
		})

		return nil
	})
}

// validateAndPrepareSave validates data before saving and prepares for transaction
func (s *KYCService) validateAndPrepareSave(
	ctx context.Context,
	profile *domain.KYCProfile,
	user *domain.User,
	isNewProfile bool,
) (bool, error) {
	// Check if user exists and is active
	if !user.IsActive {
		return false, fmt.Errorf("user account is inactive")
	}

	// Check if profile has required fields
	if err := s.validateProfileCompleteness(profile); err != nil {
		return false, fmt.Errorf("profile validation failed: %w", err)
	}

	// Determine if this is first KYC submission
	hasExistingProfile, err := s.repo.ExistsByUserID(ctx, user.ID)
	if err != nil {
		return false, fmt.Errorf("failed to check existing profile: %w", err)
	}

	isFirstSubmission := !hasExistingProfile || (hasExistingProfile && isNewProfile)

	return isFirstSubmission, nil
}

func (s *KYCService) initiateAMLCheck(
	ctx context.Context,
	profile *domain.KYCProfile,
	user *domain.User,
	isNewSubmission bool,
) (*AMLCheckInitiationResult, error) {
	// Determine AML check type based on amount/profile
	checkType, amount := s.determineAMLCheckType(profile, isNewSubmission)

	// Enqueue AML check (async - don't block response)
	checkID, err := s.amlService.CheckUser(ctx, user.ID, amount)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate AML check: %w", err)
	}

	// Update profile with AML processing status
	err = s.updateProfileAMLStatus(ctx, profile.ID, checkID, checkType)
	if err != nil {
		s.logger.Warn("Failed to update profile AML status", map[string]interface{}{
			"profile_id": profile.ID,
			"check_id":   checkID,
			"error":      err.Error(),
		})
		// Don't fail the whole operation if status update fails
	}

	// Determine if check is async and get estimated time
	isAsync, estimatedTime := s.determineAMLCheckTiming(amount, checkType)

	result := &AMLCheckInitiationResult{
		CheckID:       checkID,
		CheckType:     checkType,
		Status:        "initiated",
		EstimatedTime: estimatedTime,
		IsAsync:       isAsync,
		Message:       "AML screening initiated successfully",
	}

	s.logger.Info("AML check initiated", map[string]interface{}{
		"check_id":     checkID,
		"user_id":      user.ID,
		"profile_id":   profile.ID,
		"check_type":   checkType,
		"amount":       amount.String(),
		"is_async":     isAsync,
		"profile_type": profile.ProfileType,
	})

	return result, nil
}

// determineAMLCheckType determines the type of AML check needed
func (s *KYCService) determineAMLCheckType(profile *domain.KYCProfile, isNewSubmission bool) (string, decimal.Decimal) {
	// Default check type and amount
	checkType := "kyc_screening"
	amount := decimal.Zero

	// Determine amount based on profile type and risk factors
	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		// For individuals, use income as a proxy for transaction volume
		switch profile.AnnualIncomeRange {
		case domain.IncomeRangeLessThan10K:
			amount = decimal.NewFromInt(1000)
		case domain.IncomeRange10KTo50K:
			amount = decimal.NewFromInt(5000)
		case domain.IncomeRange50KTo100K:
			amount = decimal.NewFromInt(10000)
		case domain.IncomeRange100KTo250K:
			amount = decimal.NewFromInt(25000)
		case domain.IncomeRange250KTo500K:
			amount = decimal.NewFromInt(50000)
		case domain.IncomeRange500KTo1M:
			amount = decimal.NewFromInt(100000)
		case domain.IncomeRangeOver1M:
			amount = decimal.NewFromInt(250000)
		}
	} else {
		// For businesses, use turnover as a proxy
		switch profile.AnnualTurnoverRange {
		case domain.TurnoverRangeLessThan50K:
			amount = decimal.NewFromInt(5000)
		case domain.TurnoverRange50KTo250K:
			amount = decimal.NewFromInt(25000)
		case domain.TurnoverRange250KTo1M:
			amount = decimal.NewFromInt(100000)
		case domain.TurnoverRange1MTo5M:
			amount = decimal.NewFromInt(500000)
		case domain.TurnoverRange5MTo10M:
			amount = decimal.NewFromInt(1000000)
		case domain.TurnoverRange10MTo50M:
			amount = decimal.NewFromInt(5000000)
		case domain.TurnoverRangeOver50M:
			amount = decimal.NewFromInt(10000000)
		}
	}

	// Adjust check type based on risk factors
	if s.isHighRiskProfile(profile) {
		checkType = "enhanced_due_diligence"
		// Increase amount for high-risk profiles
		amount = amount.Mul(decimal.NewFromInt(2))
	}

	// For new submissions, use standard screening
	// For updates/reviews, use periodic review check
	if !isNewSubmission {
		checkType = "periodic_review"
	}

	return checkType, amount
}

// buildAMLProfile builds the AML profile data structure
func (s *KYCService) buildAMLProfile(
	profile *domain.KYCProfile,
	user *domain.User,
	amount decimal.Decimal,
) *AMLProfile {
	amlProfile := &AMLProfile{
		UserID:      user.ID,
		ProfileID:   profile.ID,
		ProfileType: profile.ProfileType,
		CountryCode: profile.CountryCode,
		RiskScore:   profile.AMLRiskScore,
		Amount:      amount,
	}

	// Add individual-specific fields
	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		amlProfile.Nationality = profile.Nationality
		amlProfile.Occupation = profile.Occupation
		amlProfile.IncomeRange = profile.AnnualIncomeRange
		amlProfile.SourceOfFunds = profile.SourceOfFunds
	}

	// Add business-specific fields
	if profile.ProfileType == domain.KYCProfileTypeBusiness {
		amlProfile.BusinessName = profile.CompanyName
		amlProfile.BusinessIndustry = profile.BusinessIndustry
	}

	return amlProfile
}

func (s *KYCService) updateProfileAMLStatus(
	ctx context.Context,
	profileID uuid.UUID,
	checkID string,
	checkType string,
) error {
	// Get current profile
	profile, err := s.repo.FindProfileByID(ctx, profileID)
	if err != nil {
		return fmt.Errorf("failed to find profile: %w", err)
	}

	// Update AML status to processing
	profile.AMLStatus = domain.AMLStatusProcessing

	// Update metadata with AML check reference
	if profile.Metadata == nil {
		profile.Metadata = make(domain.Metadata)
	}

	// Store AML check details in metadata
	amlData := map[string]interface{}{
		"check_id":        checkID,
		"check_type":      checkType,
		"initiated_at":    time.Now().Format(time.RFC3339),
		"last_checked_at": time.Now().Format(time.RFC3339),
		"check_history": []map[string]interface{}{
			{
				"check_id":     checkID,
				"type":         checkType,
				"initiated_at": time.Now().Format(time.RFC3339),
				"status":       "processing",
			},
		},
	}

	profile.Metadata["aml_checks"] = amlData

	// Update the profile
	err = s.repo.UpdateProfile(ctx, profile)
	if err != nil {
		return fmt.Errorf("failed to update profile AML status: %w", err)
	}

	return nil
}

// determineAMLCheckTiming determines if check is async and estimated time
func (s *KYCService) determineAMLCheckTiming(amount decimal.Decimal, checkType string) (bool, string) {
	// Determine if check should be async
	// For small amounts or simple checks, can be immediate
	// For large amounts or enhanced checks, should be async

	threshold := s.config.AML.CheckThreshold // From config

	isAsync := amount.GreaterThanOrEqual(threshold) || checkType == "enhanced_due_diligence"

	// Estimate processing time
	estimatedTime := "immediate"
	if isAsync {
		if checkType == "enhanced_due_diligence" {
			estimatedTime = "24-48 hours"
		} else if amount.GreaterThan(decimal.NewFromInt(100000)) {
			estimatedTime = "4-6 hours"
		} else {
			estimatedTime = "1-2 hours"
		}
	}

	return isAsync, estimatedTime
}

// scheduleAMLResultProcessing schedules async processing of AML results
func (s *KYCService) scheduleAMLResultProcessing(checkID string, profileID, userID uuid.UUID) {
	// This would typically use a job queue or background worker
	// For now, we'll use a goroutine with exponential backoff

	go func() {
		// Create a new context for background processing
		bgCtx := context.Background()

		// Exponential backoff for checking AML results
		maxAttempts := 10
		baseDelay := 30 * time.Second

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			time.Sleep(baseDelay * time.Duration(attempt))

			// Check AML result
			result, err := s.amlService.GetCheckResult(bgCtx, checkID)
			if err != nil {
				s.logger.Warn("Failed to get AML result", map[string]interface{}{
					"check_id":   checkID,
					"attempt":    attempt,
					"error":      err.Error(),
					"profile_id": profileID,
				})
				continue
			}

			// Process the result
			if result != nil {
				err = s.processAMLResult(bgCtx, result, profileID, userID)
				if err != nil {
					s.logger.Error("Failed to process AML result", map[string]interface{}{
						"check_id":   checkID,
						"profile_id": profileID,
						"error":      err.Error(),
					})
				} else {
					s.logger.Info("AML result processed successfully", map[string]interface{}{
						"check_id":   checkID,
						"profile_id": profileID,
						"risk_score": result.RiskScore,
						"risk_level": result.RiskLevel,
						"status":     result.Status,
					})
					break
				}
			}

			if attempt == maxAttempts {
				s.logger.Error("Max attempts reached for AML check", map[string]interface{}{
					"check_id":   checkID,
					"profile_id": profileID,
					"user_id":    userID,
				})
			}
		}
	}()
}

// processAMLResult processes the AML check result and updates the profile
func (s *KYCService) processAMLResult(
	ctx context.Context,
	result *aml.AMLResult,
	profileID uuid.UUID,
	userID uuid.UUID,
) error {
	// Get current profile
	profile, err := s.repo.FindProfileByID(ctx, profileID)
	if err != nil {
		return fmt.Errorf("failed to find profile: %w", err)
	}

	// Update AML status based on result
	amlStatus := s.mapAMLResultToStatus(result)

	// Update risk score
	riskScore := decimal.NewFromFloat(result.RiskScore)

	// Update PEP and sanction checks
	pepCheck := false
	sanctionCheck := false
	for _, flag := range result.Flags {
		if flag == "pep" {
			pepCheck = true
		}
		if flag == "sanction_match" {
			sanctionCheck = true
		}
	}

	// Update profile AML status
	err = s.repo.UpdateAMLStatus(ctx, profileID, amlStatus, riskScore, pepCheck, sanctionCheck)
	if err != nil {
		return fmt.Errorf("failed to update AML status: %w", err)
	}

	// Update metadata with result details
	if profile.Metadata == nil {
		profile.Metadata = make(domain.Metadata)
	}

	// Get existing AML checks data
	amlChecks, ok := profile.Metadata["aml_checks"].(map[string]interface{})
	if !ok {
		amlChecks = make(map[string]interface{})
	}

	// Update check result
	amlChecks["last_result"] = map[string]interface{}{
		"check_id":     result.CheckID,
		"risk_score":   result.RiskScore,
		"risk_level":   result.RiskLevel,
		"status":       result.Status,
		"flags":        result.Flags,
		"checked_at":   result.CheckedAt.Format(time.RFC3339),
		"completed_at": time.Now().Format(time.RFC3339),
	}

	// Add to check history
	history, ok := amlChecks["check_history"].([]map[string]interface{})
	if !ok {
		history = []map[string]interface{}{}
	}

	// Find and update the check in history
	for i, check := range history {
		if check["check_id"] == result.CheckID {
			check["status"] = result.Status
			check["completed_at"] = time.Now().Format(time.RFC3339)
			check["risk_score"] = result.RiskScore
			check["risk_level"] = result.RiskLevel
			history[i] = check
			break
		}
	}

	amlChecks["check_history"] = history
	profile.Metadata["aml_checks"] = amlChecks

	// Update the profile metadata
	err = s.repo.UpdateProfile(ctx, profile)
	if err != nil {
		return fmt.Errorf("failed to update profile metadata: %w", err)
	}

	// Update user risk score
	err = s.userRepo.UpdateUserRiskScore(ctx, userID, riskScore)
	if err != nil {
		s.logger.Warn("Failed to update user risk score", map[string]interface{}{
			"user_id":    userID,
			"error":      err.Error(),
			"profile_id": profileID,
		})
		// Don't fail the whole operation
	}

	// If AML check failed or requires manual review, escalate
	if result.Status == "failed" || result.Status == "manual_review" {
		s.escalateAMLReview(ctx, profileID, userID, result)
	}

	return nil
}

// mapAMLResultToStatus maps AML service result to domain AMLStatus
func (s *KYCService) mapAMLResultToStatus(result *aml.AMLResult) domain.AMLStatus {
	switch result.Status {
	case "passed":
		return domain.AMLStatusCleared
	case "failed":
		return domain.AMLStatusRejected
	case "manual_review":
		return domain.AMLStatusEscalated
	case "pending":
		return domain.AMLStatusPending
	default:
		return domain.AMLStatusProcessing
	}
}

// escalateAMLReview handles escalation for failed or manual review AML checks
func (s *KYCService) escalateAMLReview(
	ctx context.Context,
	profileID uuid.UUID,
	userID uuid.UUID,
	result *aml.AMLResult,
) {
	// Log escalation
	s.logger.Warn("AML check requires escalation", map[string]interface{}{
		"profile_id": profileID,
		"user_id":    userID,
		"check_id":   result.CheckID,
		"risk_score": result.RiskScore,
		"risk_level": result.RiskLevel,
		"status":     result.Status,
		"flags":      result.Flags,
	})

	// TODO: In production, this would:
	// 1. Create a compliance ticket
	// 2. Notify compliance officers
	// 3. Update workflow status
	// 4. Potentially restrict user activities

	// For now, just log and update metadata
	profile, err := s.repo.FindProfileByID(ctx, profileID)
	if err != nil {
		s.logger.Error("Failed to find profile for escalation", map[string]interface{}{
			"profile_id": profileID,
			"error":      err.Error(),
		})
		return
	}

	// Update metadata with escalation info
	if profile.Metadata == nil {
		profile.Metadata = make(domain.Metadata)
	}

	profile.Metadata["compliance_escalation"] = map[string]interface{}{
		"escalated_at":    time.Now().Format(time.RFC3339),
		"reason":          "AML check " + result.Status,
		"risk_level":      result.RiskLevel,
		"risk_score":      result.RiskScore,
		"check_id":        result.CheckID,
		"requires_review": true,
	}

	err = s.repo.UpdateProfile(ctx, profile)
	if err != nil {
		s.logger.Error("Failed to update profile with escalation", map[string]interface{}{
			"profile_id": profileID,
			"error":      err.Error(),
		})
	}
}

// isHighRiskProfile determines if a profile is high risk for AML purposes
func (s *KYCService) isHighRiskProfile(profile *domain.KYCProfile) bool {
	// Check high-risk countries
	if s.isHighRiskCountry(profile.CountryCode) {
		return true
	}

	// Check PEP status
	if profile.PEPCheck {
		return true
	}

	// Check sanction status
	if profile.SanctionCheck {
		return true
	}

	// Check high-risk industries for businesses
	if profile.ProfileType == domain.KYCProfileTypeBusiness {
		if s.isHighRiskIndustry(profile.BusinessIndustry) {
			return true
		}
	}

	// Check high risk score
	if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(50)) {
		return true
	}

	// Check cash-intensive businesses
	if profile.ProfileType == domain.KYCProfileTypeBusiness {
		if s.isCashIntensiveBusiness(profile.BusinessIndustry) {
			return true
		}
	}

	return false
}

// buildSubmitKYCResponse builds the response for SubmitKYCData
func (s *KYCService) buildSubmitKYCResponse(
	profile *domain.KYCProfile,
	user *domain.User,
	amlInitiation *AMLCheckInitiationResult,
	_ ExistingProfileAction,
	isDraft bool,
) *domain.SubmitKYCResponse {
	// Determine next steps based on status
	var nextSteps []string
	var estimatedReviewTime string

	if isDraft {
		nextSteps = []string{
			"Submit your KYC profile for review",
			"Upload required documents",
			"Complete any additional information requests",
		}
		estimatedReviewTime = "N/A (Draft)"
	} else {
		nextSteps = []string{
			"Your KYC profile is now under review",
			"We will notify you when your profile is reviewed",
			"You may be asked for additional information",
		}

		// Estimate review time based on profile type and risk
		if profile.ProfileType == domain.KYCProfileTypeIndividual {
			estimatedReviewTime = "1-3 business days"
		} else {
			estimatedReviewTime = "3-7 business days"
		}

		// Adjust for high risk profiles
		if profile.AMLRiskScore.GreaterThan(decimal.NewFromInt(50)) {
			estimatedReviewTime = "5-10 business days (enhanced due diligence required)"
		}
	}

	// Determine missing documents (if any) as DocumentType slice
	missingDocuments := s.determineMissingDocumentTypes(profile)

	// Build the response message
	var message string
	if isDraft {
		message = "KYC profile saved as draft successfully"
	} else {
		message = fmt.Sprintf("KYC profile submitted for review successfully. KYC Level: %d", profile.KYCLevel)

		// Include transaction limits in message
		dailyLimit := s.getDailyLimit(profile.KYCLevel)
		monthlyLimit := s.getMonthlyLimit(profile.KYCLevel)
		singleLimit := s.getSingleTransactionLimit(profile.KYCLevel)

		message += fmt.Sprintf("\nTransaction limits: Daily: %s, Monthly: %s, Single: %s",
			dailyLimit.String(), monthlyLimit.String(), singleLimit.String())

		// Include AML info in message if available
		if amlInitiation != nil {
			message += fmt.Sprintf("\nAML screening initiated (ID: %s). Estimated completion: %s",
				amlInitiation.CheckID, amlInitiation.EstimatedTime)
		}
	}

	response := &domain.SubmitKYCResponse{
		ProfileID:           profile.ID,
		UserID:              user.ID,
		SubmissionStatus:    profile.SubmissionStatus,
		AMLStatus:           profile.AMLStatus,
		Message:             message,
		NextSteps:           nextSteps,
		MissingDocuments:    missingDocuments,
		EstimatedReviewTime: estimatedReviewTime,
	}

	return response
}

// Helper methods for response building
func (s *KYCService) determineMissingDocumentTypes(profile *domain.KYCProfile) []domain.DocumentType {
	// This is a simplified version
	// In production, this would check KYC requirements for the user's country/user_type/kyc_level
	// and compare with already uploaded documents

	var missingDocs []domain.DocumentType

	// Basic document requirements for all profiles
	missingDocs = append(missingDocs,
		domain.DocumentTypeNationalID,  // Proof of Identity
		domain.DocumentTypeUtilityBill, // Proof of Address
	)

	// Add profile-specific documents
	if profile.ProfileType == domain.KYCProfileTypeIndividual {
		missingDocs = append(missingDocs, domain.DocumentTypeProofOfIncome)
	} else {
		missingDocs = append(missingDocs,
			domain.DocumentTypeBusinessRegistration,
			domain.DocumentTypeBusinessLicense, // Could use for Business Address
			domain.DocumentTypeTaxCertificate,
		)
	}

	return missingDocs
}

// Keep the old method for other uses or remove if not needed
func (s *KYCService) determineMissingDocuments(profile *domain.KYCProfile) []string {
	missingDocTypes := s.determineMissingDocumentTypes(profile)

	// Convert DocumentType to string descriptions for human-readable output
	var missingDocs []string
	for _, docType := range missingDocTypes {
		switch docType {
		case domain.DocumentTypeNationalID:
			missingDocs = append(missingDocs, "National ID")
		case domain.DocumentTypePassport:
			missingDocs = append(missingDocs, "Passport")
		case domain.DocumentTypeDriversLicense:
			missingDocs = append(missingDocs, "Driver's License")
		case domain.DocumentTypeBusinessRegistration:
			missingDocs = append(missingDocs, "Business Registration Certificate")
		case domain.DocumentTypeTaxCertificate:
			missingDocs = append(missingDocs, "Tax Certificate")
		case domain.DocumentTypeUtilityBill:
			missingDocs = append(missingDocs, "Utility Bill (Proof of Address)")
		case domain.DocumentTypeBankStatement:
			missingDocs = append(missingDocs, "Bank Statement")
		case domain.DocumentTypeProofOfIncome:
			missingDocs = append(missingDocs, "Proof of Income")
		case domain.DocumentTypeSelfieWithID:
			missingDocs = append(missingDocs, "Selfie with ID")
		case domain.DocumentTypeBusinessLicense:
			missingDocs = append(missingDocs, "Business License")
		case domain.DocumentTypeAgentLicense:
			missingDocs = append(missingDocs, "Agent License")
		}
	}

	return missingDocs
}

func (s *KYCService) getDailyLimit(kycLevel int) decimal.Decimal {
	// Define daily limits based on KYC level
	limits := map[int]decimal.Decimal{
		0: decimal.NewFromInt(100),
		1: decimal.NewFromInt(1000),
		2: decimal.NewFromInt(5000),
		3: decimal.NewFromInt(20000),
		4: decimal.NewFromInt(50000),
		5: decimal.NewFromInt(100000),
	}

	if limit, exists := limits[kycLevel]; exists {
		return limit
	}
	return decimal.NewFromInt(1000) // Default
}

func (s *KYCService) getMonthlyLimit(kycLevel int) decimal.Decimal {
	// Define monthly limits based on KYC level
	limits := map[int]decimal.Decimal{
		0: decimal.NewFromInt(500),
		1: decimal.NewFromInt(5000),
		2: decimal.NewFromInt(25000),
		3: decimal.NewFromInt(100000),
		4: decimal.NewFromInt(250000),
		5: decimal.NewFromInt(500000),
	}

	if limit, exists := limits[kycLevel]; exists {
		return limit
	}
	return decimal.NewFromInt(5000) // Default
}

func (s *KYCService) getSingleTransactionLimit(kycLevel int) decimal.Decimal {
	// Define single transaction limits based on KYC level
	limits := map[int]decimal.Decimal{
		0: decimal.NewFromInt(50),
		1: decimal.NewFromInt(500),
		2: decimal.NewFromInt(2500),
		3: decimal.NewFromInt(10000),
		4: decimal.NewFromInt(25000),
		5: decimal.NewFromInt(50000),
	}

	if limit, exists := limits[kycLevel]; exists {
		return limit
	}
	return decimal.NewFromInt(500) // Default
}

// SubmitKYCData orchestrates the complete KYC submission workflow
func (s *KYCService) SubmitKYCData(
	ctx context.Context,
	req *domain.SubmitKYCRequest,
	userID uuid.UUID,
	isDraft bool,
) (*domain.SubmitKYCResponse, error) {
	// Start timing for performance monitoring
	startTime := time.Now()

	// Log the start of KYC submission
	s.logger.Info("Starting KYC submission", map[string]interface{}{
		"user_id":      userID,
		"profile_type": req.ProfileType,
		"is_draft":     isDraft,
	})

	// ========== TASK 5.2.1: REQUEST VALIDATION ==========
	validationErrors, err := s.validateKYCSubmission(ctx, req, userID)
	if err != nil {
		s.logger.Error("KYC validation failed with system error", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
		return nil, kyderrors.Wrap(err, "failed to validate KYC submission")
	}

	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("KYC validation failed: %v", validationErrors)
	}

	// Get user for later use
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, kyderrors.Wrap(err, "failed to get user")
	}

	// ========== TASK 5.2.2: EXISTING KYC CHECK ==========
	checkResult, err := s.checkExistingKYCProfile(ctx, userID)
	if err != nil {
		return nil, kyderrors.Wrap(err, "failed to check existing KYC profile")
	}

	if !checkResult.Allowed {
		s.logger.Warn("KYC submission not allowed", map[string]interface{}{
			"user_id": userID,
			"action":  checkResult.Action,
			"message": checkResult.Message,
		})
		return nil, fmt.Errorf("kyc submission not allowed: %s", checkResult.Message)
	}

	// Determine if this is a new profile
	isNewProfile := checkResult.Action == ActionCreateNew || checkResult.Action == ActionCreateNewVersion

	// ========== TASK 5.2.3: BUILD KYC PROFILE ==========
	profile, err := s.buildKYCProfile(req, userID, isNewProfile, isDraft)
	if err != nil {
		return nil, kyderrors.Wrap(err, "failed to build KYC profile")
	}

	// ========== TASK 5.2.4: TRANSACTIONAL SAVE ==========
	// Determine if this is first submission
	hasExistingProfile, err := s.repo.ExistsByUserID(ctx, userID)
	if err != nil {
		return nil, kyderrors.Wrap(err, "failed to check existing profile")
	}
	isFirstSubmission := !hasExistingProfile || (hasExistingProfile && isNewProfile)

	// Save profile and update user
	saveResult, err := s.saveKYCProfileTransactionally(
		ctx,
		profile,
		user,
		checkResult.ExistingProfile,
		isNewProfile,
		isFirstSubmission && !isDraft, // Only update user on first real submission, not drafts
	)
	if err != nil {
		return nil, kyderrors.Wrap(err, "failed to save KYC data")
	}

	// ========== TASK 5.2.5: AML CHECK INITIATION ==========
	var amlInitiationResult *AMLCheckInitiationResult

	// Only initiate AML check for non-draft submissions
	if !isDraft {
		// Initiate AML check asynchronously
		amlResult, err := s.initiateAMLCheck(ctx, profile, user, isNewProfile)
		if err != nil {
			s.logger.Error("Failed to initiate AML check", map[string]interface{}{
				"user_id":    user.ID,
				"profile_id": profile.ID,
				"error":      err.Error(),
			})
			// Don't fail the submission if AML initiation fails
			// Just log and continue
		} else {
			// Schedule background processing of AML results
			s.scheduleAMLResultProcessing(amlResult.CheckID, profile.ID, user.ID)

			// Store AML initiation info for response
			amlInitiationResult = amlResult
		}
	}

	// ========== TASK 5.2.6: COMPLIANCE REQUIREMENTS CHECK ==========
	var complianceResult *ComplianceCheckResult

	// Only perform compliance check for non-draft submissions
	if !isDraft {
		complianceResult, err = s.checkComplianceRequirements(ctx, profile, user)
		if err != nil {
			s.logger.Error("Failed to check compliance requirements", map[string]interface{}{
				"user_id":    user.ID,
				"profile_id": profile.ID,
				"error":      err.Error(),
			})
			// Don't fail the submission if compliance check fails
			// Just log and continue
		} else {
			// Log compliance results
			s.logger.Info("Compliance requirements check completed", map[string]interface{}{
				"user_id":          user.ID,
				"profile_id":       profile.ID,
				"is_compliant":     complianceResult.IsCompliant,
				"compliance_score": complianceResult.ComplianceScore,
				"meets_minimum":    complianceResult.MeetsMinimum,
				"missing_fields":   len(complianceResult.MissingFields),
				"missing_docs":     len(complianceResult.MissingDocuments),
			})

			// If profile doesn't meet minimum requirements, we should handle it
			if !complianceResult.MeetsMinimum {
				s.logger.Warn("Profile does not meet minimum compliance requirements", map[string]interface{}{
					"user_id":    user.ID,
					"profile_id": profile.ID,
					"warnings":   complianceResult.WarningMessages,
				})
				// In production, you might want to reject the submission here
				// or change the status to "additional_info_required"
			}

			// Store compliance result in metadata if we have one
			if complianceResult != nil {
				s.storeRequirementsReference(profile, complianceResult)

				// Update the profile with compliance metadata
				err = s.repo.UpdateProfile(ctx, profile)
				if err != nil {
					s.logger.Warn("Failed to update profile with compliance metadata", map[string]interface{}{
						"profile_id": profile.ID,
						"error":      err.Error(),
					})
				}
			}
		}
	}

	// ========== BUILD RESPONSE ==========
	response := s.buildSubmitKYCResponse(saveResult.Profile, saveResult.User, amlInitiationResult, checkResult.Action, isDraft)

	// Log successful submission
	processingTime := time.Since(startTime)
	s.logger.Info("KYC submission completed", map[string]interface{}{
		"user_id":            userID,
		"profile_id":         profile.ID,
		"action_taken":       checkResult.Action,
		"is_draft":           isDraft,
		"processing_time_ms": processingTime.Milliseconds(),
		"kyc_level":          profile.KYCLevel,
		"status":             profile.SubmissionStatus,
	})

	return response, nil
}
