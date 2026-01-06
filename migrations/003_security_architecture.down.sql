-- 003_security_architecture.down.sql

-- Revert Views
DROP VIEW IF EXISTS public.secure_users;
DROP VIEW IF EXISTS public.secure_transactions;

-- Disable RLS
ALTER TABLE customer_schema.users DISABLE ROW LEVEL SECURITY;
ALTER TABLE customer_schema.wallets DISABLE ROW LEVEL SECURITY;
ALTER TABLE customer_schema.transactions DISABLE ROW LEVEL SECURITY;

-- Drop Triggers
DROP TRIGGER IF EXISTS audit_users_change ON customer_schema.users;
DROP TRIGGER IF EXISTS audit_wallets_change ON customer_schema.wallets;
DROP TRIGGER IF EXISTS audit_transactions_change ON customer_schema.transactions;

-- Drop Audit Function
DROP FUNCTION IF EXISTS audit_schema.log_data_change();

-- Drop Audit Table
DROP TABLE IF EXISTS audit_schema.data_changes;

-- Revoke Permissions
REVOKE ALL PRIVILEGES ON SCHEMA customer_schema FROM kyd_system;
REVOKE ALL PRIVILEGES ON SCHEMA admin_schema FROM kyd_admin;
REVOKE ALL PRIVILEGES ON SCHEMA audit_schema FROM kyd_auditor;

-- Drop Roles
DROP ROLE IF EXISTS kyd_admin;
DROP ROLE IF EXISTS kyd_system;
DROP ROLE IF EXISTS kyd_auditor;

-- Move Tables back to public
ALTER TABLE customer_schema.users SET SCHEMA public;
ALTER TABLE customer_schema.wallets SET SCHEMA public;
ALTER TABLE customer_schema.transactions SET SCHEMA public;
ALTER TABLE customer_schema.ledger_entries SET SCHEMA public;
ALTER TABLE customer_schema.kyc_documents SET SCHEMA public;
ALTER TABLE customer_schema.settlements SET SCHEMA public;
ALTER TABLE customer_schema.blockchain_transactions SET SCHEMA public;
ALTER TABLE customer_schema.exchange_rates SET SCHEMA public;
ALTER TABLE customer_schema.forex_rates SET SCHEMA public;

ALTER TABLE admin_schema.audit_logs SET SCHEMA public;

-- Drop Schemas
DROP SCHEMA IF EXISTS admin_schema;
DROP SCHEMA IF EXISTS customer_schema;
DROP SCHEMA IF EXISTS audit_schema;
