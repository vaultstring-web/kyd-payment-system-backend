package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type TransactionRepository struct {
	db *sqlx.DB
}

func NewTransactionRepository(db *sqlx.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

func (r *TransactionRepository) Create(ctx context.Context, tx *domain.Transaction) error {
	query := `
        INSERT INTO customer_schema.transactions (
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, fee_currency, net_amount, status, status_reason, transaction_type,
            channel, category, description, metadata, blockchain_tx_hash,
            settlement_id, initiated_at, completed_at, created_at, updated_at
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
            $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27
        )
    `

	_, err := r.db.ExecContext(ctx, query,
		tx.ID, tx.Reference, tx.SenderID, tx.ReceiverID, tx.SenderWalletID, tx.ReceiverWalletID,
		tx.Amount, tx.Currency, tx.ExchangeRate, tx.ConvertedAmount, tx.ConvertedCurrency,
		tx.FeeAmount, tx.FeeCurrency, tx.NetAmount, tx.Status, tx.StatusReason, tx.TransactionType,
		tx.Channel, tx.Category, tx.Description, tx.Metadata, tx.BlockchainTxHash,
		tx.SettlementID, tx.InitiatedAt, tx.CompletedAt, tx.CreatedAt, tx.UpdatedAt,
	)

	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" { // unique_violation
			if strings.Contains(pqErr.Constraint, "reference") || strings.Contains(pqErr.Message, "reference") {
				return errors.ErrTransactionAlreadyExists
			}
		}
		return errors.Wrap(err, "failed to create transaction")
	}

	return nil
}

func (r *TransactionRepository) Update(ctx context.Context, tx *domain.Transaction) error {
	query := `
		UPDATE customer_schema.transactions SET
			status = $1, status_reason = $2, blockchain_tx_hash = $3,
			settlement_id = $4, completed_at = $5, updated_at = $6,
			description = $7
		WHERE id = $8
	`

	_, err := r.db.ExecContext(ctx, query,
		tx.Status, tx.StatusReason, tx.BlockchainTxHash,
		tx.SettlementID, tx.CompletedAt, tx.UpdatedAt, tx.Description,
		tx.ID,
	)

	return errors.Wrap(err, "failed to update transaction")
}

func (r *TransactionRepository) Flag(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if tx.Metadata == nil {
		tx.Metadata = make(domain.Metadata)
	}
	tx.Metadata["flagged"] = true
	tx.Metadata["flag_reason"] = reason
	tx.Metadata["flagged_at"] = time.Now()

	query := `UPDATE customer_schema.transactions SET metadata = $1, updated_at = NOW() WHERE id = $2`
	_, err = r.db.ExecContext(ctx, query, tx.Metadata, id)
	return errors.Wrap(err, "failed to flag transaction")
}

func (r *TransactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	var tx domain.Transaction
	query := `
		SELECT 
			id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
			amount, currency, exchange_rate, converted_amount, converted_currency,
			fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
			status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
			metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
			created_at, updated_at
		FROM customer_schema.transactions WHERE id = $1
	`

	err := r.db.GetContext(ctx, &tx, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrTransactionNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transaction")
	}

	return &tx, nil
}

func (r *TransactionRepository) FindByReference(ctx context.Context, ref string) (*domain.Transaction, error) {
	var tx domain.Transaction
	query := `
		SELECT 
			id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
			amount, currency, exchange_rate, converted_amount, converted_currency,
			fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
			status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
			metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
			created_at, updated_at
		FROM customer_schema.transactions WHERE reference = $1
	`

	err := r.db.GetContext(ctx, &tx, query, ref)
	if err == sql.ErrNoRows {
		return nil, errors.ErrTransactionNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transaction")
	}

	return &tx, nil
}

func (r *TransactionRepository) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
		SELECT 
			id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
			amount, currency, exchange_rate, converted_amount, converted_currency,
			fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
			status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
			metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
			created_at, updated_at
		FROM customer_schema.transactions 
		WHERE sender_id = $1 OR receiver_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	err := r.db.SelectContext(ctx, &txs, query, userID, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transactions")
	}

	return txs, nil
}

func (r *TransactionRepository) GetTransactionVolume(ctx context.Context, months int) ([]*domain.TransactionVolume, error) {
	var volumes []*domain.TransactionVolume
	query := `
        SELECT
            TO_CHAR(created_at, 'Mon') as period,
            COALESCE(SUM(CASE WHEN currency = 'CNY' THEN amount ELSE 0 END), 0) as cny,
            COALESCE(SUM(CASE WHEN currency = 'MWK' THEN amount ELSE 0 END), 0) as mwk,
            COALESCE(SUM(CASE WHEN currency = 'ZMW' THEN amount ELSE 0 END), 0) as zmw,
            COALESCE(SUM(converted_amount), 0) as total
        FROM customer_schema.transactions
        WHERE created_at >= NOW() - ($1 || ' months')::INTERVAL
        GROUP BY TO_CHAR(created_at, 'Mon'), DATE_TRUNC('month', created_at)
        ORDER BY DATE_TRUNC('month', created_at) ASC
    `

	err := r.db.SelectContext(ctx, &volumes, query, months)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get transaction volume")
	}

	return volumes, nil
}

