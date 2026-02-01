package risk

import (
	"fmt"
	"sync"
	"time"

	"kyd/pkg/config"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RiskScore represents the calculated risk of a transaction (0-100)
type RiskScore int

const (
	RiskScoreLow      RiskScore = 0
	RiskScoreMedium   RiskScore = 50
	RiskScoreHigh     RiskScore = 80
	RiskScoreCritical RiskScore = 100
)

// CircuitBreaker manages global system safety
type CircuitBreaker struct {
	isOpen        bool
	failureCount  int
	lastFailure   time.Time
	threshold     int
	resetDuration time.Duration
	mutex         sync.RWMutex
}

type RiskEngine struct {
	cb *CircuitBreaker
	// In-memory cache for cool-off periods (use Redis in production)
	coolOffCache map[string]time.Time
	mu           sync.RWMutex
	config       config.RiskConfig
}

func NewRiskEngine(cfg config.RiskConfig) *RiskEngine {
	threshold := 10
	if !cfg.EnableCircuitBreaker {
		threshold = 1000000 // Effectively disabled
	}

	return &RiskEngine{
		cb: &CircuitBreaker{
			threshold:     threshold, // Max 10 consecutive failures
			resetDuration: 1 * time.Minute,
		},
		coolOffCache: make(map[string]time.Time),
		config:       cfg,
	}
}

// CheckGlobalCircuitBreaker returns error if system is in safety mode
func (re *RiskEngine) CheckGlobalCircuitBreaker() error {
	// 1. Manual System Pause (Panic Button)
	if re.config.GlobalSystemPause {
		return fmt.Errorf("system is manually paused due to security alert")
	}

	// 2. Automatic Circuit Breaker
	re.cb.mutex.RLock()
	defer re.cb.mutex.RUnlock()

	if re.cb.isOpen {
		if time.Since(re.cb.lastFailure) > re.cb.resetDuration {
			// Half-open state allowed, but we'll let the next write close it
			return nil
		}
		return fmt.Errorf("system circuit breaker open: transaction processing paused")
	}
	return nil
}

// ReportFailure increments failure count and may trip breaker
func (re *RiskEngine) ReportFailure() {
	re.cb.mutex.Lock()
	defer re.cb.mutex.Unlock()

	re.cb.failureCount++
	re.cb.lastFailure = time.Now()

	if re.cb.failureCount >= re.cb.threshold {
		re.cb.isOpen = true
	}
}

// ReportSuccess resets failure count
func (re *RiskEngine) ReportSuccess() {
	re.cb.mutex.Lock()
	defer re.cb.mutex.Unlock()

	if re.cb.isOpen {
		re.cb.isOpen = false
		re.cb.failureCount = 0
	} else if re.cb.failureCount > 0 {
		re.cb.failureCount = 0
	}
}

// CheckCoolOff enforces a waiting period for high-value transactions or new beneficiaries
func (re *RiskEngine) CheckCoolOff(userID uuid.UUID, amount decimal.Decimal) error {
	re.mu.RLock()
	defer re.mu.RUnlock()

	key := fmt.Sprintf("cooloff:%s", userID.String())
	if unlockTime, exists := re.coolOffCache[key]; exists {
		if time.Now().Before(unlockTime) {
			return fmt.Errorf("account in cool-off period until %s", unlockTime.Format(time.RFC3339))
		}
	}
	return nil
}

// CheckDailyLimit verifies if the transaction exceeds the daily limit
func (re *RiskEngine) CheckDailyLimit(currentAmount, dailyTotal decimal.Decimal) error {
	// Convert int64 limit to Decimal
	limit := decimal.NewFromInt(re.config.MaxDailyLimit)

	if dailyTotal.Add(currentAmount).GreaterThan(limit) {
		return fmt.Errorf("transaction exceeds daily limit of %s", limit.String())
	}
	return nil
}

// SetCoolOff triggers a cool-off period
func (re *RiskEngine) SetCoolOff(userID uuid.UUID, duration time.Duration) {
	re.mu.Lock()
	defer re.mu.Unlock()

	key := fmt.Sprintf("cooloff:%s", userID.String())
	re.coolOffCache[key] = time.Now().Add(duration)
}

// GetConfig returns the current risk configuration
func (re *RiskEngine) GetConfig() config.RiskConfig {
	return re.config
}

// CheckVelocity verifies if the transaction frequency exceeds limits
func (re *RiskEngine) CheckVelocity(hourlyCount int) error {
	if hourlyCount >= re.config.MaxVelocityPerHour {
		return fmt.Errorf("hourly transaction velocity limit exceeded (%d)", re.config.MaxVelocityPerHour)
	}
	return nil
}

// CheckLocation verifies if the transaction location is suspicious
func (re *RiskEngine) CheckLocation(location string) error {
	if location == re.config.SuspiciousLocationAlert {
		return fmt.Errorf("transaction from suspicious location: %s", location)
	}
	return nil
}

// CheckRestrictedCountry verifies if the transaction country is allowed
func (re *RiskEngine) CheckRestrictedCountry(countryCode string) error {
	if countryCode == "" {
		return nil
	}
	for _, restricted := range re.config.RestrictedCountries {
		if countryCode == restricted {
			return fmt.Errorf("transactions from %s are restricted", countryCode)
		}
	}
	return nil
}

// RequiresAdminApproval checks if the transaction amount is large enough to require manual approval
func (re *RiskEngine) RequiresAdminApproval(amount decimal.Decimal) bool {
	threshold := decimal.NewFromInt(re.config.AdminApprovalThreshold)
	return amount.GreaterThanOrEqual(threshold)
}

// EvaluateRisk calculates the risk score for a transaction
func (re *RiskEngine) EvaluateRisk(amount decimal.Decimal, kycLevel int, isNewDevice bool, location string) RiskScore {
	score := RiskScoreLow

	// Rule 1: High Amount vs KYC
	limit := decimal.NewFromInt(1000)
	if kycLevel == 1 {
		limit = decimal.NewFromInt(10000)
	} else if kycLevel == 2 {
		limit = decimal.NewFromInt(100000)
	}

	if amount.GreaterThan(limit) {
		score += 40
	}

	// Rule 2: New Device
	if isNewDevice {
		score += 60
	}

	// Rule 3: Very High Value (Global Config)
	if amount.GreaterThan(decimal.NewFromInt(re.config.HighValueThreshold)) {
		score += 40
	}

	// Rule 4: Suspicious Location
	if location == re.config.SuspiciousLocationAlert {
		score += 50
	}

	if score > RiskScoreCritical {
		score = RiskScoreCritical
	}

	return score
}
