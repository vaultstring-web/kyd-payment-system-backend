// Package forex implements FX rate retrieval, caching, and conversion services.
//
// ==============================================================================
// FOREX SERVICE - internal/forex/service.go
// ==============================================================================
package forex

import (
	"context"
	"fmt"
	"sync"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Service provides forex rate retrieval, caching, and conversion.
type Service struct {
	repo         Repository
	cache        RateCache
	providers    []RateProvider
	logger       logger.Logger
	mu           sync.RWMutex
	rateCache    map[string]*domain.ExchangeRate
	spreadEngine *SpreadEngine
}

// NewService constructs a forex Service with repository, cache, providers, and logger.
func NewService(repo Repository, cache RateCache, providers []RateProvider, log logger.Logger) *Service {
	s := &Service{
		repo:         repo,
		cache:        cache,
		providers:    providers,
		logger:       log,
		rateCache:    make(map[string]*domain.ExchangeRate),
		spreadEngine: NewSpreadEngine(),
	}

	// Adjust liquidity levels for MWK to improve conversions
	s.spreadEngine.SetLiquidity("MWK", 0.6)

	// Start rate update worker
	go s.startRateUpdater()

	return s
}

// GetRate retrieves the current exchange rate
func (s *Service) GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error) {
	if from == to {
		return &domain.ExchangeRate{
			BaseCurrency:   from,
			TargetCurrency: to,
			Rate:           decimal.NewFromInt(1),
			BuyRate:        decimal.NewFromInt(1),
			SellRate:       decimal.NewFromInt(1),
			ValidFrom:      time.Now(),
		}, nil
	}

	// Try cache first (In-Memory)
	key := fmt.Sprintf("%s-%s", from, to)
	s.mu.RLock()
	if rate, ok := s.rateCache[key]; ok {
		if rate.ValidTo == nil || rate.ValidTo.After(time.Now()) {
			s.mu.RUnlock()
			return rate, nil
		}
	}
	s.mu.RUnlock()

	// Try Distributed Cache (Redis)
	if s.cache != nil {
		if rate, err := s.cache.Get(key); err == nil {
			if rate.ValidTo == nil || rate.ValidTo.After(time.Now()) {
				// Populate in-memory cache for next time
				s.mu.Lock()
				s.rateCache[key] = rate
				s.mu.Unlock()
				return rate, nil
			}
		}
	}

	// Try database
	rate, err := s.repo.GetLatestRate(ctx, from, to)
	if err == nil && (rate.ValidTo == nil || rate.ValidTo.After(time.Now())) {
		s.updateCache(key, rate)
		return rate, nil
	}

	// Fetch from providers
	return s.fetchAndStoreRate(ctx, from, to)
}

func (s *Service) fetchAndStoreRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error) {
	for _, provider := range s.providers {
		rate, err := provider.GetRate(ctx, from, to)
		if err != nil {
			s.logger.Warn("Provider failed", map[string]interface{}{
				"provider": provider.Name(),
				"from":     from,
				"to":       to,
				"error":    err.Error(),
			})
			continue
		}

		// Apply dynamic spread using spread engine
		spreadRes := s.spreadEngine.CalculateSpread(string(from), string(to), rate.Rate)
		rate.BuyRate = spreadRes.BuyRate
		rate.SellRate = spreadRes.SellRate
		rate.Spread = spreadRes.Spread
		rate.ID = uuid.New()
		rate.CreatedAt = time.Now()

		// Store in database
		if err := s.repo.CreateRate(ctx, rate); err != nil {
			s.logger.Error("Failed to store rate", map[string]interface{}{"error": err.Error()})
		}

		// Update cache
		key := fmt.Sprintf("%s-%s", from, to)
		s.updateCache(key, rate)

		return rate, nil
	}

	return nil, errors.ErrRateNotAvailable
}

// GetHistory retrieves historical exchange rates for a currency pair.
func (s *Service) GetHistory(ctx context.Context, from, to domain.Currency, limit int) ([]*domain.ExchangeRate, error) {
	return s.repo.GetRateHistory(ctx, from, to, limit)
}

func (s *Service) updateCache(key string, rate *domain.ExchangeRate) {
	s.mu.Lock()
	s.rateCache[key] = rate
	s.mu.Unlock()
}

func (s *Service) startRateUpdater() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.updateAllRates()
	}
}

func (s *Service) updateAllRates() {
	ctx := context.Background()
	pairs := []struct{ from, to domain.Currency }{
		{domain.MWK, domain.CNY},
		{domain.CNY, domain.MWK},
	}

	for _, pair := range pairs {
		_, err := s.fetchAndStoreRate(ctx, pair.from, pair.to)
		if err != nil {
			s.logger.Error("Failed to update rate", map[string]interface{}{
				"from":  pair.from,
				"to":    pair.to,
				"error": err.Error(),
			})
		}
	}
}

// CalculateRequest represents a conversion request payload.
type CalculateRequest struct {
	Amount float64         `json:"amount" validate:"required,gt=0"`
	From   domain.Currency `json:"from" validate:"required"`
	To     domain.Currency `json:"to" validate:"required"`
}

// CalculateResponse represents the conversion result payload.
type CalculateResponse struct {
	SourceAmount      decimal.Decimal `json:"source_amount"`
	SourceCurrency    domain.Currency `json:"source_currency"`
	ConvertedAmount   decimal.Decimal `json:"converted_amount"`
	ConvertedCurrency domain.Currency `json:"converted_currency"`
	Rate              decimal.Decimal `json:"rate"`
	FeeAmount         decimal.Decimal `json:"fee_amount"`
	TotalAmount       decimal.Decimal `json:"total_amount"`
}

// Calculate performs a currency conversion using the latest sell rate.
func (s *Service) Calculate(ctx context.Context, req *CalculateRequest) (*CalculateResponse, error) {
	rate, err := s.GetRate(ctx, req.From, req.To)
	if err != nil {
		return nil, err
	}

	amountDec := decimal.NewFromFloat(req.Amount)
	convertedAmount := amountDec.Mul(rate.SellRate)
	feeAmount := amountDec.Mul(decimal.NewFromFloat(0.015))
	totalAmount := amountDec.Add(feeAmount)

	return &CalculateResponse{
		SourceAmount:      amountDec,
		SourceCurrency:    req.From,
		ConvertedAmount:   convertedAmount,
		ConvertedCurrency: req.To,
		Rate:              rate.SellRate,
		FeeAmount:         feeAmount,
		TotalAmount:       totalAmount,
	}, nil
}

// Repository defines persistence operations for forex rates.
type Repository interface {
	CreateRate(ctx context.Context, rate *domain.ExchangeRate) error
	GetLatestRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error)
	GetRateHistory(ctx context.Context, from, to domain.Currency, limit int) ([]*domain.ExchangeRate, error)
}

// RateCache defines cache operations for forex rates.
type RateCache interface {
	Get(key string) (*domain.ExchangeRate, error)
	Set(key string, rate *domain.ExchangeRate, ttl time.Duration) error
}

// RateProvider supplies external forex rates.
type RateProvider interface {
	Name() string
	GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error)
}

// ==============================================================================
// CONTINUE IN NEXT MESSAGE - Blockchain Integration, Repositories, HTTP Handlers
// ==============================================================================
