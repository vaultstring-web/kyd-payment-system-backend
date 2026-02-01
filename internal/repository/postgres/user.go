// ==============================================================================
// POSTGRESQL REPOSITORIES - internal/repository/postgres/
// ==============================================================================

// USER REPOSITORY - internal/repository/postgres/user.go
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kyd/internal/domain"
	"kyd/internal/security"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
)

type UserRepository struct {
	db     *sqlx.DB
	crypto *security.CryptoService
}

func NewUserRepository(db *sqlx.DB, crypto *security.CryptoService) *UserRepository {
	return &UserRepository{db: db, crypto: crypto}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	// Encrypt sensitive fields
	encEmail, err := r.crypto.Encrypt(user.Email)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt email")
	}
	encPhone, err := r.crypto.Encrypt(user.Phone)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt phone")
	}
	encFirstName, err := r.crypto.Encrypt(user.FirstName)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt first name")
	}
	encLastName, err := r.crypto.Encrypt(user.LastName)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt last name")
	}
	var encTOTPSecret *string
	if user.TOTPSecret != nil {
		enc, err := r.crypto.Encrypt(*user.TOTPSecret)
		if err != nil {
			return errors.Wrap(err, "failed to encrypt TOTP secret")
		}
		encTOTPSecret = &enc
	}

	// Generate blind indexes
	emailHash := r.crypto.BlindIndex(user.Email)
	var phoneHash *string
	if user.Phone != "" {
		ph := r.crypto.BlindIndex(user.Phone)
		phoneHash = &ph
	}

	query := `
		INSERT INTO customer_schema.users (
			id, email, phone, password_hash, first_name, last_name,
			user_type, kyc_level, kyc_status, country_code, date_of_birth,
			business_name, risk_score, is_active, created_at, updated_at,
			email_hash, phone_hash, totp_secret, is_totp_enabled
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
	`

	_, err = r.db.ExecContext(ctx, query,
		user.ID, encEmail, encPhone, user.PasswordHash, encFirstName, encLastName,
		user.UserType, user.KYCLevel, user.KYCStatus, user.CountryCode, user.DateOfBirth,
		user.BusinessName, user.RiskScore, user.IsActive, user.CreatedAt, user.UpdatedAt,
		emailHash, phoneHash, encTOTPSecret, user.IsTOTPEnabled,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create user")
	}

	return nil
}

