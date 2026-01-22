// ==============================================================================
// KYC REPOSITORY IMPLEMENTATION
// internal/repository/postgres/kyc_repository.go
// ==============================================================================
// Implements KYC repository interfaces for PostgreSQL
// Follows existing patterns from auth, wallet, transaction repositories
// ==============================================================================

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
)

// ==============================================================================
// TRANSACTION TYPES & WRAPPERS
// ==============================================================================

// SQLxTransaction wraps sqlx.Tx to implement the kyc.Transaction interface
type SQLxTransaction struct {
	tx *sqlx.Tx
	id string
}

func NewSQLxTransaction(tx *sqlx.Tx) *SQLxTransaction {
	return &SQLxTransaction{
		tx: tx,
		id: uuid.New().String(),
	}
}

func (t *SQLxTransaction) Commit() error {
	return t.tx.Commit()
}

func (t *SQLxTransaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *SQLxTransaction) GetID() string {
	return t.id
}

// ==============================================================================
// KYC PROFILE REPOSITORY
// ==============================================================================

// KYCProfileRepository implements KYC profile persistence
type KYCProfileRepository struct {
	db *sqlx.DB
}

// NewKYCProfileRepository creates a new KYCProfileRepository
func NewKYCProfileRepository(db *sqlx.DB) *KYCProfileRepository {
	return &KYCProfileRepository{db: db}
}

// BeginTransaction starts a new database transaction
func (r *KYCProfileRepository) BeginTransaction(ctx context.Context) (*SQLxTransaction, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	return NewSQLxTransaction(tx), nil
}

// Create inserts a new KYC profile (non-transactional)
func (r *KYCProfileRepository) Create(ctx context.Context, profile *domain.KYCProfile) error {
	return r.CreateProfileTx(ctx, nil, profile)
}

// CreateProfileTx creates a new KYC profile within a transaction
func (r *KYCProfileRepository) CreateProfileTx(ctx context.Context, tx interface{}, profile *domain.KYCProfile) error {
	query := `
		INSERT INTO customer_schema.kyc_profiles (
			id, user_id, profile_type, date_of_birth, place_of_birth, nationality,
			occupation, employer_name, annual_income_range, source_of_funds,
			company_name, company_registration_number, company_tax_id, business_nature,
			incorporation_date, annual_turnover_range, number_of_employees, business_industry,
			address_line1, address_line2, city, state_province, postal_code, country_code,
			phone_number, alt_phone_number, submission_status, kyc_level, review_notes,
			reviewed_by, reviewed_at, next_review_date, aml_risk_score, aml_check_status,
			pep_check, sanction_check, pep_type, sanction_list, edd_required, edd_level,
			edd_review_date, metadata, created_at, updated_at, submitted_at, approved_at, rejected_at
		) VALUES (
			:id, :user_id, :profile_type, :date_of_birth, :place_of_birth, :nationality,
			:occupation, :employer_name, :annual_income_range, :source_of_funds,
			:company_name, :company_registration_number, :company_tax_id, :business_nature,
			:incorporation_date, :annual_turnover_range, :number_of_employees, :business_industry,
			:address_line1, :address_line2, :city, :state_province, :postal_code, :country_code,
			:phone_number, :alt_phone_number, :submission_status, :kyc_level, :review_notes,
			:reviewed_by, :reviewed_at, :next_review_date, :aml_risk_score, :aml_check_status,
			:pep_check, :sanction_check, :pep_type, :sanction_list, :edd_required, :edd_level,
			:edd_review_date, :metadata, :created_at, :updated_at, :submitted_at, :approved_at, :rejected_at
		)
	`

	var err error
	if tx == nil {
		_, err = r.db.NamedExecContext(ctx, query, profile)
	} else {
		_, err = tx.(*SQLxTransaction).tx.NamedExecContext(ctx, query, profile)
	}

	if err != nil {
		return errors.Wrap(err, "failed to create KYC profile")
	}

	return nil
}

// Update updates an existing KYC profile (non-transactional)
func (r *KYCProfileRepository) Update(ctx context.Context, profile *domain.KYCProfile) error {
	return r.UpdateProfileTx(ctx, nil, profile)
}

// UpdateProfileTx updates an existing KYC profile within a transaction
func (r *KYCProfileRepository) UpdateProfileTx(ctx context.Context, tx interface{}, profile *domain.KYCProfile) error {
	query := `
		UPDATE customer_schema.kyc_profiles SET
			profile_type = :profile_type,
			date_of_birth = :date_of_birth,
			place_of_birth = :place_of_birth,
			nationality = :nationality,
			occupation = :occupation,
			employer_name = :employer_name,
			annual_income_range = :annual_income_range,
			source_of_funds = :source_of_funds,
			company_name = :company_name,
			company_registration_number = :company_registration_number,
			company_tax_id = :company_tax_id,
			business_nature = :business_nature,
			incorporation_date = :incorporation_date,
			annual_turnover_range = :annual_turnover_range,
			number_of_employees = :number_of_employees,
			business_industry = :business_industry,
			address_line1 = :address_line1,
			address_line2 = :address_line2,
			city = :city,
			state_province = :state_province,
			postal_code = :postal_code,
			country_code = :country_code,
			phone_number = :phone_number,
			alt_phone_number = :alt_phone_number,
			submission_status = :submission_status,
			kyc_level = :kyc_level,
			review_notes = :review_notes,
			reviewed_by = :reviewed_by,
			reviewed_at = :reviewed_at,
			next_review_date = :next_review_date,
			aml_risk_score = :aml_risk_score,
			aml_check_status = :aml_check_status,
			pep_check = :pep_check,
			sanction_check = :sanction_check,
			pep_type = :pep_type,
			sanction_list = :sanction_list,
			edd_required = :edd_required,
			edd_level = :edd_level,
			edd_review_date = :edd_review_date,
			metadata = :metadata,
			updated_at = :updated_at,
			submitted_at = :submitted_at,
			approved_at = :approved_at,
			rejected_at = :rejected_at
		WHERE id = :id
	`

	var result sql.Result
	var err error

	if tx == nil {
		result, err = r.db.NamedExecContext(ctx, query, profile)
	} else {
		result, err = tx.(*SQLxTransaction).tx.NamedExecContext(ctx, query, profile)
	}

	if err != nil {
		return errors.Wrap(err, "failed to update KYC profile")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrKYCProfileNotFound
	}

	return nil
}

