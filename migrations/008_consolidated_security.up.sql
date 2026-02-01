-- 010_consolidated_security.up.sql
-- This migration consolidates differential privacy and union attack prevention measures.

-- Create privacy schema for differential privacy functions
CREATE SCHEMA IF NOT EXISTS privacy_schema;

-- =====================================================
-- PRIVACY BUDGET MANAGEMENT TABLES
-- =====================================================

-- User privacy budget tracking
CREATE TABLE IF NOT EXISTS privacy_schema.user_privacy_budgets (
    user_id VARCHAR(255) PRIMARY KEY,
    daily_epsilon DECIMAL(10,6) DEFAULT 1.0,
    weekly_epsilon DECIMAL(10,6) DEFAULT 3.0,
    monthly_epsilon DECIMAL(10,6) DEFAULT 10.0,
    consumed_daily DECIMAL(10,6) DEFAULT 0.0,
    consumed_weekly DECIMAL(10,6) DEFAULT 0.0,
    consumed_monthly DECIMAL(10,6) DEFAULT 0.0,
    last_reset TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Query history for audit
CREATE TABLE IF NOT EXISTS privacy_schema.query_audit_log (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    query_type VARCHAR(50) NOT NULL,
    table_name VARCHAR(255) NOT NULL,
    epsilon_consumed DECIMAL(10,6) NOT NULL,
    delta_consumed DECIMAL(10,6) DEFAULT 0.00001,
    sensitivity DECIMAL(10,6) NOT NULL,
    noise_mechanism VARCHAR(20) NOT NULL,
    original_result DECIMAL(20,6),
    noisy_result DECIMAL(20,6),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_query_audit_user ON privacy_schema.query_audit_log(user_id);
CREATE INDEX idx_query_audit_created ON privacy_schema.query_audit_log(created_at);

-- =====================================================
-- NOISE GENERATION FUNCTIONS
-- =====================================================

-- Generate Laplace noise (double precision)
CREATE OR REPLACE FUNCTION privacy_schema.laplace_noise(scale DOUBLE PRECISION)
RETURNS DOUBLE PRECISION AS $$
DECLARE
    u DOUBLE PRECISION;
BEGIN
    -- Generate uniform random number in (-0.5, 0.5)
    u := random() - 0.5;
    
    -- Inverse CDF of Laplace distribution
    IF u < 0 THEN
        RETURN scale * ln(1.0 + 2.0 * u);
    ELSE
        RETURN -scale * ln(1.0 - 2.0 * u);
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Generate geometric noise for count queries
CREATE OR REPLACE FUNCTION privacy_schema.geometric_noise(epsilon DOUBLE PRECISION)
RETURNS INTEGER AS $$
DECLARE
    p DOUBLE PRECISION;
    u DOUBLE PRECISION;
    magnitude INTEGER;
    sign INTEGER;
BEGIN
    IF epsilon <= 0 THEN
        RETURN 0;
    END IF;
    
    p := 1.0 - exp(-epsilon);
    
    -- Generate sign (positive or negative)
    sign := CASE WHEN random() < 0.5 THEN -1 ELSE 1 END;
    
    -- Generate magnitude using inverse CDF
    u := random();
    magnitude := floor(ln(u) / ln(1 - p));
    
    RETURN sign * magnitude;
END;
$$ LANGUAGE plpgsql;

-- Generate Gaussian noise for (ε,δ)-differential privacy
CREATE OR REPLACE FUNCTION privacy_schema.gaussian_noise(epsilon DOUBLE PRECISION, delta DOUBLE PRECISION, sensitivity DOUBLE PRECISION)
RETURNS DOUBLE PRECISION AS $$
DECLARE
    sigma DOUBLE PRECISION;
    u1 DOUBLE PRECISION;
    u2 DOUBLE PRECISION;
    z0 DOUBLE PRECISION;
BEGIN
    IF epsilon <= 0 OR delta <= 0 THEN
        RETURN 0;
    END IF;
    
    -- Calculate standard deviation for Gaussian mechanism
    sigma := sqrt(2 * ln(1.25 / delta)) * sensitivity / epsilon;
    
    -- Generate Gaussian noise using Box-Muller transform
    u1 := random();
    u2 := random();
    
    z0 := sqrt(-2 * ln(u1)) * cos(2 * pi() * u2);
    
    RETURN sigma * z0;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- PRIVACY BUDGET MANAGEMENT FUNCTIONS
-- =====================================================

-- Check if user has sufficient privacy budget
CREATE OR REPLACE FUNCTION privacy_schema.check_budget(
    user_id VARCHAR(255),
    epsilon_requested DECIMAL(10,6),
    delta_requested DECIMAL(10,6) DEFAULT 0.00001
)
RETURNS TABLE (
    has_budget BOOLEAN,
    daily_remaining DECIMAL(10,6),
    weekly_remaining DECIMAL(10,6),
    monthly_remaining DECIMAL(10,6),
    message TEXT
) AS $$
DECLARE
    budget_record RECORD;
    current_time TIMESTAMPTZ := NOW();
    needs_reset BOOLEAN := FALSE;
BEGIN
    -- Get or create budget record
    SELECT * INTO budget_record 
    FROM privacy_schema.user_privacy_budgets 
    WHERE user_privacy_budgets.user_id = check_budget.user_id;
    
    IF NOT FOUND THEN
        -- Create new budget record
        INSERT INTO privacy_schema.user_privacy_budgets (user_id)
        VALUES (check_budget.user_id)
        RETURNING * INTO budget_record;
    END IF;
    
    -- Check if budget needs reset (daily reset)
    IF DATE_TRUNC('day', current_time) > DATE_TRUNC('day', budget_record.last_reset) THEN
        needs_reset := TRUE;
    END IF;
    
    -- Reset budgets if needed
    IF needs_reset THEN
        UPDATE privacy_schema.user_privacy_budgets
        SET 
            consumed_daily = 0.0,
            consumed_weekly = CASE 
                WHEN DATE_TRUNC('week', current_time) > DATE_TRUNC('week', budget_record.last_reset) THEN 0.0
                ELSE consumed_weekly
            END,
            consumed_monthly = CASE 
                WHEN DATE_TRUNC('month', current_time) > DATE_TRUNC('month', budget_record.last_reset) THEN 0.0
                ELSE consumed_monthly
            END,
            last_reset = current_time,
            updated_at = current_time
        WHERE user_id = check_budget.user_id
        RETURNING * INTO budget_record;
    END IF;
    
    -- Check budget availability
    IF budget_record.consumed_daily + epsilon_requested > budget_record.daily_epsilon THEN
        RETURN QUERY SELECT 
            FALSE,
            budget_record.daily_epsilon - budget_record.consumed_daily,
            budget_record.weekly_epsilon - budget_record.consumed_weekly,
            budget_record.monthly_epsilon - budget_record.consumed_monthly,
            FORMAT('Daily budget exceeded: requested %.6f, available %.6f', 
                   epsilon_requested, budget_record.daily_epsilon - budget_record.consumed_daily);
        RETURN;
    END IF;
    
    IF budget_record.consumed_weekly + epsilon_requested > budget_record.weekly_epsilon THEN
        RETURN QUERY SELECT 
            FALSE,
            budget_record.daily_epsilon - budget_record.consumed_daily,
            budget_record.weekly_epsilon - budget_record.consumed_weekly,
            budget_record.monthly_epsilon - budget_record.consumed_monthly,
            FORMAT('Weekly budget exceeded: requested %.6f, available %.6f', 
                   epsilon_requested, budget_record.weekly_epsilon - budget_record.consumed_weekly);
        RETURN;
    END IF;
    
    IF budget_record.consumed_monthly + epsilon_requested > budget_record.monthly_epsilon THEN
        RETURN QUERY SELECT 
            FALSE,
            budget_record.daily_epsilon - budget_record.consumed_daily,
            budget_record.weekly_epsilon - budget_record.consumed_weekly,
            budget_record.monthly_epsilon - budget_record.consumed_monthly,
            FORMAT('Monthly budget exceeded: requested %.6f, available %.6f', 
                   epsilon_requested, budget_record.monthly_epsilon - budget_record.consumed_monthly);
        RETURN;
    END IF;
    
    -- Budget available
    RETURN QUERY SELECT 
        TRUE,
        budget_record.daily_epsilon - budget_record.consumed_daily,
        budget_record.weekly_epsilon - budget_record.consumed_weekly,
        budget_record.monthly_epsilon - budget_record.consumed_monthly,
        'Budget available'::TEXT;
    
END;
$$ LANGUAGE plpgsql;

-- Consume privacy budget
CREATE OR REPLACE FUNCTION privacy_schema.consume_budget(
    user_id VARCHAR(255),
    epsilon_consumed DECIMAL(10,6),
    delta_consumed DECIMAL(10,6) DEFAULT 0.00001,
    query_type VARCHAR(50) DEFAULT 'unknown',
    table_name VARCHAR(255) DEFAULT 'unknown'
)
RETURNS BOOLEAN AS $$
DECLARE
    budget_check RECORD;
BEGIN
    -- First check if budget is available
    SELECT * INTO budget_check FROM privacy_schema.check_budget(user_id, epsilon_consumed, delta_consumed);
    
    IF NOT budget_check.has_budget THEN
        RAISE EXCEPTION 'Privacy budget insufficient: %', budget_check.message;
    END IF;
    
    -- Consume budget
    UPDATE privacy_schema.user_privacy_budgets
    SET 
        consumed_daily = consumed_daily + epsilon_consumed,
        consumed_weekly = consumed_weekly + epsilon_consumed,
        consumed_monthly = consumed_monthly + epsilon_consumed,
        updated_at = NOW()
    WHERE user_privacy_budgets.user_id = consume_budget.user_id;
    
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- PRIVACY-AWARE AGGREGATE FUNCTIONS
-- =====================================================

-- NOISY_COUNT: Count with geometric noise
CREATE OR REPLACE FUNCTION privacy_schema.noisy_count(
    user_id VARCHAR(255),
    table_name VARCHAR(255),
    epsilon DECIMAL(10,6) DEFAULT 1.0,
    where_clause TEXT DEFAULT ''
)
RETURNS INTEGER AS $$
DECLARE
    true_count INTEGER;
    noise INTEGER;
    query_text TEXT;
    budget_okay BOOLEAN;
BEGIN
    -- Get true count
    query_text := FORMAT('SELECT COUNT(*) FROM %s', table_name);
    IF where_clause != '' THEN
        query_text := query_text || ' WHERE ' || where_clause;
    END IF;
    
    EXECUTE query_text INTO true_count;
    
    -- Consume privacy budget
    budget_okay := privacy_schema.consume_budget(user_id, epsilon, 0.00001, 'NOISY_COUNT', table_name);
    
    -- Generate geometric noise
    noise := privacy_schema.geometric_noise(epsilon);
    
    -- Log the query
    INSERT INTO privacy_schema.query_audit_log (
        user_id, query_type, table_name, epsilon_consumed, delta_consumed, 
        sensitivity, noise_mechanism, original_result, noisy_result
    ) VALUES (
        user_id, 'NOISY_COUNT', table_name, epsilon, 0.00001, 
        1.0, 'geometric', true_count, true_count + noise
    );
    
    -- Return noisy count (ensure non-negative)
    RETURN GREATEST(0, true_count + noise);
END;
$$ LANGUAGE plpgsql;

-- NOISY_SUM: Sum with Laplace noise scaled by data range
CREATE OR REPLACE FUNCTION privacy_schema.noisy_sum(
    user_id VARCHAR(255),
    table_name VARCHAR(255),
    column_name VARCHAR(255),
    epsilon DECIMAL(10,6) DEFAULT 1.0,
    max_value DECIMAL(20,6) DEFAULT 1000.0,
    where_clause TEXT DEFAULT ''
)
RETURNS DECIMAL(20,6) AS $$
DECLARE
    true_sum DECIMAL(20,6);
    noise DOUBLE PRECISION;
    sensitivity DECIMAL(20,6);
    query_text TEXT;
    budget_okay BOOLEAN;
BEGIN
    -- Get true sum
    query_text := FORMAT('SELECT COALESCE(SUM(%s), 0) FROM %s', column_name, table_name);
    IF where_clause != '' THEN
        query_text := query_text || ' WHERE ' || where_clause;
    END IF;
    
    EXECUTE query_text INTO true_sum;
    
    -- Calculate sensitivity (maximum possible change)
    sensitivity := max_value;
    
    -- Consume privacy budget
    budget_okay := privacy_schema.consume_budget(user_id, epsilon, 0.00001, 'NOISY_SUM', table_name);
    
    -- Generate Laplace noise
    noise := privacy_schema.laplace_noise(sensitivity::DOUBLE PRECISION / epsilon::DOUBLE PRECISION);
    
    -- Log the query
    INSERT INTO privacy_schema.query_audit_log (
        user_id, query_type, table_name, epsilon_consumed, delta_consumed, 
        sensitivity, noise_mechanism, original_result, noisy_result
    ) VALUES (
        user_id, 'NOISY_SUM', table_name, epsilon, 0.00001, 
        sensitivity, 'laplace', true_sum, true_sum + noise
    );
    
    -- Return noisy sum (ensure non-negative)
    RETURN GREATEST(0.0, true_sum + noise);
END;
$$ LANGUAGE plpgsql;

-- NOISY_AVG: Average with proportional noise
CREATE OR REPLACE FUNCTION privacy_schema.noisy_avg(
    user_id VARCHAR(255),
    table_name VARCHAR(255),
    column_name VARCHAR(255),
    epsilon DECIMAL(10,6) DEFAULT 1.0,
    max_value DECIMAL(20,6) DEFAULT 1000.0,
    where_clause TEXT DEFAULT ''
)
RETURNS DECIMAL(20,6) AS $$
DECLARE
    true_avg DECIMAL(20,6);
    noise DOUBLE PRECISION;
    sensitivity DECIMAL(20,6);
    query_text TEXT;
    budget_okay BOOLEAN;
BEGIN
    -- Get true average
    query_text := FORMAT('SELECT COALESCE(AVG(%s), 0) FROM %s', column_name, table_name);
    IF where_clause != '' THEN
        query_text := query_text || ' WHERE ' || where_clause;
    END IF;
    
    EXECUTE query_text INTO true_avg;
    
    -- Calculate sensitivity (maximum possible change)
    sensitivity := max_value;
    
    -- Consume privacy budget
    budget_okay := privacy_schema.consume_budget(user_id, epsilon, 0.00001, 'NOISY_AVG', table_name);
    
    -- Generate Laplace noise
    noise := privacy_schema.laplace_noise(sensitivity::DOUBLE PRECISION / epsilon::DOUBLE PRECISION);
    
    -- Log the query
    INSERT INTO privacy_schema.query_audit_log (
        user_id, query_type, table_name, epsilon_consumed, delta_consumed, 
        sensitivity, noise_mechanism, original_result, noisy_result
    ) VALUES (
        user_id, 'NOISY_AVG', table_name, epsilon, 0.00001, 
        sensitivity, 'laplace', true_avg, true_avg + noise
    );
    
    -- Return noisy average (ensure non-negative)
    RETURN GREATEST(0.0, true_avg + noise);
END;
$$ LANGUAGE plpgsql;

-- NOISY_STDDEV: Standard deviation with controlled variance
CREATE OR REPLACE FUNCTION privacy_schema.noisy_stddev(
    user_id VARCHAR(255),
    table_name VARCHAR(255),
    column_name VARCHAR(255),
    epsilon DECIMAL(10,6) DEFAULT 1.0,
    max_value DECIMAL(20,6) DEFAULT 1000.0,
    where_clause TEXT DEFAULT ''
)
RETURNS DECIMAL(20,6) AS $$
DECLARE
    true_stddev DECIMAL(20,6);
    noise DOUBLE PRECISION;
    sensitivity DECIMAL(20,6);
    query_text TEXT;
    budget_okay BOOLEAN;
BEGIN
    -- Get true standard deviation
    query_text := FORMAT('SELECT COALESCE(STDDEV(%s), 0) FROM %s', column_name, table_name);
    IF where_clause != '' THEN
        query_text := query_text || ' WHERE ' || where_clause;
    END IF;
    
    EXECUTE query_text INTO true_stddev;
    
    -- Calculate sensitivity (higher for stddev)
    sensitivity := max_value * 2.0;
    
    -- Consume privacy budget
    budget_okay := privacy_schema.consume_budget(user_id, epsilon, 0.00001, 'NOISY_STDDEV', table_name);
    
    -- Generate Laplace noise
    noise := privacy_schema.laplace_noise(sensitivity::DOUBLE PRECISION / epsilon::DOUBLE PRECISION);
    
    -- Log the query
    INSERT INTO privacy_schema.query_audit_log (
        user_id, query_type, table_name, epsilon_consumed, delta_consumed, 
        sensitivity, noise_mechanism, original_result, noisy_result
    ) VALUES (
        user_id, 'NOISY_STDDEV', table_name, epsilon, 0.00001, 
        sensitivity, 'laplace', true_stddev, true_stddev + noise
    );
    
    -- Return noisy standard deviation (ensure non-negative)
    RETURN GREATEST(0.0, true_stddev + noise);
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- UTILITY FUNCTIONS
-- =====================================================

-- Get user privacy budget status
CREATE OR REPLACE FUNCTION privacy_schema.get_budget_status(user_id VARCHAR(255))
RETURNS TABLE (
    daily_consumed DECIMAL(10,6),
    daily_remaining DECIMAL(10,6),
    weekly_consumed DECIMAL(10,6),
    weekly_remaining DECIMAL(10,6),
    monthly_consumed DECIMAL(10,6),
    monthly_remaining DECIMAL(10,6),
    query_count BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        upb.consumed_daily,
        upb.daily_epsilon - upb.consumed_daily,
        upb.consumed_weekly,
        upb.weekly_epsilon - upb.consumed_weekly,
        upb.consumed_monthly,
        upb.monthly_epsilon - upb.consumed_monthly,
        (SELECT COUNT(*) FROM privacy_schema.query_audit_log qal WHERE qal.user_id = get_budget_status.user_id)
    FROM privacy_schema.user_privacy_budgets upb
    WHERE upb.user_id = get_budget_status.user_id;
END;
$$ LANGUAGE plpgsql;

-- Reset user privacy budget
CREATE OR REPLACE FUNCTION privacy_schema.reset_user_budget(user_id VARCHAR(255))
RETURNS BOOLEAN AS $$
BEGIN
    UPDATE privacy_schema.user_privacy_budgets
    SET 
        consumed_daily = 0.0,
        consumed_weekly = 0.0,
        consumed_monthly = 0.0,
        last_reset = NOW(),
        updated_at = NOW()
    WHERE user_privacy_budgets.user_id = reset_user_budget.user_id;
    
    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- PRIVACY-ENHANCED VIEWS FOR COMMON QUERIES
-- =====================================================

-- Privacy-enhanced user statistics view
CREATE OR REPLACE VIEW privacy_schema.user_statistics AS
SELECT 
    'total_users' as metric,
    privacy_schema.noisy_count('system', 'customer_schema.users', 0.1) as value,
    'count' as metric_type,
    NOW() as calculated_at
UNION ALL
SELECT 
    'active_users' as metric,
    privacy_schema.noisy_count('system', 'customer_schema.users', 0.1, 'is_active = true') as value,
    'count' as metric_type,
    NOW() as calculated_at
UNION ALL
SELECT 
    'verified_users' as metric,
    privacy_schema.noisy_count('system', 'customer_schema.users', 0.1, 'kyc_status = ''verified''') as value,
    'count' as metric_type,
    NOW() as calculated_at;

-- Privacy-enhanced transaction statistics view
CREATE OR REPLACE VIEW privacy_schema.transaction_statistics AS
SELECT 
    'total_transactions' as metric,
    privacy_schema.noisy_count('system', 'customer_schema.transactions', 0.1) as value,
    'count' as metric_type,
    NOW() as calculated_at
UNION ALL
SELECT 
    'total_volume' as metric,
    privacy_schema.noisy_sum('system', 'customer_schema.transactions', 'amount', 0.1, 1000000.0) as value,
    'sum' as metric_type,
    NOW() as calculated_at
UNION ALL
SELECT 
    'avg_transaction_amount' as metric,
    privacy_schema.noisy_avg('system', 'customer_schema.transactions', 'amount', 0.1, 1000000.0) as value,
    'avg' as metric_type,
    NOW() as calculated_at;

-- Privacy-enhanced wallet statistics view
CREATE OR REPLACE VIEW privacy_schema.wallet_statistics AS
SELECT 
    'total_wallets' as metric,
    privacy_schema.noisy_count('system', 'customer_schema.wallets', 0.1) as value,
    'count' as metric_type,
    NOW() as calculated_at
UNION ALL
SELECT 
    'active_wallets' as metric,
    privacy_schema.noisy_count('system', 'customer_schema.wallets', 0.1, 'status = ''active''') as value,
    'count' as metric_type,
    NOW() as calculated_at
UNION ALL
SELECT 
    'total_balance_mwk' as metric,
    privacy_schema.noisy_sum('system', 'customer_schema.wallets', 'available_balance', 0.1, 1000000.0, 'currency = ''MWK''') as value,
    'sum' as metric_type,
    NOW() as calculated_at
UNION ALL
SELECT 
    'avg_balance_mwk' as metric,
    privacy_schema.noisy_avg('system', 'customer_schema.wallets', 'available_balance', 0.1, 1000000.0, 'currency = ''MWK''') as value,
    'avg' as metric_type,
    NOW() as calculated_at;

-- =====================================================
-- PRIVACY PROTECTION FUNCTIONS
-- =====================================================

-- Function to safely get user count with privacy protection
CREATE OR REPLACE FUNCTION privacy_schema.safe_user_count(
    requesting_user_id VARCHAR(255),
    epsilon DECIMAL(10,6) DEFAULT 0.1,
    filter_condition TEXT DEFAULT ''
)
RETURNS TABLE (
    count_result INTEGER,
    epsilon_used DECIMAL(10,6),
    privacy_message TEXT
) AS $$
DECLARE
    budget_check RECORD;
    noisy_result INTEGER;
BEGIN
    -- Check budget availability
    SELECT * INTO budget_check FROM privacy_schema.check_budget(requesting_user_id, epsilon);
    
    IF NOT budget_check.has_budget THEN
        RETURN QUERY SELECT 
            NULL::INTEGER, 
            0.0::DECIMAL,
            FORMAT('Privacy budget insufficient: %s', budget_check.message);
        RETURN;
    END IF;
    
    -- Get noisy count
    IF filter_condition != '' THEN
        noisy_result := privacy_schema.noisy_count(requesting_user_id, 'customer_schema.users', epsilon, filter_condition);
    ELSE
        noisy_result := privacy_schema.noisy_count(requesting_user_id, 'customer_schema.users', epsilon);
    END IF;
    
    RETURN QUERY SELECT 
        noisy_result,
        epsilon,
        FORMAT('Query executed with ε=%.6f differential privacy', epsilon);
END;
$$ LANGUAGE plpgsql;

-- Function to safely get transaction statistics with privacy protection
CREATE OR REPLACE FUNCTION privacy_schema.safe_transaction_stats(
    requesting_user_id VARCHAR(255),
    epsilon DECIMAL(10,6) DEFAULT 0.1,
    date_range_start DATE DEFAULT NULL,
    date_range_end DATE DEFAULT NULL
)
RETURNS TABLE (
    total_transactions INTEGER,
    total_volume DECIMAL(20,6),
    avg_amount DECIMAL(20,6),
    epsilon_used DECIMAL(10,6),
    privacy_message TEXT
) AS $$
DECLARE
    budget_check RECORD;
    where_clause TEXT := '';
BEGIN
    -- Check budget availability
    SELECT * INTO budget_check FROM privacy_schema.check_budget(requesting_user_id, epsilon * 3); -- 3 queries
    
    IF NOT budget_check.has_budget THEN
        RETURN QUERY SELECT 
            NULL::INTEGER, 
            NULL::DECIMAL,
            NULL::DECIMAL,
            0.0::DECIMAL,
            FORMAT('Privacy budget insufficient: %s', budget_check.message);
        RETURN;
    END IF;
    
    -- Build where clause for date range
    IF date_range_start IS NOT NULL AND date_range_end IS NOT NULL THEN
        where_clause := FORMAT('created_at BETWEEN ''%s'' AND ''%s''', date_range_start, date_range_end);
    END IF;
    
    RETURN QUERY
    SELECT 
        privacy_schema.noisy_count(requesting_user_id, 'customer_schema.transactions', epsilon/3, where_clause),
        privacy_schema.noisy_sum(requesting_user_id, 'customer_schema.transactions', 'amount', epsilon/3, 1000000.0, where_clause),
        privacy_schema.noisy_avg(requesting_user_id, 'customer_schema.transactions', 'amount', epsilon/3, 1000000.0, where_clause),
        epsilon,
        FORMAT('Transaction stats with ε=%.6f differential privacy', epsilon);
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- PRIVACY AUDIT FUNCTIONS
-- =====================================================

-- Get privacy audit report for a user
CREATE OR REPLACE FUNCTION privacy_schema.get_user_audit_report(user_id VARCHAR(255))
RETURNS TABLE (
    query_date DATE,
    query_count BIGINT,
    total_epsilon DECIMAL(10,6),
    query_types TEXT[],
    tables_accessed TEXT[]
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        DATE_TRUNC('day', qal.created_at)::DATE as query_date,
        COUNT(*) as query_count,
        SUM(qal.epsilon_consumed) as total_epsilon,
        ARRAY_AGG(DISTINCT qal.query_type) as query_types,
        ARRAY_AGG(DISTINCT qal.table_name) as tables_accessed
    FROM privacy_schema.query_audit_log qal
    WHERE qal.user_id = get_user_audit_report.user_id
    GROUP BY DATE_TRUNC('day', qal.created_at)
    ORDER BY query_date DESC
    LIMIT 30;
END;
$$ LANGUAGE plpgsql;

-- Get system-wide privacy statistics
CREATE OR REPLACE FUNCTION privacy_schema.get_system_privacy_stats()
RETURNS TABLE (
    total_queries BIGINT,
    total_epsilon_consumed DECIMAL(20,6),
    unique_users BIGINT,
    avg_epsilon_per_user DECIMAL(20,6),
    most_active_user VARCHAR(255),
    top_query_type VARCHAR(50)
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        COUNT(*) as total_queries,
        SUM(qal.epsilon_consumed) as total_epsilon_consumed,
        COUNT(DISTINCT qal.user_id) as unique_users,
        AVG(qal.epsilon_consumed) as avg_epsilon_per_user,
        (SELECT user_id FROM privacy_schema.query_audit_log GROUP BY user_id ORDER BY SUM(epsilon_consumed) DESC LIMIT 1) as most_active_user,
        (SELECT query_type FROM privacy_schema.query_audit_log GROUP BY query_type ORDER BY COUNT(*) DESC LIMIT 1) as top_query_type
    FROM privacy_schema.query_audit_log qal;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- PRIVACY BUDGET ADMINISTRATION
-- =====================================================

-- Grant permissions to appropriate roles
GRANT USAGE ON SCHEMA privacy_schema TO kyd_system;
GRANT SELECT ON ALL TABLES IN SCHEMA privacy_schema TO kyd_system;
GRANT INSERT ON privacy_schema.query_audit_log TO kyd_system;
GRANT UPDATE ON privacy_schema.user_privacy_budgets TO kyd_system;

GRANT SELECT ON privacy_schema.user_statistics TO kyd_admin;
GRANT SELECT ON privacy_schema.transaction_statistics TO kyd_admin;
GRANT SELECT ON privacy_schema.wallet_statistics TO kyd_admin;

-- Create function for privacy officers to manage budgets
CREATE OR REPLACE FUNCTION privacy_schema.adjust_user_budget(
    target_user_id VARCHAR(255),
    new_daily_epsilon DECIMAL(10,6),
    new_weekly_epsilon DECIMAL(10,6),
    new_monthly_epsilon DECIMAL(10,6),
    admin_user_id VARCHAR(255)
)
RETURNS BOOLEAN AS $$
BEGIN
    -- Log the budget adjustment
    INSERT INTO admin_schema.audit_logs (table_name, operation, performed_by, details)
    VALUES (
        'user_privacy_budgets', 
        'UPDATE', 
        admin_user_id,
        FORMAT('Adjusted budget for user %s: daily=%.6f, weekly=%.6f, monthly=%.6f', 
               target_user_id, new_daily_epsilon, new_weekly_epsilon, new_monthly_epsilon)
    );
    
    -- Update budget
    UPDATE privacy_schema.user_privacy_budgets
    SET 
        daily_epsilon = new_daily_epsilon,
        weekly_epsilon = new_weekly_epsilon,
        monthly_epsilon = new_monthly_epsilon,
        updated_at = NOW()
    WHERE user_id = target_user_id;
    
    RETURN FOUND;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- UNION ATTACK PREVENTION
-- =====================================================

-- Create a new schema for the views
CREATE SCHEMA IF NOT EXISTS safe_schema;

-- Create a view for the users table
CREATE OR REPLACE VIEW safe_schema.users_view AS
SELECT
    id,
    user_type,
    kyc_level,
    kyc_status,
    country_code,
    is_active
FROM
    customer_schema.users;

-- Create a view for the wallets table
CREATE OR REPLACE VIEW safe_schema.wallets_view AS
SELECT
    id,
    user_id,
    currency,
    available_balance,
    status
FROM
    customer_schema.wallets;

-- Create a view for the transactions table
CREATE OR REPLACE VIEW safe_schema.transactions_view AS
SELECT
    id,
    sender_id,
    receiver_id,
    amount,
    currency,
    status,
    transaction_type,
    initiated_at
FROM
    customer_schema.transactions;

-- Grant usage on the new schema to the kyd_system role
GRANT USAGE ON SCHEMA safe_schema TO kyd_system;

-- Grant select on the new views to the kyd_system role
GRANT SELECT ON safe_schema.users_view TO kyd_system;
GRANT SELECT ON safe_schema.wallets_view TO kyd_system;
GRANT SELECT ON safe_schema.transactions_view TO kyd_system;

-- Revoke direct table access from the kyd_system role
REVOKE SELECT ON customer_schema.users FROM kyd_system;
REVOKE SELECT ON customer_schema.wallets FROM kyd_system;
REVOKE SELECT ON customer_schema.transactions FROM kyd_system;

-- =====================================================
-- MISSING AUDIT TABLES (Restored for 007 compatibility)
-- =====================================================

CREATE TABLE IF NOT EXISTS audit_schema.transactions_audit (
    id SERIAL PRIMARY KEY,
    operation VARCHAR(50) NOT NULL,
    actor_role VARCHAR(50),
    ts TIMESTAMPTZ DEFAULT NOW(),
    before_data JSONB,
    after_data JSONB,
    can_rollback BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Helper function for audit encryption (placeholder)
CREATE OR REPLACE FUNCTION audit_schema._encrypt_or_plain(data JSONB)
RETURNS JSONB AS $$
BEGIN
    -- Return plain data for now
    RETURN data;
END;
$$ LANGUAGE plpgsql;

GRANT ALL PRIVILEGES ON audit_schema.transactions_audit TO kyd_system;
GRANT ALL PRIVILEGES ON audit_schema.transactions_audit TO kyd_admin;

