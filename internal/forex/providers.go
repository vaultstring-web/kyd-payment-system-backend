// ==============================================================================
// FOREX RATE PROVIDERS - internal/forex/providers.go
// ==============================================================================
package forex

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"kyd/internal/domain"
)

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
		"MWK-USD": decimal.NewFromFloat(0.00058),
		"USD-MWK": decimal.NewFromFloat(1724.14),
		"CNY-USD": decimal.NewFromFloat(0.14),
		"USD-CNY": decimal.NewFromFloat(7.14),
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