DROP SCHEMA IF EXISTS customer_schema CASCADE;
DROP SCHEMA IF EXISTS admin_schema CASCADE;
DROP SCHEMA IF EXISTS audit_schema CASCADE;
DROP SCHEMA IF EXISTS privacy_schema CASCADE;
DROP VIEW IF EXISTS public.secure_users;
DROP VIEW IF EXISTS public.secure_transactions;
DROP EXTENSION IF EXISTS "uuid-ossp";
