package risk

import (
	"fmt"
	"sync"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/config"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RiskScore represents the calculated risk of a transaction (0-100)
type RiskScore int

type RiskStatus struct {
	GlobalSystemPause      bool       `json:"global_system_pause"`
	CircuitBreakerOpen     bool       `json:"circuit_breaker_open"`
	FailureCount           int        `json:"failure_count"`
	LastFailure            *time.Time `json:"last_failure,omitempty"`
	Threshold              int        `json:"threshold"`
	ResetDurationInSeconds int64      `json:"reset_duration_seconds"`
}

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

var defaultEngine *RiskEngine

func NewRiskEngine(cfg config.RiskConfig) *RiskEngine {
	threshold := 10
	if !cfg.EnableCircuitBreaker {
		threshold = 1000000 // Effectively disabled
	}

	engine := &RiskEngine{
		cb: &CircuitBreaker{
			threshold:     threshold, // Max 10 consecutive failures
			resetDuration: 1 * time.Minute,
		},
		coolOffCache: make(map[string]time.Time),
		config:       cfg,
	}

	defaultEngine = engine
	return engine
}

func GetDefaultRiskEngine() *RiskEngine {
	return defaultEngine
}

// CheckGlobalCircuitBreaker returns error if system is in safety mode
func (re *RiskEngine) CheckGlobalCircuitBreaker() error {
	re.mu.RLock()
	globalPause := re.config.GlobalSystemPause
	re.mu.RUnlock()

	if globalPause {
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
	re.mu.RLock()
	defer re.mu.RUnlock()
	return re.config
}

func (re *RiskEngine) CoolOffUserCount() int {
	re.mu.RLock()
	defer re.mu.RUnlock()
	return len(re.coolOffCache)
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
func (re *RiskEngine) EvaluateRisk(amount decimal.Decimal, kycLevel int, isNewDevice bool, location string, accountAgeDays int) RiskScore {
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

	if amount.GreaterThan(decimal.NewFromInt(re.config.HighValueThreshold)) {
		score += 40
	}

	// Rule 4: Suspicious Location
	if location == re.config.SuspiciousLocationAlert {
		score += 50
	}

	if accountAgeDays >= 0 && accountAgeDays < 7 {
		if amount.GreaterThan(decimal.NewFromInt(re.config.HighValueThreshold)) {
			score += 40
		}
	}

	if score > RiskScoreCritical {
		score = RiskScoreCritical
	}

	return score
}

func (re *RiskEngine) GetStatus() RiskStatus {
	re.mu.RLock()
	globalPause := re.config.GlobalSystemPause
	re.mu.RUnlock()

	re.cb.mutex.RLock()
	isOpen := re.cb.isOpen
	failureCount := re.cb.failureCount
	lastFailure := re.cb.lastFailure
	threshold := re.cb.threshold
	resetDuration := re.cb.resetDuration
	re.cb.mutex.RUnlock()

	var lastFailurePtr *time.Time
	if !lastFailure.IsZero() {
		t := lastFailure
		lastFailurePtr = &t
	}

	return RiskStatus{
		GlobalSystemPause:      globalPause,
		CircuitBreakerOpen:     isOpen,
		FailureCount:           failureCount,
		LastFailure:            lastFailurePtr,
		Threshold:              threshold,
		ResetDurationInSeconds: int64(resetDuration.Seconds()),
	}
}

func (re *RiskEngine) SetGlobalSystemPause(paused bool) {
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.GlobalSystemPause = paused
}

func (re *RiskEngine) SetMaxDailyLimit(limit int64) {
	if limit <= 0 {
		return
	}
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.MaxDailyLimit = limit
}

func (re *RiskEngine) SetHighValueThreshold(threshold int64) {
	if threshold <= 0 {
		return
	}
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.HighValueThreshold = threshold
}

func (re *RiskEngine) SetMaxVelocityPerHour(limit int) {
	if limit <= 0 {
		return
	}
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.MaxVelocityPerHour = limit
}

func (re *RiskEngine) SetMaxVelocityPerDay(limit int) {
	if limit <= 0 {
		return
	}
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.MaxVelocityPerDay = limit
}

func (re *RiskEngine) SetSuspiciousLocationAlert(location string) {
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.SuspiciousLocationAlert = location
}

func (re *RiskEngine) SetAdminApprovalThreshold(threshold int64) {
	if threshold <= 0 {
		return
	}
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.AdminApprovalThreshold = threshold
}

func (re *RiskEngine) SetRestrictedCountries(countries []string) {
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.RestrictedCountries = countries
}

func (re *RiskEngine) SetEnableDisputeResolution(enabled bool) {
	re.mu.Lock()
	defer re.mu.Unlock()
	re.config.EnableDisputeResolution = enabled
}

func (re *RiskEngine) OnBlockchainMismatch(event *domain.SecurityEvent) {
	if event == nil {
		return
	}
	if event.Type != domain.SecurityEventTypeBlockchainMismatch {
		return
	}

	anomalyCount := 1
	if event.Metadata != nil {
		if v, ok := event.Metadata["anomaly_count"]; ok {
			switch c := v.(type) {
			case int:
				if c > 0 {
					anomalyCount = c
				}
			case int64:
				if c > 0 {
					anomalyCount = int(c)
				}
			case float64:
				if c > 0 {
					anomalyCount = int(c)
				}
			}
		}
	}

	re.cb.mutex.RLock()
	threshold := re.cb.threshold
	re.cb.mutex.RUnlock()

	totalFailures := 1 + anomalyCount
	if anomalyCount >= 3 && threshold > 0 {
		totalFailures = threshold
	}
	for i := 0; i < totalFailures; i++ {
		re.ReportFailure()
	}
}