func (r *TransactionRepository) GetSystemStats(ctx context.Context) (*domain.SystemStats, error) {
	var stats domain.SystemStats
	query := `
        SELECT
            COUNT(*) as total_transactions,
            COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0) as completed,
            COALESCE(SUM(CASE WHEN status IN ('pending', 'processing', 'settling') THEN 1 ELSE 0 END), 0) as pending,
            COALESCE(SUM(CASE WHEN status = 'pending_approval' THEN 1 ELSE 0 END), 0) as pending_approvals,
            COALESCE(SUM(CASE WHEN status = 'flagged' THEN 1 ELSE 0 END), 0) as flagged,
            COALESCE(SUM(converted_amount), 0) as total_volume,
            COALESCE(SUM(fee_amount), 0) as total_fees,
            (SELECT COUNT(DISTINCT user_id) FROM (SELECT sender_id as user_id FROM customer_schema.transactions UNION SELECT receiver_id FROM customer_schema.transactions) as u) as active_users
        FROM customer_schema.transactions
    `

	err := r.db.GetContext(ctx, &stats, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get system stats")
	}

	return &stats, nil
}

func (r *TransactionRepository) FindByStatus(ctx context.Context, status domain.TransactionStatus, limit, offset int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
			status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
			metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
			created_at, updated_at
		FROM customer_schema.transactions 
		WHERE status = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3
    `

	err := r.db.SelectContext(ctx, &txs, query, status, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transactions by status")
	}

	return txs, nil
}

func (r *TransactionRepository) CountByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	var total int
	query := `
        SELECT COUNT(*) 
        FROM customer_schema.transactions 
        WHERE sender_id = $1 OR receiver_id = $1
    `
	err := r.db.GetContext(ctx, &total, query, userID)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count transactions")
	}
	return total, nil
}

func (r *TransactionRepository) FindFlagged(ctx context.Context, limit, offset int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
            metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
            created_at, updated_at
        FROM customer_schema.transactions 
        WHERE metadata->>'flagged' = 'true'
        ORDER BY created_at DESC
        LIMIT $1 OFFSET $2
    `
	err := r.db.SelectContext(ctx, &txs, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find flagged transactions")
	}
	return txs, nil
}

func (r *TransactionRepository) FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
            metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
            created_at, updated_at
        FROM customer_schema.transactions 
        WHERE sender_wallet_id = $1 OR receiver_wallet_id = $1
        ORDER BY created_at DESC
        LIMIT $2 OFFSET $3
    `

	err := r.db.SelectContext(ctx, &txs, query, walletID, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transactions")
	}

	return txs, nil
}

func (r *TransactionRepository) FindStuckPending(ctx context.Context, olderThan time.Duration, limit int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
            metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
            created_at, updated_at
        FROM customer_schema.transactions 
        WHERE status = 'pending' AND created_at < NOW() - $1::INTERVAL
        ORDER BY created_at ASC
        LIMIT $2
    `
	// Convert duration to postgres interval string
	interval := fmt.Sprintf("%d seconds", int(olderThan.Seconds()))

	err := r.db.SelectContext(ctx, &txs, query, interval, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find stuck pending transactions")
	}

	return txs, nil
}

func (r *TransactionRepository) CountByWalletID(ctx context.Context, walletID uuid.UUID) (int, error) {
	var total int
	query := `
        SELECT COUNT(*) 
        FROM customer_schema.transactions 
        WHERE sender_wallet_id = $1 OR receiver_wallet_id = $1
    `
	err := r.db.GetContext(ctx, &total, query, walletID)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count transactions")
	}
	return total, nil
}

func (r *TransactionRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
            metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
            created_at, updated_at
        FROM customer_schema.transactions 
        ORDER BY created_at DESC
        LIMIT $1 OFFSET $2
    `
	err := r.db.SelectContext(ctx, &txs, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transactions")
	}
	return txs, nil
}

func (r *TransactionRepository) CountAll(ctx context.Context) (int, error) {
	var total int
	query := `SELECT COUNT(*) FROM customer_schema.transactions`
	err := r.db.GetContext(ctx, &total, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count transactions")
	}
	return total, nil
}

func (r *TransactionRepository) FindPendingSettlement(ctx context.Context, limit int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
            metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
            created_at, updated_at
        FROM customer_schema.transactions 
        WHERE status = 'pending_settlement' AND settlement_id IS NULL
        ORDER BY completed_at ASC
        LIMIT $1
    `

	err := r.db.SelectContext(ctx, &txs, query, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find pending settlements")
	}

	return txs, nil
}

