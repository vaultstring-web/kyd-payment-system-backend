CREATE OR REPLACE FUNCTION enforce_wallet_currency() RETURNS trigger AS $$
DECLARE cc VARCHAR(2);
BEGIN
  IF TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND NEW.currency <> OLD.currency) THEN
  PERFORM 1 FROM customer_schema.users WHERE id = NEW.user_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'user not found for wallet';
  END IF;

  SELECT country_code INTO cc FROM customer_schema.users WHERE id = NEW.user_id;
  IF cc IS NULL THEN
    RAISE EXCEPTION 'missing country_code for user';
  END IF;

  IF cc = 'CN' AND NEW.currency <> 'CNY' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  ELSIF cc = 'MW' AND NEW.currency <> 'MWK' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  ELSIF cc <> 'CN' AND cc <> 'MW' AND NEW.currency <> 'USD' THEN
    RAISE EXCEPTION 'currency not allowed for user country';
  END IF;

  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
