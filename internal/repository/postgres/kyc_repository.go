// ==============================================================================
// KYC REPOSITORY IMPLEMENTATION
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

// Create inserts a new KYC profile
func (r *KYCProfileRepository) Create(ctx context.Context, profile *domain.KYCProfile) error {
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

	_, err := r.db.NamedExecContext(ctx, query, profile)
	if err != nil {
		return errors.Wrap(err, "failed to create KYC profile")
	}

	return nil
}

// Update updates an existing KYC profile
func (r *KYCProfileRepository) Update(ctx context.Context, profile *domain.KYCProfile) error {
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

	result, err := r.db.NamedExecContext(ctx, query, profile)
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

// FindByID finds a KYC profile by ID
func (r *KYCProfileRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.KYCProfile, error) {
	var profile domain.KYCProfile
	query := `SELECT * FROM customer_schema.kyc_profiles WHERE id = $1`

	err := r.db.GetContext(ctx, &profile, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCProfileNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find KYC profile by ID")
	}

	return &profile, nil
}

// FindByUserID finds a KYC profile by user ID
func (r *KYCProfileRepository) FindByUserID(ctx context.Context, userID uuid.UUID) (*domain.KYCProfile, error) {
	var profile domain.KYCProfile
	query := `SELECT * FROM customer_schema.kyc_profiles WHERE user_id = $1`

	err := r.db.GetContext(ctx, &profile, query, userID)
	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCProfileNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find KYC profile by user ID")
	}

	return &profile, nil
}

// ExistsByUserID checks if a KYC profile exists for a user
func (r *KYCProfileRepository) ExistsByUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM customer_schema.kyc_profiles WHERE user_id = $1)`

	err := r.db.GetContext(ctx, &exists, query, userID)
	if err != nil {
		return false, errors.Wrap(err, "failed to check KYC profile existence")
	}

	return exists, nil
}

// UpdateSubmissionStatus updates only the submission status and related timestamps
func (r *KYCProfileRepository) UpdateSubmissionStatus(ctx context.Context, id uuid.UUID, status domain.KYCSubmissionStatus, notes string) error {
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

	result, err := r.db.ExecContext(ctx, query, status, notes, now, id)
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

// UpdateAMLStatus updates AML-related fields
func (r *KYCProfileRepository) UpdateAMLStatus(ctx context.Context, id uuid.UUID, status domain.AMLStatus, riskScore decimal.Decimal, pepCheck, sanctionCheck bool) error {
	query := `
		UPDATE customer_schema.kyc_profiles SET
			aml_check_status = $1,
			aml_risk_score = $2,
			pep_check = $3,
			sanction_check = $4,
			updated_at = NOW()
		WHERE id = $5
	`

	result, err := r.db.ExecContext(ctx, query, status, riskScore, pepCheck, sanctionCheck, id)
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

// UpdateKYCLevel updates the KYC level
func (r *KYCProfileRepository) UpdateKYCLevel(ctx context.Context, id uuid.UUID, level int) error {
	query := `
		UPDATE customer_schema.kyc_profiles SET
			kyc_level = $1,
			updated_at = NOW()
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, level, id)
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
func (r *KYCDocumentRepository) Create(ctx context.Context, document *domain.KYCDocument) error {
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

	_, err := r.db.NamedExecContext(ctx, query, document)
	if err != nil {
		return errors.Wrap(err, "failed to create KYC document")
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
