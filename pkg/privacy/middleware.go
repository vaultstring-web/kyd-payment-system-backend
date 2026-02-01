package privacy

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// PrivacyMiddleware provides application-level differential privacy protection
type PrivacyMiddleware struct {
	db             *sql.DB
	budgetManager  *BudgetManager
	noiseGenerator *NoiseGenerator
	config         MiddlewareConfig
}

// MiddlewareConfig holds configuration for privacy middleware
type MiddlewareConfig struct {
	DefaultEpsilon      float64
	DefaultDelta        float64
	MaxQuerySensitivity float64
	EnableAuditLogging  bool
	CacheResults        bool
	CacheTTL            time.Duration
}

// DefaultMiddlewareConfig provides reasonable defaults
var DefaultMiddlewareConfig = MiddlewareConfig{
	DefaultEpsilon:      1.0,
	DefaultDelta:        1e-5,
	MaxQuerySensitivity: 10000.0,
	EnableAuditLogging:  true,
	CacheResults:        true,
	CacheTTL:            5 * time.Minute,
}

// QueryContext holds context for privacy-protected queries
type QueryContext struct {
	UserID      string
	QueryType   string
	Table       string
	Epsilon     float64
	Delta       float64
	Sensitivity float64
	WhereClause string
	CacheKey    string
}

// NewPrivacyMiddleware creates a new privacy middleware instance
func NewPrivacyMiddleware(db *sql.DB, config MiddlewareConfig) *PrivacyMiddleware {
	return &PrivacyMiddleware{
		db:             db,
		budgetManager:  NewBudgetManager(DefaultBudgetConfig),
		noiseGenerator: NewNoiseGenerator(),
		config:         config,
	}
}

// ValidateQuery checks for UNION attacks and other query issues
func (pm *PrivacyMiddleware) ValidateQuery(query string) error {
	if strings.Contains(strings.ToLower(query), "union") {
		// Log alert for potential UNION attack
		// pm.logAlert(ctx, "UNION pattern detected in query")
		return fmt.Errorf("invalid query structure")
	}
	return nil
}

// ExecuteNoisyCount executes a privacy-protected count query
func (pm *PrivacyMiddleware) ExecuteNoisyCount(ctx context.Context, qc QueryContext) (int64, error) {
	// Check privacy budget
	if err := pm.budgetManager.ConsumeBudget(qc.UserID, qc.Epsilon, qc.Delta, qc.QueryType, qc.Table); err != nil {
		return 0, fmt.Errorf("privacy budget exceeded: %w", err)
	}

	// Get true count from database
	var trueCount int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM safe_schema.%s_view", qc.Table)
	var args []interface{}
	if qc.WhereClause != "" {
		query += " WHERE " + qc.WhereClause
		// For now, we don't have parameterized where clauses
		// This is a limitation that will be addressed in future iterations
	}

	if err := pm.ValidateQuery(query); err != nil {
		return 0, err
	}

	// Use prepared statement for security
	stmt, err := pm.db.PrepareContext(ctx, query)
	if err != nil {
		return 0, pm.handleError(fmt.Errorf("failed to prepare count query: %w", err))
	}
	defer stmt.Close()

	err = stmt.QueryRowContext(ctx, args...).Scan(&trueCount)
	if err != nil {
		return 0, pm.handleError(fmt.Errorf("failed to execute count query: %w", err))
	}

	// Generate geometric noise for count queries
	noise := pm.noiseGenerator.GeometricNoise(qc.Epsilon)
	noisyCount := trueCount + int64(noise)

	// Ensure non-negative result
	if noisyCount < 0 {
		noisyCount = 0
	}

	// Audit logging
	if pm.config.EnableAuditLogging {
		pm.logQuery(ctx, qc, float64(trueCount), float64(noisyCount))
	}

	return noisyCount, nil
}

// ExecuteNoisySum executes a privacy-protected sum query
func (pm *PrivacyMiddleware) ExecuteNoisySum(ctx context.Context, qc QueryContext, column string, maxValue float64) (float64, error) {
	// Check privacy budget
	if err := pm.budgetManager.ConsumeBudget(qc.UserID, qc.Epsilon, qc.Delta, qc.QueryType, qc.Table); err != nil {
		return 0, fmt.Errorf("privacy budget exceeded: %w", err)
	}

	// Get true sum from database
	var trueSum sql.NullFloat64
	query := fmt.Sprintf("SELECT COALESCE(SUM(%s), 0) FROM safe_schema.%s_view", column, qc.Table)
	var args []interface{}
	if qc.WhereClause != "" {
		query += " WHERE " + qc.WhereClause
	}

	if err := pm.ValidateQuery(query); err != nil {
		return 0, err
	}

	// Use prepared statement for security
	stmt, err := pm.db.PrepareContext(ctx, query)
	if err != nil {
		return 0, pm.handleError(fmt.Errorf("failed to prepare sum query: %w", err))
	}
	defer stmt.Close()

	err = stmt.QueryRowContext(ctx, args...).Scan(&trueSum)
	if err != nil {
		return 0, pm.handleError(fmt.Errorf("failed to execute sum query: %w", err))
	}

	trueSumValue := 0.0
	if trueSum.Valid {
		trueSumValue = trueSum.Float64
	}

	// Calculate sensitivity and generate Laplace noise
	sensitivity := maxValue
	scale := sensitivity / qc.Epsilon
	noise := pm.noiseGenerator.LaplaceNoise(scale)
	noisySum := trueSumValue + noise

	// Ensure non-negative result
	if noisySum < 0 {
		noisySum = 0
	}

	// Audit logging
	if pm.config.EnableAuditLogging {
		pm.logQuery(ctx, qc, trueSumValue, noisySum)
	}

	return noisySum, nil
}

