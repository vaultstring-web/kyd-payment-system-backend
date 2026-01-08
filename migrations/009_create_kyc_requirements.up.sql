-- ==============================================================================
-- KYC REQUIREMENTS TABLE MIGRATION
-- ==============================================================================
-- Creates table for storing country/user-type specific KYC requirements
-- Includes document specifications and validation rules
-- ==============================================================================

-- Create kyc_requirements table in customer_schema
CREATE TABLE IF NOT EXISTS customer_schema.kyc_requirements (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    
    -- Targeting criteria
    country_code VARCHAR(2) NOT NULL,
    user_type VARCHAR(20) NOT NULL CHECK (user_type IN ('individual', 'merchant', 'agent', 'business')),
    kyc_level INTEGER NOT NULL DEFAULT 1 CHECK (kyc_level >= 1 AND kyc_level <= 5),
    
    -- ========== DOCUMENT REQUIREMENTS ==========
    -- Document types (JSON array of required document types)
    required_documents JSONB DEFAULT '[]'::jsonb,
    
    -- Field requirements (JSON array of required field names)
    required_fields JSONB DEFAULT '[]'::jsonb,
    
    -- Conditional documents (business rules)
    conditional_documents JSONB DEFAULT '{}'::jsonb,
    
    -- ========== DOCUMENT SPECIFICATIONS ==========
    -- File constraints
    max_file_size_mb INTEGER DEFAULT 5 CHECK (max_file_size_mb > 0 AND max_file_size_mb <= 50),
    min_file_size_kb INTEGER DEFAULT 10,
    
    -- Allowed MIME types (JSON array)
    allowed_mime_types JSONB DEFAULT '[
        "image/jpeg",
        "image/png", 
        "image/jpg",
        "application/pdf"
    ]'::jsonb,
    
    -- Allowed file extensions
    allowed_extensions JSONB DEFAULT '["jpg", "jpeg", "png", "pdf"]'::jsonb,
    
    -- Image specific constraints
    min_image_width INTEGER DEFAULT 600,
    min_image_height INTEGER DEFAULT 600,
    max_image_width INTEGER DEFAULT 4000,
    max_image_height INTEGER DEFAULT 4000,
    image_aspect_ratio_range JSONB DEFAULT '{"min": 0.5, "max": 2.0}'::jsonb,
    
    -- ========== VALIDATION RULES ==========
    -- Age restrictions
    min_age_years INTEGER DEFAULT 18,
    max_age_years INTEGER,
    
    -- Income/amount restrictions
    min_annual_income DECIMAL(20,2),
    max_annual_income DECIMAL(20,2),
    min_business_turnover DECIMAL(20,2),
    max_business_turnover DECIMAL(20,2),
    
    -- Transaction limits tied to KYC level
    daily_transaction_limit DECIMAL(20,2),
    monthly_transaction_limit DECIMAL(20,2),
    max_single_transaction DECIMAL(20,2),
    
    -- ========== DISPLAY & LOCALIZATION ==========
    -- Human-readable information
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    instructions TEXT,
    
    -- Country-specific labels (for form fields)
    field_labels JSONB DEFAULT '{}'::jsonb,
    field_placeholders JSONB DEFAULT '{}'::jsonb,
    field_help_text JSONB DEFAULT '{}'::jsonb,
    
    -- ========== REVIEW & PROCESSING ==========
    -- Review timelines
    estimated_review_days INTEGER DEFAULT 3 CHECK (estimated_review_days >= 0),
    expedited_review_available BOOLEAN DEFAULT FALSE,
    expedited_review_hours INTEGER,
    
    -- Review workflow
    auto_approval_threshold DECIMAL(5,2) DEFAULT 85.00, -- Risk score below which auto-approve
    manual_review_required BOOLEAN DEFAULT TRUE,
    reviewer_role VARCHAR(50) DEFAULT 'compliance_officer',
    
    -- ========== COMPLIANCE & REGULATIONS ==========
    -- Regulatory references
    regulation_references JSONB DEFAULT '[]'::jsonb,
    legal_basis TEXT,
    last_regulatory_update DATE,
    
    -- ========== STATUS & VERSIONING ==========
    -- Activation
    is_active BOOLEAN DEFAULT TRUE,
    effective_from DATE NOT NULL DEFAULT CURRENT_DATE,
    effective_to DATE,
    
    -- Version tracking
    version INTEGER DEFAULT 1,
    previous_version_id UUID REFERENCES customer_schema.kyc_requirements(id),
    
    -- Audit
    created_by UUID REFERENCES customer_schema.users(id),
    updated_by UUID REFERENCES customer_schema.users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    -- Constraints
    UNIQUE(country_code, user_type, kyc_level, version),
    
    -- Validation constraints
    CONSTRAINT chk_effective_dates CHECK (
        effective_to IS NULL OR effective_to > effective_from
    ),
    CONSTRAINT chk_limits CHECK (
        daily_transaction_limit IS NULL OR 
        monthly_transaction_limit IS NULL OR 
        monthly_transaction_limit >= daily_transaction_limit
    ),
    CONSTRAINT chk_income_ranges CHECK (
        (min_annual_income IS NULL AND max_annual_income IS NULL) OR
        (min_annual_income IS NOT NULL AND max_annual_income IS NOT NULL AND max_annual_income > min_annual_income)
    )
);

