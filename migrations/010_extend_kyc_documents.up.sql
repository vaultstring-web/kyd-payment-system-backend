-- ==============================================================================
-- EXTEND KYC_DOCUMENTS TABLE MIGRATION
-- ==============================================================================
-- Adds file metadata and virus scanning columns to existing kyc_documents table
-- Maintains backward compatibility with existing data
-- ==============================================================================

-- ========== ADD NEW COLUMNS TO EXISTING TABLE ==========
-- File metadata and storage information
ALTER TABLE customer_schema.kyc_documents 
ADD COLUMN IF NOT EXISTS file_name VARCHAR(255),
ADD COLUMN IF NOT EXISTS file_size_bytes BIGINT CHECK (file_size_bytes >= 0),
ADD COLUMN IF NOT EXISTS mime_type VARCHAR(100),
ADD COLUMN IF NOT EXISTS file_extension VARCHAR(10),
ADD COLUMN IF NOT EXISTS storage_provider VARCHAR(50) DEFAULT 'local' CHECK (storage_provider IN ('local', 's3', 'minio', 'gcs')),
ADD COLUMN IF NOT EXISTS storage_bucket VARCHAR(255),
ADD COLUMN IF NOT EXISTS storage_key VARCHAR(500),
ADD COLUMN IF NOT EXISTS storage_region VARCHAR(100),
ADD COLUMN IF NOT EXISTS public_url VARCHAR(500),
ADD COLUMN IF NOT EXISTS internal_path VARCHAR(500);

-- File integrity and validation
ALTER TABLE customer_schema.kyc_documents 
ADD COLUMN IF NOT EXISTS file_hash_sha256 VARCHAR(64),
ADD COLUMN IF NOT EXISTS file_hash_md5 VARCHAR(32),
ADD COLUMN IF NOT EXISTS checksum_algorithm VARCHAR(20) DEFAULT 'sha256' CHECK (checksum_algorithm IN ('md5', 'sha256', 'sha512')),
ADD COLUMN IF NOT EXISTS file_integrity_verified BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS integrity_verified_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS integrity_verified_by UUID REFERENCES customer_schema.users(id);

-- Virus scanning and security
ALTER TABLE customer_schema.kyc_documents 
ADD COLUMN IF NOT EXISTS virus_scan_status VARCHAR(20) DEFAULT 'pending' CHECK (virus_scan_status IN (
    'pending', 'scanning', 'clean', 'infected', 'error', 'quarantined', 'skipped'
)),
ADD COLUMN IF NOT EXISTS virus_scan_result TEXT,
ADD COLUMN IF NOT EXISTS virus_scan_engine VARCHAR(50),
ADD COLUMN IF NOT EXISTS virus_scan_version VARCHAR(20),
ADD COLUMN IF NOT EXISTS virus_signatures TEXT,
ADD COLUMN IF NOT EXISTS scanned_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS scanned_by VARCHAR(100), -- Could be service name like 'clamav-service'

-- Document processing and OCR
ADD COLUMN IF NOT EXISTS ocr_status VARCHAR(20) DEFAULT 'pending' CHECK (ocr_status IN (
    'pending', 'processing', 'completed', 'failed', 'skipped'
)),
ADD COLUMN IF NOT EXISTS ocr_text TEXT,
ADD COLUMN IF NOT EXISTS ocr_confidence DECIMAL(5,2) CHECK (ocr_confidence >= 0 AND ocr_confidence <= 100),
ADD COLUMN IF NOT EXISTS ocr_processed_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS extracted_fields JSONB DEFAULT '{}';

-- Document validation and quality
ALTER TABLE customer_schema.kyc_documents 
ADD COLUMN IF NOT EXISTS validation_status VARCHAR(20) DEFAULT 'pending' CHECK (validation_status IN (
    'pending', 'validating', 'valid', 'invalid', 'warning'
)),
ADD COLUMN IF NOT EXISTS validation_errors JSONB DEFAULT '[]',
ADD COLUMN IF NOT EXISTS validation_warnings JSONB DEFAULT '[]',
ADD COLUMN IF NOT EXISTS is_blurred BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS is_expired BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS is_tampered BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS quality_score DECIMAL(5,2) CHECK (quality_score >= 0 AND quality_score <= 100),
ADD COLUMN IF NOT EXISTS validation_notes TEXT,
ADD COLUMN IF NOT EXISTS validated_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS validated_by UUID REFERENCES customer_schema.users(id);