func (r *TransactionRepository) FindBySettlementID(ctx context.Context, settlementID uuid.UUID) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
		SELECT 
			id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
			amount, currency, exchange_rate, converted_amount, converted_currency,
			fee_amount, COALESCE(fee_currency, '') AS fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
			status, COALESCE(status_reason, '') AS status_reason, transaction_type, COALESCE(channel, '') AS channel, COALESCE(category, '') AS category, COALESCE(description, '') AS description,
			metadata, COALESCE(blockchain_tx_hash, '') AS blockchain_tx_hash, settlement_id, initiated_at, completed_at,
			created_at, updated_at
		FROM customer_schema.transactions WHERE settlement_id = $1
	`

	err := r.db.SelectContext(ctx, &txs, query, settlementID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transactions")
	}

	return txs, nil
}

func (r *TransactionRepository) BatchUpdateSettlementID(ctx context.Context, txIDs []uuid.UUID, settlementID uuid.UUID) error {
	query, args, err := sqlx.In(`
		UPDATE customer_schema.transactions 
		SET settlement_id = ?, status = 'settling', updated_at = NOW() 
		WHERE id IN (?)`, settlementID, txIDs)
	if err != nil {
		return errors.Wrap(err, "failed to build batch update query")
	}

	query = r.db.Rebind(query)
	_, err = r.db.ExecContext(ctx, query, args...)
	return errors.Wrap(err, "failed to batch update transactions")
}

func (r *TransactionRepository) DeleteByWalletID(ctx context.Context, walletID uuid.UUID) error {
	query := `DELETE FROM customer_schema.transactions WHERE sender_wallet_id = $1 OR receiver_wallet_id = $1`
	_, err := r.db.ExecContext(ctx, query, walletID)
	return errors.Wrap(err, "failed to delete transactions by wallet")
}

func (r *TransactionRepository) GetDailyTotal(ctx context.Context, userID uuid.UUID, currency domain.Currency) (decimal.Decimal, error) {
	var total decimal.NullDecimal
	query := `
        SELECT SUM(amount) 
        FROM customer_schema.transactions 
        WHERE sender_id = $1 
          AND currency = $2
          AND created_at > NOW() - INTERVAL '24 hours' 
          AND status != 'failed' 
          AND status != 'cancelled'
    `
	err := r.db.GetContext(ctx, &total, query, userID, currency)
	if err != nil {
		return decimal.Zero, errors.Wrap(err, "failed to get daily total")
	}
	if !total.Valid {
		return decimal.Zero, nil
	}
	return total.Decimal, nil
}

func (r *TransactionRepository) GetHourlyHighValueCount(ctx context.Context, userID uuid.UUID, threshold decimal.Decimal) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) 
        FROM customer_schema.transactions 
        WHERE sender_id = $1 
          AND amount > $2
          AND created_at > NOW() - INTERVAL '1 hour' 
          AND status != 'failed' 
          AND status != 'cancelled'
    `
	err := r.db.GetContext(ctx, &count, query, userID, threshold)
	if err != nil {
		return 0, errors.Wrap(err, "failed to get hourly high value count")
	}
	return count, nil
}

func (r *TransactionRepository) GetHourlyCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	query := `
        SELECT COUNT(*) 
        FROM customer_schema.transactions 
        WHERE sender_id = $1 
          AND created_at > NOW() - INTERVAL '1 hour' 
          AND status != 'failed' 
          AND status != 'cancelled'
    `
	err := r.db.GetContext(ctx, &count, query, userID)
	if err != nil {
		return 0, errors.Wrap(err, "failed to get hourly count")
	}
	return count, nil
}

func (r *TransactionRepository) SumVolume(ctx context.Context) (decimal.Decimal, error) {
	var total decimal.Decimal
	// Sum converted_amount for simplicity, or amount if currency matches base
	// Assuming converted_amount is in base currency (MWK)
	query := `SELECT COALESCE(SUM(converted_amount), 0) FROM customer_schema.transactions WHERE status = 'completed'`
	err := r.db.GetContext(ctx, &total, query)
	if err != nil {
		return decimal.Zero, errors.Wrap(err, "failed to sum transaction volume")
	}
	return total, nil
}

func (r *TransactionRepository) SumEarnings(ctx context.Context) (decimal.Decimal, error) {
	var total decimal.Decimal
	// Sum fee_amount
	query := `SELECT COALESCE(SUM(fee_amount), 0) FROM customer_schema.transactions WHERE status = 'completed'`
	err := r.db.GetContext(ctx, &total, query)
	if err != nil {
		return decimal.Zero, errors.Wrap(err, "failed to sum earnings")
	}
	return total, nil
}

func (r *TransactionRepository) CountByStatus(ctx context.Context, status domain.TransactionStatus) (int, error) {
	var total int
	query := `SELECT COUNT(*) FROM customer_schema.transactions WHERE status = $1`
	err := r.db.GetContext(ctx, &total, query, status)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count transactions by status")
	}
	return total, nil
}
