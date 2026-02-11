package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"

	"kyd/internal/domain"
	"kyd/pkg/errors"
)

type WalletRepository struct {
	db *sqlx.DB
}

func NewWalletRepository(db *sqlx.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	query := `
		INSERT INTO customer_schema.wallets (
			id, user_id, wallet_address, currency, available_balance, ledger_balance, reserved_balance, status, created_at, updated_at
		) VALUES (
			:id, :user_id, :wallet_address, :currency, :available_balance, :ledger_balance, :reserved_balance, :status, :created_at, :updated_at
		)
	`
	_, err := r.db.NamedExecContext(ctx, query, wallet)
	return errors.Wrap(err, "failed to create wallet")
}

func (r *WalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	wallet.UpdatedAt = time.Now()
	query := `
		UPDATE customer_schema.wallets SET
			available_balance = :available_balance,
			ledger_balance = :ledger_balance,
			reserved_balance = :reserved_balance,
			status = :status,
			last_transaction_at = :last_transaction_at,
			updated_at = :updated_at
		WHERE id = :id
	`
	_, err := r.db.NamedExecContext(ctx, query, wallet)
	return errors.Wrap(err, "failed to update wallet")
}

func (r *WalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	query := `SELECT * FROM customer_schema.wallets WHERE id = $1`
	err := r.db.GetContext(ctx, wallet, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.ErrWalletNotFound
		}
		return nil, errors.Wrap(err, "failed to find wallet by id")
	}
	return wallet, nil
}

func (r *WalletRepository) FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
	var wallets []*domain.Wallet
	query := `SELECT * FROM customer_schema.wallets WHERE user_id = $1`
	err := r.db.SelectContext(ctx, &wallets, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallets by user id")
	}
	return wallets, nil
}

func (r *WalletRepository) FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	query := `SELECT * FROM customer_schema.wallets WHERE user_id = $1 AND currency = $2`
	err := r.db.GetContext(ctx, wallet, query, userID, currency)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Return nil if not found, let service handle logic
		}
		return nil, errors.Wrap(err, "failed to find wallet by user and currency")
	}
	return wallet, nil
}

func (r *WalletRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.Wallet, error) {
	var wallets []*domain.Wallet
	query := `SELECT * FROM customer_schema.wallets ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	err := r.db.SelectContext(ctx, &wallets, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find all wallets")
	}
	return wallets, nil
}

func (r *WalletRepository) Count(ctx context.Context) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM customer_schema.wallets`
	err := r.db.GetContext(ctx, &count, query)
	return count, errors.Wrap(err, "failed to count wallets")
}

// ReserveFunds moves funds from available to reserved balance
func (r *WalletRepository) ReserveFunds(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	// This should ideally be a transaction, but for simplicity here:
	// We can use a direct SQL update with check
	query := `
		UPDATE customer_schema.wallets SET
			available_balance = available_balance - $1,
			reserved_balance = reserved_balance + $1,
			updated_at = NOW()
		WHERE id = $2 AND available_balance >= $1
	`
	result, err := r.db.ExecContext(ctx, query, amount, walletID)
	if err != nil {
		return errors.Wrap(err, "failed to reserve funds")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrInsufficientBalance
	}
	return nil
}

func (r *WalletRepository) FindByAddress(ctx context.Context, address string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	query := `SELECT * FROM customer_schema.wallets WHERE wallet_address = $1`
	err := r.db.GetContext(ctx, wallet, query, address)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.ErrWalletNotFound
		}
		return nil, errors.Wrap(err, "failed to find wallet by address")
	}
	return wallet, nil
}

func (r *WalletRepository) SearchByAddress(ctx context.Context, partialAddress string, limit int) ([]*domain.Wallet, error) {
	var wallets []*domain.Wallet
	query := `SELECT * FROM customer_schema.wallets WHERE wallet_address LIKE $1 LIMIT $2`
	err := r.db.SelectContext(ctx, &wallets, query, "%"+partialAddress+"%", limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to search wallets by address")
	}
	return wallets, nil
}

func (r *WalletRepository) CreditWallet(ctx context.Context, id uuid.UUID, amount decimal.Decimal) error {
	query := `
		UPDATE customer_schema.wallets SET
			available_balance = available_balance + $1,
			ledger_balance = ledger_balance + $1,
			updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, amount, id)
	return errors.Wrap(err, "failed to credit wallet")
}

func (r *WalletRepository) UpdateWalletAddress(ctx context.Context, id uuid.UUID, address string) error {
	query := `UPDATE customer_schema.wallets SET wallet_address = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, address, id)
	return errors.Wrap(err, "failed to update wallet address")
}

func (r *WalletRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*domain.Wallet, error) {
	if len(ids) == 0 {
		return []*domain.Wallet{}, nil
	}
	query, args, err := sqlx.In("SELECT * FROM customer_schema.wallets WHERE id IN (?)", ids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build query")
	}
	query = r.db.Rebind(query)
	var wallets []*domain.Wallet
	err = r.db.SelectContext(ctx, &wallets, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallets by ids")
	}
	return wallets, nil
}

func (r *WalletRepository) FindAllWithFilter(ctx context.Context, limit, offset int, userID *uuid.UUID) ([]*domain.Wallet, error) {
	var wallets []*domain.Wallet
	query := `SELECT * FROM customer_schema.wallets`
	args := []interface{}{}

	if userID != nil {
		query += ` WHERE user_id = $1`
		args = append(args, *userID)
	}

	query += ` ORDER BY created_at DESC LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2)
	args = append(args, limit, offset)

	err := r.db.SelectContext(ctx, &wallets, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallets with filter")
	}
	return wallets, nil
}

func (r *WalletRepository) CountWithFilter(ctx context.Context, userID *uuid.UUID) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM customer_schema.wallets`
	args := []interface{}{}

	if userID != nil {
		query += ` WHERE user_id = $1`
		args = append(args, *userID)
	}

	err := r.db.GetContext(ctx, &count, query, args...)
	return count, errors.Wrap(err, "failed to count wallets with filter")
}

func (r *WalletRepository) DebitWallet(ctx context.Context, id uuid.UUID, amount decimal.Decimal) error {
	query := `
		UPDATE customer_schema.wallets SET
			available_balance = available_balance - $1,
			ledger_balance = ledger_balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND available_balance >= $1
	`
	result, err := r.db.ExecContext(ctx, query, amount, id)
	if err != nil {
		return errors.Wrap(err, "failed to debit wallet")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrInsufficientBalance
	}
	return nil
}