-- Document expiration and retention
ALTER TABLE customer_schema.kyc_documents 
ADD COLUMN IF NOT EXISTS retention_policy VARCHAR(50) DEFAULT 'kyc_compliance' CHECK (retention_policy IN (
    'kyc_compliance', 'transactional', 'temporary', 'permanent'
)),
ADD COLUMN IF NOT EXISTS retention_days INTEGER DEFAULT 1825, -- 5 years default for compliance
ADD COLUMN IF NOT EXISTS scheduled_deletion_date DATE,
ADD COLUMN IF NOT EXISTS is_archived BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS archive_reason VARCHAR(100);

-- Document access and audit
ALTER TABLE customer_schema.kyc_documents 
ADD COLUMN IF NOT EXISTS access_level VARCHAR(20) DEFAULT 'confidential' CHECK (access_level IN (
    'public', 'internal', 'confidential', 'restricted'
)),
ADD COLUMN IF NOT EXISTS download_count INTEGER DEFAULT 0,
ADD COLUMN IF NOT EXISTS last_downloaded_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS last_downloaded_by UUID REFERENCES customer_schema.users(id),
ADD COLUMN IF NOT EXISTS view_count INTEGER DEFAULT 0,
ADD COLUMN IF NOT EXISTS last_viewed_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS last_viewed_by UUID REFERENCES customer_schema.users(id);

-- Business process tracking
ALTER TABLE customer_schema.kyc_documents 
ADD COLUMN IF NOT EXISTS process_id UUID,
ADD COLUMN IF NOT EXISTS workflow_step VARCHAR(50),
ADD COLUMN IF NOT EXISTS review_cycle INTEGER DEFAULT 1,
ADD COLUMN IF NOT EXISTS is_manual_review_required BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS manual_review_notes TEXT,
ADD COLUMN IF NOT EXISTS manual_reviewed_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS manual_reviewed_by UUID REFERENCES customer_schema.users(id);

-- ========== UPDATE EXISTING URL COLUMNS ==========
-- Add comments to existing columns for clarity
COMMENT ON COLUMN customer_schema.kyc_documents.front_image_url IS 'Legacy front image URL (deprecated - use storage_key)';
COMMENT ON COLUMN customer_schema.kyc_documents.back_image_url IS 'Legacy back image URL (deprecated - use storage_key)';
COMMENT ON COLUMN customer_schema.kyc_documents.selfie_image_url IS 'Legacy selfie image URL (deprecated - use storage_key)';

-- ========== CREATE INDEXES FOR NEW COLUMNS ==========
-- Performance indexes for common queries
CREATE INDEX IF NOT EXISTS idx_kyc_docs_virus_scan_status 
ON customer_schema.kyc_documents(virus_scan_status) 
WHERE virus_scan_status IN ('pending', 'scanning');

CREATE INDEX IF NOT EXISTS idx_kyc_docs_validation_status 
ON customer_schema.kyc_documents(validation_status) 
WHERE validation_status IN ('pending', 'validating');

CREATE INDEX IF NOT EXISTS idx_kyc_docs_ocr_status 
ON customer_schema.kyc_documents(ocr_status) 
WHERE ocr_status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS idx_kyc_docs_storage_provider 
ON customer_schema.kyc_documents(storage_provider, storage_bucket);

CREATE INDEX IF NOT EXISTS idx_kyc_docs_file_size 
ON customer_schema.kyc_documents(file_size_bytes) 
WHERE file_size_bytes IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_kyc_docs_retention 
ON customer_schema.kyc_documents(scheduled_deletion_date) 
WHERE scheduled_deletion_date IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_kyc_docs_archived 
ON customer_schema.kyc_documents(is_archived) 
WHERE is_archived = true;

-- ========== CREATE COMPOSITE INDEXES ==========
CREATE INDEX IF NOT EXISTS idx_kyc_docs_user_status 
ON customer_schema.kyc_documents(user_id, verification_status, virus_scan_status);

CREATE INDEX IF NOT EXISTS idx_kyc_docs_type_status 
ON customer_schema.kyc_documents(document_type, verification_status, validation_status);

-- ========== ADD CONSTRAINTS ==========
-- Ensure at least one storage method exists
ALTER TABLE customer_schema.kyc_documents
ADD CONSTRAINT chk_storage_method CHECK (
    -- Either legacy URL or new storage method must be present
    front_image_url IS NOT NULL OR 
    back_image_url IS NOT NULL OR 
    selfie_image_url IS NOT NULL OR
    storage_key IS NOT NULL
);

