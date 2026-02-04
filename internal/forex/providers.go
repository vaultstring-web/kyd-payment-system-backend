// ==============================================================================
// FOREX RATE PROVIDERS - internal/forex/providers.go
// ==============================================================================
package forex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ExchangeRateAPIProvider fetches real rates from open.er-api.com
type ExchangeRateAPIProvider struct {
	client     *http.Client
	cache      map[string]cachedRates
	cacheMutex sync.RWMutex
	cacheTTL   time.Duration
}

type cachedRates struct {
	rates     map[string]float64
	fetchedAt time.Time
}

type erAPIResponse struct {
	Result   string             `json:"result"`
	BaseCode string             `json:"base_code"`
	Rates    map[string]float64 `json:"rates"`
}

func NewExchangeRateAPIProvider() *ExchangeRateAPIProvider {
	return &ExchangeRateAPIProvider{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
		cache:    make(map[string]cachedRates),
		cacheTTL: 5 * time.Minute,
	}
}

func (p *ExchangeRateAPIProvider) Name() string {
	return "ExchangeRateAPI"
}

func (p *ExchangeRateAPIProvider) GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error) {
	rates, err := p.fetchRates(ctx, string(from))
	if err != nil {
		return nil, err
	}

	targetRate, ok := rates[string(to)]
	if !ok {
		return nil, fmt.Errorf("rate not found for %s to %s", from, to)
	}

	return &domain.ExchangeRate{
		ID:             uuid.New(),
		BaseCurrency:   from,
		TargetCurrency: to,
		Rate:           decimal.NewFromFloat(targetRate),
		Source:         p.Name(),
		ValidFrom:      time.Now(),
	}, nil
}

func (p *ExchangeRateAPIProvider) fetchRates(ctx context.Context, base string) (map[string]float64, error) {
	p.cacheMutex.RLock()
	if cached, ok := p.cache[base]; ok {
		if time.Since(cached.fetchedAt) < p.cacheTTL {
			p.cacheMutex.RUnlock()
			return cached.rates, nil
		}
	}
	p.cacheMutex.RUnlock()

	url := fmt.Sprintf("https://open.er-api.com/v6/latest/%s", base)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp erAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Result != "success" {
		return nil, fmt.Errorf("API returned error result: %s", apiResp.Result)
	}

	p.cacheMutex.Lock()
	p.cache[base] = cachedRates{
		rates:     apiResp.Rates,
		fetchedAt: time.Now(),
	}
	p.cacheMutex.Unlock()

	return apiResp.Rates, nil
}

// MockRateProvider provides mock exchange rates for testing
type MockRateProvider struct{}

func NewMockRateProvider() *MockRateProvider {
	return &MockRateProvider{}
}

func (p *MockRateProvider) Name() string {
	return "MockProvider"
}

func (p *MockRateProvider) GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error) {
	// Mock rates for MWK-CNY
	rates := map[string]decimal.Decimal{
		"MWK-CNY": decimal.NewFromFloat(0.0085),
		"CNY-MWK": decimal.NewFromFloat(117.65),
	}

	key := string(from) + "-" + string(to)
	rate, ok := rates[key]
	if !ok {
		rate = decimal.NewFromInt(1)
	}

	return &domain.ExchangeRate{
		ID:             uuid.New(),
		BaseCurrency:   from,
		TargetCurrency: to,
		Rate:           rate,
		Source:         p.Name(),
		ValidFrom:      time.Now(),
	}, nil
}

// ==============================================================================
// DYNAMIC SPREAD ENGINE
// Calculates spreads based on volatility, liquidity, and time of day
// ==============================================================================

// SpreadEngine calculates dynamic spreads for forex rates.
type SpreadEngine struct {
	mu          sync.RWMutex
	volatility  map[string]float64
	liquidity   map[string]float64
	rateHistory map[string][]rateSnapshot
	config      SpreadConfig
}

type rateSnapshot struct {
	rate      decimal.Decimal
	timestamp time.Time
}

// SpreadConfig holds spread calculation parameters.
type SpreadConfig struct {
	BaseSpread           float64
	MinSpread            float64
	MaxSpread            float64
	VolatilityMultiplier float64
	LiquidityMultiplier  float64
	OffHoursMultiplier   float64
	WeekendMultiplier    float64
	BusinessHoursStart   int
	BusinessHoursEnd     int
}

// SpreadResult contains the spread calculation output.
type SpreadResult struct {
	BaseRate  decimal.Decimal    `json:"base_rate"`
	BuyRate   decimal.Decimal    `json:"buy_rate"`
	SellRate  decimal.Decimal    `json:"sell_rate"`
	Spread    decimal.Decimal    `json:"spread"`
	SpreadPct float64            `json:"spread_pct"`
	Factors   map[string]float64 `json:"factors"`
	Timestamp time.Time          `json:"timestamp"`
}

// NewSpreadEngine creates a new spread engine with default config.
func NewSpreadEngine() *SpreadEngine {
	se := &SpreadEngine{
		volatility:  make(map[string]float64),
		liquidity:   make(map[string]float64),
		rateHistory: make(map[string][]rateSnapshot),
		config: SpreadConfig{
			BaseSpread:           0.015,
			MinSpread:            0.005,
			MaxSpread:            0.05,
			VolatilityMultiplier: 2.0,
			LiquidityMultiplier:  1.5,
			OffHoursMultiplier:   1.5,
			WeekendMultiplier:    1.3,
			BusinessHoursStart:   8,
			BusinessHoursEnd:     18,
		},
	}

	se.liquidity["EUR"] = 0.95
	se.liquidity["GBP"] = 0.9
	se.liquidity["CNY"] = 0.8
	se.liquidity["JPY"] = 0.85
	se.liquidity["MWK"] = 0.3
	se.liquidity["ZAR"] = 0.5

	go se.startVolatilityTracker()

	return se
}

