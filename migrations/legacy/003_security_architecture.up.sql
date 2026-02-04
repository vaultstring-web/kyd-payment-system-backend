-- 003_security_architecture.up.sql

-- 1. SCHEMA ISOLATION PATTERN
CREATE SCHEMA IF NOT EXISTS admin_schema;
CREATE SCHEMA IF NOT EXISTS customer_schema;
CREATE SCHEMA IF NOT EXISTS audit_schema;

-- Move existing tables to customer_schema (Business Data)
ALTER TABLE IF EXISTS public.users SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.wallets SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.transactions SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.ledger_entries SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.kyc_documents SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.settlements SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.blockchain_transactions SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.exchange_rates SET SCHEMA customer_schema;
ALTER TABLE IF EXISTS public.forex_rates SET SCHEMA customer_schema;

-- Move audit logs to admin_schema (System Logs)
ALTER TABLE IF EXISTS public.audit_logs SET SCHEMA admin_schema;

-- 2. PERMISSION HIERARCHY & ROLES
-- Note: In a real production env, these roles might be created by IaC. 
-- We create them here if they don't exist.
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'kyd_admin') THEN
        CREATE ROLE kyd_admin WITH LOGIN PASSWORD 'admin_secure_pass';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'kyd_system') THEN
        CREATE ROLE kyd_system WITH LOGIN PASSWORD 'system_secure_pass';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'kyd_auditor') THEN
        CREATE ROLE kyd_auditor WITH LOGIN PASSWORD 'auditor_secure_pass';
    END IF;
END
$$;

-- Grant Schema Usages
GRANT USAGE ON SCHEMA customer_schema TO kyd_system;
GRANT USAGE ON SCHEMA admin_schema TO kyd_admin;
GRANT USAGE ON SCHEMA audit_schema TO kyd_auditor;
GRANT USAGE ON SCHEMA audit_schema TO kyd_system;

GRANT CREATE ON SCHEMA customer_schema TO kyd_system;
GRANT CREATE ON SCHEMA admin_schema TO kyd_admin;

-- Grant Table Permissions (System User)
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA customer_schema TO kyd_system;
-- NO DELETE for System User
REVOKE DELETE ON ALL TABLES IN SCHEMA customer_schema FROM kyd_system;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA customer_schema TO kyd_system;

-- Grant Table Permissions (Admin)
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA customer_schema TO kyd_admin;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA admin_schema TO kyd_admin;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA audit_schema TO kyd_admin;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA customer_schema TO kyd_admin;

-- Grant Table Permissions (Auditor)
GRANT SELECT ON ALL TABLES IN SCHEMA admin_schema TO kyd_auditor;
GRANT SELECT ON ALL TABLES IN SCHEMA audit_schema TO kyd_auditor;
GRANT DELETE ON ALL TABLES IN SCHEMA audit_schema TO kyd_auditor; -- Deletions only on audit records

-- 3. COMPREHENSIVE AUDIT TRAIL SYSTEM (Trigger-based)
-- Create Audit Table in audit_schema for detailed trail
CREATE TABLE IF NOT EXISTS audit_schema.data_changes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    schema_name TEXT NOT NULL,
    table_name TEXT NOT NULL,
    operation TEXT NOT NULL, -- INSERT, UPDATE, DELETE
    record_id UUID,
    old_values JSONB,
    new_values JSONB,
    changed_by TEXT, -- app.current_user_id
    client_ip TEXT, -- app.client_ip
    user_agent TEXT, -- app.user_agent
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_audit_changes_table ON audit_schema.data_changes(schema_name, table_name);
CREATE INDEX idx_audit_changes_created ON audit_schema.data_changes(created_at);

-- Audit Trigger Function
CREATE OR REPLACE FUNCTION audit_schema.log_data_change()
RETURNS TRIGGER AS $$
DECLARE
    current_user_id TEXT;
    current_ip TEXT;
    current_ua TEXT;