-- File size constraints based on document type
ALTER TABLE customer_schema.kyc_documents
ADD CONSTRAINT chk_file_size_by_type CHECK (
    -- Different size limits for different document types
    (document_type IN ('national_id', 'passport', 'drivers_license') AND file_size_bytes <= 10485760) OR -- 10MB for IDs
    (document_type IN ('utility_bill', 'bank_statement', 'proof_of_income') AND file_size_bytes <= 5242880) OR -- 5MB for bills
    (document_type IN ('business_registration', 'tax_certificate') AND file_size_bytes <= 10485760) OR -- 10MB for business docs
    (document_type = 'selfie' AND file_size_bytes <= 5242880) OR -- 5MB for selfies
    file_size_bytes IS NULL
);

-- ========== CREATE HELPER FUNCTIONS ==========
-- Function to calculate document completeness for a user
CREATE OR REPLACE FUNCTION customer_schema.calculate_document_completeness(
    p_user_id UUID,
    p_required_documents JSONB DEFAULT NULL
) RETURNS TABLE (
    total_required INTEGER,
    submitted INTEGER,
    verified INTEGER,
    pending_verification INTEGER,
    completeness_percentage DECIMAL(5,2)
) AS $$
DECLARE
    v_country_code VARCHAR(2);
    v_user_type VARCHAR(20);
    v_kyc_level INTEGER;
    v_documents JSONB;
BEGIN
    -- Get user info
    SELECT country_code, user_type, kyc_level 
    INTO v_country_code, v_user_type, v_kyc_level
    FROM customer_schema.users 
    WHERE id = p_user_id;
    
    -- Get required documents if not provided
    IF p_required_documents IS NULL THEN
        SELECT required_documents INTO v_documents
        FROM customer_schema.kyc_requirements
        WHERE country_code = v_country_code
        AND user_type = v_user_type
        AND kyc_level = v_kyc_level
        AND is_active = true
        LIMIT 1;
    ELSE
        v_documents := p_required_documents;
    END IF;
    
    -- Calculate counts
    RETURN QUERY
    WITH document_counts AS (
        SELECT 
            jsonb_array_length(v_documents) as total_required,
            COUNT(DISTINCT kd.document_type) FILTER (
                WHERE kd.document_type = ANY(SELECT jsonb_array_elements_text(v_documents))
            ) as submitted,
            COUNT(DISTINCT kd.document_type) FILTER (
                WHERE kd.document_type = ANY(SELECT jsonb_array_elements_text(v_documents))
                AND kd.verification_status = 'verified'
                AND kd.virus_scan_status = 'clean'
                AND kd.validation_status = 'valid'
            ) as verified,
            COUNT(DISTINCT kd.document_type) FILTER (
                WHERE kd.document_type = ANY(SELECT jsonb_array_elements_text(v_documents))
                AND kd.verification_status IN ('pending', 'processing')
            ) as pending_verification
        FROM customer_schema.kyc_documents kd
        WHERE kd.user_id = p_user_id
    )
    SELECT 
        total_required,
        submitted,
        verified,
        pending_verification,
        CASE 
            WHEN total_required = 0 THEN 100.00
            ELSE ROUND((verified::DECIMAL / total_required) * 100, 2)
        END as completeness_percentage
    FROM document_counts;
END;
$$ LANGUAGE plpgsql;

-- Function to validate document before verification
CREATE OR REPLACE FUNCTION customer_schema.validate_document_for_verification(
    p_document_id UUID
) RETURNS TABLE (
    is_valid BOOLEAN,
    validation_errors TEXT[],
    validation_warnings TEXT[]
) AS $$
DECLARE
    v_document RECORD;
    v_errors TEXT[] := '{}';
    v_warnings TEXT[] := '{}';