// FindProfileByIDTx finds a KYC profile by ID within a transaction
func (r *KYCProfileRepository) FindProfileByIDTx(ctx context.Context, tx interface{}, id uuid.UUID) (*domain.KYCProfile, error) {
	var profile domain.KYCProfile
	query := `SELECT * FROM customer_schema.kyc_profiles WHERE id = $1`

	var err error
	if tx == nil {
		err = r.db.GetContext(ctx, &profile, query, id)
	} else {
		err = tx.(*SQLxTransaction).tx.GetContext(ctx, &profile, query, id)
	}

	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCProfileNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find KYC profile by ID")
	}

	return &profile, nil
}

// FindProfileByUserIDTx finds a KYC profile by user ID within a transaction
func (r *KYCProfileRepository) FindProfileByUserIDTx(ctx context.Context, tx interface{}, userID uuid.UUID) (*domain.KYCProfile, error) {
	var profile domain.KYCProfile
	query := `SELECT * FROM customer_schema.kyc_profiles WHERE user_id = $1`

	var err error
	if tx == nil {
		err = r.db.GetContext(ctx, &profile, query, userID)
	} else {
		err = tx.(*SQLxTransaction).tx.GetContext(ctx, &profile, query, userID)
	}

	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCProfileNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find KYC profile by user ID")
	}

	return &profile, nil
}