-- ========== INDEXES FOR PERFORMANCE ==========
-- Primary lookup (country + user_type + kyc_level)
CREATE INDEX idx_kyc_req_lookup ON customer_schema.kyc_requirements(country_code, user_type, kyc_level);

-- Active requirements queries
CREATE INDEX idx_kyc_req_active ON customer_schema.kyc_requirements(is_active) 
WHERE is_active = true;

-- Date range queries
CREATE INDEX idx_kyc_req_effective ON customer_schema.kyc_requirements(effective_from, effective_to);

-- Version queries
CREATE INDEX idx_kyc_req_version ON customer_schema.kyc_requirements(version, previous_version_id);

-- ========== ROW LEVEL SECURITY ==========
-- Enable RLS (following existing pattern)
ALTER TABLE customer_schema.kyc_requirements ENABLE ROW LEVEL SECURITY;

-- Policy: Read-only access for authenticated users, full access for admin
CREATE POLICY kyc_requirements_read ON customer_schema.kyc_requirements
    FOR SELECT
    USING (
        current_user = 'kyd_system' OR
        current_user = 'kyd_admin' OR
        current_user = 'postgres' OR
        current_setting('app.current_user_id', true) IS NOT NULL
    );

CREATE POLICY kyc_requirements_admin ON customer_schema.kyc_requirements
    FOR ALL
    TO kyd_admin
    USING (true);

-- ========== AUDIT TRIGGER ==========
-- Add to existing audit trigger system
CREATE TRIGGER audit_kyc_requirements_change
AFTER INSERT OR UPDATE OR DELETE ON customer_schema.kyc_requirements
FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();

-- ========== UPDATED_AT TRIGGER ==========
CREATE TRIGGER update_kyc_requirements_updated_at 
BEFORE UPDATE ON customer_schema.kyc_requirements
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ========== VERSIONING TRIGGER ==========
-- Ensure new versions get incremented version numbers
CREATE OR REPLACE FUNCTION customer_schema.increment_kyc_requirement_version()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- For new records, set version to 1
        NEW.version := 1;
    ELSIF TG_OP = 'UPDATE' THEN
        -- If any significant field changes, create new version
        IF OLD.country_code != NEW.country_code OR
           OLD.user_type != NEW.user_type OR
           OLD.kyc_level != NEW.kyc_level OR
           OLD.required_documents != NEW.required_documents OR
           OLD.required_fields != NEW.required_fields THEN
           
            -- Archive old version
            INSERT INTO customer_schema.kyc_requirements_archive
            SELECT OLD.*;
            
            -- Set previous_version_id for new version
            NEW.previous_version_id := OLD.id;
            NEW.version := OLD.version + 1;
            NEW.id := uuid_generate_v4(); -- New ID for new version
            NEW.created_at := NOW(); -- Reset created_at for new version
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER increment_kyc_requirement_version
BEFORE INSERT OR UPDATE ON customer_schema.kyc_requirements
FOR EACH ROW EXECUTE FUNCTION customer_schema.increment_kyc_requirement_version();

-- ========== ARCHIVE TABLE FOR OLD VERSIONS ==========
-- Create archive table for old versions (optional, for history tracking)
CREATE TABLE IF NOT EXISTS customer_schema.kyc_requirements_archive (
    LIKE customer_schema.kyc_requirements INCLUDING ALL,
    archived_at TIMESTAMPTZ DEFAULT NOW(),
    archived_reason VARCHAR(100)
);

-- ========== SEED DATA ==========
-- Insert default KYC requirements for CN (China), MW (Malawi), and US (United States)

