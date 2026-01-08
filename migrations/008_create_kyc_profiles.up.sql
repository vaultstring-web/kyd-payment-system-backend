-- ==============================================================================
-- KYC PROFILES TABLE MIGRATION
-- ==============================================================================
-- Creates table for storing detailed KYC profile information
-- Follows existing customer_schema pattern with RLS support
-- ==============================================================================

-- Create kyc_profiles table in customer_schema
CREATE TABLE IF NOT EXISTS customer_schema.kyc_profiles (
    -- Primary key and reference
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES customer_schema.users(id) ON DELETE CASCADE,
    
    -- Profile type classification
    profile_type VARCHAR(20) NOT NULL CHECK (profile_type IN ('individual', 'business')),
    
    -- ========== INDIVIDUAL FIELDS ==========
    -- Personal identification
    date_of_birth DATE,
    place_of_birth VARCHAR(100),
    nationality VARCHAR(2), -- ISO 3166-1 alpha-2
    
    -- Employment and financial
    occupation VARCHAR(100),
    employer_name VARCHAR(255),
    annual_income_range VARCHAR(50) CHECK (annual_income_range IN (
        'less_than_10k', '10k_50k', '50k_100k', '100k_250k', '250k_500k', '500k_1m', 'over_1m'
    )),
    source_of_funds VARCHAR(100) CHECK (source_of_funds IN (
        'employment', 'business', 'investments', 'inheritance', 'savings', 'other'
    )),
    
    -- ========== BUSINESS FIELDS ==========
    -- Company information
    company_name VARCHAR(255),
    company_registration_number VARCHAR(100),
    company_tax_id VARCHAR(100),
    business_nature VARCHAR(255),
    incorporation_date DATE,
    
    -- Business financials
    annual_turnover_range VARCHAR(50) CHECK (annual_turnover_range IN (
        'less_than_50k', '50k_250k', '250k_1m', '1m_5m', '5m_10m', '10m_50m', 'over_50m'
    )),
    number_of_employees INTEGER,
    business_industry VARCHAR(100),
    
    -- ========== ADDRESS INFORMATION ==========
    -- Physical address (required for verification)
    address_line1 VARCHAR(255) NOT NULL,
    address_line2 VARCHAR(255),
    city VARCHAR(100) NOT NULL,
    state_province VARCHAR(100),
    postal_code VARCHAR(20),
    country_code VARCHAR(2) NOT NULL,
    
    -- ========== CONTACT INFORMATION ==========
    -- Primary contact (in addition to user.phone)
    phone_number VARCHAR(50),
    alt_phone_number VARCHAR(50),
    
    -- ========== KYC STATUS TRACKING ==========
    -- Submission workflow
    submission_status VARCHAR(20) DEFAULT 'draft' CHECK (submission_status IN (
        'draft', 'submitted', 'under_review', 'additional_info_required',
        'approved', 'rejected', 'suspended'
    )),
    

    kyc_level INTEGER DEFAULT 1 CHECK (kyc_level >= 0 AND kyc_level <= 5),

    -- Review process
    review_notes TEXT,
    reviewed_by UUID REFERENCES customer_schema.users(id),
    reviewed_at TIMESTAMPTZ,
    next_review_date DATE,
    
    -- ========== AML/CFT COMPLIANCE ==========
    -- Risk assessment
    aml_risk_score DECIMAL(5,2) DEFAULT 0.00 CHECK (aml_risk_score >= 0 AND aml_risk_score <= 100),
    aml_check_status VARCHAR(20) DEFAULT 'pending' CHECK (aml_check_status IN (
        'pending', 'processing', 'cleared', 'flagged', 'escalated', 'rejected'
    )),
    
    -- Sanctions/PEP screening
    pep_check BOOLEAN DEFAULT FALSE,
    sanction_check BOOLEAN DEFAULT FALSE,
    pep_type VARCHAR(50), -- 'domestic', 'foreign', 'international_organization'
    sanction_list VARCHAR(100), -- Which list flagged (OFAC, UN, EU, etc.)
    
    -- Enhanced due diligence
    edd_required BOOLEAN DEFAULT FALSE,
    edd_level INTEGER DEFAULT 0,
    edd_review_date DATE,
    
    -- ========== AUDIT & METADATA ==========
    -- JSON metadata for flexible field storage
    metadata JSONB DEFAULT '{}',
    
    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    rejected_at TIMESTAMPTZ,
    
    -- Constraints
    UNIQUE(user_id),
    
    -- Conditional constraints
    CONSTRAINT chk_individual_fields CHECK (
        profile_type != 'individual' OR (
            date_of_birth IS NOT NULL AND
            nationality IS NOT NULL AND
            occupation IS NOT NULL
        )
    ),
    
    CONSTRAINT chk_business_fields CHECK (
        profile_type != 'business' OR (
            company_name IS NOT NULL AND
            company_registration_number IS NOT NULL AND
            business_nature IS NOT NULL
        )
    ),
    
    CONSTRAINT chk_age_restriction CHECK (
        date_of_birth IS NULL OR 
        date_of_birth <= CURRENT_DATE - INTERVAL '18 years'
    )
);

-- ========== INDEXES FOR PERFORMANCE ==========
-- User lookup (most common query)
CREATE INDEX idx_kyc_profiles_user_id ON customer_schema.kyc_profiles(user_id);

-- Status-based queries (admin dashboards)
CREATE INDEX idx_kyc_profiles_submission_status ON customer_schema.kyc_profiles(submission_status);
CREATE INDEX idx_kyc_profiles_aml_status ON customer_schema.kyc_profiles(aml_check_status);