BEGIN
    -- Get document details
    SELECT * INTO v_document
    FROM customer_schema.kyc_documents
    WHERE id = p_document_id;
    
    -- Check if document exists
    IF v_document IS NULL THEN
        v_errors := v_errors || 'Document not found';
        RETURN QUERY SELECT false, v_errors, v_warnings;
        RETURN;
    END IF;
    
    -- Basic validations
    IF v_document.file_size_bytes IS NULL OR v_document.file_size_bytes = 0 THEN
        v_errors := v_errors || 'File size is zero or not recorded';
    END IF;
    
    IF v_document.file_size_bytes > 10485760 THEN -- 10MB
        v_errors := v_errors || 'File size exceeds 10MB limit';
    ELSIF v_document.file_size_bytes > 5242880 THEN -- 5MB
        v_warnings := v_warnings || 'File size is large (>5MB), consider compression';
    END IF;
    
    IF v_document.mime_type IS NULL THEN
        v_warnings := v_warnings || 'MIME type not detected';
    END IF;
    
    IF v_document.virus_scan_status != 'clean' THEN
        v_errors := v_errors || 'Virus scan not completed or failed';
    END IF;
    
    IF v_document.validation_status != 'valid' THEN
        v_errors := v_errors || 'Document validation failed';
    END IF;
    
    IF v_document.expiry_date IS NOT NULL AND v_document.expiry_date < CURRENT_DATE THEN
        v_errors := v_errors || 'Document has expired';
    END IF;
    
    -- Return results
    RETURN QUERY SELECT 
        array_length(v_errors, 1) IS NULL OR array_length(v_errors, 1) = 0,
        v_errors,
        v_warnings;
END;
$$ LANGUAGE plpgsql;

-- ========== CREATE VIEWS ==========
-- View for documents requiring virus scanning
CREATE OR REPLACE VIEW customer_schema.documents_pending_virus_scan AS
SELECT 
    id,
    user_id,
    document_type,
    file_name,
    file_size_bytes,
    mime_type,
    storage_key,
    created_at
FROM customer_schema.kyc_documents
WHERE virus_scan_status IN ('pending', 'scanning')
ORDER BY created_at ASC;

-- View for documents requiring validation
CREATE OR REPLACE VIEW customer_schema.documents_pending_validation AS
SELECT 
    id,
    user_id,
    document_type,
    file_name,
    validation_errors,
    created_at
FROM customer_schema.kyc_documents
WHERE validation_status IN ('pending', 'validating')
AND virus_scan_status = 'clean'
ORDER BY created_at ASC;

-- View for documents scheduled for deletion
CREATE OR REPLACE VIEW customer_schema.documents_scheduled_for_deletion AS
SELECT 
    id,
    user_id,
    document_type,
    file_name,
    scheduled_deletion_date,
    retention_policy,
    created_at
FROM customer_schema.kyc_documents
WHERE scheduled_deletion_date IS NOT NULL
AND scheduled_deletion_date <= CURRENT_DATE + INTERVAL '7 days' -- Next 7 days
ORDER BY scheduled_deletion_date ASC;

-- View for document statistics
CREATE OR REPLACE VIEW customer_schema.document_statistics AS
SELECT 
    storage_provider,
    COUNT(*) as total_documents,
    SUM(file_size_bytes) as total_storage_bytes,
    AVG(file_size_bytes) as avg_file_size_bytes,
    COUNT(*) FILTER (WHERE virus_scan_status = 'clean') as clean_documents,
    COUNT(*) FILTER (WHERE virus_scan_status = 'infected') as infected_documents,
    COUNT(*) FILTER (WHERE validation_status = 'valid') as valid_documents,
    COUNT(*) FILTER (WHERE is_archived = true) as archived_documents
FROM customer_schema.kyc_documents
GROUP BY storage_provider;

-- ========== UPDATE EXISTING DATA ==========
-- Backfill storage_key from existing URL columns where possible
UPDATE customer_schema.kyc_documents
SET 
    storage_key = CASE 
        WHEN front_image_url IS NOT NULL THEN 'legacy/' || id::text || '/front'
        WHEN back_image_url IS NOT NULL THEN 'legacy/' || id::text || '/back'
        WHEN selfie_image_url IS NOT NULL THEN 'legacy/' || id::text || '/selfie'
        ELSE NULL
    END,
    storage_provider = 'local',
    mime_type = 'image/jpeg' -- Assuming legacy images are JPEG
WHERE storage_key IS NULL
AND (front_image_url IS NOT NULL OR back_image_url IS NOT NULL OR selfie_image_url IS NOT NULL);

-- Set default retention dates for existing documents
UPDATE customer_schema.kyc_documents
SET 
    retention_policy = 'kyc_compliance',
    retention_days = 1825,
    scheduled_deletion_date = created_at + INTERVAL '1825 days'
