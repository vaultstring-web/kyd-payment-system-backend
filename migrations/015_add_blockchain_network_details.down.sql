DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_tables WHERE schemaname = 'customer_schema' AND tablename = 'blockchain_networks') THEN
        ALTER TABLE customer_schema.blockchain_networks DROP COLUMN IF EXISTS rpc_url;
        ALTER TABLE customer_schema.blockchain_networks DROP COLUMN IF EXISTS chain_id;
        ALTER TABLE customer_schema.blockchain_networks DROP COLUMN IF EXISTS symbol;
    END IF;
END $$;