CREATE OR REPLACE FUNCTION enforce_wallet_currency() RETURNS trigger AS $$
DECLARE cc VARCHAR(2);
BEGIN
  SELECT country_code INTO cc FROM users WHERE id = NEW.user_id;
  IF cc IS NULL THEN
    RAISE EXCEPTION 'user not found for wallet';
  END IF;
  IF cc = 'CN' AND NEW.currency <> 'CNY' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  ELSIF cc = 'MW' AND NEW.currency <> 'MWK' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  ELSIF cc <> 'CN' AND cc <> 'MW' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS wallets_currency_enforcement ON wallets;
CREATE TRIGGER wallets_currency_enforcement
BEFORE INSERT OR UPDATE ON wallets
FOR EACH ROW
EXECUTE FUNCTION enforce_wallet_currency();
