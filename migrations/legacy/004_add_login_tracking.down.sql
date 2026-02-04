ALTER TABLE customer_schema.users 
DROP COLUMN IF NOT EXISTS failed_login_attempts,
DROP COLUMN IF NOT EXISTS locked_until;
