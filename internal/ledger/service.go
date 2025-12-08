// ==============================================================================
// LEDGER SERVICE - internal/ledger/service.go
// ==============================================================================
package ledger

import (
	"context"
	"database/sql"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// PostTransaction performs double-entry bookkeeping atomically
func (s *Service) PostTransaction(ctx context.Context, posting *LedgerPosting) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock wallets in deterministic order to prevent deadlocks
	walletIDs := []uuid.UUID{posting.DebitWalletID, posting.CreditWalletID}
	if posting.DebitWalletID.String() > posting.CreditWalletID.String() {
		walletIDs = []uuid.UUID{posting.CreditWalletID, posting.DebitWalletID}
	}

	for _, walletID := range walletIDs {
		var balance decimal.Decimal
		err := tx.QueryRowContext(ctx,
			`SELECT available_balance FROM wallets WHERE id = $1 FOR UPDATE`,
			walletID,
		).Scan(&balance)
		if err != nil {
			return err
		}
	}

	// Debit sender wallet
	result, err := tx.ExecContext(ctx, `
		UPDATE wallets 
		SET 
			available_balance = available_balance - $1,
			ledger_balance = ledger_balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND available_balance >= $1
	`, posting.DebitAmount, posting.DebitWalletID)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.ErrInsufficientBalance
	}

	// Credit receiver wallet
	_, err = tx.ExecContext(ctx, `
		UPDATE wallets 
		SET 
			available_balance = available_balance + $1,
			ledger_balance = ledger_balance + $1,
			last_transaction_at = NOW(),
			updated_at = NOW()
		WHERE id = $2
	`, posting.CreditAmount, posting.CreditWalletID)

	if err != nil {
		return err
	}

	// Create ledger entries (immutable audit trail)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (
			id, transaction_id, wallet_id, entry_type, 
			amount, currency, balance_after, created_at
		) VALUES 
		($1, $2, $3, 'debit', $4, $5, 
			(SELECT available_balance FROM wallets WHERE id = $3), NOW()),
		($6, $2, $7, 'credit', $8, $9, 
			(SELECT available_balance FROM wallets WHERE id = $7), NOW())
	`,
		uuid.New(), posting.TransactionID, posting.DebitWalletID,
		posting.DebitAmount, posting.Currency,
		uuid.New(), posting.TransactionID, posting.CreditWalletID,
		posting.CreditAmount, posting.ConvertedCurrency,
	)

	if err != nil {
		return err
	}

	return tx.Commit()
}

type LedgerPosting struct {
	TransactionID     uuid.UUID
	DebitWalletID     uuid.UUID
	CreditWalletID    uuid.UUID
	DebitAmount       decimal.Decimal
	CreditAmount      decimal.Decimal
	Currency          domain.Currency
	ConvertedCurrency domain.Currency
	ExchangeRate      decimal.Decimal
	FeeAmount         decimal.Decimal
}

// ==============================================================================
// CONTINUE IN NEXT MESSAGE - HTTP Handlers, Repositories, Docker, Kubernetes
// ==============================================================================
