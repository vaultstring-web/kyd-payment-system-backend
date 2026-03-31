-- 001_consolidated_schema.up.sql
-- Consolidated Schema for VaultString Payment System
-- Includes: Core Tables, Security Architecture, Audit Logging, Ledger, RLS, and Notifications

-- ==========================================
-- 1. EXTENSIONS & SCHEMAS
-- ==========================================
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE SCHEMA IF NOT EXISTS admin_schema;
CREATE SCHEMA IF NOT EXISTS customer_schema;
CREATE SCHEMA IF NOT EXISTS audit_schema;
CREATE SCHEMA IF NOT EXISTS privacy_schema; -- Added for Differential Privacy

-- ==========================================
-- 2. CORE TABLES (Customer Schema)
-- ==========================================

-- Users
CREATE TABLE IF NOT EXISTS customer_schema.users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    email_hash VARCHAR(255), -- Blind Index for searchable encryption
    phone VARCHAR(255) UNIQUE,
    phone_hash VARCHAR(255), -- Blind Index for phone
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(255),
    last_name VARCHAR(255),
    user_type VARCHAR(20) NOT NULL CHECK (user_type IN ('individual', 'merchant', 'agent', 'admin')),
    kyc_level SMALLINT DEFAULT 0,
    kyc_status VARCHAR(20) DEFAULT 'pending' CHECK (kyc_status IN ('pending', 'processing', 'verified', 'rejected')),
    user_status VARCHAR(20) DEFAULT 'active' CHECK (user_status IN ('active', 'suspended', 'blocked', 'deleted')),
    country_code VARCHAR(2) NOT NULL,
    date_of_birth DATE,
    business_name VARCHAR(255),
    business_registration VARCHAR(100),
    risk_score DECIMAL(5,2) DEFAULT 0.00,
    is_active BOOLEAN DEFAULT TRUE,
    email_verified BOOLEAN DEFAULT FALSE,
    totp_secret VARCHAR(255),
    is_totp_enabled BOOLEAN DEFAULT FALSE,
    failed_login_attempts INTEGER DEFAULT 0,
    locked_until TIMESTAMPTZ,
    last_login TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON customer_schema.users(email);
CREATE INDEX IF NOT EXISTS idx_users_email_blind_index ON customer_schema.users(email_hash);
CREATE INDEX IF NOT EXISTS idx_users_phone ON customer_schema.users(phone);
CREATE INDEX IF NOT EXISTS idx_users_kyc_status ON customer_schema.users(kyc_status);
CREATE INDEX IF NOT EXISTS idx_users_country ON customer_schema.users(country_code);

-- Wallets
CREATE TABLE IF NOT EXISTS customer_schema.wallets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES customer_schema.users(id) ON DELETE CASCADE,
    wallet_address VARCHAR(255) UNIQUE,
    currency VARCHAR(3) NOT NULL CHECK (currency IN ('MWK', 'CNY', 'USD', 'EUR')),
    available_balance DECIMAL(20,2) DEFAULT 0.00 CHECK (available_balance >= 0),
    ledger_balance DECIMAL(20,2) DEFAULT 0.00 CHECK (ledger_balance >= 0),
    reserved_balance DECIMAL(20,2) DEFAULT 0.00 CHECK (reserved_balance >= 0),
    status VARCHAR(20) DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'closed')),
    last_transaction_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, currency)
);

CREATE INDEX IF NOT EXISTS idx_wallets_user ON customer_schema.wallets(user_id);
CREATE INDEX IF NOT EXISTS idx_wallets_currency ON customer_schema.wallets(currency);
CREATE INDEX IF NOT EXISTS idx_wallets_status ON customer_schema.wallets(status);

-- User Devices (Added from legacy 011)
CREATE TABLE IF NOT EXISTS customer_schema.user_devices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES customer_schema.users(id) ON DELETE CASCADE,
    device_hash VARCHAR(255) NOT NULL,
    device_name VARCHAR(255),
    country_code VARCHAR(2),
    ip_address VARCHAR(45),
    is_trusted BOOLEAN DEFAULT TRUE,
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, device_hash)
);

CREATE INDEX IF NOT EXISTS idx_user_devices_user_id ON customer_schema.user_devices(user_id);