WHERE scheduled_deletion_date IS NULL
AND retention_policy IS NULL;

-- Set virus scan status for existing documents (assume clean if verified)
UPDATE customer_schema.kyc_documents
SET 
    virus_scan_status = CASE 
        WHEN verification_status = 'verified' THEN 'clean'
        ELSE 'pending'
    END,
    scanned_at = CASE 
        WHEN verification_status = 'verified' THEN verified_at
        ELSE NULL
    END
WHERE virus_scan_status IS NULL;

-- Set validation status for existing documents
UPDATE customer_schema.kyc_documents
SET 
    validation_status = CASE 
        WHEN verification_status = 'verified' THEN 'valid'
        WHEN verification_status = 'rejected' THEN 'invalid'
        ELSE 'pending'
    END,
    validated_at = CASE 
        WHEN verification_status = 'verified' THEN verified_at
        WHEN verification_status = 'rejected' THEN verified_at
        ELSE NULL
    END
WHERE validation_status IS NULL;

-- ========== CREATE TRIGGERS ==========
-- Trigger to auto-set file extension from file_name
CREATE OR REPLACE FUNCTION customer_schema.set_file_extension()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.file_name IS NOT NULL AND NEW.file_extension IS NULL THEN
        NEW.file_extension := LOWER(SUBSTRING(NEW.file_name FROM '\.([^\.]+)$'));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_file_extension_trigger
BEFORE INSERT OR UPDATE OF file_name ON customer_schema.kyc_documents
FOR EACH ROW EXECUTE FUNCTION customer_schema.set_file_extension();

-- Trigger to update scheduled deletion date when retention changes
CREATE OR REPLACE FUNCTION customer_schema.update_scheduled_deletion()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.retention_days IS NOT NULL AND NEW.retention_days > 0 THEN
        NEW.scheduled_deletion_date := COALESCE(NEW.verified_at, NEW.created_at) + 
                                      (NEW.retention_days || ' days')::INTERVAL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_scheduled_deletion_trigger
BEFORE INSERT OR UPDATE OF retention_days, verified_at ON customer_schema.kyc_documents
FOR EACH ROW EXECUTE FUNCTION customer_schema.update_scheduled_deletion();

-- ========== ADD COMMENTS FOR DOCUMENTATION ==========
COMMENT ON TABLE customer_schema.kyc_documents IS 'Stores KYC document metadata, file information, and processing status';
COMMENT ON COLUMN customer_schema.kyc_documents.storage_key IS 'Unique identifier for file in storage provider (e.g., S3 key, file path)';
COMMENT ON COLUMN customer_schema.kyc_documents.virus_scan_status IS 'Status of virus/malware scanning process';
COMMENT ON COLUMN customer_schema.kyc_documents.validation_status IS 'Status of document validation (format, quality, authenticity)';
COMMENT ON COLUMN customer_schema.kyc_documents.retention_policy IS 'Policy governing how long document should be retained';

