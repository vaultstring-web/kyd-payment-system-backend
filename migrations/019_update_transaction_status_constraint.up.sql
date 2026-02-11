ALTER TABLE customer_schema.transactions 
DROP CONSTRAINT transactions_status_check;

ALTER TABLE customer_schema.transactions 
ADD CONSTRAINT transactions_status_check 
CHECK (status IN (
    'pending', 
    'processing', 
    'reserved', 
    'settling', 
    'completed', 
    'failed', 
    'cancelled', 
    'refunded', 
    'disputed', 
    'reversed',
    'pending_approval',
    'pending_settlement',
    'requires_review',
    'admin_investigation'
));
