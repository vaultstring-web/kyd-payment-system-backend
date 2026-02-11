CREATE UNIQUE INDEX IF NOT EXISTS idx_exchange_rates_unique 
ON customer_schema.exchange_rates (base_currency, target_currency, valid_from);