/*
-- ========== DOWN MIGRATION ==========
-- 010_extend_kyc_documents.down.sql

-- Drop triggers
DROP TRIGGER IF EXISTS set_file_extension_trigger ON customer_schema.kyc_documents;
DROP TRIGGER IF EXISTS update_scheduled_deletion_trigger ON customer_schema.kyc_documents;

-- Drop functions
DROP FUNCTION IF EXISTS customer_schema.set_file_extension();
DROP FUNCTION IF EXISTS customer_schema.update_scheduled_deletion();
DROP FUNCTION IF EXISTS customer_schema.validate_document_for_verification(UUID);
DROP FUNCTION IF EXISTS customer_schema.calculate_document_completeness(UUID, JSONB);

-- Drop views
DROP VIEW IF EXISTS customer_schema.document_statistics;
DROP VIEW IF EXISTS customer_schema.documents_scheduled_for_deletion;
DROP VIEW IF EXISTS customer_schema.documents_pending_validation;
DROP VIEW IF EXISTS customer_schema.documents_pending_virus_scan;

-- Drop indexes
DROP INDEX IF EXISTS idx_kyc_docs_user_status;
DROP INDEX IF EXISTS idx_kyc_docs_type_status;
DROP INDEX IF EXISTS idx_kyc_docs_archived;
DROP INDEX IF EXISTS idx_kyc_docs_retention;
DROP INDEX IF EXISTS idx_kyc_docs_file_size;
DROP INDEX IF EXISTS idx_kyc_docs_storage_provider;
DROP INDEX IF EXISTS idx_kyc_docs_ocr_status;
DROP INDEX IF EXISTS idx_kyc_docs_validation_status;
DROP INDEX IF EXISTS idx_kyc_docs_virus_scan_status;

-- Drop constraints
ALTER TABLE customer_schema.kyc_documents 
DROP CONSTRAINT IF EXISTS chk_file_size_by_type;

ALTER TABLE customer_schema.kyc_documents 
DROP CONSTRAINT IF EXISTS chk_storage_method;

-- Remove comments
COMMENT ON COLUMN customer_schema.kyc_documents.front_image_url IS NULL;
COMMENT ON COLUMN customer_schema.kyc_documents.back_image_url IS NULL;
COMMENT ON COLUMN customer_schema.kyc_documents.selfie_image_url IS NULL;
COMMENT ON TABLE customer_schema.kyc_documents IS NULL;

-- Remove all newly added columns (grouped by category for safety)
ALTER TABLE customer_schema.kyc_documents 
DROP COLUMN IF EXISTS file_name,
DROP COLUMN IF EXISTS file_size_bytes,
DROP COLUMN IF EXISTS mime_type,
DROP COLUMN IF EXISTS file_extension,
DROP COLUMN IF EXISTS storage_provider,
DROP COLUMN IF EXISTS storage_bucket,
DROP COLUMN IF EXISTS storage_key,
DROP COLUMN IF EXISTS storage_region,
DROP COLUMN IF EXISTS public_url,
DROP COLUMN IF EXISTS internal_path,
DROP COLUMN IF EXISTS file_hash_sha256,
DROP COLUMN IF EXISTS file_hash_md5,
DROP COLUMN IF EXISTS checksum_algorithm,
DROP COLUMN IF EXISTS file_integrity_verified,
DROP COLUMN IF EXISTS integrity_verified_at,
DROP COLUMN IF EXISTS integrity_verified_by,
DROP COLUMN IF EXISTS virus_scan_status,
DROP COLUMN IF EXISTS virus_scan_result,
DROP COLUMN IF EXISTS virus_scan_engine,
DROP COLUMN IF EXISTS virus_scan_version,
DROP COLUMN IF EXISTS virus_signatures,
DROP COLUMN IF EXISTS scanned_at,
DROP COLUMN IF EXISTS scanned_by,
DROP COLUMN IF EXISTS ocr_status,
DROP COLUMN IF EXISTS ocr_text,
DROP COLUMN IF EXISTS ocr_confidence,
DROP COLUMN IF EXISTS ocr_processed_at,
DROP COLUMN IF EXISTS extracted_fields,
DROP COLUMN IF EXISTS validation_status,
DROP COLUMN IF EXISTS validation_errors,
DROP COLUMN IF EXISTS validation_warnings,
DROP COLUMN IF EXISTS is_blurred,
DROP COLUMN IF EXISTS is_expired,
DROP COLUMN IF EXISTS is_tampered,
DROP COLUMN IF EXISTS quality_score,
DROP COLUMN IF EXISTS validation_notes,
DROP COLUMN IF EXISTS validated_at,
DROP COLUMN IF EXISTS validated_by,
DROP COLUMN IF EXISTS retention_policy,
DROP COLUMN IF EXISTS retention_days,
DROP COLUMN IF EXISTS scheduled_deletion_date,
DROP COLUMN IF EXISTS is_archived,
DROP COLUMN IF EXISTS archived_at,
DROP COLUMN IF EXISTS archive_reason,
DROP COLUMN IF EXISTS access_level,
DROP COLUMN IF EXISTS download_count,
DROP COLUMN IF EXISTS last_downloaded_at,
DROP COLUMN IF EXISTS last_downloaded_by,
DROP COLUMN IF EXISTS view_count,
DROP COLUMN IF EXISTS last_viewed_at,
DROP COLUMN IF EXISTS last_viewed_by,
DROP COLUMN IF EXISTS process_id,
DROP COLUMN IF EXISTS workflow_step,
DROP COLUMN IF EXISTS review_cycle,
DROP COLUMN IF EXISTS is_manual_review_required,
DROP COLUMN IF EXISTS manual_review_notes,
DROP COLUMN IF EXISTS manual_reviewed_at,
DROP COLUMN IF EXISTS manual_reviewed_by;
*/