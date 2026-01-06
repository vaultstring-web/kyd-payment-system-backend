CREATE OR REPLACE FUNCTION public.enforce_transaction_limits()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = customer_schema, public
AS $function$
DECLARE
  s_country TEXT;
  r_country TEXT;
  s_type TEXT;
BEGIN
  SELECT country_code, user_type INTO s_country, s_type
  FROM customer_schema.users WHERE id = NEW.sender_id;

  SELECT country_code INTO r_country
  FROM customer_schema.users WHERE id = NEW.receiver_id;

  IF s_country IS NULL OR r_country IS NULL THEN
    RAISE EXCEPTION 'missing country for users';
  END IF;

  IF s_type IS NULL THEN
    RAISE EXCEPTION 'missing user_type for sender';
  END IF;

  IF s_type = 'individual' AND NEW.amount > 20000 THEN
    INSERT INTO audit_schema.transactions_audit(
      operation, actor_role, ts, before_data, after_data, can_rollback
    ) VALUES (
      'VIOLATION', current_user, NOW(), NULL, audit_schema._encrypt_or_plain(to_jsonb(NEW)), FALSE
    );
    RAISE EXCEPTION 'amount exceeds individual tier limit';
  ELSIF s_type = 'agent' AND NEW.amount > 100000 THEN
    INSERT INTO audit_schema.transactions_audit(
      operation, actor_role, ts, before_data, after_data, can_rollback
    ) VALUES (
      'VIOLATION', current_user, NOW(), NULL, audit_schema._encrypt_or_plain(to_jsonb(NEW)), FALSE
    );
    RAISE EXCEPTION 'amount exceeds agent tier limit';
  ELSIF s_type = 'merchant' AND NEW.amount > 500000 THEN
    INSERT INTO audit_schema.transactions_audit(
      operation, actor_role, ts, before_data, after_data, can_rollback
    ) VALUES (
      'VIOLATION', current_user, NOW(), NULL, audit_schema._encrypt_or_plain(to_jsonb(NEW)), FALSE
    );
    RAISE EXCEPTION 'amount exceeds merchant tier limit';
  END IF;

  IF (s_country = 'MW' AND r_country = 'CN') OR (s_country = 'CN' AND r_country = 'MW') THEN
    IF NEW.amount > 50000 THEN
      INSERT INTO audit_schema.transactions_audit(
        operation, actor_role, ts, before_data, after_data, can_rollback
      ) VALUES (
        'VIOLATION', current_user, NOW(), NULL, audit_schema._encrypt_or_plain(to_jsonb(NEW)), FALSE
      );
      RAISE EXCEPTION 'amount exceeds MW-CN cross-border limit';
    END IF;
  END IF;

  RETURN NEW;
END;
$function$;