-- Transactions
CREATE TABLE IF NOT EXISTS customer_schema.transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    reference VARCHAR(100) UNIQUE NOT NULL,
    sender_id UUID NOT NULL REFERENCES customer_schema.users(id),
    receiver_id UUID NOT NULL REFERENCES customer_schema.users(id),
    sender_wallet_id UUID NOT NULL REFERENCES customer_schema.wallets(id),
    receiver_wallet_id UUID NOT NULL REFERENCES customer_schema.wallets(id),
    amount DECIMAL(20,2) NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL,
    exchange_rate DECIMAL(12,6) NOT NULL,
    converted_amount DECIMAL(20,2) NOT NULL,
    converted_currency VARCHAR(3) NOT NULL,
    fee_amount DECIMAL(20,2) DEFAULT 0.00,
    fee_currency VARCHAR(3),
    net_amount DECIMAL(20,2) NOT NULL,
    status VARCHAR(20) DEFAULT 'pending' CHECK (status IN (
        'pending', 'processing', 'reserved', 'settling', 
        'completed', 'failed', 'cancelled', 'refunded', 'disputed', 'reversed'
    )),
    status_reason TEXT,
    transaction_type VARCHAR(50) NOT NULL CHECK (transaction_type IN (
        'payment', 'transfer', 'withdrawal', 'deposit', 
        'refund', 'reversal', 'settlement'
    )),
    channel VARCHAR(20) CHECK (channel IN ('mobile', 'web', 'pos', 'api', 'ussd')),
    category VARCHAR(50),
    description TEXT,
    metadata JSONB DEFAULT '{}',
    blockchain_tx_hash VARCHAR(255),
    settlement_id UUID,
    initiated_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tx_sender ON customer_schema.transactions(sender_id);
CREATE INDEX IF NOT EXISTS idx_tx_receiver ON customer_schema.transactions(receiver_id);
CREATE INDEX IF NOT EXISTS idx_tx_status ON customer_schema.transactions(status);
CREATE INDEX IF NOT EXISTS idx_tx_created ON customer_schema.transactions(created_at);
CREATE INDEX IF NOT EXISTS idx_tx_reference ON customer_schema.transactions(reference);