-- Country-based segmentation
CREATE INDEX idx_kyc_profiles_country ON customer_schema.kyc_profiles(country_code);

-- Review date queries (compliance scheduling)
CREATE INDEX idx_kyc_profiles_next_review ON customer_schema.kyc_profiles(next_review_date) 
WHERE next_review_date IS NOT NULL;

-- Date range queries for reporting
CREATE INDEX idx_kyc_profiles_created_at ON customer_schema.kyc_profiles(created_at);
CREATE INDEX idx_kyc_profiles_submitted_at ON customer_schema.kyc_profiles(submitted_at);

-- ========== ROW LEVEL SECURITY ==========
-- Enable RLS (following existing pattern)
ALTER TABLE customer_schema.kyc_profiles ENABLE ROW LEVEL SECURITY;

-- Policy: Users can only see their own profiles
CREATE POLICY kyc_profile_isolation ON customer_schema.kyc_profiles
    FOR ALL
    USING (
        user_id::text = current_setting('app.current_user_id', true) OR
        current_user = 'kyd_system' OR
        current_user = 'kyd_admin' OR
        current_user = 'postgres'
    );

-- ========== AUDIT TRIGGER ==========
-- Add to existing audit trigger system (from migration 003)
CREATE TRIGGER audit_kyc_profiles_change
AFTER INSERT OR UPDATE OR DELETE ON customer_schema.kyc_profiles
FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();

-- ========== UPDATED_AT TRIGGER ==========
-- Add updated_at trigger (following existing pattern)
CREATE TRIGGER update_kyc_profiles_updated_at 
BEFORE UPDATE ON customer_schema.kyc_profiles
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ========== STATUS VALIDATION FUNCTION ==========
-- Ensure status transitions are valid
CREATE OR REPLACE FUNCTION customer_schema.validate_kyc_status_transition()
RETURNS TRIGGER AS $$
BEGIN
    -- Prevent moving from final states back to draft/submitted
    IF OLD.submission_status IN ('approved', 'rejected', 'suspended') AND
       NEW.submission_status IN ('draft', 'submitted') THEN
        RAISE EXCEPTION 'Cannot move from % to %', OLD.submission_status, NEW.submission_status;
    END IF;
    
    -- Set timestamps based on status changes
    IF NEW.submission_status = 'submitted' AND OLD.submission_status != 'submitted' THEN
        NEW.submitted_at = NOW();
    END IF;
    
    IF NEW.submission_status = 'approved' AND OLD.submission_status != 'approved' THEN
        NEW.approved_at = NOW();
        NEW.reviewed_at = NOW(); -- Auto-set reviewed_at
    END IF;
    
    IF NEW.submission_status = 'rejected' AND OLD.submission_status != 'rejected' THEN
        NEW.rejected_at = NOW();
        NEW.reviewed_at = NOW(); -- Auto-set reviewed_at
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER validate_kyc_status
BEFORE UPDATE OF submission_status ON customer_schema.kyc_profiles
FOR EACH ROW EXECUTE FUNCTION customer_schema.validate_kyc_status_transition();

-- ========== SAMPLE DATA FOR DEVELOPMENT ==========
-- Insert sample KYC profiles for existing users (development only)
-- Note: Remove in production or use seed script instead
DO $$
BEGIN
    -- Only insert if in development environment (based on presence of test user)
    IF EXISTS (SELECT 1 FROM customer_schema.users WHERE email = 'test@example.com') THEN
        INSERT INTO customer_schema.kyc_profiles (
            user_id,
            profile_type,
            date_of_birth,
            nationality,
            occupation,
            annual_income_range,
            source_of_funds,
            address_line1,
            city,
            country_code,
            submission_status,
            aml_risk_score,
            aml_check_status
        ) 
        SELECT 
            u.id,
            'individual',
            '1990-01-15',
            'CN',
            'Software Engineer',
            '100k_250k',
            'employment',
            '123 Main Street',
            'Beijing',
            'CN',
            'approved',
            12.50,
            'cleared'
        FROM customer_schema.users u 
        WHERE u.email = 'test@example.com'
        AND NOT EXISTS (
            SELECT 1 FROM customer_schema.kyc_profiles kp 
            WHERE kp.user_id = u.id
        );
    END IF;
END $$;

-- ========== DOWN MIGRATION ==========
-- Note: This comment block is for the corresponding .down.sql file
/*
-- 008_create_kyc_profiles.down.sql
DROP TRIGGER IF EXISTS validate_kyc_status ON customer_schema.kyc_profiles;
DROP TRIGGER IF EXISTS update_kyc_profiles_updated_at ON customer_schema.kyc_profiles;
DROP TRIGGER IF EXISTS audit_kyc_profiles_change ON customer_schema.kyc_profiles;

DROP POLICY IF EXISTS kyc_profile_isolation ON customer_schema.kyc_profiles;

DROP FUNCTION IF EXISTS customer_schema.validate_kyc_status_transition();

DROP INDEX IF EXISTS idx_kyc_profiles_user_id;
DROP INDEX IF EXISTS idx_kyc_profiles_submission_status;
DROP INDEX IF EXISTS idx_kyc_profiles_aml_status;
DROP INDEX IF EXISTS idx_kyc_profiles_country;
DROP INDEX IF EXISTS idx_kyc_profiles_next_review;
DROP INDEX IF EXISTS idx_kyc_profiles_created_at;
DROP INDEX IF EXISTS idx_kyc_profiles_submitted_at;

DROP TABLE IF EXISTS customer_schema.kyc_profiles;
*/