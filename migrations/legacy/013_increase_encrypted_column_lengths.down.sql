DROP VIEW IF EXISTS public.secure_users;

ALTER TABLE customer_schema.users ALTER COLUMN phone TYPE VARCHAR(50);
ALTER TABLE customer_schema.users ALTER COLUMN first_name TYPE VARCHAR(100);
ALTER TABLE customer_schema.users ALTER COLUMN last_name TYPE VARCHAR(100);

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

GRANT SELECT ON public.secure_users TO kyd_system;