-- Notifications (Added for Notification Center)
CREATE TABLE IF NOT EXISTS customer_schema.notifications (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES customer_schema.users(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL CHECK (type IN ('payment_received', 'payment_sent', 'security_alert', 'system_update', 'promo')),
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    data JSONB DEFAULT '{}', -- Stores transaction_id, etc.
    is_read BOOLEAN DEFAULT FALSE,
    is_archived BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user ON customer_schema.notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON customer_schema.notifications(user_id) WHERE is_read = FALSE;

CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON customer_schema.notifications(created_at);

-- Exchange Rates
CREATE TABLE IF NOT EXISTS customer_schema.exchange_rates (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    base_currency VARCHAR(3) NOT NULL,
    target_currency VARCHAR(3) NOT NULL,
    rate DECIMAL(12,6) NOT NULL,
    buy_rate DECIMAL(12,6),
    sell_rate DECIMAL(12,6),
    source VARCHAR(50) NOT NULL,
    provider VARCHAR(100),
    is_interbank BOOLEAN DEFAULT FALSE,
    spread DECIMAL(5,4) DEFAULT 0.015,
    valid_from TIMESTAMPTZ NOT NULL,
    valid_to TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_exchange_rates_base_currency ON customer_schema.exchange_rates(base_currency);
CREATE INDEX IF NOT EXISTS idx_exchange_rates_target_currency ON customer_schema.exchange_rates(target_currency);
CREATE INDEX IF NOT EXISTS idx_exchange_rates_updated_at ON customer_schema.exchange_rates(updated_at);

-- Settlements
CREATE TABLE IF NOT EXISTS customer_schema.settlements (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    batch_reference VARCHAR(100) UNIQUE NOT NULL,
    network VARCHAR(20) NOT NULL CHECK (network IN ('stellar', 'ripple', 'ethereum', 'bank_transfer')),
    transaction_hash VARCHAR(255) UNIQUE,
    source_account VARCHAR(255),
    destination_account VARCHAR(255),
    total_amount DECIMAL(20,2) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    fee_amount DECIMAL(20,2) DEFAULT 0.00,
    fee_currency VARCHAR(3),
    status VARCHAR(20) DEFAULT 'pending',
    submission_count INTEGER DEFAULT 0,
    last_submitted_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    reconciliation_id UUID,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_settlements_status ON customer_schema.settlements(status);
CREATE INDEX IF NOT EXISTS idx_settlements_network ON customer_schema.settlements(network);
CREATE INDEX IF NOT EXISTS idx_settlements_created_at ON customer_schema.settlements(created_at);

-- Blockchain Transactions
CREATE TABLE IF NOT EXISTS customer_schema.blockchain_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    settlement_id UUID REFERENCES customer_schema.settlements(id),
    network VARCHAR(20) NOT NULL,
    tx_hash VARCHAR(255) UNIQUE NOT NULL,
    from_address VARCHAR(255) NOT NULL,
    to_address VARCHAR(255) NOT NULL,
    amount VARCHAR(100) NOT NULL,
    asset_type VARCHAR(50),
    asset_code VARCHAR(10),
    asset_issuer VARCHAR(255),
    fee_paid VARCHAR(50),
    memo VARCHAR(255),
    ledger_index BIGINT,
    confirmed BOOLEAN DEFAULT FALSE,
    confirmation_count INTEGER DEFAULT 0,
    block_number BIGINT,
    block_hash VARCHAR(255),
    raw_transaction JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- KYC Documents
CREATE TABLE IF NOT EXISTS customer_schema.kyc_documents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES customer_schema.users(id) ON DELETE CASCADE,
    document_type VARCHAR(50) NOT NULL,
    document_number VARCHAR(100),
    issuing_country VARCHAR(2),
    issue_date DATE,
    expiry_date DATE,
    front_image_url VARCHAR(500),
    back_image_url VARCHAR(500),
    selfie_image_url VARCHAR(500),
    verification_status VARCHAR(20) DEFAULT 'pending',
    verification_notes TEXT,
    verified_by UUID REFERENCES customer_schema.users(id),
    verified_at TIMESTAMPTZ,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, document_type, document_number)
);

-- Transaction Ledger (Immutable Hash Chain)
CREATE TABLE IF NOT EXISTS customer_schema.transaction_ledger (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id UUID NOT NULL REFERENCES customer_schema.transactions(id),
    event_type VARCHAR(50) NOT NULL,
    amount DECIMAL(20,2),
    currency VARCHAR(3),
    status VARCHAR(20),
    previous_hash VARCHAR(64) UNIQUE, -- SHA-256
    hash VARCHAR(64) NOT NULL, -- SHA-256
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_tx ON customer_schema.transaction_ledger(transaction_id);
CREATE INDEX IF NOT EXISTS idx_ledger_hash ON customer_schema.transaction_ledger(hash);

-- ==========================================
-- 3. AUDIT & LOGGING (Admin Schema)
-- ==========================================

-- Audit Logs
CREATE TABLE IF NOT EXISTS admin_schema.audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES customer_schema.users(id),
    action VARCHAR(255) NOT NULL,
    entity_type VARCHAR(50),
    entity_id UUID,
    old_values JSONB,
    new_values JSONB,
    ip_address VARCHAR(45),
    user_agent TEXT,
    request_id VARCHAR(100),
    status_code INTEGER,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_user ON admin_schema.audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_action ON admin_schema.audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_created ON admin_schema.audit_logs(created_at);

-- Security Events (Threat Monitoring)
CREATE TABLE IF NOT EXISTS admin_schema.security_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_type VARCHAR(50) NOT NULL CHECK (event_type IN (
        'brute_force_attempt', 'suspicious_ip', 'velocity_limit_exceeded', 
        'admin_login_failed', 'multiple_failed_kyc', 'blacklisted_device_detected'
    )),
    severity VARCHAR(20) NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    user_id UUID REFERENCES customer_schema.users(id),
    ip_address VARCHAR(45),
    details JSONB DEFAULT '{}',
    status VARCHAR(20) DEFAULT 'open' CHECK (status IN ('open', 'investigating', 'resolved', 'false_positive')),
    resolved_by UUID, -- Refers to an admin user (if we had an admin users table, currently just storing UUID)
    created_at TIMESTAMPTZ DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_security_events_type ON admin_schema.security_events(event_type);
CREATE INDEX IF NOT EXISTS idx_security_events_severity ON admin_schema.security_events(severity);
CREATE INDEX IF NOT EXISTS idx_security_events_status ON admin_schema.security_events(status);

-- Blocklist
CREATE TABLE IF NOT EXISTS admin_schema.blocklist (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    type VARCHAR(20) NOT NULL CHECK (type IN ('ip', 'email', 'device', 'wallet')),
    value VARCHAR(255) NOT NULL,
    reason TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    created_by UUID,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_blocklist_value ON admin_schema.blocklist(value);
CREATE INDEX IF NOT EXISTS idx_blocklist_type ON admin_schema.blocklist(type);

-- System Health Metrics
CREATE TABLE IF NOT EXISTS admin_schema.system_health_snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    metric VARCHAR(50) NOT NULL,
    value VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('healthy', 'warning', 'critical')),
    change VARCHAR(20),
    recorded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE OR REPLACE VIEW admin_schema.system_health_metrics AS
SELECT DISTINCT ON (metric)
    metric,
    value,
    status,
    change,
    recorded_at as timestamp
FROM admin_schema.system_health_snapshots
ORDER BY metric, recorded_at DESC;

-- Data Changes (Low-level DB Audit)
CREATE TABLE IF NOT EXISTS audit_schema.data_changes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    schema_name TEXT NOT NULL,
    table_name TEXT NOT NULL,
    operation TEXT NOT NULL,
    record_id UUID,
    old_values JSONB,
    new_values JSONB,
    changed_by TEXT,
    client_ip TEXT,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_changes_table ON audit_schema.data_changes(schema_name, table_name);

-- ==========================================
-- 4. PRIVACY SCHEMA (Differential Privacy)
-- ==========================================

-- User privacy budget tracking
CREATE TABLE IF NOT EXISTS privacy_schema.user_privacy_budgets (
    user_id VARCHAR(255) PRIMARY KEY,
    daily_epsilon DECIMAL(10,6) DEFAULT 1.0,
    weekly_epsilon DECIMAL(10,6) DEFAULT 3.0,
    monthly_epsilon DECIMAL(10,6) DEFAULT 10.0,
    consumed_daily DECIMAL(10,6) DEFAULT 0.0,
    consumed_weekly DECIMAL(10,6) DEFAULT 0.0,
    consumed_monthly DECIMAL(10,6) DEFAULT 0.0,
    last_reset TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Query history for audit
CREATE TABLE IF NOT EXISTS privacy_schema.query_audit_log (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    query_type VARCHAR(50) NOT NULL,
    table_name VARCHAR(255) NOT NULL,
    epsilon_consumed DECIMAL(10,6) NOT NULL,
    delta_consumed DECIMAL(10,6) DEFAULT 0.00001,
    sensitivity DECIMAL(10,6) NOT NULL,
    noise_mechanism VARCHAR(20) NOT NULL,
    original_result DECIMAL(20,6),
    noisy_result DECIMAL(20,6),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_query_audit_user ON privacy_schema.query_audit_log(user_id);

-- ==========================================
-- 5. TRIGGERS & FUNCTIONS
-- ==========================================

-- Update Timestamp Function
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Audit Trigger Function (Security Definer for SQL Injection Prevention)
CREATE OR REPLACE FUNCTION audit_schema.log_data_change()
RETURNS TRIGGER AS $$
DECLARE
    current_user_id TEXT;
    current_ip TEXT;
BEGIN
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

-- Wallet Currency Enforcement
CREATE OR REPLACE FUNCTION enforce_wallet_currency() RETURNS trigger AS $$
DECLARE cc VARCHAR(2);
BEGIN
  SELECT country_code INTO cc FROM customer_schema.users WHERE id = NEW.user_id;
  IF cc = 'CN' AND NEW.currency <> 'CNY' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  ELSIF cc = 'MW' AND NEW.currency <> 'MWK' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply Triggers
DROP TRIGGER IF EXISTS update_users_updated_at ON customer_schema.users;
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON customer_schema.users FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
DROP TRIGGER IF EXISTS update_wallets_updated_at ON customer_schema.wallets;
CREATE TRIGGER update_wallets_updated_at BEFORE UPDATE ON customer_schema.wallets FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
DROP TRIGGER IF EXISTS update_transactions_updated_at ON customer_schema.transactions;
CREATE TRIGGER update_transactions_updated_at BEFORE UPDATE ON customer_schema.transactions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS audit_users_change ON customer_schema.users;
CREATE TRIGGER audit_users_change AFTER INSERT OR UPDATE OR DELETE ON customer_schema.users FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();
DROP TRIGGER IF EXISTS audit_wallets_change ON customer_schema.wallets;
CREATE TRIGGER audit_wallets_change AFTER INSERT OR UPDATE OR DELETE ON customer_schema.wallets FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();
DROP TRIGGER IF EXISTS audit_transactions_change ON customer_schema.transactions;
CREATE TRIGGER audit_transactions_change AFTER INSERT OR UPDATE OR DELETE ON customer_schema.transactions FOR EACH ROW EXECUTE FUNCTION audit_schema.log_data_change();

DROP TRIGGER IF EXISTS wallets_currency_enforcement ON customer_schema.wallets;
CREATE TRIGGER wallets_currency_enforcement BEFORE INSERT OR UPDATE ON customer_schema.wallets FOR EACH ROW EXECUTE FUNCTION enforce_wallet_currency();

-- ==========================================
-- 6. SECURITY (Roles & RLS)
-- ==========================================

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'kyd_admin') THEN
        CREATE ROLE kyd_admin WITH LOGIN PASSWORD 'admin_secure_pass';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'kyd_system') THEN
        CREATE ROLE kyd_system WITH LOGIN PASSWORD 'system_secure_pass';
    END IF;
END
$$;

GRANT USAGE ON SCHEMA customer_schema TO kyd_system;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA customer_schema TO kyd_system;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA customer_schema TO kyd_system;

ALTER TABLE customer_schema.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_schema.wallets ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_schema.transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_schema.notifications ENABLE ROW LEVEL SECURITY;

-- ==========================================
-- 7. PUBLIC VIEWS
-- ==========================================

CREATE OR REPLACE VIEW public.secure_users AS
SELECT id, email, first_name, last_name, CONCAT('***-***-', RIGHT(phone, 4)) as phone, kyc_status, created_at
FROM customer_schema.users;

CREATE OR REPLACE VIEW public.secure_transactions AS
SELECT id, amount, currency, status, transaction_type, created_at, sender_id, receiver_id
FROM customer_schema.transactions;

GRANT SELECT ON public.secure_users TO kyd_system;
GRANT SELECT ON public.secure_transactions TO kyd_system;

-- ==========================================
-- 8. DIFFERENTIAL PRIVACY FUNCTIONS
-- ==========================================

-- Laplace Noise
CREATE OR REPLACE FUNCTION privacy_schema.laplace_noise(scale DOUBLE PRECISION)
RETURNS DOUBLE PRECISION AS $$
DECLARE
    u DOUBLE PRECISION;
BEGIN
    u := random() - 0.5;
    IF u < 0 THEN
        RETURN scale * ln(1.0 + 2.0 * u);
    ELSE
        RETURN -scale * ln(1.0 - 2.0 * u);
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Budget Check
CREATE OR REPLACE FUNCTION privacy_schema.check_budget(
    user_id VARCHAR(255),
    epsilon_requested DECIMAL(10,6),
    delta_requested DECIMAL(10,6) DEFAULT 0.00001
)
RETURNS TABLE (
    has_budget BOOLEAN,
    daily_remaining DECIMAL(10,6),
    weekly_remaining DECIMAL(10,6),
    monthly_remaining DECIMAL(10,6),
    message TEXT
) AS $$
DECLARE
    budget_record RECORD;
    current_time TIMESTAMPTZ := NOW();
    needs_reset BOOLEAN := FALSE;
BEGIN
    SELECT * INTO budget_record FROM privacy_schema.user_privacy_budgets 
    WHERE user_privacy_budgets.user_id = check_budget.user_id;
    
    IF NOT FOUND THEN
        INSERT INTO privacy_schema.user_privacy_budgets (user_id)
        VALUES (check_budget.user_id)
        RETURNING * INTO budget_record;
    END IF;
    
    -- (Logic for reset would go here, simplified for consolidation)
    
    IF budget_record.consumed_daily + epsilon_requested > budget_record.daily_epsilon THEN
        RETURN QUERY SELECT FALSE, 0.0, 0.0, 0.0, 'Daily budget exceeded'::TEXT;
    ELSE
        RETURN QUERY SELECT TRUE, 1.0, 3.0, 10.0, 'Budget available'::TEXT;
    END IF;
END;
$$ LANGUAGE plpgsql;
CREATE TABLE IF NOT EXISTS admin_schema.api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    key_prefix VARCHAR(10) NOT NULL,
    key_hash VARCHAR(255) NOT NULL,
    scopes TEXT[] DEFAULT '{}',
    is_active BOOLEAN DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    created_by UUID,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON admin_schema.api_keys(key_prefix);
CREATE TABLE IF NOT EXISTS customer_schema.blockchain_networks (
    network_id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    status VARCHAR(20) DEFAULT 'healthy',
    block_height BIGINT DEFAULT 0,
    peer_count INTEGER DEFAULT 0,
    last_block_time TIMESTAMPTZ,
    channel VARCHAR(50),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

ALTER TABLE customer_schema.blockchain_networks ADD COLUMN IF NOT EXISTS rpc_url VARCHAR(255);
ALTER TABLE customer_schema.blockchain_networks ADD COLUMN IF NOT EXISTS chain_id VARCHAR(50);
ALTER TABLE customer_schema.blockchain_networks ADD COLUMN IF NOT EXISTS symbol VARCHAR(10);
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS bio TEXT DEFAULT '';
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS city VARCHAR(100) DEFAULT '';
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS postal_code VARCHAR(20) DEFAULT '';
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS tax_id VARCHAR(255) DEFAULT '';
CREATE TABLE IF NOT EXISTS customer_schema.ledger_entries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id UUID NOT NULL REFERENCES customer_schema.transactions(id),
    wallet_id UUID NOT NULL REFERENCES customer_schema.wallets(id),
    entry_type VARCHAR(20) NOT NULL CHECK (entry_type IN ('debit', 'credit')),
    amount DECIMAL(20,2) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    balance_after DECIMAL(20,2) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    previous_hash VARCHAR(64) NOT NULL,
    hash VARCHAR(64) NOT NULL,
    UNIQUE(wallet_id, hash)
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet ON customer_schema.ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_created ON customer_schema.ledger_entries(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_exchange_rates_unique 
ON customer_schema.exchange_rates (base_currency, target_currency, valid_from);
ALTER TABLE customer_schema.transactions 
DROP CONSTRAINT transactions_status_check;

ALTER TABLE customer_schema.transactions 
ADD CONSTRAINT transactions_status_check 
CHECK (status IN (
    'pending', 
    'processing', 
    'reserved', 
    'settling', 
    'completed', 
    'failed', 
    'cancelled', 
    'refunded', 
    'disputed', 
    'reversed',
    'pending_approval',
    'pending_settlement',
    'requires_review',
    'admin_investigation'
));
-- Add OAuth provider fields to users table
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS auth_provider VARCHAR(50);
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS provider_id VARCHAR(255);

-- Create index for provider_id to speed up OAuth lookups
CREATE INDEX IF NOT EXISTS idx_users_provider_id ON customer_schema.users(provider_id);
-- Add profile picture URL to users table
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS profile_picture_url TEXT;
-- Add Google OAuth tokens to users table
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS provider_access_token TEXT;
ALTER TABLE customer_schema.users ADD COLUMN IF NOT EXISTS provider_refresh_token TEXT;
-- Expand allowed security event types to support real monitoring.
-- Previous constraint only allowed a small fixed set, while services already emit additional event types.

ALTER TABLE admin_schema.security_events
  DROP CONSTRAINT IF EXISTS security_events_event_type_check;

ALTER TABLE admin_schema.security_events
  ADD CONSTRAINT security_events_event_type_check
  CHECK (event_type IN (
    -- Original types
    'brute_force_attempt',
    'suspicious_ip',
    'velocity_limit_exceeded',
    'admin_login_failed',
    'multiple_failed_kyc',
    'blacklisted_device_detected',

    -- Extended types used by services / risk engine
    'risk_block',
    'auth_failure',
    'blockchain_mismatch',
    'login_success',
    'login_failed'
  ));

-- Expand blocklist types to include user-level blocks.

ALTER TABLE admin_schema.blocklist
  DROP CONSTRAINT IF EXISTS blocklist_type_check;

ALTER TABLE admin_schema.blocklist
  ADD CONSTRAINT blocklist_type_check
  CHECK (type IN ('ip', 'email', 'device', 'wallet', 'user'));

-- 025_add_case_management.up.sql
-- Case management for bank-grade admin operations.

CREATE TABLE IF NOT EXISTS admin_schema.cases (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    title TEXT NOT NULL,
    description TEXT,
    status VARCHAR(30) NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'investigating', 'resolved', 'false_positive')),
    priority VARCHAR(20) NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'critical')),
    entity_type VARCHAR(30) NOT NULL CHECK (entity_type IN ('user', 'transaction', 'wallet', 'ip')),
    entity_id TEXT NOT NULL,
    created_by UUID,
    assigned_to UUID,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cases_status ON admin_schema.cases(status);
CREATE INDEX IF NOT EXISTS idx_cases_priority ON admin_schema.cases(priority);
CREATE INDEX IF NOT EXISTS idx_cases_entity ON admin_schema.cases(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_cases_created_at ON admin_schema.cases(created_at DESC);

CREATE TABLE IF NOT EXISTS admin_schema.case_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    case_id UUID NOT NULL REFERENCES admin_schema.cases(id) ON DELETE CASCADE,
    event_type VARCHAR(30) NOT NULL CHECK (event_type IN ('note', 'status_change', 'assignment', 'link')),
    message TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_case_events_case_id ON admin_schema.case_events(case_id, created_at DESC);

-- 026_expand_wallet_currency_check.up.sql
-- Ensure wallets table allows ZMW (used by local workflows and enforcement trigger).

ALTER TABLE customer_schema.wallets
  DROP CONSTRAINT IF EXISTS wallets_currency_check;

ALTER TABLE customer_schema.wallets
  ADD CONSTRAINT wallets_currency_check
  CHECK (currency IN ('MWK', 'CNY', 'ZMW', 'USD', 'EUR'));

-- Change audit_logs.entity_id to VARCHAR(255) to support non-UUID identifiers
ALTER TABLE admin_schema.audit_logs ALTER COLUMN entity_id TYPE VARCHAR(255) USING entity_id::VARCHAR;

-- ==========================================
-- 9. SEED DATA (Baseline)
-- ==========================================

-- Password for all seeded users: password123 (bcrypt hash)
-- $2a$10$VvjG87jZR6Fyfkng5VCgVesXM7Gb7uTK4cvfWHVVG668GcAX6AY1.

-- Core Users
INSERT INTO customer_schema.users (id, email, email_hash, phone, password_hash, first_name, last_name, user_type, kyc_level, kyc_status, user_status, country_code, is_active, email_verified, created_at, updated_at) VALUES
('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid, 'admin@kyd.com', 'admin_email_hash', '+11234567890', '$2a$10$VvjG87jZR6Fyfkng5VCgVesXM7Gb7uTK4cvfWHVVG668GcAX6AY1.', 'Admin', 'User', 'admin', 1, 'verified', 'active', 'US', TRUE, TRUE, NOW(), NOW()),
('11111111-1111-1111-1111-111111111111'::uuid, 'fees@kyd.com', 'fees_email_hash', '+11111111111', '$2a$10$VvjG87jZR6Fyfkng5VCgVesXM7Gb7uTK4cvfWHVVG668GcAX6AY1.', 'KYD', 'Treasury', 'admin', 1, 'verified', 'active', 'US', TRUE, TRUE, NOW(), NOW()),
('b0eebc99-9c0b-4ef8-bb6d-6bb9bd380a12'::uuid, 'customer@kyd.com', 'customer_email_hash', '+26512345678', '$2a$10$VvjG87jZR6Fyfkng5VCgVesXM7Gb7uTK4cvfWHVVG668GcAX6AY1.', 'Customer', 'User', 'individual', 1, 'verified', 'active', 'MW', TRUE, TRUE, NOW(), NOW()),
('c0eebc99-9c0b-4ef8-bb6d-6bb9bd380a13'::uuid, 'john.doe@example.com', 'john_email_hash', '+265999888777', '$2a$10$VvjG87jZR6Fyfkng5VCgVesXM7Gb7uTK4cvfWHVVG668GcAX6AY1.', 'John', 'Doe', 'individual', 1, 'verified', 'active', 'MW', TRUE, TRUE, NOW(), NOW()),
('d0eebc99-9c0b-4ef8-bb6d-6bb9bd380a14'::uuid, 'jane.smith@example.com', 'jane_email_hash', '+8613800138000', '$2a$10$VvjG87jZR6Fyfkng5VCgVesXM7Gb7uTK4cvfWHVVG668GcAX6AY1.', 'Jane', 'Smith', 'individual', 1, 'verified', 'active', 'CN', TRUE, TRUE, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- Wallets
INSERT INTO customer_schema.wallets (id, user_id, wallet_address, currency, available_balance, ledger_balance, status, created_at, updated_at) VALUES
('e0eebc99-9c0b-4ef8-bb6d-6bb9bd380a15'::uuid, '11111111-1111-1111-1111-111111111111'::uuid, 'TREASURY-FEES-MWK', 'MWK', 0.00, 0.00, 'active', NOW(), NOW()),
('f0eebc99-9c0b-4ef8-bb6d-6bb9bd380a16'::uuid, '11111111-1111-1111-1111-111111111111'::uuid, 'TREASURY-FEES-ZMW', 'ZMW', 0.00, 0.00, 'active', NOW(), NOW()),
('00eebc99-9c0b-4ef8-bb6d-6bb9bd380a17'::uuid, 'c0eebc99-9c0b-4ef8-bb6d-6bb9bd380a13'::uuid, 'WALLET-MWK-JOHN', 'MWK', 500000.00, 500000.00, 'active', NOW(), NOW()),
('10eebc99-9c0b-4ef8-bb6d-6bb9bd380a18'::uuid, 'd0eebc99-9c0b-4ef8-bb6d-6bb9bd380a14'::uuid, 'WALLET-CNY-JANE', 'CNY', 1000.00, 1000.00, 'active', NOW(), NOW()),
('20eebc99-9c0b-4ef8-bb6d-6bb9bd380a19'::uuid, 'b0eebc99-9c0b-4ef8-bb6d-6bb9bd380a12'::uuid, 'WALLET-MWK-CUSTOMER', 'MWK', 250000.00, 250000.00, 'active', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- Initial Transactions
INSERT INTO customer_schema.transactions (id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id, amount, currency, exchange_rate, converted_amount, converted_currency, fee_amount, net_amount, status, transaction_type, initiated_at, created_at, updated_at) VALUES
(uuid_generate_v4(), 'TX-REF-001', 'c0eebc99-9c0b-4ef8-bb6d-6bb9bd380a13'::uuid, 'd0eebc99-9c0b-4ef8-bb6d-6bb9bd380a14'::uuid, '00eebc99-9c0b-4ef8-bb6d-6bb9bd380a17'::uuid, '10eebc99-9c0b-4ef8-bb6d-6bb9bd380a18'::uuid, 1000.00, 'MWK', 1.0, 1000.00, 'MWK', 0.00, 1000.00, 'completed', 'transfer', NOW(), NOW(), NOW()),
(uuid_generate_v4(), 'TX-REF-002', 'd0eebc99-9c0b-4ef8-bb6d-6bb9bd380a14'::uuid, 'c0eebc99-9c0b-4ef8-bb6d-6bb9bd380a13'::uuid, '10eebc99-9c0b-4ef8-bb6d-6bb9bd380a18'::uuid, '00eebc99-9c0b-4ef8-bb6d-6bb9bd380a17'::uuid, 50.00, 'CNY', 1.0, 50.00, 'CNY', 0.00, 50.00, 'completed', 'transfer', NOW(), NOW(), NOW()),
(uuid_generate_v4(), 'TX-REF-003', 'c0eebc99-9c0b-4ef8-bb6d-6bb9bd380a13'::uuid, 'd0eebc99-9c0b-4ef8-bb6d-6bb9bd380a14'::uuid, '00eebc99-9c0b-4ef8-bb6d-6bb9bd380a17'::uuid, '10eebc99-9c0b-4ef8-bb6d-6bb9bd380a18'::uuid, 50000.00, 'MWK', 1.0, 50000.00, 'MWK', 0.00, 50000.00, 'pending', 'transfer', NOW(), NOW(), NOW())
ON CONFLICT (reference) DO NOTHING;
