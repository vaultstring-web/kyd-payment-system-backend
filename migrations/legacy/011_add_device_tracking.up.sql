-- 011_add_device_tracking.up.sql
CREATE TABLE IF NOT EXISTS customer_schema.user_devices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES customer_schema.users(id),
    device_hash VARCHAR(255) NOT NULL,
    device_name VARCHAR(255),
    country_code VARCHAR(2),
    ip_address VARCHAR(45),
    is_trusted BOOLEAN DEFAULT TRUE,
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(user_id, device_hash)
);

CREATE INDEX idx_user_devices_user_id ON customer_schema.user_devices(user_id);