func (r *UserRepository) decryptUser(user *domain.User) error {
	var err error
	if user.Email != "" {
		user.Email, err = r.crypto.Decrypt(user.Email)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt email")
		}
	}
	if user.Phone != "" {
		user.Phone, err = r.crypto.Decrypt(user.Phone)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt phone")
		}
	}
	if user.FirstName != "" {
		user.FirstName, err = r.crypto.Decrypt(user.FirstName)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt first name")
		}
	}
	if user.LastName != "" {
		user.LastName, err = r.crypto.Decrypt(user.LastName)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt last name")
		}
	}
	if user.TOTPSecret != nil {
		dec, err := r.crypto.Decrypt(*user.TOTPSecret)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt TOTP secret")
		}
		user.TOTPSecret = &dec
	}
	return nil
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var user domain.User
	query := `
		SELECT 
			id, email, phone, password_hash, first_name, last_name,
			user_type, kyc_level, kyc_status, country_code, date_of_birth,
			business_name, business_registration, risk_score, is_active,
			email_verified, totp_secret, is_totp_enabled, last_login,
			failed_login_attempts, locked_until, created_at, updated_at
		FROM customer_schema.users WHERE id = $1`

	err := r.db.GetContext(ctx, &user, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	if err := r.decryptUser(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// FindByIDs fetches multiple users by their IDs in a single query.
func (r *UserRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*domain.User, error) {
	if len(ids) == 0 {
		return []*domain.User{}, nil
	}
	var users []*domain.User
	query, args, err := sqlx.In(`
		SELECT 
			id, email, phone, first_name, last_name, user_type, kyc_level, kyc_status,
			country_code, date_of_birth, business_name, risk_score, is_active,
			failed_login_attempts, locked_until, last_login, created_at, updated_at, is_totp_enabled
		FROM customer_schema.users
		WHERE id IN (?)`, ids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build query")
	}
	query = r.db.Rebind(query)

	err = r.db.SelectContext(ctx, &users, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch users by ids")
	}

	for _, user := range users {
		if err := r.decryptUser(user); err != nil {
			// Log error but maybe continue? For strict security, we fail.
			return nil, err
		}
	}

	return users, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	// Use blind index for search
	emailHash := r.crypto.BlindIndex(email)
	query := `
		SELECT 
			id, email, phone, password_hash, first_name, last_name,
			user_type, kyc_level, kyc_status, country_code, date_of_birth,
			business_name, business_registration, risk_score, is_active,
			email_verified, totp_secret, is_totp_enabled, last_login,
			failed_login_attempts, locked_until, created_at, updated_at
		FROM customer_schema.users WHERE email_hash = $1`

	err := r.db.GetContext(ctx, &user, query, emailHash)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	if err := r.decryptUser(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var exists bool
	emailHash := r.crypto.BlindIndex(email)
	query := `SELECT EXISTS(SELECT 1 FROM customer_schema.users WHERE email_hash = $1)`

	err := r.db.GetContext(ctx, &exists, query, emailHash)
	if err != nil {
		return false, errors.Wrap(err, "failed to check user existence")
	}

	return exists, nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	// Encrypt fields
	encEmail, err := r.crypto.Encrypt(user.Email)
	if err != nil {
		return err
	}
	encPhone, err := r.crypto.Encrypt(user.Phone)
	if err != nil {
		return err
	}
	encFirstName, err := r.crypto.Encrypt(user.FirstName)
	if err != nil {
		return err
	}
	encLastName, err := r.crypto.Encrypt(user.LastName)
	if err != nil {
		return err
	}
	var encTOTPSecret *string
	if user.TOTPSecret != nil {
		enc, err := r.crypto.Encrypt(*user.TOTPSecret)
		if err != nil {
			return err
		}
		encTOTPSecret = &enc
	}

	// Hashes
	emailHash := r.crypto.BlindIndex(user.Email)
	var phoneHash *string
	if user.Phone != "" {
		ph := r.crypto.BlindIndex(user.Phone)
		phoneHash = &ph
	}

	query := `
		UPDATE customer_schema.users SET
			email = $1, phone = $2, first_name = $3, last_name = $4,
			user_type = $5, kyc_level = $6, kyc_status = $7, last_login = $8,
			password_hash = $9, failed_login_attempts = $10, locked_until = $11,
			updated_at = $12, email_hash = $13, phone_hash = $14,
			totp_secret = $15, is_totp_enabled = $16
		WHERE id = $17
	`

	_, err = r.db.ExecContext(ctx, query,
		encEmail, encPhone, encFirstName, encLastName,
		user.UserType, user.KYCLevel, user.KYCStatus, user.LastLogin,
		user.PasswordHash, user.FailedLoginAttempts, user.LockedUntil,
		user.UpdatedAt, emailHash, phoneHash, encTOTPSecret, user.IsTOTPEnabled, user.ID,
	)

	return errors.Wrap(err, "failed to update user")
}

// SetEmailVerified marks a user's email as verified.
func (r *UserRepository) SetEmailVerified(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE customer_schema.users SET
			email_verified = TRUE,
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return errors.Wrap(err, "failed to set email verified")
}

// UpdateLoginSecurity updates failed login attempts and lock status.
func (r *UserRepository) UpdateLoginSecurity(ctx context.Context, id uuid.UUID, attempts int, lockedUntil *time.Time) error {
	query := `
		UPDATE customer_schema.users SET
			failed_login_attempts = $1,
			locked_until = $2,
			updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, attempts, lockedUntil, id)
	return errors.Wrap(err, "failed to update login security")
}

func (r *UserRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.User, error) {
	var users []*domain.User
	query := `
			SELECT 
			id, email, phone, first_name, last_name, user_type, kyc_level, kyc_status,
			country_code, date_of_birth, business_name, risk_score, is_active,
			failed_login_attempts, locked_until, last_login, created_at, updated_at
			FROM customer_schema.users
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		`
	err := r.db.SelectContext(ctx, &users, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list users")
	}

	fmt.Printf("FindAll: Found %d users. Decrypting...\n", len(users))
	for i, user := range users {
		fmt.Printf("FindAll: Decrypting user %d: ID=%s, EmailLen=%d\n", i, user.ID, len(user.Email))
		if err := r.decryptUser(user); err != nil {
			fmt.Printf("FindAll: Decryption failed for user %s: %v\n", user.ID, err)
			return nil, err
		}
		fmt.Printf("FindAll: Decrypted user %d: Email=%s\n", i, user.Email)
	}

	return users, nil
}

func (r *UserRepository) CountAll(ctx context.Context) (int, error) {
	var total int
	query := `SELECT COUNT(*) FROM customer_schema.users`
	err := r.db.GetContext(ctx, &total, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count users")
	}
	return total, nil
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
		INSERT INTO customer_schema.wallets (
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
	query := `SELECT * FROM customer_schema.wallets WHERE id = $1`

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
	query := `SELECT * FROM customer_schema.wallets WHERE user_id = $1 ORDER BY created_at DESC`

	err := r.db.SelectContext(ctx, &wallets, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallets")
	}

	return wallets, nil
}

func (r *WalletRepository) Count(ctx context.Context) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM customer_schema.wallets`
	err := r.db.GetContext(ctx, &count, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count wallets")
	}
	return count, nil
}

func (r *WalletRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*domain.Wallet, error) {
	if len(ids) == 0 {
		return []*domain.Wallet{}, nil
	}
	var wallets []*domain.Wallet
	query, args, err := sqlx.In(`SELECT * FROM customer_schema.wallets WHERE id IN (?)`, ids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build query")
	}
	query = r.db.Rebind(query)

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

	query += ` ORDER BY created_at DESC LIMIT `
	if userID != nil {
		query += `$2 OFFSET $3`
		args = append(args, limit, offset)
	} else {
		query += `$1 OFFSET $2`
		args = append(args, limit, offset)
	}

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
	if err != nil {
		return 0, errors.Wrap(err, "failed to count wallets with filter")
	}
	return count, nil
}

func (r *WalletRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.Wallet, error) {
	return r.FindAllWithFilter(ctx, limit, offset, nil)
}

func (r *WalletRepository) FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error) {
	var wallet domain.Wallet
	query := `SELECT * FROM customer_schema.wallets WHERE user_id = $1 AND currency = $2`

	err := r.db.GetContext(ctx, &wallet, query, userID, currency)
	if err == sql.ErrNoRows {
		return nil, errors.ErrWalletNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallet")
	}

	return &wallet, nil
}

func (r *WalletRepository) FindByAddress(ctx context.Context, address string) (*domain.Wallet, error) {
	var wallet domain.Wallet
	query := `SELECT * FROM customer_schema.wallets WHERE wallet_address = $1`
	err := r.db.GetContext(ctx, &wallet, query, address)
	if err == sql.ErrNoRows {
		return nil, errors.ErrWalletNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallet by address")
	}
	return &wallet, nil
}

func (r *WalletRepository) SearchByAddress(ctx context.Context, partialAddress string, limit int) ([]*domain.Wallet, error) {
	var wallets []*domain.Wallet
	query := `SELECT * FROM customer_schema.wallets WHERE wallet_address LIKE $1 LIMIT $2`
	// Add wildcards for partial match
	searchPattern := "%" + partialAddress + "%"

	err := r.db.SelectContext(ctx, &wallets, query, searchPattern, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to search wallets")
	}
	return wallets, nil
}

func (r *WalletRepository) DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	query := `
		UPDATE customer_schema.wallets 
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
		UPDATE customer_schema.wallets 
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
		UPDATE customer_schema.wallets SET
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

func (r *WalletRepository) UpdateWalletAddress(ctx context.Context, walletID uuid.UUID, address string) error {
	query := `UPDATE customer_schema.wallets SET wallet_address = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, address, walletID)
	return errors.Wrap(err, "failed to update wallet address")
}

func (r *WalletRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM customer_schema.wallets WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return errors.Wrap(err, "failed to delete wallet")
}

func (r *WalletRepository) DeleteLedgerEntriesByWalletID(ctx context.Context, walletID uuid.UUID) error {
	query := `DELETE FROM customer_schema.ledger_entries WHERE wallet_id = $1`
	_, err := r.db.ExecContext(ctx, query, walletID)
	return errors.Wrap(err, "failed to delete ledger entries by wallet")
}

func (r *UserRepository) FindByKYCStatus(ctx context.Context, status string, limit, offset int) ([]*domain.User, int, error) {
	var users []*domain.User
	query := `
		SELECT 
			id, email, phone, first_name, last_name, user_type, kyc_level, kyc_status,
			country_code, date_of_birth, business_name, risk_score, is_active,
			failed_login_attempts, locked_until, last_login, created_at, updated_at
		FROM customer_schema.users
		WHERE kyc_status = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	err := r.db.SelectContext(ctx, &users, query, status, limit, offset)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to list users by kyc status")
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM customer_schema.users WHERE kyc_status = $1`
	err = r.db.GetContext(ctx, &total, countQuery, status)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to count users by kyc status")
	}

	return users, total, nil
}
