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
