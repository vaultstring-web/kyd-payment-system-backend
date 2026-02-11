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
