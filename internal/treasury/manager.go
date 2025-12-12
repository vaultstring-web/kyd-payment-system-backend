// Package treasury implements treasury management, liquidity monitoring, and risk controls.
package treasury

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TreasuryManager handles treasury operations, liquidity, and risk management.
type TreasuryManager struct {
	mu              sync.RWMutex
	positions       map[string]*TreasuryPosition
	liquidityPools  map[string]*LiquidityPool
	riskMetrics     *RiskMetrics
	config          TreasuryConfig
	alerts          chan Alert
}

// TreasuryConfig holds configuration for treasury operations.
type TreasuryConfig struct {
	MinReserveRatio     float64
	MaxExposure         decimal.Decimal
	VaRConfidence       float64
	VaRHorizon          time.Duration
	StressTestScenarios []StressScenario
	LiquidityThreshold  decimal.Decimal
}

// TreasuryPosition represents a position in a currency or asset.
type TreasuryPosition struct {
	ID            string          `json:"id"`
	Currency      string          `json:"currency"`
	AssetType     string          `json:"asset_type"`
	Quantity      decimal.Decimal `json:"quantity"`
	AvgPrice      decimal.Decimal `json:"avg_price"`
	CurrentPrice  decimal.Decimal `json:"current_price"`
	MarketValue   decimal.Decimal `json:"market_value"`
	UnrealizedPnL decimal.Decimal `json:"unrealized_pnl"`
	RealizedPnL   decimal.Decimal `json:"realized_pnl"`
	Account       string          `json:"account"`
	LastValuation time.Time       `json:"last_valuation"`
	CreatedAt     time.Time       `json:"created_at"`
}

// LiquidityPool represents available liquidity in a currency.
type LiquidityPool struct {
	Currency      string          `json:"currency"`
	Total         decimal.Decimal `json:"total"`
	Available     decimal.Decimal `json:"available"`
	Committed     decimal.Decimal `json:"committed"`
	Reserved      decimal.Decimal `json:"reserved"`
	MinRequired   decimal.Decimal `json:"min_required"`
	LastUpdated   time.Time       `json:"last_updated"`
}

// RiskMetrics holds calculated risk measures.
type RiskMetrics struct {
	mu                 sync.RWMutex
	VaR95              decimal.Decimal `json:"var_95"`
	VaR99              decimal.Decimal `json:"var_99"`
	ExpectedShortfall  decimal.Decimal `json:"expected_shortfall"`
	MaxDrawdown        decimal.Decimal `json:"max_drawdown"`
	SharpeRatio        float64         `json:"sharpe_ratio"`
	TotalExposure      decimal.Decimal `json:"total_exposure"`
	NetExposure        decimal.Decimal `json:"net_exposure"`
	ConcentrationRisk  map[string]float64
	LastCalculated     time.Time       `json:"last_calculated"`
}

// StressScenario defines a stress test scenario.
type StressScenario struct {
	Name        string
	Description string
	Shocks      map[string]float64
}

// Alert represents a treasury alert.
type Alert struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Data      map[string]interface{}
	Timestamp time.Time `json:"timestamp"`
}

// NewTreasuryManager creates a new treasury manager.
func NewTreasuryManager(config TreasuryConfig) *TreasuryManager {
	tm := &TreasuryManager{
		positions:      make(map[string]*TreasuryPosition),
		liquidityPools: make(map[string]*LiquidityPool),
		riskMetrics:    &RiskMetrics{ConcentrationRisk: make(map[string]float64)},
		config:         config,
		alerts:         make(chan Alert, 1000),
	}

	// Initialize default stress scenarios
	if len(tm.config.StressTestScenarios) == 0 {
		tm.config.StressTestScenarios = []StressScenario{
			{
				Name:        "market_crash",
				Description: "Major market downturn",
				Shocks:      map[string]float64{"USD": -0.10, "EUR": -0.12, "GBP": -0.15, "MWK": -0.25, "CNY": -0.08},
			},
			{
				Name:        "currency_crisis",
				Description: "Emerging market currency crisis",
				Shocks:      map[string]float64{"MWK": -0.50, "ZAR": -0.30, "KES": -0.25},
			},
			{
				Name:        "liquidity_crunch",
				Description: "Global liquidity crisis",
				Shocks:      map[string]float64{"USD": -0.05, "EUR": -0.08, "GBP": -0.10, "MWK": -0.40, "CNY": -0.15},
			},
		}
	}

	// Initialize default liquidity pools
	currencies := []string{"USD", "EUR", "GBP", "CNY", "MWK", "ZAR"}
	for _, c := range currencies {
		tm.liquidityPools[c] = &LiquidityPool{
			Currency:    c,
			Total:       decimal.Zero,
			Available:   decimal.Zero,
			MinRequired: decimal.NewFromInt(100000),
			LastUpdated: time.Now(),
		}
	}

	go tm.startRiskMonitor()

	return tm
}