-- China (CN) - Individual (KYC Level 1)
INSERT INTO customer_schema.kyc_requirements (
    country_code,
    user_type,
    kyc_level,
    display_name,
    description,
    required_documents,
    required_fields,
    instructions,
    daily_transaction_limit,
    monthly_transaction_limit,
    max_single_transaction,
    estimated_review_days
) VALUES (
    'CN',
    'individual',
    1,
    'China Individual - Basic Verification',
    'Basic KYC for individuals in China. Required for daily transactions up to ¥50,000.',
    '["national_id", "selfie_with_id"]'::jsonb,
    '["first_name", "last_name", "date_of_birth", "phone_number", "address_line1", "city", "province"]'::jsonb,
    '请上传您的身份证正反面照片以及手持身份证的自拍照片。确保所有信息清晰可见。',
    50000.00,
    200000.00,
    20000.00,
    1
) ON CONFLICT (country_code, user_type, kyc_level, version) DO NOTHING;

-- China (CN) - Merchant (KYC Level 1)
INSERT INTO customer_schema.kyc_requirements (
    country_code,
    user_type,
    kyc_level,
    display_name,
    description,
    required_documents,
    required_fields,
    instructions,
    daily_transaction_limit,
    monthly_transaction_limit,
    max_single_transaction,
    estimated_review_days
) VALUES (
    'CN',
    'merchant',
    1,
    'China Merchant - Business Verification',
    'Basic business verification for merchants in China.',
    '["business_license", "tax_registration", "bank_account_proof", "legal_representative_id"]'::jsonb,
    '["company_name", "business_registration_number", "tax_id", "legal_representative_name", "business_address", "phone_number"]'::jsonb,
    '请上传营业执照、税务登记证、银行开户证明和法人代表身份证。',
    200000.00,
    1000000.00,
    100000.00,
    3
) ON CONFLICT (country_code, user_type, kyc_level, version) DO NOTHING;

-- Malawi (MW) - Individual (KYC Level 1)
INSERT INTO customer_schema.kyc_requirements (
    country_code,
    user_type,
    kyc_level,
    display_name,
    description,
    required_documents,
    required_fields,
    instructions,
    daily_transaction_limit,
    monthly_transaction_limit,
    max_single_transaction,
    estimated_review_days
) VALUES (
    'MW',
    'individual',
    1,
    'Malawi Individual - Basic Verification',
    'Basic KYC for individuals in Malawi. Required for daily transactions up to MWK 1,000,000.',
    '["national_id", "passport", "drivers_license", "utility_bill"]'::jsonb,
    '["first_name", "last_name", "date_of_birth", "phone_number", "address_line1", "city", "district"]'::jsonb,
    'Please upload your National ID, Passport, or Driver''s License and a recent utility bill for address verification.',
    1000000.00,
    5000000.00,
    500000.00,
    2
) ON CONFLICT (country_code, user_type, kyc_level, version) DO NOTHING;

-- Malawi (MW) - Agent (KYC Level 1)
INSERT INTO customer_schema.kyc_requirements (
    country_code,
    user_type,
    kyc_level,
    display_name,
    description,
    required_documents,
    required_fields,
    instructions,
    daily_transaction_limit,
    monthly_transaction_limit,
    max_single_transaction,
    estimated_review_days
) VALUES (
    'MW',
    'agent',
    1,
    'Malawi Agent - Agent Verification',
    'Verification for agents in Malawi handling customer transactions.',
    '["national_id", "agent_license", "business_registration", "bank_statement"]'::jsonb,
    '["first_name", "last_name", "date_of_birth", "phone_number", "business_name", "agent_license_number", "business_address"]'::jsonb,
    'Upload your National ID, Agent License certificate, business registration, and 3 months bank statements.',
    5000000.00,
    25000000.00,
    2000000.00,
    5
) ON CONFLICT (country_code, user_type, kyc_level, version) DO NOTHING;

-- United States (US) - Individual (KYC Level 1) - For reference/completeness
INSERT INTO customer_schema.kyc_requirements (
    country_code,
    user_type,
    kyc_level,
    display_name,
    description,
    required_documents,
    required_fields,
    instructions,
    daily_transaction_limit,
    monthly_transaction_limit,
    max_single_transaction,
    estimated_review_days
) VALUES (
    'US',
    'individual',
    1,
    'USA Individual - Basic Verification',
    'Basic KYC for individuals in the United States.',
    '["passport", "drivers_license", "social_security_card", "utility_bill"]'::jsonb,
    '["first_name", "last_name", "date_of_birth", "ssn_last_4", "address_line1", "city", "state", "zip_code"]'::jsonb,
    'Upload government-issued ID (passport or driver''s license) and proof of address (utility bill).',
    10000.00,
    50000.00,
    5000.00,
    2
) ON CONFLICT (country_code, user_type, kyc_level, version) DO NOTHING;