// ExecuteNoisyAvg executes a privacy-protected average query
func (pm *PrivacyMiddleware) ExecuteNoisyAvg(ctx context.Context, qc QueryContext, column string, maxValue float64) (float64, error) {
	// Check privacy budget
	if err := pm.budgetManager.ConsumeBudget(qc.UserID, qc.Epsilon, qc.Delta, qc.QueryType, qc.Table); err != nil {
		return 0, fmt.Errorf("privacy budget exceeded: %w", err)
	}

	// Get true average from database
	var trueAvg sql.NullFloat64
	query := fmt.Sprintf("SELECT COALESCE(AVG(%s), 0) FROM safe_schema.%s_view", column, qc.Table)
	var args []interface{}
	if qc.WhereClause != "" {
		query += " WHERE " + qc.WhereClause
	}

	if err := pm.ValidateQuery(query); err != nil {
		return 0, err
	}

	// Use prepared statement for security
	stmt, err := pm.db.PrepareContext(ctx, query)
	if err != nil {
		return 0, pm.handleError(fmt.Errorf("failed to prepare avg query: %w", err))
	}
	defer stmt.Close()

	err = stmt.QueryRowContext(ctx, args...).Scan(&trueAvg)
	if err != nil {
		return 0, pm.handleError(fmt.Errorf("failed to execute avg query: %w", err))
	}

	trueAvgValue := 0.0
	if trueAvg.Valid {
		trueAvgValue = trueAvg.Float64
	}

	// Calculate sensitivity and generate Laplace noise
	sensitivity := maxValue
	scale := sensitivity / qc.Epsilon
	noise := pm.noiseGenerator.LaplaceNoise(scale)
	noisyAvg := trueAvgValue + noise

	// Ensure non-negative result
	if noisyAvg < 0 {
		noisyAvg = 0
	}

	// Audit logging
	if pm.config.EnableAuditLogging {
		pm.logQuery(ctx, qc, trueAvgValue, noisyAvg)
	}

	return noisyAvg, nil
}

// GetBudgetStatus returns current privacy budget status for a user
func (pm *PrivacyMiddleware) GetBudgetStatus(userID string) (map[string]interface{}, error) {
	return pm.budgetManager.GetBudgetStatus(userID)
}

// logQuery logs privacy-protected query execution
func (pm *PrivacyMiddleware) logQuery(ctx context.Context, qc QueryContext, originalResult, noisyResult float64) {
	query := `
		INSERT INTO privacy_schema.query_audit_log (
			user_id, query_type, table_name, epsilon_consumed, delta_consumed,
			sensitivity, noise_mechanism, original_result, noisy_result
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	mechanism := "laplace"
	if qc.QueryType == "COUNT" {
		mechanism = "geometric"
	}

	_, err := pm.db.ExecContext(ctx, query,
		qc.UserID, qc.QueryType, qc.Table, qc.Epsilon, qc.Delta,
		qc.Sensitivity, mechanism, originalResult, noisyResult,
	)
	if err != nil {
		// Log error but don't fail the query
		fmt.Printf("Failed to log privacy query: %v\n", err)
	}
}

// ValidateQuerySensitivity validates that query sensitivity is within acceptable bounds
func (pm *PrivacyMiddleware) ValidateQuerySensitivity(sensitivity float64) error {
	if sensitivity > pm.config.MaxQuerySensitivity {
		return fmt.Errorf("query sensitivity exceeds maximum allowed value")
	}
	return nil
}

// handleError returns a generic error message and logs the original error
func (pm *PrivacyMiddleware) handleError(err error) error {
	// Log the original error for internal review
	// In a real application, this would use a structured logger
	fmt.Printf("Internal error: %v\n", err)
	return fmt.Errorf("an unexpected error occurred")
}

// CalculateOptimalEpsilon calculates optimal epsilon based on query importance and remaining budget
func (pm *PrivacyMiddleware) CalculateOptimalEpsilon(userID string, queryImportance float64) (float64, error) {
	if queryImportance <= 0 || queryImportance > 1 {
		return 0, fmt.Errorf("query importance must be between 0 and 1")
	}

	status, err := pm.GetBudgetStatus(userID)
	if err != nil {
		return 0, err
	}

	dailyRemaining := status["daily_remaining"].(float64)
	weeklyRemaining := status["weekly_remaining"].(float64)
	monthlyRemaining := status["monthly_remaining"].(float64)

	// Use the most restrictive budget
	minRemaining := dailyRemaining
	if weeklyRemaining < minRemaining {
		minRemaining = weeklyRemaining
	}
	if monthlyRemaining < minRemaining {
		minRemaining = monthlyRemaining
	}

	// Calculate epsilon based on importance and remaining budget
	epsilon := minRemaining * queryImportance * 0.1 // Conservative allocation

	if epsilon < 0.001 {
		return 0, fmt.Errorf("insufficient privacy budget remaining")
	}

	return math.Min(epsilon, 1.0), nil // Cap at 1.0 for safety
}