// CalculateSpread computes the dynamic spread for a currency pair.
func (se *SpreadEngine) CalculateSpread(from, to string, baseRate decimal.Decimal) SpreadResult {
	se.mu.RLock()
	defer se.mu.RUnlock()

	spread := se.config.BaseSpread
	factors := make(map[string]float64)

	// Factor 1: Volatility adjustment
	pair := from + "-" + to
	if vol, ok := se.volatility[pair]; ok && vol > 0.01 {
		volAdjustment := 1 + (vol * se.config.VolatilityMultiplier)
		spread *= volAdjustment
		factors["volatility"] = volAdjustment
	} else {
		factors["volatility"] = 1.0
	}

	// Factor 2: Liquidity adjustment
	fromLiq := se.getLiquidity(from)
	toLiq := se.getLiquidity(to)
	pairLiquidity := (fromLiq + toLiq) / 2
	if pairLiquidity < 0.7 {
		liqAdjustment := 1 + ((1 - pairLiquidity) * se.config.LiquidityMultiplier)
		spread *= liqAdjustment
		factors["liquidity"] = liqAdjustment
	} else {
		factors["liquidity"] = 1.0
	}

	// Factor 3: Time of day adjustment
	now := time.Now()
	hour := now.Hour()
	timeAdjustment := 1.0

	if hour < se.config.BusinessHoursStart || hour >= se.config.BusinessHoursEnd {
		timeAdjustment = se.config.OffHoursMultiplier
		spread *= timeAdjustment
	}
	factors["time_of_day"] = timeAdjustment

	// Factor 4: Weekend adjustment
	weekendAdjustment := 1.0
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		weekendAdjustment = se.config.WeekendMultiplier
		spread *= weekendAdjustment
	}
	factors["weekend"] = weekendAdjustment

	// Factor 5: Exotic pair adjustment
	majorCurrencies := map[string]bool{"EUR": true, "GBP": true, "JPY": true, "CHF": true, "CNY": true}
	if !majorCurrencies[from] || !majorCurrencies[to] {
		exoticAdjustment := 1.2
		spread *= exoticAdjustment
		factors["exotic"] = exoticAdjustment
	} else {
		factors["exotic"] = 1.0
	}

	// Clamp to limits
	if spread < se.config.MinSpread {
		spread = se.config.MinSpread
	}
	if spread > se.config.MaxSpread {
		spread = se.config.MaxSpread
	}

	// Calculate buy/sell rates
	halfSpread := spread / 2
	buyRate := baseRate.Mul(decimal.NewFromFloat(1 + halfSpread))
	sellRate := baseRate.Mul(decimal.NewFromFloat(1 - halfSpread))

	return SpreadResult{
		BaseRate:  baseRate,
		BuyRate:   buyRate,
		SellRate:  sellRate,
		Spread:    decimal.NewFromFloat(spread),
		SpreadPct: spread * 100,
		Factors:   factors,
		Timestamp: now,
	}
}

func (se *SpreadEngine) getLiquidity(currency string) float64 {
	if liq, ok := se.liquidity[currency]; ok {
		return liq
	}
	return 0.5
}

// RecordRate adds a rate observation for volatility calculation.
func (se *SpreadEngine) RecordRate(pair string, rate decimal.Decimal) {
	se.mu.Lock()
	defer se.mu.Unlock()

	history := se.rateHistory[pair]
	history = append(history, rateSnapshot{rate: rate, timestamp: time.Now()})

	if len(history) > 1000 {
		history = history[len(history)-1000:]
	}
	se.rateHistory[pair] = history
}

func (se *SpreadEngine) startVolatilityTracker() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		se.updateVolatility()
	}
}

func (se *SpreadEngine) updateVolatility() {
	se.mu.Lock()
	defer se.mu.Unlock()

	for pair, history := range se.rateHistory {
		if len(history) < 10 {
			continue
		}

		cutoff := time.Now().Add(-1 * time.Hour)
		var recentRates []float64
		for _, h := range history {
			if h.timestamp.After(cutoff) {
				f, _ := h.rate.Float64()
				recentRates = append(recentRates, f)
			}
		}

		if len(recentRates) < 2 {
			continue
		}

		var returns []float64
		for i := 1; i < len(recentRates); i++ {
			if recentRates[i-1] > 0 {
				ret := (recentRates[i] - recentRates[i-1]) / recentRates[i-1]
				returns = append(returns, ret)
			}
		}

		if len(returns) == 0 {
			continue
		}

		var sum float64
		for _, r := range returns {
			sum += r
		}
		mean := sum / float64(len(returns))

		var variance float64
		for _, r := range returns {
			variance += (r - mean) * (r - mean)
		}
		variance /= float64(len(returns))

		se.volatility[pair] = math.Sqrt(variance)
	}
}

// SetLiquidity allows admin to update liquidity levels.
func (se *SpreadEngine) SetLiquidity(currency string, level float64) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.liquidity[currency] = level
}

// GetVolatility returns current volatility for a pair.
func (se *SpreadEngine) GetVolatility(pair string) float64 {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.volatility[pair]
}

// UpdateConfig updates spread configuration.
func (se *SpreadEngine) UpdateConfig(config SpreadConfig) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.config = config
}
