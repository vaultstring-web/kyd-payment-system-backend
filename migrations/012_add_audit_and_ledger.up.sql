-- Audit Logs (aligning with internal/repository/postgres/audit.go)
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
CREATE INDEX IF NOT EXISTS idx_audit_entity ON admin_schema.audit_logs(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_request_id ON admin_schema.audit_logs(request_id);

-- Ledger (Immutable Hash Chain)
CREATE TABLE IF NOT EXISTS customer_schema.transaction_ledger (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id UUID NOT NULL REFERENCES customer_schema.transactions(id),
    event_type VARCHAR(50) NOT NULL,
    amount DECIMAL(20,2),
    currency VARCHAR(3),
    status VARCHAR(20),
    previous_hash VARCHAR(64) UNIQUE, -- SHA-256, Unique to ensure linear chain
    hash VARCHAR(64) NOT NULL, -- SHA-256
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_tx ON customer_schema.transaction_ledger(transaction_id);
CREATE INDEX IF NOT EXISTS idx_ledger_hash ON customer_schema.transaction_ledger(hash);
