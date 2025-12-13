-- Backfill net_amount to avoid NULL scan issues in listings
UPDATE transactions
SET net_amount = COALESCE(converted_amount, amount)
WHERE net_amount IS NULL;

-- Ensure net_amount is NOT NULL going forward
ALTER TABLE transactions
    ALTER COLUMN net_amount SET NOT NULL;