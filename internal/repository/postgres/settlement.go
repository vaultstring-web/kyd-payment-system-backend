// ==============================================================================
// SETTLEMENT REPOSITORY - internal/repository/postgres/settlement.go
// ==============================================================================
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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
			reconciliation_id, metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		)
	`

	_, err := r.db.ExecContext(ctx, query,
		settlement.ID, settlement.BatchReference, settlement.Network,
		settlement.TransactionHash, settlement.SourceAccount, settlement.DestinationAccount,
		settlement.TotalAmount, settlement.Currency, settlement.FeeAmount, settlement.FeeCurrency,
		settlement.Status, settlement.SubmissionCount, settlement.LastSubmittedAt,
		settlement.ConfirmedAt, settlement.CompletedAt, settlement.ReconciliationID,
		settlement.Metadata, settlement.CreatedAt, settlement.UpdatedAt,
	)

	return errors.Wrap(err, "failed to create settlement")
}

func (r *SettlementRepository) Update(ctx context.Context, settlement *domain.Settlement) error {
	query := `
		UPDATE customer_schema.settlements SET
			transaction_hash = $1, status = $2, submission_count = $3,
			last_submitted_at = $4, confirmed_at = $5, completed_at = $6,
			reconciliation_id = $7, metadata = $8, updated_at = $9
		WHERE id = $10
	`

	_, err := r.db.ExecContext(ctx, query,
		settlement.TransactionHash, settlement.Status, settlement.SubmissionCount,
		settlement.LastSubmittedAt, settlement.ConfirmedAt, settlement.CompletedAt,
		settlement.ReconciliationID, settlement.Metadata, settlement.UpdatedAt,
		settlement.ID,
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

func (r *SettlementRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.Settlement, error) {
	var settlements []*domain.Settlement
	query := `
		SELECT 
			id, batch_reference, network, transaction_hash, source_account,
			destination_account, total_amount, currency, fee_amount, fee_currency,
			status, submission_count, last_submitted_at, confirmed_at, completed_at,
			reconciliation_id, metadata, created_at, updated_at
		FROM customer_schema.settlements
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	err := r.db.SelectContext(ctx, &settlements, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list settlements")
	}
	return settlements, nil
}

func (r *SettlementRepository) CountAll(ctx context.Context) (int, error) {
	var total int
	query := `SELECT COUNT(*) FROM customer_schema.settlements`
	err := r.db.GetContext(ctx, &total, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count settlements")
	}
	return total, nil
}

func (r *SettlementRepository) FindAllWithFilters(ctx context.Context, limit, offset int, status string, currency string, network string) ([]*domain.Settlement, error) {
	var settlements []*domain.Settlement
	query := `
		SELECT 
			id, batch_reference, network, transaction_hash, source_account,
			destination_account, total_amount, currency, fee_amount, fee_currency,
			status, submission_count, last_submitted_at, confirmed_at, completed_at,
			reconciliation_id, metadata, created_at, updated_at
		FROM customer_schema.settlements
	`

	var (
		clauses []string
		args    []interface{}
	)

	if strings.TrimSpace(status) != "" {
		args = append(args, strings.TrimSpace(status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(currency) != "" {
		args = append(args, strings.TrimSpace(currency))
		clauses = append(clauses, fmt.Sprintf("currency = $%d", len(args)))
	}
	if strings.TrimSpace(network) != "" {
		args = append(args, strings.TrimSpace(network))
		clauses = append(clauses, fmt.Sprintf("network = $%d", len(args)))
	}

	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	query += ` ORDER BY created_at DESC LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2)
	args = append(args, limit, offset)

	err := r.db.SelectContext(ctx, &settlements, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list settlements")
	}
	return settlements, nil
}

func (r *SettlementRepository) CountAllWithFilters(ctx context.Context, status string, currency string, network string) (int, error) {
	var total int
	query := `SELECT COUNT(*) FROM customer_schema.settlements`

	var (
		clauses []string
		args    []interface{}
	)

	if strings.TrimSpace(status) != "" {
		args = append(args, strings.TrimSpace(status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(currency) != "" {
		args = append(args, strings.TrimSpace(currency))
		clauses = append(clauses, fmt.Sprintf("currency = $%d", len(args)))
	}
	if strings.TrimSpace(network) != "" {
		args = append(args, strings.TrimSpace(network))
		clauses = append(clauses, fmt.Sprintf("network = $%d", len(args)))
	}

	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	err := r.db.GetContext(ctx, &total, query, args...)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count settlements")
	}
	return total, nil
}

func (r *SettlementRepository) FindSubmitted(ctx context.Context) ([]*domain.Settlement, error) {
	var settlements []*domain.Settlement
	query := `
		SELECT 
			id, batch_reference, network, transaction_hash, source_account,
			destination_account, total_amount, currency, fee_amount, fee_currency,
			status, submission_count, last_submitted_at, confirmed_at, completed_at,
			metadata, created_at, updated_at
		FROM customer_schema.settlements
		WHERE status = $1
	`
	err := r.db.SelectContext(ctx, &settlements, query, domain.SettlementStatusSubmitted)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find submitted settlements")
	}
	return settlements, nil
}
