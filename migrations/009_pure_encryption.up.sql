-- Add blind index columns for searchable encryption
ALTER TABLE customer_schema.users 
ADD COLUMN IF NOT EXISTS email_hash VARCHAR(64),
ADD COLUMN IF NOT EXISTS phone_hash VARCHAR(64);

-- Create indexes on hash columns for fast lookups
CREATE INDEX IF NOT EXISTS idx_users_email_hash ON customer_schema.users(email_hash);
CREATE INDEX IF NOT EXISTS idx_users_phone_hash ON customer_schema.users(phone_hash);

-- Drop old unique constraints on plaintext columns
-- Note: We handle this safely by checking if they exist
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'users_email_key') THEN
        ALTER TABLE customer_schema.users DROP CONSTRAINT users_email_key;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'users_phone_key') THEN
        ALTER TABLE customer_schema.users DROP CONSTRAINT users_phone_key;
    END IF;
END $$;

-- Add unique constraints to hash columns to enforce uniqueness on the actual data
-- We use a partial index or just allow nulls if phone is optional
ALTER TABLE customer_schema.users ADD CONSTRAINT users_email_hash_key UNIQUE (email_hash);

-- Phone is nullable in original schema, so we only enforce unique if not null
CREATE UNIQUE INDEX users_phone_hash_unique_idx ON customer_schema.users (phone_hash) WHERE phone_hash IS NOT NULL;