// UpdatePosition updates or creates a treasury position.
func (tm *TreasuryManager) UpdatePosition(pos TreasuryPosition) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if pos.ID == "" {
		pos.ID = uuid.New().String()
		pos.CreatedAt = time.Now()
	}

	pos.MarketValue = pos.Quantity.Mul(pos.CurrentPrice)
	pos.UnrealizedPnL = pos.Quantity.Mul(pos.CurrentPrice.Sub(pos.AvgPrice))
	pos.LastValuation = time.Now()

	tm.positions[pos.ID] = &pos

	return nil
}

// GetTotalExposure calculates total exposure across all positions.
func (tm *TreasuryManager) GetTotalExposure() decimal.Decimal {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	total := decimal.Zero
	for _, pos := range tm.positions {
		total = total.Add(pos.MarketValue.Abs())
	}
	return total
}

// GetNetExposure calculates net exposure (long - short).
func (tm *TreasuryManager) GetNetExposure() decimal.Decimal {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	net := decimal.Zero
	for _, pos := range tm.positions {
		net = net.Add(pos.MarketValue)
	}
	return net
}

// CalculateVaR computes Value at Risk using historical simulation.
func (tm *TreasuryManager) CalculateVaR(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)

	index := int(float64(len(sorted)) * (1 - confidence))
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return -sorted[index]
}

// CalculateMonteCarloVaR computes VaR using Monte Carlo simulation.
func (tm *TreasuryManager) CalculateMonteCarloVaR(portfolioValue decimal.Decimal, volatility float64, confidence float64, simulations int) decimal.Decimal {
	if simulations == 0 {
		simulations = 10000
	}

	var results []float64
	pv, _ := portfolioValue.Float64()

	for i := 0; i < simulations; i++ {
		// Simple random walk simulation
		randomReturn := tm.generateRandomReturn(0, volatility)
		simulatedValue := pv * (1 + randomReturn)
		results = append(results, simulatedValue-pv)
	}

	sort.Float64s(results)
	index := int(float64(simulations) * (1 - confidence))
	if index >= len(results) {
		index = len(results) - 1
	}

	return decimal.NewFromFloat(-results[index])
}

func (tm *TreasuryManager) generateRandomReturn(mean, stdDev float64) float64 {
	// Box-Muller transform for normal distribution
	u1 := float64(time.Now().UnixNano()%1000000) / 1000000.0
	u2 := float64((time.Now().UnixNano()+1)%1000000) / 1000000.0

	if u1 == 0 {
		u1 = 0.0001
	}

	z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	return mean + z*stdDev
}

// RunStressTest executes a stress test scenario.
func (tm *TreasuryManager) RunStressTest(scenario StressScenario) StressTestResult {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := StressTestResult{
		ScenarioName: scenario.Name,
		Timestamp:    time.Now(),
		ImpactByCurrency: make(map[string]decimal.Decimal),
	}

	totalImpact := decimal.Zero

	for _, pos := range tm.positions {
		shock, exists := scenario.Shocks[pos.Currency]
		if !exists {
			shock = 0
		}

		impact := pos.MarketValue.Mul(decimal.NewFromFloat(shock))
		result.ImpactByCurrency[pos.Currency] = result.ImpactByCurrency[pos.Currency].Add(impact)
		totalImpact = totalImpact.Add(impact)
	}

	result.TotalImpact = totalImpact
	result.PortfolioPctChange = 0

	totalValue := tm.GetTotalExposure()
	if !totalValue.IsZero() {
		impactFloat, _ := totalImpact.Float64()
		totalFloat, _ := totalValue.Float64()
		result.PortfolioPctChange = (impactFloat / totalFloat) * 100
	}

	return result
}

// StressTestResult holds stress test output.
type StressTestResult struct {
	ScenarioName       string                     `json:"scenario_name"`
	TotalImpact        decimal.Decimal            `json:"total_impact"`
	PortfolioPctChange float64                    `json:"portfolio_pct_change"`
	ImpactByCurrency   map[string]decimal.Decimal `json:"impact_by_currency"`
	Timestamp          time.Time                  `json:"timestamp"`
}

// UpdateLiquidity updates a liquidity pool.
func (tm *TreasuryManager) UpdateLiquidity(currency string, total, available, committed, reserved decimal.Decimal) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pool, exists := tm.liquidityPools[currency]
	if !exists {
		pool = &LiquidityPool{Currency: currency}
		tm.liquidityPools[currency] = pool
	}

	pool.Total = total
	pool.Available = available
	pool.Committed = committed
	pool.Reserved = reserved
	pool.LastUpdated = time.Now()

	// Check if below minimum
	if available.LessThan(pool.MinRequired) {
		tm.sendAlert(Alert{
			ID:       uuid.New().String(),
			Type:     "liquidity_warning",
			Severity: "high",
			Message:  fmt.Sprintf("Liquidity for %s below minimum requirement", currency),
			Data:     map[string]interface{}{"currency": currency, "available": available.String(), "required": pool.MinRequired.String()},
			Timestamp: time.Now(),
		})
	}
}

