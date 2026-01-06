// ==============================================================================
// SETTLEMENT REPOSITORY - internal/repository/postgres/settlement.go
// ==============================================================================
package postgres

import (
	"context"
	"database/sql"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type SettlementRepository struct {
	db *sqlx.DB
}

func NewSettlementRepository(db *sqlx.DB) *SettlementRepository {
	return &SettlementRepository{db: db}
}

func (r *SettlementRepository) Create(ctx context.Context, settlement *domain.Settlement) error {
	query := `
		INSERT INTO customer_schema.settlements (
			id, batch_reference, network, transaction_hash, source_account,
			destination_account, total_amount, currency, fee_amount, fee_currency,
			status, submission_count, last_submitted_at, confirmed_at, completed_at,
			metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
	`

	_, err := r.db.ExecContext(ctx, query,
		settlement.ID, settlement.BatchReference, settlement.Network,
		settlement.TransactionHash, settlement.SourceAccount, settlement.DestinationAccount,
		settlement.TotalAmount, settlement.Currency, settlement.FeeAmount, settlement.FeeCurrency,
		settlement.Status, settlement.SubmissionCount, settlement.LastSubmittedAt,
		settlement.ConfirmedAt, settlement.CompletedAt, settlement.Metadata,
		settlement.CreatedAt, settlement.UpdatedAt,
	)

	return errors.Wrap(err, "failed to create settlement")
}

func (r *SettlementRepository) Update(ctx context.Context, settlement *domain.Settlement) error {
	query := `
		UPDATE customer_schema.settlements SET
			transaction_hash = $1, status = $2, submission_count = $3,
			last_submitted_at = $4, confirmed_at = $5, completed_at = $6,
			metadata = $7, updated_at = $8
		WHERE id = $9
	`

	_, err := r.db.ExecContext(ctx, query,
		settlement.TransactionHash, settlement.Status, settlement.SubmissionCount,
		settlement.LastSubmittedAt, settlement.ConfirmedAt, settlement.CompletedAt,
		settlement.Metadata, settlement.UpdatedAt, settlement.ID,
	)

	return errors.Wrap(err, "failed to update settlement")
}

func (r *SettlementRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Settlement, error) {
	var settlement domain.Settlement
	query := `SELECT * FROM customer_schema.settlements WHERE id = $1`

	err := r.db.GetContext(ctx, &settlement, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrSettlementNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find settlement")
	}

	return &settlement, nil
}
