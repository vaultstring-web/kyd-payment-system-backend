-- KYD Payment System - Consolidated Schema
-- This migration aligns with pkg/domain/models.go

BEGIN;

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  email TEXT NOT NULL UNIQUE,
  phone TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  first_name TEXT NOT NULL,
  last_name TEXT NOT NULL,
  user_type TEXT NOT NULL,
  kyc_level INT NOT NULL DEFAULT 0,
  kyc_status TEXT NOT NULL DEFAULT 'pending',
  country_code TEXT NOT NULL,
  date_of_birth TIMESTAMP NULL,
  business_name TEXT NULL,
  business_registration TEXT NULL,
  risk_score NUMERIC(24,12) NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  last_login TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Wallets
CREATE TABLE IF NOT EXISTS wallets (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  wallet_address TEXT NULL,
  currency TEXT NOT NULL,
  available_balance NUMERIC(24,12) NOT NULL DEFAULT 0,
  ledger_balance NUMERIC(24,12) NOT NULL DEFAULT 0,
  reserved_balance NUMERIC(24,12) NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  last_transaction_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Transactions
CREATE TABLE IF NOT EXISTS transactions (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  reference TEXT NOT NULL UNIQUE,
  sender_id UUID NOT NULL REFERENCES users(id),
  receiver_id UUID NOT NULL REFERENCES users(id),
  sender_wallet_id UUID NOT NULL REFERENCES wallets(id),
  receiver_wallet_id UUID NOT NULL REFERENCES wallets(id),
  amount NUMERIC(24,12) NOT NULL,
  currency TEXT NOT NULL,
  exchange_rate NUMERIC(24,12) NOT NULL DEFAULT 1,
  converted_amount NUMERIC(24,12) NOT NULL DEFAULT 0,
  converted_currency TEXT NOT NULL,
  fee_amount NUMERIC(24,12) NOT NULL DEFAULT 0,
  fee_currency TEXT NOT NULL,
  net_amount NUMERIC(24,12) NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  status_reason TEXT NULL,
  transaction_type TEXT NOT NULL,
  channel TEXT NULL,
  category TEXT NULL,
  description TEXT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  blockchain_tx_hash TEXT NULL,
  settlement_id UUID NULL,
  initiated_at TIMESTAMP NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Exchange Rates
CREATE TABLE IF NOT EXISTS exchange_rates (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  base_currency TEXT NOT NULL,
  target_currency TEXT NOT NULL,
  rate NUMERIC(24,12) NOT NULL,
  buy_rate NUMERIC(24,12) NOT NULL,
  sell_rate NUMERIC(24,12) NOT NULL,
  source TEXT NOT NULL,
  provider TEXT NULL,
  is_interbank BOOLEAN NOT NULL DEFAULT FALSE,
  spread NUMERIC(24,12) NOT NULL DEFAULT 0,
  valid_from TIMESTAMP NOT NULL,
  valid_to TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Settlements
CREATE TABLE IF NOT EXISTS settlements (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  batch_reference TEXT NOT NULL UNIQUE,
  network TEXT NOT NULL,
  transaction_hash TEXT NULL,
  source_account TEXT NULL,
  destination_account TEXT NULL,
  total_amount NUMERIC(24,12) NOT NULL,
  currency TEXT NOT NULL,
  fee_amount NUMERIC(24,12) NOT NULL DEFAULT 0,
  fee_currency TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  submission_count INT NOT NULL DEFAULT 0,
  last_submitted_at TIMESTAMP NULL,
  confirmed_at TIMESTAMP NULL,
  completed_at TIMESTAMP NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

COMMIT;