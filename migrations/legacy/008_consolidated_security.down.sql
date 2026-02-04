-- 010_consolidated_security.down.sql
-- This migration reverts the consolidation of differential privacy and union attack prevention measures.

-- =====================================================
-- REVERT UNION ATTACK PREVENTION
-- =====================================================

-- Grant direct table access back to the kyd_system role
GRANT SELECT ON users TO kyd_system;
GRANT SELECT ON wallets TO kyd_system;
GRANT SELECT ON transactions TO kyd_system;

-- Drop the views
DROP VIEW IF EXISTS safe_schema.users_view;
DROP VIEW IF EXISTS safe_schema.wallets_view;
DROP VIEW IF EXISTS safe_schema.transactions_view;

-- Drop the schema
DROP SCHEMA IF EXISTS safe_schema;

-- =====================================================
-- REVERT PRIVACY BUDGET ADMINISTRATION
-- =====================================================

-- Drop the function for adjusting user budgets
DROP FUNCTION IF EXISTS privacy_schema.adjust_user_budget(VARCHAR, DECIMAL, DECIMAL, DECIMAL, VARCHAR);

-- Revoke permissions
REVOKE SELECT ON privacy_schema.user_statistics FROM kyd_admin;
REVOKE SELECT ON privacy_schema.transaction_statistics FROM kyd_admin;
REVOKE SELECT ON privacy_schema.wallet_statistics FROM kyd_admin;

REVOKE USAGE ON SCHEMA privacy_schema FROM kyd_system;
REVOKE SELECT ON ALL TABLES IN SCHEMA privacy_schema FROM kyd_system;
REVOKE INSERT ON privacy_schema.query_audit_log FROM kyd_system;
REVOKE UPDATE ON privacy_schema.user_privacy_budgets FROM kyd_system;

-- =====================================================
-- REVERT PRIVACY AUDIT FUNCTIONS
-- =====================================================

-- Drop privacy audit functions
DROP FUNCTION IF EXISTS privacy_schema.get_user_audit_report(VARCHAR);
DROP FUNCTION IF EXISTS privacy_schema.get_system_privacy_stats();

-- =====================================================
-- REVERT PRIVACY PROTECTION FUNCTIONS
-- =====================================================

-- Drop safe query functions
DROP FUNCTION IF EXISTS privacy_schema.safe_user_count(VARCHAR, DECIMAL, TEXT);
DROP FUNCTION IF EXISTS privacy_schema.safe_transaction_stats(VARCHAR, DECIMAL, DATE, DATE);

-- =====================================================
-- REVERT PRIVACY-ENHANCED VIEWS
-- =====================================================

-- Drop privacy-enhanced views
DROP VIEW IF EXISTS privacy_schema.user_statistics;
DROP VIEW IF EXISTS privacy_schema.transaction_statistics;
DROP VIEW IF EXISTS privacy_schema.wallet_statistics;

-- =====================================================
-- REVERT UTILITY FUNCTIONS
-- =====================================================

-- Drop utility functions
DROP FUNCTION IF EXISTS privacy_schema.get_budget_status(VARCHAR);
DROP FUNCTION IF EXISTS privacy_schema.reset_user_budget(VARCHAR);

-- =====================================================
-- REVERT PRIVACY-AWARE AGGREGATE FUNCTIONS
-- =====================================================

-- Drop noisy aggregate functions
DROP FUNCTION IF EXISTS privacy_schema.noisy_count(VARCHAR, VARCHAR, DECIMAL, TEXT);
DROP FUNCTION IF EXISTS privacy_schema.noisy_sum(VARCHAR, VARCHAR, VARCHAR, DECIMAL, DECIMAL, TEXT);
DROP FUNCTION IF EXISTS privacy_schema.noisy_avg(VARCHAR, VARCHAR, VARCHAR, DECIMAL, DECIMAL, TEXT);
DROP FUNCTION IF EXISTS privacy_schema.noisy_stddev(VARCHAR, VARCHAR, VARCHAR, DECIMAL, DECIMAL, TEXT);

-- =====================================================
-- REVERT PRIVACY BUDGET MANAGEMENT FUNCTIONS
-- =====================================================

-- Drop budget management functions
DROP FUNCTION IF EXISTS privacy_schema.consume_budget(VARCHAR, DECIMAL, DECIMAL, VARCHAR, VARCHAR);
DROP FUNCTION IF EXISTS privacy_schema.check_budget(VARCHAR, DECIMAL, DECIMAL);

-- =====================================================
-- REVERT NOISE GENERATION FUNCTIONS
-- =====================================================

-- Drop noise generation functions
DROP FUNCTION IF EXISTS privacy_schema.laplace_noise(DOUBLE PRECISION);
DROP FUNCTION IF EXISTS privacy_schema.geometric_noise(DOUBLE PRECISION);
DROP FUNCTION IF EXISTS privacy_schema.gaussian_noise(DOUBLE PRECISION, DOUBLE PRECISION, DOUBLE PRECISION);

-- =====================================================
-- REVERT PRIVACY BUDGET MANAGEMENT TABLES
-- =====================================================

-- Drop audit log table
DROP TABLE IF EXISTS privacy_schema.query_audit_log;

-- Drop user privacy budgets table
DROP TABLE IF EXISTS privacy_schema.user_privacy_budgets;

-- =====================================================
-- REVERT PRIVACY SCHEMA
-- =====================================================

-- Drop the privacy schema
DROP SCHEMA IF EXISTS privacy_schema;