-- China (CN) - Individual (KYC Level 2 - Enhanced)
INSERT INTO customer_schema.kyc_requirements (
    country_code,
    user_type,
    kyc_level,
    display_name,
    description,
    required_documents,
    required_fields,
    instructions,
    daily_transaction_limit,
    monthly_transaction_limit,
    max_single_transaction,
    estimated_review_days
) VALUES (
    'CN',
    'individual',
    2,
    'China Individual - Enhanced Verification',
    'Enhanced KYC for higher transaction limits in China.',
    '["national_id", "selfie_with_id", "bank_statement", "proof_of_income"]'::jsonb,
    '["first_name", "last_name", "date_of_birth", "phone_number", "address_line1", "city", "province", "occupation", "annual_income"]'::jsonb,
    '上传身份证、手持身份证自拍、最近3个月银行流水和收入证明。',
    200000.00,
    1000000.00,
    100000.00,
    3
) ON CONFLICT (country_code, user_type, kyc_level, version) DO NOTHING;

-- Malawi (MW) - Individual (KYC Level 2 - Enhanced)
INSERT INTO customer_schema.kyc_requirements (
    country_code,
    user_type,
    kyc_level,
    display_name,
    description,
    required_documents,
    required_fields,
    instructions,
    daily_transaction_limit,
    monthly_transaction_limit,
    max_single_transaction,
    estimated_review_days
) VALUES (
    'MW',
    'individual',
    2,
    'Malawi Individual - Enhanced Verification',
    'Enhanced KYC for higher transaction limits in Malawi.',
    '["national_id", "passport", "utility_bill", "bank_statement", "proof_of_income"]'::jsonb,
    '["first_name", "last_name", "date_of_birth", "phone_number", "address_line1", "city", "district", "occupation", "employer"]'::jsonb,
    'Upload National ID/Passport, utility bill, 3 months bank statements, and proof of income.',
    5000000.00,
    20000000.00,
    2000000.00,
    3
) ON CONFLICT (country_code, user_type, kyc_level, version) DO NOTHING;

-- ========== HELPER VIEWS ==========
-- View for current active requirements
CREATE OR REPLACE VIEW customer_schema.current_kyc_requirements AS
SELECT *
FROM customer_schema.kyc_requirements
WHERE is_active = true
AND (effective_to IS NULL OR effective_to >= CURRENT_DATE)
ORDER BY country_code, user_type, kyc_level;

-- View for requirement completeness check
CREATE OR REPLACE VIEW customer_schema.user_kyc_completeness AS
SELECT 
    u.id as user_id,
    u.country_code,
    u.user_type,
    kyc.submission_status as kyc_status,
    kyc.kyc_level,
    req.required_documents,
    req.required_fields,
    (
        SELECT COUNT(DISTINCT kd.document_type)
        FROM customer_schema.kyc_documents kd
        WHERE kd.user_id = u.id 
        AND kd.verification_status = 'verified'
        AND kd.document_type = ANY(SELECT jsonb_array_elements_text(req.required_documents))
    ) as documents_submitted_count,
    jsonb_array_length(req.required_documents) as documents_required_count
FROM customer_schema.users u
LEFT JOIN customer_schema.kyc_profiles kyc ON u.id = kyc.user_id
LEFT JOIN customer_schema.current_kyc_requirements req ON 
    u.country_code = req.country_code 
    AND u.user_type = req.user_type 
    AND COALESCE(kyc.kyc_level, 1) = req.kyc_level;

/*
-- ========== DOWN MIGRATION ==========
-- 009_create_kyc_requirements.down.sql

-- Drop views
DROP VIEW IF EXISTS customer_schema.user_kyc_completeness;
DROP VIEW IF EXISTS customer_schema.current_kyc_requirements;

-- Drop triggers
DROP TRIGGER IF EXISTS increment_kyc_requirement_version ON customer_schema.kyc_requirements;
DROP TRIGGER IF EXISTS update_kyc_requirements_updated_at ON customer_schema.kyc_requirements;
DROP TRIGGER IF EXISTS audit_kyc_requirements_change ON customer_schema.kyc_requirements;

-- Drop functions
DROP FUNCTION IF EXISTS customer_schema.increment_kyc_requirement_version();

-- Drop policies
DROP POLICY IF EXISTS kyc_requirements_admin ON customer_schema.kyc_requirements;
DROP POLICY IF EXISTS kyc_requirements_read ON customer_schema.kyc_requirements;

-- Drop archive table
DROP TABLE IF EXISTS customer_schema.kyc_requirements_archive;

-- Drop indexes
DROP INDEX IF EXISTS idx_kyc_req_lookup;
DROP INDEX IF EXISTS idx_kyc_req_active;
DROP INDEX IF EXISTS idx_kyc_req_effective;
DROP INDEX IF EXISTS idx_kyc_req_version;

-- Drop table
DROP TABLE IF EXISTS customer_schema.kyc_requirements;
*/