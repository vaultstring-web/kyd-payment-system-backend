-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    phone VARCHAR(50) UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    user_type VARCHAR(20) NOT NULL CHECK (user_type IN ('individual', 'merchant', 'agent', 'admin')),
    kyc_level SMALLINT DEFAULT 0,
    kyc_status VARCHAR(20) DEFAULT 'pending' CHECK (kyc_status IN ('pending', 'processing', 'verified', 'rejected')),
    country_code VARCHAR(2) NOT NULL,
    date_of_birth DATE,
    business_name VARCHAR(255),
    business_registration VARCHAR(100),
    risk_score DECIMAL(5,2) DEFAULT 0.00,
    is_active BOOLEAN DEFAULT TRUE,
    last_login TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_phone ON users(phone);
CREATE INDEX idx_users_kyc_status ON users(kyc_status);
CREATE INDEX idx_users_country ON users(country_code);

-- Wallets table
CREATE TABLE IF NOT EXISTS wallets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
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

CREATE INDEX idx_wallets_user ON wallets(user_id);
CREATE INDEX idx_wallets_currency ON wallets(currency);
CREATE INDEX idx_wallets_status ON wallets(status);

-- Transactions table
CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    reference VARCHAR(100) UNIQUE NOT NULL,
    sender_id UUID NOT NULL REFERENCES users(id),
    receiver_id UUID NOT NULL REFERENCES users(id),
    sender_wallet_id UUID NOT NULL REFERENCES wallets(id),
    receiver_wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount DECIMAL(20,2) NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL,
    exchange_rate DECIMAL(12,6) NOT NULL,
    converted_amount DECIMAL(20,2) NOT NULL,
    converted_currency VARCHAR(3) NOT NULL,
    fee_amount DECIMAL(20,2) DEFAULT 0.00,
    fee_currency VARCHAR(3),
    net_amount DECIMAL(20,2),
    status VARCHAR(20) DEFAULT 'pending' CHECK (status IN (
        'pending', 'processing', 'reserved', 'settling', 
        'completed', 'failed', 'cancelled', 'refunded'
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

CREATE INDEX idx_tx_sender ON transactions(sender_id);
CREATE INDEX idx_tx_receiver ON transactions(receiver_id);
CREATE INDEX idx_tx_status ON transactions(status);
CREATE INDEX idx_tx_created ON transactions(created_at);
CREATE INDEX idx_tx_reference ON transactions(reference);
CREATE INDEX idx_tx_type ON transactions(transaction_type);
CREATE INDEX idx_tx_settlement ON transactions(settlement_id);

-- Exchange rates table
CREATE TABLE IF NOT EXISTS exchange_rates (
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
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_rate_pair ON exchange_rates(base_currency, target_currency);
CREATE INDEX idx_rate_validity ON exchange_rates(valid_from, valid_to);
CREATE INDEX idx_rate_source ON exchange_rates(source);
CREATE UNIQUE INDEX idx_rate_unique ON exchange_rates(base_currency, target_currency, valid_from);

-- Settlements table
CREATE TABLE IF NOT EXISTS settlements (
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
    status VARCHAR(20) DEFAULT 'pending' CHECK (status IN (
        'pending', 'processing', 'submitted', 'confirmed', 
        'completed', 'failed', 'reconciled'
    )),
    submission_count INTEGER DEFAULT 0,
    last_submitted_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    reconciliation_id UUID,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_settlement_status ON settlements(status);
CREATE INDEX idx_settlement_network ON settlements(network);
CREATE INDEX idx_settlement_created ON settlements(created_at);

-- Blockchain transactions table
CREATE TABLE IF NOT EXISTS blockchain_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    settlement_id UUID REFERENCES settlements(id),
    network VARCHAR(20) NOT NULL CHECK (network IN ('stellar', 'ripple', 'ethereum')),
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

CREATE INDEX idx_blockchain_tx_hash ON blockchain_transactions(tx_hash);
CREATE INDEX idx_blockchain_network ON blockchain_transactions(network);
CREATE INDEX idx_blockchain_settlement ON blockchain_transactions(settlement_id);

-- Ledger entries (immutable audit trail)
CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id UUID NOT NULL REFERENCES transactions(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('debit', 'credit')),
    amount DECIMAL(20,2) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    balance_after DECIMAL(20,2) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_ledger_transaction ON ledger_entries(transaction_id);
CREATE INDEX idx_ledger_wallet ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_created ON ledger_entries(created_at);

-- KYC documents table
CREATE TABLE IF NOT EXISTS kyc_documents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    document_type VARCHAR(50) NOT NULL CHECK (document_type IN (
        'national_id', 'passport', 'drivers_license', 
        'business_registration', 'tax_certificate', 'utility_bill'
    )),
    document_number VARCHAR(100),
    issuing_country VARCHAR(2),
    issue_date DATE,
    expiry_date DATE,
    front_image_url VARCHAR(500),
    back_image_url VARCHAR(500),
    selfie_image_url VARCHAR(500),
    verification_status VARCHAR(20) DEFAULT 'pending' CHECK (verification_status IN (
        'pending', 'processing', 'verified', 'rejected'
    )),
    verification_notes TEXT,
    verified_by UUID REFERENCES users(id),
    verified_at TIMESTAMPTZ,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, document_type, document_number)
);

CREATE INDEX idx_kyc_user ON kyc_documents(user_id);
CREATE INDEX idx_kyc_status ON kyc_documents(verification_status);

-- Audit logs table
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    action VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50),
    entity_id UUID,
    old_values JSONB,
    new_values JSONB,
    ip_address INET,
    user_agent TEXT,
    request_id VARCHAR(100),
    status_code INTEGER,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_audit_user ON audit_logs(user_id);
CREATE INDEX idx_audit_action ON audit_logs(action);
CREATE INDEX idx_audit_created ON audit_logs(created_at);
CREATE INDEX idx_audit_entity ON audit_logs(entity_type, entity_id);

-- Create updated_at trigger function
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Add triggers for updated_at
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_wallets_updated_at BEFORE UPDATE ON wallets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_transactions_updated_at BEFORE UPDATE ON transactions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_settlements_updated_at BEFORE UPDATE ON settlements
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_kyc_documents_updated_at BEFORE UPDATE ON kyc_documents
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Backfill and enforce NOT NULL for net_amount (finalized schema)
UPDATE transactions
SET net_amount = COALESCE(converted_amount, amount)
WHERE net_amount IS NULL;

ALTER TABLE transactions
    ALTER COLUMN net_amount SET NOT NULL;

-- Wallet currency enforcement (country-based rule)
CREATE OR REPLACE FUNCTION enforce_wallet_currency() RETURNS trigger AS $$
DECLARE cc VARCHAR(2);
BEGIN
  SELECT country_code INTO cc FROM customer_schema.users WHERE id = NEW.user_id;
  IF cc IS NULL THEN
    RAISE EXCEPTION 'user not found for wallet';
  END IF;
  IF cc = 'CN' AND NEW.currency <> 'CNY' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  ELSIF cc = 'MW' AND NEW.currency <> 'MWK' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  ELSIF cc <> 'CN' AND cc <> 'MW' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS wallets_currency_enforcement ON wallets;
CREATE TRIGGER wallets_currency_enforcement
BEFORE INSERT OR UPDATE ON wallets
FOR EACH ROW
EXECUTE FUNCTION enforce_wallet_currency();