// ExistsByUserIDTx checks if a KYC profile exists for a user within a transaction
func (r *KYCProfileRepository) ExistsByUserIDTx(ctx context.Context, tx interface{}, userID uuid.UUID) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM customer_schema.kyc_profiles WHERE user_id = $1)`

	var err error
	if tx == nil {
		err = r.db.GetContext(ctx, &exists, query, userID)
	} else {
		err = tx.(*SQLxTransaction).tx.GetContext(ctx, &exists, query, userID)
	}

	if err != nil {
		return false, errors.Wrap(err, "failed to check KYC profile existence")
	}

	return exists, nil
}

// UpdateSubmissionStatusTx updates only the submission status within a transaction
func (r *KYCProfileRepository) UpdateSubmissionStatusTx(ctx context.Context, tx interface{}, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error {
	now := time.Now()
	query := `
		UPDATE customer_schema.kyc_profiles SET
			submission_status = $1,
			review_notes = COALESCE($2, review_notes),
			reviewed_at = CASE 
				WHEN $1 IN ('approved', 'rejected') THEN $3
				ELSE reviewed_at 
			END,
			approved_at = CASE 
				WHEN $1 = 'approved' THEN $3
				ELSE approved_at 
			END,
			rejected_at = CASE 
				WHEN $1 = 'rejected' THEN $3
				ELSE rejected_at 
			END,
			updated_at = $3
		WHERE id = $4
	`

	var result sql.Result
	var err error

	if tx == nil {
		result, err = r.db.ExecContext(ctx, query, status, notes, now, id)
	} else {
		result, err = tx.(*SQLxTransaction).tx.ExecContext(ctx, query, status, notes, now, id)
	}

	if err != nil {
		return errors.Wrap(err, "failed to update KYC submission status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrKYCProfileNotFound
	}

	return nil
}

// UpdateAMLStatusTx updates AML-related fields within a transaction
func (r *KYCProfileRepository) UpdateAMLStatusTx(ctx context.Context, tx interface{}, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error {
	query := `
		UPDATE customer_schema.kyc_profiles SET
			aml_check_status = $1,
			aml_risk_score = $2,
			pep_check = $3,
			sanction_check = $4,
			updated_at = NOW()
		WHERE id = $5
	`

	var result sql.Result
	var err error

	if tx == nil {
		result, err = r.db.ExecContext(ctx, query, status, riskScore, pepCheck, sanctionCheck, id)
	} else {
		result, err = tx.(*SQLxTransaction).tx.ExecContext(ctx, query, status, riskScore, pepCheck, sanctionCheck, id)
	}

	if err != nil {
		return errors.Wrap(err, "failed to update AML status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrKYCProfileNotFound
	}

	return nil
}

// UpdateKYCLevelTx updates the KYC level within a transaction
func (r *KYCProfileRepository) UpdateKYCLevelTx(ctx context.Context, tx interface{}, id uuid.UUID, level int) error {
	query := `
		UPDATE customer_schema.kyc_profiles SET
			kyc_level = $1,
			updated_at = NOW()
		WHERE id = $2
	`

	var result sql.Result
	var err error

	if tx == nil {
		result, err = r.db.ExecContext(ctx, query, level, id)
	} else {
		result, err = tx.(*SQLxTransaction).tx.ExecContext(ctx, query, level, id)
	}

	if err != nil {
		return errors.Wrap(err, "failed to update KYC level")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrKYCProfileNotFound
	}

	return nil
}

// FindPendingReview finds KYC profiles pending review
func (r *KYCProfileRepository) FindPendingReview(ctx context.Context, limit, offset int) ([]*domain.KYCProfile, error) {
	var profiles []*domain.KYCProfile
	query := `
		SELECT * FROM customer_schema.kyc_profiles 
		WHERE submission_status IN ('submitted', 'under_review', 'additional_info_required')
		ORDER BY submitted_at ASC, created_at ASC
		LIMIT $1 OFFSET $2
	`

	err := r.db.SelectContext(ctx, &profiles, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find pending review profiles")
	}

	return profiles, nil
}

// CountPendingReview counts KYC profiles pending review
func (r *KYCProfileRepository) CountPendingReview(ctx context.Context) (int, error) {
	var count int
	query := `
		SELECT COUNT(*) FROM customer_schema.kyc_profiles 
		WHERE submission_status IN ('submitted', 'under_review', 'additional_info_required')
	`

	err := r.db.GetContext(ctx, &count, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count pending review profiles")
	}

	return count, nil
}

// FindByStatus finds KYC profiles by status
func (r *KYCProfileRepository) FindByStatus(ctx context.Context, status domain.KYCSubmissionStatus, limit, offset int) ([]*domain.KYCProfile, error) {
	var profiles []*domain.KYCProfile
	query := `
		SELECT * FROM customer_schema.kyc_profiles 
		WHERE submission_status = $1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`

	err := r.db.SelectContext(ctx, &profiles, query, status, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to find profiles by status: %s", status))
	}

	return profiles, nil
}

// FindExpiringReviews finds profiles with upcoming review dates
func (r *KYCProfileRepository) FindExpiringReviews(ctx context.Context, days int) ([]*domain.KYCProfile, error) {
	var profiles []*domain.KYCProfile
	query := `
		SELECT * FROM customer_schema.kyc_profiles 
		WHERE next_review_date IS NOT NULL 
		AND next_review_date <= CURRENT_DATE + $1 * INTERVAL '1 day'
		AND submission_status = 'approved'
		ORDER BY next_review_date ASC
	`

	err := r.db.SelectContext(ctx, &profiles, query, days)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find expiring reviews")
	}

	return profiles, nil
}

// Delete deletes a KYC profile
func (r *KYCProfileRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM customer_schema.kyc_profiles WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "failed to delete KYC profile")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrKYCProfileNotFound
	}

	return nil
}

// ==============================================================================
// KYC REQUIREMENTS REPOSITORY
// ==============================================================================

// KYCRequirementsRepository implements KYC requirements persistence
type KYCRequirementsRepository struct {
	db *sqlx.DB
}

// NewKYCRequirementsRepository creates a new KYCRequirementsRepository
func NewKYCRequirementsRepository(db *sqlx.DB) *KYCRequirementsRepository {
	return &KYCRequirementsRepository{db: db}
}

// FindByCountryAndUserType finds requirements by country, user type, and KYC level
func (r *KYCRequirementsRepository) FindByCountryAndUserType(ctx context.Context, countryCode, userType string, kycLevel int) (*domain.KYCRequirements, error) {
	var requirements domain.KYCRequirements
	query := `
		SELECT * FROM customer_schema.kyc_requirements 
		WHERE country_code = $1 
		AND user_type = $2 
		AND kyc_level = $3 
		AND is_active = true
		AND (effective_to IS NULL OR effective_to >= CURRENT_DATE)
		ORDER BY version DESC
		LIMIT 1
	`

	err := r.db.GetContext(ctx, &requirements, query, countryCode, userType, kycLevel)
	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCRequirementsNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find KYC requirements")
	}

	return &requirements, nil
}

// FindByID finds requirements by ID
func (r *KYCRequirementsRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.KYCRequirements, error) {
	var requirements domain.KYCRequirements
	query := `SELECT * FROM customer_schema.kyc_requirements WHERE id = $1`

	err := r.db.GetContext(ctx, &requirements, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCRequirementsNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find KYC requirements by ID")
	}

	return &requirements, nil
}

// FindAllActive finds all active requirements
func (r *KYCRequirementsRepository) FindAllActive(ctx context.Context, limit, offset int) ([]*domain.KYCRequirements, error) {
	var requirements []*domain.KYCRequirements
	query := `
		SELECT * FROM customer_schema.kyc_requirements 
		WHERE is_active = true
		AND (effective_to IS NULL OR effective_to >= CURRENT_DATE)
		ORDER BY country_code, user_type, kyc_level, version DESC
		LIMIT $1 OFFSET $2
	`

	err := r.db.SelectContext(ctx, &requirements, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find all active requirements")
	}

	return requirements, nil
}

// Create creates new requirements
func (r *KYCRequirementsRepository) Create(ctx context.Context, requirements *domain.KYCRequirements) error {
	query := `
		INSERT INTO customer_schema.kyc_requirements (
			id, country_code, user_type, kyc_level, required_documents, required_fields,
			conditional_documents, max_file_size_mb, min_file_size_kb, allowed_mime_types,
			allowed_extensions, min_image_width, min_image_height, max_image_width,
			max_image_height, image_aspect_ratio_range, min_age_years, max_age_years,
			min_annual_income, max_annual_income, min_business_turnover, max_business_turnover,
			daily_transaction_limit, monthly_transaction_limit, max_single_transaction,
			display_name, description, instructions, field_labels, field_placeholders,
			field_help_text, estimated_review_days, expedited_review_available,
			expedited_review_hours, auto_approval_threshold, manual_review_required,
			reviewer_role, regulation_references, legal_basis, last_regulatory_update,
			is_active, effective_from, effective_to, version, previous_version_id,
			created_by, updated_by, created_at, updated_at
		) VALUES (
			:id, :country_code, :user_type, :kyc_level, :required_documents, :required_fields,
			:conditional_documents, :max_file_size_mb, :min_file_size_kb, :allowed_mime_types,
			:allowed_extensions, :min_image_width, :min_image_height, :max_image_width,
			:max_image_height, :image_aspect_ratio_range, :min_age_years, :max_age_years,
			:min_annual_income, :max_annual_income, :min_business_turnover, :max_business_turnover,
			:daily_transaction_limit, :monthly_transaction_limit, :max_single_transaction,
			:display_name, :description, :instructions, :field_labels, :field_placeholders,
			:field_help_text, :estimated_review_days, :expedited_review_available,
			:expedited_review_hours, :auto_approval_threshold, :manual_review_required,
			:reviewer_role, :regulation_references, :legal_basis, :last_regulatory_update,
			:is_active, :effective_from, :effective_to, :version, :previous_version_id,
			:created_by, :updated_by, :created_at, :updated_at
		)
	`

	_, err := r.db.NamedExecContext(ctx, query, requirements)
	if err != nil {
		return errors.Wrap(err, "failed to create KYC requirements")
	}

	return nil
}

// Update updates existing requirements
func (r *KYCRequirementsRepository) Update(ctx context.Context, requirements *domain.KYCRequirements) error {
	query := `
		UPDATE customer_schema.kyc_requirements SET
			country_code = :country_code,
			user_type = :user_type,
			kyc_level = :kyc_level,
			required_documents = :required_documents,
			required_fields = :required_fields,
			conditional_documents = :conditional_documents,
			max_file_size_mb = :max_file_size_mb,
			min_file_size_kb = :min_file_size_kb,
			allowed_mime_types = :allowed_mime_types,
			allowed_extensions = :allowed_extensions,
			min_image_width = :min_image_width,
			min_image_height = :min_image_height,
			max_image_width = :max_image_width,
			max_image_height = :max_image_height,
			image_aspect_ratio_range = :image_aspect_ratio_range,
			min_age_years = :min_age_years,
			max_age_years = :max_age_years,
			min_annual_income = :min_annual_income,
			max_annual_income = :max_annual_income,
			min_business_turnover = :min_business_turnover,
			max_business_turnover = :max_business_turnover,
			daily_transaction_limit = :daily_transaction_limit,
			monthly_transaction_limit = :monthly_transaction_limit,
			max_single_transaction = :max_single_transaction,
			display_name = :display_name,
			description = :description,
			instructions = :instructions,
			field_labels = :field_labels,
			field_placeholders = :field_placeholders,
			field_help_text = :field_help_text,
			estimated_review_days = :estimated_review_days,
			expedited_review_available = :expedited_review_available,
			expedited_review_hours = :expedited_review_hours,
			auto_approval_threshold = :auto_approval_threshold,
			manual_review_required = :manual_review_required,
			reviewer_role = :reviewer_role,
			regulation_references = :regulation_references,
			legal_basis = :legal_basis,
			last_regulatory_update = :last_regulatory_update,
			is_active = :is_active,
			effective_from = :effective_from,
			effective_to = :effective_to,
			version = :version,
			previous_version_id = :previous_version_id,
			updated_by = :updated_by,
			updated_at = :updated_at
		WHERE id = :id
	`

	result, err := r.db.NamedExecContext(ctx, query, requirements)
	if err != nil {
		return errors.Wrap(err, "failed to update KYC requirements")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrKYCRequirementsNotFound
	}

	return nil
}

// Deactivate deactivates requirements by setting is_active = false
func (r *KYCRequirementsRepository) Deactivate(ctx context.Context, id uuid.UUID, deactivatedBy uuid.UUID) error {
	query := `
		UPDATE customer_schema.kyc_requirements SET
			is_active = false,
			effective_to = CURRENT_DATE,
			updated_by = $1,
			updated_at = NOW()
		WHERE id = $2 AND is_active = true
	`

	result, err := r.db.ExecContext(ctx, query, deactivatedBy, id)
	if err != nil {
		return errors.Wrap(err, "failed to deactivate KYC requirements")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrKYCRequirementsNotFound
	}

	return nil
}

// FindByCountry finds all requirements for a country
func (r *KYCRequirementsRepository) FindByCountry(ctx context.Context, countryCode string, activeOnly bool) ([]*domain.KYCRequirements, error) {
	var requirements []*domain.KYCRequirements
	query := `
		SELECT * FROM customer_schema.kyc_requirements 
		WHERE country_code = $1
	`

	if activeOnly {
		query += ` AND is_active = true AND (effective_to IS NULL OR effective_to >= CURRENT_DATE)`
	}

	query += ` ORDER BY user_type, kyc_level, version DESC`

	err := r.db.SelectContext(ctx, &requirements, query, countryCode)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find requirements by country")
	}

	return requirements, nil
}

// CountAllActive counts all active requirements
func (r *KYCRequirementsRepository) CountAllActive(ctx context.Context) (int, error) {
	var count int
	query := `
		SELECT COUNT(*) FROM customer_schema.kyc_requirements 
		WHERE is_active = true
		AND (effective_to IS NULL OR effective_to >= CURRENT_DATE)
	`

	err := r.db.GetContext(ctx, &count, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count active requirements")
	}

	return count, nil
}

// ==============================================================================
// KYC DOCUMENT REPOSITORY
// ==============================================================================

// KYCDocumentRepository implements KYC document persistence
type KYCDocumentRepository struct {
	db *sqlx.DB
}

// NewKYCDocumentRepository creates a new KYCDocumentRepository
func NewKYCDocumentRepository(db *sqlx.DB) *KYCDocumentRepository {
	return &KYCDocumentRepository{db: db}
}

// Create creates a new KYC document
// CreateDocument creates a new KYC document within a transaction
func (r *KYCDocumentRepository) CreateDocument(ctx context.Context, tx interface{}, document *domain.KYCDocument) error {
	query := `
		INSERT INTO customer_schema.kyc_documents (
			id, user_id, document_type, document_number, issuing_country, issue_date,
			expiry_date, front_image_url, back_image_url, selfie_image_url,
			file_name, file_size_bytes, mime_type, file_extension, storage_provider,
			storage_bucket, storage_key, storage_region, public_url, internal_path,
			file_hash_sha256, file_hash_md5, checksum_algorithm, file_integrity_verified,
			integrity_verified_at, integrity_verified_by, virus_scan_status, virus_scan_result,
			virus_scan_engine, virus_scan_version, virus_signatures, scanned_at, scanned_by,
			ocr_status, ocr_text, ocr_confidence, ocr_processed_at, extracted_fields,
			validation_status, validation_errors, validation_warnings, is_blurred, is_expired,
			is_tampered, quality_score, validation_notes, validated_at, validated_by,
			verification_status, verification_notes, verified_by, verified_at,
			retention_policy, retention_days, scheduled_deletion_date, is_archived,
			archived_at, archive_reason, access_level, download_count, last_downloaded_at,
			last_downloaded_by, view_count, last_viewed_at, last_viewed_by, process_id,
			workflow_step, review_cycle, is_manual_review_required, manual_review_notes,
			manual_reviewed_at, manual_reviewed_by, metadata, created_at, updated_at
		) VALUES (
			:id, :user_id, :document_type, :document_number, :issuing_country, :issue_date,
			:expiry_date, :front_image_url, :back_image_url, :selfie_image_url,
			:file_name, :file_size_bytes, :mime_type, :file_extension, :storage_provider,
			:storage_bucket, :storage_key, :storage_region, :public_url, :internal_path,
			:file_hash_sha256, :file_hash_md5, :checksum_algorithm, :file_integrity_verified,
			:integrity_verified_at, :integrity_verified_by, :virus_scan_status, :virus_scan_result,
			:virus_scan_engine, :virus_scan_version, :virus_signatures, :scanned_at, :scanned_by,
			:ocr_status, :ocr_text, :ocr_confidence, :ocr_processed_at, :extracted_fields,
			:validation_status, :validation_errors, :validation_warnings, :is_blurred, :is_expired,
			:is_tampered, :quality_score, :validation_notes, :validated_at, :validated_by,
			:verification_status, :verification_notes, :verified_by, :verified_at,
			:retention_policy, :retention_days, :scheduled_deletion_date, :is_archived,
			:archived_at, :archive_reason, :access_level, :download_count, :last_downloaded_at,
			:last_downloaded_by, :view_count, :last_viewed_at, :last_viewed_by, :process_id,
			:workflow_step, :review_cycle, :is_manual_review_required, :manual_review_notes,
			:manual_reviewed_at, :manual_reviewed_by, :metadata, :created_at, :updated_at
		)
	`

	var err error
	if tx == nil {
		_, err = r.db.NamedExecContext(ctx, query, document)
	} else {
		_, err = tx.(*SQLxTransaction).tx.NamedExecContext(ctx, query, document)
	}

	if err != nil {
		return errors.Wrap(err, "failed to create KYC document")
	}

	return nil
}

// UpdateDocumentVerificationStatusTx updates document verification status within a transaction
func (r *KYCDocumentRepository) UpdateDocumentVerificationStatusTx(ctx context.Context, tx interface{}, id uuid.UUID, status domain.DocumentVerificationStatus, notes string, verifiedBy uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE customer_schema.kyc_documents SET
			verification_status = $1,
			verification_notes = COALESCE($2, verification_notes),
			verified_by = $3,
			verified_at = $4,
			updated_at = $4
		WHERE id = $5
	`

	var result sql.Result
	var err error

	if tx == nil {
		result, err = r.db.ExecContext(ctx, query, status, notes, verifiedBy, now, id)
	} else {
		result, err = tx.(*SQLxTransaction).tx.ExecContext(ctx, query, status, notes, verifiedBy, now, id)
	}

	if err != nil {
		return errors.Wrap(err, "failed to update document verification status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// Update updates an existing KYC document
func (r *KYCDocumentRepository) Update(ctx context.Context, document *domain.KYCDocument) error {
	query := `
		UPDATE customer_schema.kyc_documents SET
			document_type = :document_type,
			document_number = :document_number,
			issuing_country = :issuing_country,
			issue_date = :issue_date,
			expiry_date = :expiry_date,
			front_image_url = :front_image_url,
			back_image_url = :back_image_url,
			selfie_image_url = :selfie_image_url,
			file_name = :file_name,
			file_size_bytes = :file_size_bytes,
			mime_type = :mime_type,
			file_extension = :file_extension,
			storage_provider = :storage_provider,
			storage_bucket = :storage_bucket,
			storage_key = :storage_key,
			storage_region = :storage_region,
			public_url = :public_url,
			internal_path = :internal_path,
			file_hash_sha256 = :file_hash_sha256,
			file_hash_md5 = :file_hash_md5,
			checksum_algorithm = :checksum_algorithm,
			file_integrity_verified = :file_integrity_verified,
			integrity_verified_at = :integrity_verified_at,
			integrity_verified_by = :integrity_verified_by,
			virus_scan_status = :virus_scan_status,
			virus_scan_result = :virus_scan_result,
			virus_scan_engine = :virus_scan_engine,
			virus_scan_version = :virus_scan_version,
			virus_signatures = :virus_signatures,
			scanned_at = :scanned_at,
			scanned_by = :scanned_by,
			ocr_status = :ocr_status,
			ocr_text = :ocr_text,
			ocr_confidence = :ocr_confidence,
			ocr_processed_at = :ocr_processed_at,
			extracted_fields = :extracted_fields,
			validation_status = :validation_status,
			validation_errors = :validation_errors,
			validation_warnings = :validation_warnings,
			is_blurred = :is_blurred,
			is_expired = :is_expired,
			is_tampered = :is_tampered,
			quality_score = :quality_score,
			validation_notes = :validation_notes,
			validated_at = :validated_at,
			validated_by = :validated_by,
			verification_status = :verification_status,
			verification_notes = :verification_notes,
			verified_by = :verified_by,
			verified_at = :verified_at,
			retention_policy = :retention_policy,
			retention_days = :retention_days,
			scheduled_deletion_date = :scheduled_deletion_date,
			is_archived = :is_archived,
			archived_at = :archived_at,
			archive_reason = :archive_reason,
			access_level = :access_level,
			download_count = :download_count,
			last_downloaded_at = :last_downloaded_at,
			last_downloaded_by = :last_downloaded_by,
			view_count = :view_count,
			last_viewed_at = :last_viewed_at,
			last_viewed_by = :last_viewed_by,
			process_id = :process_id,
			workflow_step = :workflow_step,
			review_cycle = :review_cycle,
			is_manual_review_required = :is_manual_review_required,
			manual_review_notes = :manual_review_notes,
			manual_reviewed_at = :manual_reviewed_at,
			manual_reviewed_by = :manual_reviewed_by,
			metadata = :metadata,
			updated_at = :updated_at
		WHERE id = :id
	`

	result, err := r.db.NamedExecContext(ctx, query, document)
	if err != nil {
		return errors.Wrap(err, "failed to update KYC document")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// UpdateDocumentVirusScanStatusTx updates virus scan status within a transaction
func (r *KYCDocumentRepository) UpdateDocumentVirusScanStatusTx(ctx context.Context, tx interface{}, id uuid.UUID, status domain.VirusScanStatus, resultStr, engine, version string) error {
	now := time.Now()
	query := `
		UPDATE customer_schema.kyc_documents SET
			virus_scan_status = $1,
			virus_scan_result = COALESCE($2, virus_scan_result),
			virus_scan_engine = COALESCE($3, virus_scan_engine),
			virus_scan_version = COALESCE($4, virus_scan_version),
			scanned_at = $5,
			updated_at = $5
		WHERE id = $6
	`

	var result sql.Result
	var err error

	if tx == nil {
		result, err = r.db.ExecContext(ctx, query, status, resultStr, engine, version, now, id)
	} else {
		result, err = tx.(*SQLxTransaction).tx.ExecContext(ctx, query, status, resultStr, engine, version, now, id)
	}

	if err != nil {
		return errors.Wrap(err, "failed to update virus scan status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// FindByID finds a document by ID
func (r *KYCDocumentRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.KYCDocument, error) {
	var document domain.KYCDocument
	query := `SELECT * FROM customer_schema.kyc_documents WHERE id = $1`

	err := r.db.GetContext(ctx, &document, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrDocumentNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find document by ID")
	}

	return &document, nil
}

// FindByUserID finds all documents for a user
func (r *KYCDocumentRepository) FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.KYCDocument, error) {
	var documents []*domain.KYCDocument
	query := `
		SELECT * FROM customer_schema.kyc_documents 
		WHERE user_id = $1 
		ORDER BY created_at DESC
	`

	err := r.db.SelectContext(ctx, &documents, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find documents by user ID")
	}

	return documents, nil
}

// FindByUserIDAndType finds documents for a user by type
func (r *KYCDocumentRepository) FindByUserIDAndType(ctx context.Context, userID uuid.UUID, docType domain.DocumentType) ([]*domain.KYCDocument, error) {
	var documents []*domain.KYCDocument
	query := `
		SELECT * FROM customer_schema.kyc_documents 
		WHERE user_id = $1 AND document_type = $2
		ORDER BY created_at DESC
	`

	err := r.db.SelectContext(ctx, &documents, query, userID, docType)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find documents by user ID and type")
	}

	return documents, nil
}

// UpdateVerificationStatus updates document verification status
func (r *KYCDocumentRepository) UpdateVerificationStatus(ctx context.Context, id uuid.UUID, status domain.DocumentVerificationStatus, notes string, verifiedBy uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE customer_schema.kyc_documents SET
			verification_status = $1,
			verification_notes = COALESCE($2, verification_notes),
			verified_by = $3,
			verified_at = $4,
			updated_at = $4
		WHERE id = $5
	`

	result, err := r.db.ExecContext(ctx, query, status, notes, verifiedBy, now, id)
	if err != nil {
		return errors.Wrap(err, "failed to update document verification status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// UpdateVirusScanStatus updates virus scan status
func (r *KYCDocumentRepository) UpdateVirusScanStatus(ctx context.Context, id uuid.UUID, status domain.VirusScanStatus, resultStr, engine, version string) error {
	now := time.Now()
	query := `
		UPDATE customer_schema.kyc_documents SET
			virus_scan_status = $1,
			virus_scan_result = COALESCE($2, virus_scan_result),
			virus_scan_engine = COALESCE($3, virus_scan_engine),
			virus_scan_version = COALESCE($4, virus_scan_version),
			scanned_at = $5,
			updated_at = $5
		WHERE id = $6
	`

	result, err := r.db.ExecContext(ctx, query, status, resultStr, engine, version, now, id)
	if err != nil {
		return errors.Wrap(err, "failed to update virus scan status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// FindPendingVirusScan finds documents pending virus scanning
func (r *KYCDocumentRepository) FindPendingVirusScan(ctx context.Context, limit int) ([]*domain.KYCDocument, error) {
	var documents []*domain.KYCDocument
	query := `
		SELECT * FROM customer_schema.kyc_documents 
		WHERE virus_scan_status IN ('pending', 'scanning')
		ORDER BY created_at ASC
		LIMIT $1
	`

	err := r.db.SelectContext(ctx, &documents, query, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find documents pending virus scan")
	}

	return documents, nil
}

// FindPendingVerification finds documents pending verification
func (r *KYCDocumentRepository) FindPendingVerification(ctx context.Context, limit, offset int) ([]*domain.KYCDocument, error) {
	var documents []*domain.KYCDocument
	query := `
		SELECT * FROM customer_schema.kyc_documents 
		WHERE verification_status IN ('pending', 'processing')
		AND virus_scan_status = 'clean'
		AND validation_status = 'valid'
		ORDER BY created_at ASC
		LIMIT $1 OFFSET $2
	`

	err := r.db.SelectContext(ctx, &documents, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find documents pending verification")
	}

	return documents, nil
}

// CountByUserIDAndStatus counts documents by user ID and status
func (r *KYCDocumentRepository) CountByUserIDAndStatus(ctx context.Context, userID uuid.UUID, status domain.DocumentVerificationStatus) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM customer_schema.kyc_documents WHERE user_id = $1 AND verification_status = $2`

	err := r.db.GetContext(ctx, &count, query, userID, status)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count documents by user ID and status")
	}

	return count, nil
}

// Delete deletes a document
func (r *KYCDocumentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM customer_schema.kyc_documents WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "failed to delete document")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// MarkAsArchived marks a document as archived
func (r *KYCDocumentRepository) MarkAsArchived(ctx context.Context, id uuid.UUID, reason string) error {
	now := time.Now()
	query := `
		UPDATE customer_schema.kyc_documents SET
			is_archived = true,
			archived_at = $1,
			archive_reason = $2,
			updated_at = $1
		WHERE id = $3
	`

	result, err := r.db.ExecContext(ctx, query, now, reason, id)
	if err != nil {
		return errors.Wrap(err, "failed to mark document as archived")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// IncrementDownloadCount increments the download count for a document
func (r *KYCDocumentRepository) IncrementDownloadCount(ctx context.Context, id uuid.UUID, downloadedBy uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE customer_schema.kyc_documents SET
			download_count = download_count + 1,
			last_downloaded_at = $1,
			last_downloaded_by = $2,
			updated_at = $1
		WHERE id = $3
	`

	result, err := r.db.ExecContext(ctx, query, now, downloadedBy, id)
	if err != nil {
		return errors.Wrap(err, "failed to increment download count")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

// FindDocumentsDueForDeletion finds documents scheduled for deletion
func (r *KYCDocumentRepository) FindDocumentsDueForDeletion(ctx context.Context, limit int) ([]*domain.KYCDocument, error) {
	var documents []*domain.KYCDocument
	query := `
		SELECT * FROM customer_schema.kyc_documents 
		WHERE scheduled_deletion_date IS NOT NULL 
		AND scheduled_deletion_date <= CURRENT_DATE
		AND is_archived = false
		ORDER BY scheduled_deletion_date ASC
		LIMIT $1
	`

	err := r.db.SelectContext(ctx, &documents, query, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find documents due for deletion")
	}

	return documents, nil
}

// KYCRepositoryComposite implements the complete Repository interface
type KYCRepositoryComposite struct {
	profileRepo      *KYCProfileRepository
	documentRepo     *KYCDocumentRepository
	requirementsRepo *KYCRequirementsRepository
	userRepo         *UserRepository
}

// NewKYCRepositoryComposite creates a composite repository
func NewKYCRepositoryComposite(db *sqlx.DB) *KYCRepositoryComposite {
	return &KYCRepositoryComposite{
		profileRepo:      NewKYCProfileRepository(db),
		documentRepo:     NewKYCDocumentRepository(db),
		requirementsRepo: NewKYCRequirementsRepository(db),
		userRepo:         NewUserRepository(db),
	}
}

// Implement all Repository interface methods by delegating to appropriate repos

// Profile operations
func (r *KYCRepositoryComposite) CreateProfile(ctx context.Context, profile *domain.KYCProfile) error {
	return r.profileRepo.Create(ctx, profile)
}

func (r *KYCRepositoryComposite) UpdateProfile(ctx context.Context, profile *domain.KYCProfile) error {
	return r.profileRepo.Update(ctx, profile)
}

func (r *KYCRepositoryComposite) BeginTransaction(ctx context.Context) (*SQLxTransaction, error) {
	return r.profileRepo.BeginTransaction(ctx)
}

func (r *KYCRepositoryComposite) CreateProfileTx(ctx context.Context, tx interface{}, profile *domain.KYCProfile) error {
	return r.profileRepo.CreateProfileTx(ctx, tx, profile)
}

func (r *KYCRepositoryComposite) UpdateProfileTx(ctx context.Context, tx interface{}, profile *domain.KYCProfile) error {
	return r.profileRepo.UpdateProfileTx(ctx, tx, profile)
}

func (r *KYCRepositoryComposite) UpdateSubmissionStatusTx(ctx context.Context, tx interface{}, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error {
	return r.profileRepo.UpdateSubmissionStatusTx(ctx, tx, id, status, notes)
}

func (r *KYCRepositoryComposite) UpdateAMLStatusTx(ctx context.Context, tx interface{}, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error {
	return r.profileRepo.UpdateAMLStatusTx(ctx, tx, id, status, riskScore, pepCheck, sanctionCheck)
}

func (r *KYCRepositoryComposite) UpdateKYCLevelTx(ctx context.Context, tx interface{}, id uuid.UUID, level int) error {
	return r.profileRepo.UpdateKYCLevelTx(ctx, tx, id, level)
}

func (r *KYCRepositoryComposite) FindProfileByIDTx(ctx context.Context, tx interface{}, id uuid.UUID) (*domain.KYCProfile, error) {
	return r.profileRepo.FindProfileByIDTx(ctx, tx, id)
}

func (r *KYCRepositoryComposite) FindProfileByUserIDTx(ctx context.Context, tx interface{}, userID uuid.UUID) (*domain.KYCProfile, error) {
	return r.profileRepo.FindProfileByUserIDTx(ctx, tx, userID)
}

func (r *KYCRepositoryComposite) ExistsByUserIDTx(ctx context.Context, tx interface{}, userID uuid.UUID) (bool, error) {
	return r.profileRepo.ExistsByUserIDTx(ctx, tx, userID)
}

func (r *KYCRepositoryComposite) FindProfileByID(ctx context.Context, id uuid.UUID) (*domain.KYCProfile, error) {
	return r.profileRepo.FindProfileByIDTx(ctx, nil, id)
}

func (r *KYCRepositoryComposite) FindProfileByUserID(ctx context.Context, userID uuid.UUID) (*domain.KYCProfile, error) {
	return r.profileRepo.FindProfileByUserIDTx(ctx, nil, userID)
}

func (r *KYCRepositoryComposite) ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	return r.profileRepo.ExistsByUserIDTx(ctx, nil, userID)
}

func (r *KYCRepositoryComposite) UpdateSubmissionStatus(ctx context.Context, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error {
	return r.profileRepo.UpdateSubmissionStatusTx(ctx, nil, id, status, notes)
}

func (r *KYCRepositoryComposite) UpdateAMLStatus(ctx context.Context, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error {
	return r.profileRepo.UpdateAMLStatusTx(ctx, nil, id, status, riskScore, pepCheck, sanctionCheck)
}

func (r *KYCRepositoryComposite) UpdateKYCLevel(ctx context.Context, id uuid.UUID, level int) error {
	return r.profileRepo.UpdateKYCLevelTx(ctx, nil, id, level)
}

func (r *KYCRepositoryComposite) FindPendingReview(ctx context.Context, limit, offset int) ([]*domain.KYCProfile, error) {
	// Implementation using non-transactional query
	var profiles []*domain.KYCProfile
	query := `
		SELECT * FROM customer_schema.kyc_profiles 
		WHERE submission_status IN ('submitted', 'under_review', 'additional_info_required')
		ORDER BY submitted_at ASC, created_at ASC
		LIMIT $1 OFFSET $2
	`

	err := r.profileRepo.db.SelectContext(ctx, &profiles, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find pending review profiles")
	}

	return profiles, nil
}

func (r *KYCRepositoryComposite) CountPendingReview(ctx context.Context) (int, error) {
	var count int
	query := `
		SELECT COUNT(*) FROM customer_schema.kyc_profiles 
		WHERE submission_status IN ('submitted', 'under_review', 'additional_info_required')
	`

	err := r.profileRepo.db.GetContext(ctx, &count, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count pending review profiles")
	}

	return count, nil
}

func (r *KYCRepositoryComposite) FindByStatus(ctx context.Context, status domain.KYCSubmissionStatus, limit, offset int) ([]*domain.KYCProfile, error) {
	var profiles []*domain.KYCProfile
	query := `
		SELECT * FROM customer_schema.kyc_profiles 
		WHERE submission_status = $1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`

	err := r.profileRepo.db.SelectContext(ctx, &profiles, query, status, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to find profiles by status: %s", status))
	}

	return profiles, nil
}

// Requirements operations
func (r *KYCRepositoryComposite) FindRequirementsByCountryAndUserType(ctx context.Context, countryCode, userType string, kycLevel int) (*domain.KYCRequirements, error) {
	return r.requirementsRepo.FindByCountryAndUserType(ctx, countryCode, userType, kycLevel)
}

func (r *KYCRepositoryComposite) FindAllActiveRequirements(ctx context.Context, limit, offset int) ([]*domain.KYCRequirements, error) {
	return r.requirementsRepo.FindAllActive(ctx, limit, offset)
}

// Document operations
func (r *KYCRepositoryComposite) CreateDocument(ctx context.Context, document *domain.KYCDocument) error {
	return r.documentRepo.CreateDocument(ctx, nil, document)
}

func (r *KYCRepositoryComposite) UpdateDocument(ctx context.Context, document *domain.KYCDocument) error {
	query := `
		UPDATE customer_schema.kyc_documents SET
			document_type = :document_type,
			document_number = :document_number,
			issuing_country = :issuing_country,
			issue_date = :issue_date,
			expiry_date = :expiry_date,
			front_image_url = :front_image_url,
			back_image_url = :back_image_url,
			selfie_image_url = :selfie_image_url,
			file_name = :file_name,
			file_size_bytes = :file_size_bytes,
			mime_type = :mime_type,
			file_extension = :file_extension,
			storage_provider = :storage_provider,
			storage_bucket = :storage_bucket,
			storage_key = :storage_key,
			storage_region = :storage_region,
			public_url = :public_url,
			internal_path = :internal_path,
			file_hash_sha256 = :file_hash_sha256,
			file_hash_md5 = :file_hash_md5,
			checksum_algorithm = :checksum_algorithm,
			file_integrity_verified = :file_integrity_verified,
			integrity_verified_at = :integrity_verified_at,
			integrity_verified_by = :integrity_verified_by,
			virus_scan_status = :virus_scan_status,
			virus_scan_result = :virus_scan_result,
			virus_scan_engine = :virus_scan_engine,
			virus_scan_version = :virus_scan_version,
			virus_signatures = :virus_signatures,
			scanned_at = :scanned_at,
			scanned_by = :scanned_by,
			ocr_status = :ocr_status,
			ocr_text = :ocr_text,
			ocr_confidence = :ocr_confidence,
			ocr_processed_at = :ocr_processed_at,
			extracted_fields = :extracted_fields,
			validation_status = :validation_status,
			validation_errors = :validation_errors,
			validation_warnings = :validation_warnings,
			is_blurred = :is_blurred,
			is_expired = :is_expired,
			is_tampered = :is_tampered,
			quality_score = :quality_score,
			validation_notes = :validation_notes,
			validated_at = :validated_at,
			validated_by = :validated_by,
			verification_status = :verification_status,
			verification_notes = :verification_notes,
			verified_by = :verified_by,
			verified_at = :verified_at,
			retention_policy = :retention_policy,
			retention_days = :retention_days,
			scheduled_deletion_date = :scheduled_deletion_date,
			is_archived = :is_archived,
			archived_at = :archived_at,
			archive_reason = :archive_reason,
			access_level = :access_level,
			download_count = :download_count,
			last_downloaded_at = :last_downloaded_at,
			last_downloaded_by = :last_downloaded_by,
			view_count = :view_count,
			last_viewed_at = :last_viewed_at,
			last_viewed_by = :last_viewed_by,
			process_id = :process_id,
			workflow_step = :workflow_step,
			review_cycle = :review_cycle,
			is_manual_review_required = :is_manual_review_required,
			manual_review_notes = :manual_review_notes,
			manual_reviewed_at = :manual_reviewed_at,
			manual_reviewed_by = :manual_reviewed_by,
			metadata = :metadata,
			updated_at = :updated_at
		WHERE id = :id
	`

	result, err := r.documentRepo.db.NamedExecContext(ctx, query, document)
	if err != nil {
		return errors.Wrap(err, "failed to update KYC document")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.ErrDocumentNotFound
	}

	return nil
}

func (r *KYCRepositoryComposite) FindDocumentByID(ctx context.Context, id uuid.UUID) (*domain.KYCDocument, error) {
	return r.documentRepo.FindByID(ctx, id)
}

func (r *KYCRepositoryComposite) FindDocumentsByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.KYCDocument, error) {
	return r.documentRepo.FindByUserID(ctx, userID)
}

func (r *KYCRepositoryComposite) FindDocumentsByUserIDAndType(ctx context.Context, userID uuid.UUID, docType domain.DocumentType) ([]*domain.KYCDocument, error) {
	return r.documentRepo.FindByUserIDAndType(ctx, userID, docType)
}

func (r *KYCRepositoryComposite) UpdateDocumentVerificationStatus(ctx context.Context, id uuid.UUID, status domain.DocumentVerificationStatus, notes string, verifiedBy uuid.UUID) error {
	return r.documentRepo.UpdateDocumentVerificationStatusTx(ctx, nil, id, status, notes, verifiedBy)
}

func (r *KYCRepositoryComposite) UpdateDocumentVirusScanStatus(ctx context.Context, id uuid.UUID, status domain.VirusScanStatus, resultStr, engine, version string) error {
	return r.documentRepo.UpdateDocumentVirusScanStatusTx(ctx, nil, id, status, resultStr, engine, version)
}

func (r *KYCRepositoryComposite) FindPendingVirusScan(ctx context.Context, limit int) ([]*domain.KYCDocument, error) {
	return r.documentRepo.FindPendingVirusScan(ctx, limit)
}

func (r *KYCRepositoryComposite) FindPendingVerification(ctx context.Context, limit, offset int) ([]*domain.KYCDocument, error) {
	return r.documentRepo.FindPendingVerification(ctx, limit, offset)
}

func (r *KYCRepositoryComposite) CountDocumentsByUserIDAndStatus(ctx context.Context, userID uuid.UUID, status domain.DocumentVerificationStatus) (int, error) {
	return r.documentRepo.CountByUserIDAndStatus(ctx, userID, status)
}

func (r *KYCRepositoryComposite) DeleteDocument(ctx context.Context, id uuid.UUID) error {
	return r.documentRepo.Delete(ctx, id)
}
