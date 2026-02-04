ALTER TABLE customer_schema.transactions DROP CONSTRAINT IF EXISTS transactions_status_check;
ALTER TABLE customer_schema.transactions ADD CONSTRAINT transactions_status_check CHECK (status IN (
    'pending', 'processing', 'reserved', 'settling', 
    'pending_settlement', 'completed', 'failed', 'cancelled', 'refunded',
    'disputed', 'reversed', 'pending_approval'
));
