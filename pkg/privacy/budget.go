package privacy

import (
	"fmt"
	"sync"
	"time"
)

// BudgetManager tracks privacy budget consumption per user/query
type BudgetManager struct {
	mu      sync.RWMutex
	budgets map[string]*UserBudget
	config  BudgetConfig
}

// UserBudget tracks privacy budget for a specific user
type UserBudget struct {
	UserID          string
	DailyBudget     float64
	WeeklyBudget    float64
	MonthlyBudget   float64
	ConsumedDaily   float64
	ConsumedWeekly  float64
	ConsumedMonthly float64
	LastReset       time.Time
	QueryHistory    []QueryRecord
}

// QueryRecord tracks individual query privacy consumption
type QueryRecord struct {
	Timestamp time.Time
	Epsilon   float64
	Delta     float64
	QueryType string
	Table     string
}

// BudgetConfig defines privacy budget allocation
type BudgetConfig struct {
	DailyEpsilon   float64 // Daily privacy loss limit
	WeeklyEpsilon  float64 // Weekly privacy loss limit
	MonthlyEpsilon float64 // Monthly privacy loss limit
	Delta          float64 // Failure probability
	ResetHour      int     // Hour of day for budget reset (0-23)
}

// DefaultBudgetConfig provides reasonable defaults
var DefaultBudgetConfig = BudgetConfig{
	DailyEpsilon:   1.0,  // ε = 1.0 per day
	WeeklyEpsilon:  3.0,  // ε = 3.0 per week
	MonthlyEpsilon: 10.0, // ε = 10.0 per month
	Delta:          1e-5, // δ = 0.00001
	ResetHour:      0,    // Reset at midnight
}

// NewBudgetManager creates a new privacy budget manager
func NewBudgetManager(config BudgetConfig) *BudgetManager {
	return &BudgetManager{
		budgets: make(map[string]*UserBudget),
		config:  config,
	}
}

// CheckBudget checks if user has sufficient privacy budget
func (bm *BudgetManager) CheckBudget(userID string, epsilon, delta float64) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	budget, exists := bm.budgets[userID]
	if !exists {
		budget = &UserBudget{
			UserID:        userID,
			DailyBudget:   bm.config.DailyEpsilon,
			WeeklyBudget:  bm.config.WeeklyEpsilon,
			MonthlyBudget: bm.config.MonthlyEpsilon,
			LastReset:     time.Now(),
			QueryHistory:  make([]QueryRecord, 0),
		}
		bm.budgets[userID] = budget
	}

	// Reset budget if needed
	bm.resetIfNeeded(budget)

	// Check if requested budget exceeds limits
	if budget.ConsumedDaily+epsilon > budget.DailyBudget {
		return fmt.Errorf("daily privacy budget exceeded: requested %.4f, available %.4f", epsilon, budget.DailyBudget-budget.ConsumedDaily)
	}
	if budget.ConsumedWeekly+epsilon > budget.WeeklyBudget {
		return fmt.Errorf("weekly privacy budget exceeded: requested %.4f, available %.4f", epsilon, budget.WeeklyBudget-budget.ConsumedWeekly)
	}
	if budget.ConsumedMonthly+epsilon > budget.MonthlyBudget {
		return fmt.Errorf("monthly privacy budget exceeded: requested %.4f, available %.4f", epsilon, budget.MonthlyBudget-budget.ConsumedMonthly)
	}

	return nil
}

// ConsumeBudget consumes privacy budget for a query
func (bm *BudgetManager) ConsumeBudget(userID string, epsilon, delta float64, queryType, table string) error {
	if err := bm.CheckBudget(userID, epsilon, delta); err != nil {
		return err
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	budget := bm.budgets[userID]

	// Consume budget
	budget.ConsumedDaily += epsilon
	budget.ConsumedWeekly += epsilon
	budget.ConsumedMonthly += epsilon

	// Record query
	budget.QueryHistory = append(budget.QueryHistory, QueryRecord{
		Timestamp: time.Now(),
		Epsilon:   epsilon,
		Delta:     delta,
		QueryType: queryType,
		Table:     table,
	})

	return nil
}

// GetBudgetStatus returns current budget status for a user
func (bm *BudgetManager) GetBudgetStatus(userID string) (map[string]interface{}, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	budget, exists := bm.budgets[userID]
	if !exists {
		return nil, fmt.Errorf("user budget not found")
	}

	bm.resetIfNeeded(budget)

	return map[string]interface{}{
		"daily_consumed":    budget.ConsumedDaily,
		"daily_remaining":   budget.DailyBudget - budget.ConsumedDaily,
		"weekly_consumed":   budget.ConsumedWeekly,
		"weekly_remaining":  budget.WeeklyBudget - budget.ConsumedWeekly,
		"monthly_consumed":  budget.ConsumedMonthly,
		"monthly_remaining": budget.MonthlyBudget - budget.ConsumedMonthly,
		"query_count":       len(budget.QueryHistory),
	}, nil
}

// resetIfNeeded resets budget if daily period has passed
func (bm *BudgetManager) resetIfNeeded(budget *UserBudget) {
	now := time.Now()
	lastReset := budget.LastReset

	// Check if we need to reset daily budget
	if now.Year() != lastReset.Year() || now.YearDay() != lastReset.YearDay() {
		budget.ConsumedDaily = 0
		budget.LastReset = now
	}

	// Check if we need to reset weekly budget
	_, currentWeek := now.ISOWeek()
	_, lastResetWeek := lastReset.ISOWeek()
	if now.Year() != lastReset.Year() || currentWeek != lastResetWeek {
		budget.ConsumedWeekly = 0
	}

	// Check if we need to reset monthly budget
	if now.Year() != lastReset.Year() || now.Month() != lastReset.Month() {
		budget.ConsumedMonthly = 0
	}
}