BEGIN
    -- Try to get context from session variables
    BEGIN
        current_user_id := current_setting('app.current_user_id');
    EXCEPTION WHEN OTHERS THEN
        current_user_id := 'system';
    END;
    
    BEGIN
        current_ip := current_setting('app.client_ip');
    EXCEPTION WHEN OTHERS THEN
        current_ip := NULL;
    END;

    IF (TG_OP = 'INSERT') THEN
        INSERT INTO audit_schema.data_changes (
            schema_name, table_name, operation, record_id, new_values, changed_by, client_ip
        ) VALUES (
            TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP, NEW.id, row_to_json(NEW)::jsonb, current_user_id, current_ip
        );
        RETURN NEW;
    ELSIF (TG_OP = 'UPDATE') THEN
        INSERT INTO audit_schema.data_changes (
            schema_name, table_name, operation, record_id, old_values, new_values, changed_by, client_ip
        ) VALUES (
            TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP, NEW.id, row_to_json(OLD)::jsonb, row_to_json(NEW)::jsonb, current_user_id, current_ip
        );
        RETURN NEW;
    ELSIF (TG_OP = 'DELETE') THEN
        INSERT INTO audit_schema.data_changes (
            schema_name, table_name, operation, record_id, old_values, changed_by, client_ip
        ) VALUES (
            TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP, OLD.id, row_to_json(OLD)::jsonb, current_user_id, current_ip
        );
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Apply Audit Triggers to Sensitive Tables
CREATE TRIGGER audit_users_change
AFTER INSERT OR UPDATE OR DELETE ON customer_schema.users
FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();

CREATE TRIGGER audit_wallets_change
AFTER INSERT OR UPDATE OR DELETE ON customer_schema.wallets
FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();

CREATE TRIGGER audit_transactions_change
AFTER INSERT OR UPDATE OR DELETE ON customer_schema.transactions
FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();

-- 4. ROW LEVEL SECURITY (RLS)
ALTER TABLE customer_schema.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_schema.wallets ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_schema.transactions ENABLE ROW LEVEL SECURITY;

-- Policies
-- Admin can do everything
CREATE POLICY admin_all ON customer_schema.users
    FOR ALL
    TO kyd_admin
    USING (true);

-- System User (The App) usually needs to see everything to function, 
-- UNLESS we are strictly passing user context.
-- For now, we allow System User to see everything to prevent breaking the app,
-- BUT we add a policy that uses app.current_user_id if set.

-- Policy: Users can only see their own data
CREATE POLICY user_isolation ON customer_schema.users
    FOR ALL
    USING (
        id::text = current_setting('app.current_user_id', true)
        OR current_user = 'kyd_system' -- Allow system user (app) to bypass for now
        OR current_user = 'postgres'
    );

CREATE POLICY wallet_isolation ON customer_schema.wallets
    FOR ALL
    USING (
        user_id::text = current_setting('app.current_user_id', true)
        OR current_user = 'kyd_system'
        OR current_user = 'postgres'
    );

CREATE POLICY transaction_isolation ON customer_schema.transactions
    FOR ALL
    USING (
        (sender_id::text = current_setting('app.current_user_id', true) OR receiver_id::text = current_setting('app.current_user_id', true))
        OR current_user = 'kyd_system'
        OR current_user = 'postgres'
    );

-- 5. VIEW-BASED SECURITY LAYER
-- Create Secure Views in public schema (so they are easily accessible)
-- or in a dedicated 'api' schema. Let's use 'public' as a facade.

CREATE OR REPLACE VIEW public.secure_users AS
SELECT 
    id, 
    email, 
    first_name, 
    last_name, 
    -- Masked Phone
    CONCAT('***-***-', RIGHT(phone, 4)) as phone,
    kyc_status,
    kyc_level,
    created_at
FROM customer_schema.users;

CREATE OR REPLACE VIEW public.secure_transactions AS
SELECT
    t.id,
    t.amount,
    t.currency,
    t.status,
    t.transaction_type as type,
    t.created_at,
    -- Masked User ID for non-owners (logic depends on viewer)
    t.sender_id,
    t.receiver_id
FROM customer_schema.transactions t;

-- Grant access to views
GRANT SELECT ON public.secure_users TO kyd_system;
GRANT SELECT ON public.secure_transactions TO kyd_system;
