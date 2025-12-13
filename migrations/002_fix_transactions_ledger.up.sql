-- Fix transactions status CHECK to include all defined statuses
ALTER TABLE transactions
  DROP CONSTRAINT IF EXISTS transactions_status_check;

ALTER TABLE transactions
  ADD CONSTRAINT transactions_status_check
  CHECK (status IN (
    'pending', 'processing', 'reserved', 'settling',
    'completed', 'failed', 'cancelled', 'refunded'
  ));

-- Ensure net_amount column exists (nullable to avoid breaking existing rows)
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'transactions' AND column_name = 'net_amount'
  ) THEN
    ALTER TABLE transactions ADD COLUMN net_amount DECIMAL(20,2);
  END IF;
END$$;

-- Ensure metadata column is JSONB with default
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'transactions' AND column_name = 'metadata'
  ) THEN
    ALTER TABLE transactions ADD COLUMN metadata JSONB DEFAULT '{}';
  ELSE
    ALTER TABLE transactions ALTER COLUMN metadata SET DEFAULT '{}';
  END IF;
END$$;

-- Add wallet_id column to ledger_entries to match service insert
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'ledger_entries' AND column_name = 'wallet_id'
  ) THEN
    ALTER TABLE ledger_entries ADD COLUMN wallet_id UUID;
    CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON ledger_entries(wallet_id);
  END IF;
END$$;