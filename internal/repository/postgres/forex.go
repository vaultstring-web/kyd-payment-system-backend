// ==============================================================================
// FOREX REPOSITORY - internal/repository/postgres/forex.go
// ==============================================================================
package postgres

import (
	"context"

	"github.com/jmoiron/sqlx"
	"kyd/internal/domain"
	"kyd/pkg/errors"
)

type ForexRepository struct {
	db *sqlx.DB
}

func NewForexRepository(db *sqlx.DB) *ForexRepository {
	return &ForexRepository{db: db}
}

func (r *ForexRepository) CreateRate(ctx context.Context, rate *domain.ExchangeRate) error {
	query := `
		INSERT INTO exchange_rates (
			id, base_currency, target_currency, rate, buy_rate, sell_rate,
			source, provider, is_interbank, spread, valid_from, valid_to, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := r.db.ExecContext(ctx, query,
		rate.ID, rate.BaseCurrency, rate.TargetCurrency, rate.Rate,
		rate.BuyRate, rate.SellRate, rate.Source, rate.Provider,
		rate.IsInterbank, rate.Spread, rate.ValidFrom, rate.ValidTo,
		rate.CreatedAt,
	)

	return errors.Wrap(err, "failed to create exchange rate")
}

func (r *ForexRepository) GetLatestRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error) {
	var rate domain.ExchangeRate
	query := `
		SELECT * FROM exchange_rates
		WHERE base_currency = $1 AND target_currency = $2
		AND (valid_to IS NULL OR valid_to > NOW())
		ORDER BY valid_from DESC
		LIMIT 1
	`

	err := r.db.GetContext(ctx, &rate, query, from, to)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest rate")
	}

	return &rate, nil
}

func (r *ForexRepository) GetRateHistory(ctx context.Context, from, to domain.Currency, limit int) ([]*domain.ExchangeRate, error) {
	var rates []*domain.ExchangeRate
	query := `
		SELECT * FROM exchange_rates
		WHERE base_currency = $1 AND target_currency = $2
		ORDER BY valid_from DESC
		LIMIT $3
	`

	err := r.db.SelectContext(ctx, &rates, query, from, to, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get rate history")
	}

	return rates, nil
}