// GetLiquidityStatus returns current liquidity status.
func (tm *TreasuryManager) GetLiquidityStatus() map[string]*LiquidityPool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make(map[string]*LiquidityPool)
	for k, v := range tm.liquidityPools {
		copy := *v
		result[k] = &copy
	}
	return result
}

// CheckLiquidity verifies if sufficient liquidity exists for a transaction.
func (tm *TreasuryManager) CheckLiquidity(currency string, amount decimal.Decimal) (bool, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	pool, exists := tm.liquidityPools[currency]
	if !exists {
		return false, fmt.Errorf("currency not supported: %s", currency)
	}

	if pool.Available.LessThan(amount) {
		return false, fmt.Errorf("insufficient liquidity: available %s, required %s", pool.Available.String(), amount.String())
	}

	return true, nil
}

// ReserveLiquidity reserves liquidity for a pending transaction.
func (tm *TreasuryManager) ReserveLiquidity(currency string, amount decimal.Decimal) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pool, exists := tm.liquidityPools[currency]
	if !exists {
		return fmt.Errorf("currency not supported: %s", currency)
	}

	if pool.Available.LessThan(amount) {
		return fmt.Errorf("insufficient liquidity")
	}

	pool.Available = pool.Available.Sub(amount)
	pool.Reserved = pool.Reserved.Add(amount)
	pool.LastUpdated = time.Now()

	return nil
}

// ReleaseLiquidity releases reserved liquidity.
func (tm *TreasuryManager) ReleaseLiquidity(currency string, amount decimal.Decimal) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	pool, exists := tm.liquidityPools[currency]
	if !exists {
		return
	}

	pool.Reserved = pool.Reserved.Sub(amount)
	pool.Available = pool.Available.Add(amount)
	pool.LastUpdated = time.Now()
}

// GetRiskMetrics returns current risk metrics.
func (tm *TreasuryManager) GetRiskMetrics() *RiskMetrics {
	tm.riskMetrics.mu.RLock()
	defer tm.riskMetrics.mu.RUnlock()

	copy := *tm.riskMetrics
	return &copy
}

// CalculateConcentrationRisk calculates portfolio concentration.
func (tm *TreasuryManager) CalculateConcentrationRisk() map[string]float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	totalValue := decimal.Zero
	currencyExposure := make(map[string]decimal.Decimal)

	for _, pos := range tm.positions {
		currencyExposure[pos.Currency] = currencyExposure[pos.Currency].Add(pos.MarketValue.Abs())
		totalValue = totalValue.Add(pos.MarketValue.Abs())
	}

	concentration := make(map[string]float64)
	if totalValue.IsZero() {
		return concentration
	}

	for currency, exposure := range currencyExposure {
		expFloat, _ := exposure.Float64()
		totalFloat, _ := totalValue.Float64()
		concentration[currency] = (expFloat / totalFloat) * 100
	}

	return concentration
}

func (tm *TreasuryManager) startRiskMonitor() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		tm.updateRiskMetrics()
	}
}

func (tm *TreasuryManager) updateRiskMetrics() {
	tm.riskMetrics.mu.Lock()
	defer tm.riskMetrics.mu.Unlock()

	tm.riskMetrics.TotalExposure = tm.GetTotalExposure()
	tm.riskMetrics.NetExposure = tm.GetNetExposure()
	tm.riskMetrics.ConcentrationRisk = tm.CalculateConcentrationRisk()
	tm.riskMetrics.LastCalculated = time.Now()

	// Check for concentration risk alerts
	for currency, concentration := range tm.riskMetrics.ConcentrationRisk {
		if concentration > 50 {
			tm.sendAlert(Alert{
				ID:        uuid.New().String(),
				Type:      "concentration_risk",
				Severity:  "medium",
				Message:   fmt.Sprintf("High concentration in %s: %.1f%%", currency, concentration),
				Timestamp: time.Now(),
			})
		}
	}
}

func (tm *TreasuryManager) sendAlert(alert Alert) {
	select {
	case tm.alerts <- alert:
	default:
	}
}

// GetAlerts returns the alerts channel.
func (tm *TreasuryManager) GetAlerts() <-chan Alert {
	return tm.alerts
}

// GetPositions returns all treasury positions.
func (tm *TreasuryManager) GetPositions(ctx context.Context) []*TreasuryPosition {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make([]*TreasuryPosition, 0, len(tm.positions))
	for _, pos := range tm.positions {
		copy := *pos
		result = append(result, &copy)
	}
	return result
}
