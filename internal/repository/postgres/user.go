// ==============================================================================
// POSTGRESQL REPOSITORIES - internal/repository/postgres/
// ==============================================================================

// USER REPOSITORY - internal/repository/postgres/user.go
package postgres

import (
	"context"
	"database/sql"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
)

type UserRepository struct {
	db *sqlx.DB
}

func NewUserRepository(db *sqlx.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	query := `
		INSERT INTO users (
			id, email, phone, password_hash, first_name, last_name,
			user_type, kyc_level, kyc_status, country_code, date_of_birth,
			business_name, risk_score, is_active, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
	`

	_, err := r.db.ExecContext(ctx, query,
		user.ID, user.Email, user.Phone, user.PasswordHash, user.FirstName, user.LastName,
		user.UserType, user.KYCLevel, user.KYCStatus, user.CountryCode, user.DateOfBirth,
		user.BusinessName, user.RiskScore, user.IsActive, user.CreatedAt, user.UpdatedAt,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create user")
	}

	return nil
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var user domain.User
	query := `SELECT * FROM users WHERE id = $1`

	err := r.db.GetContext(ctx, &user, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	return &user, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	query := `SELECT * FROM users WHERE email = $1`

	err := r.db.GetContext(ctx, &user, query, email)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	return &user, nil
}

func (r *UserRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`

	err := r.db.GetContext(ctx, &exists, query, email)
	if err != nil {
		return false, errors.Wrap(err, "failed to check user existence")
	}

	return exists, nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	query := `
		UPDATE users SET
			email = $1, phone = $2, first_name = $3, last_name = $4,
			user_type = $5, kyc_level = $6, kyc_status = $7, last_login = $8,
			updated_at = $9
		WHERE id = $10
	`

	_, err := r.db.ExecContext(ctx, query,
		user.Email, user.Phone, user.FirstName, user.LastName,
		user.UserType, user.KYCLevel, user.KYCStatus, user.LastLogin,
		user.UpdatedAt, user.ID,
	)

	return errors.Wrap(err, "failed to update user")
}

// WALLET REPOSITORY - internal/repository/postgres/wallet.go
type WalletRepository struct {
	db *sqlx.DB
}

func NewWalletRepository(db *sqlx.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	query := `
		INSERT INTO wallets (
			id, user_id, wallet_address, currency, available_balance,
			ledger_balance, reserved_balance, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.ExecContext(ctx, query,
		wallet.ID, wallet.UserID, wallet.WalletAddress, wallet.Currency,
		wallet.AvailableBalance, wallet.LedgerBalance, wallet.ReservedBalance,
		wallet.Status, wallet.CreatedAt, wallet.UpdatedAt,
	)

	return errors.Wrap(err, "failed to create wallet")
}

func (r *WalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	var wallet domain.Wallet
	query := `SELECT * FROM wallets WHERE id = $1`

	err := r.db.GetContext(ctx, &wallet, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrWalletNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallet")
	}

	return &wallet, nil
}

func (r *WalletRepository) FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
	var wallets []*domain.Wallet
	query := `SELECT * FROM wallets WHERE user_id = $1 ORDER BY created_at DESC`

	err := r.db.SelectContext(ctx, &wallets, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallets")
	}

	return wallets, nil
}

func (r *WalletRepository) FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error) {
	var wallet domain.Wallet
	query := `SELECT * FROM wallets WHERE user_id = $1 AND currency = $2`

	err := r.db.GetContext(ctx, &wallet, query, userID, currency)
	if err == sql.ErrNoRows {
		return nil, errors.ErrWalletNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallet")
	}

	return &wallet, nil
}

func (r *WalletRepository) DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	query := `
		UPDATE wallets 
		SET available_balance = available_balance - $1,
		    ledger_balance = ledger_balance - $1,
		    updated_at = NOW()
		WHERE id = $2 AND available_balance >= $1
	`

	result, err := r.db.ExecContext(ctx, query, amount, walletID)
	if err != nil {
		return errors.Wrap(err, "failed to debit wallet")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.ErrInsufficientBalance
	}

	return nil
}

func (r *WalletRepository) CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	query := `
		UPDATE wallets 
		SET available_balance = available_balance + $1,
		    ledger_balance = ledger_balance + $1,
		    last_transaction_at = NOW(),
		    updated_at = NOW()
		WHERE id = $2
	`

	_, err := r.db.ExecContext(ctx, query, amount, walletID)
	return errors.Wrap(err, "failed to credit wallet")
}

func (r *WalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	query := `
		UPDATE wallets SET
			available_balance = $1,
			ledger_balance = $2,
			reserved_balance = $3,
			status = $4,
			updated_at = $5
		WHERE id = $6
	`

	_, err := r.db.ExecContext(ctx, query,
		wallet.AvailableBalance, wallet.LedgerBalance, wallet.ReservedBalance,
		wallet.Status, wallet.UpdatedAt, wallet.ID,
	)

	return errors.Wrap(err, "failed to update wallet")
}

// TRANSACTION REPOSITORY - internal/repository/postgres/transaction.go
type TransactionRepository struct {
	db *sqlx.DB
}

func NewTransactionRepository(db *sqlx.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

func (r *TransactionRepository) Create(ctx context.Context, tx *domain.Transaction) error {
	query := `
		INSERT INTO transactions (
			id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
			amount, currency, exchange_rate, converted_amount, converted_currency,
			fee_amount, fee_currency, status, status_reason, transaction_type,
			channel, category, description, metadata, blockchain_tx_hash,
			settlement_id, initiated_at, completed_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26
		)
	`

	_, err := r.db.ExecContext(ctx, query,
		tx.ID, tx.Reference, tx.SenderID, tx.ReceiverID, tx.SenderWalletID, tx.ReceiverWalletID,
		tx.Amount, tx.Currency, tx.ExchangeRate, tx.ConvertedAmount, tx.ConvertedCurrency,
		tx.FeeAmount, tx.FeeCurrency, tx.Status, tx.StatusReason, tx.TransactionType,
		tx.Channel, tx.Category, tx.Description, tx.Metadata, tx.BlockchainTxHash,
		tx.SettlementID, tx.InitiatedAt, tx.CompletedAt, tx.CreatedAt, tx.UpdatedAt,
	)

	return errors.Wrap(err, "failed to create transaction")
}

func (r *TransactionRepository) Update(ctx context.Context, tx *domain.Transaction) error {
	query := `
		UPDATE transactions SET
			status = $1, status_reason = $2, blockchain_tx_hash = $3,
			settlement_id = $4, completed_at = $5, updated_at = $6
		WHERE id = $7
	`

	_, err := r.db.ExecContext(ctx, query,
		tx.Status, tx.StatusReason, tx.BlockchainTxHash,
		tx.SettlementID, tx.CompletedAt, tx.UpdatedAt, tx.ID,
	)

	return errors.Wrap(err, "failed to update transaction")
}

func (r *TransactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	var tx domain.Transaction
	query := `SELECT * FROM transactions WHERE id = $1`

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
	query := `SELECT * FROM transactions WHERE reference = $1`

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
		SELECT * FROM transactions 
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

func (r *TransactionRepository) FindPendingSettlement(ctx context.Context, limit int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
		SELECT * FROM transactions 
		WHERE status = 'completed' AND settlement_id IS NULL
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
	query := `SELECT * FROM transactions WHERE settlement_id = $1`

	err := r.db.SelectContext(ctx, &txs, query, settlementID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transactions")
	}

	return txs, nil
}
