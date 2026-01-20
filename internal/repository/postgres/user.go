// ==============================================================================
// POSTGRESQL REPOSITORIES - internal/repository/postgres/
// ==============================================================================

// USER REPOSITORY - internal/repository/postgres/user.go
package postgres

import (
	"context"
	"database/sql"
	"time"

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
		INSERT INTO customer_schema.users (
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
	query := `SELECT * FROM customer_schema.users WHERE id = $1`

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
	query := `SELECT * FROM customer_schema.users WHERE email = $1`

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
	query := `SELECT EXISTS(SELECT 1 FROM customer_schema.users WHERE email = $1)`

	err := r.db.GetContext(ctx, &exists, query, email)
	if err != nil {
		return false, errors.Wrap(err, "failed to check user existence")
	}

	return exists, nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	query := `
		UPDATE customer_schema.users SET
			email = $1, phone = $2, first_name = $3, last_name = $4,
			user_type = $5, kyc_level = $6, kyc_status = $7, last_login = $8,
			password_hash = $9, failed_login_attempts = $10, locked_until = $11,
			updated_at = $12
		WHERE id = $13
	`

	_, err := r.db.ExecContext(ctx, query,
		user.Email, user.Phone, user.FirstName, user.LastName,
		user.UserType, user.KYCLevel, user.KYCStatus, user.LastLogin,
		user.PasswordHash, user.FailedLoginAttempts, user.LockedUntil,
		user.UpdatedAt, user.ID,
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

// TRANSACTION REPOSITORY - internal/repository/postgres/transaction.go
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

	return errors.Wrap(err, "failed to create transaction")
}

func (r *TransactionRepository) Update(ctx context.Context, tx *domain.Transaction) error {
	query := `
		UPDATE customer_schema.transactions SET
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
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, status_reason, transaction_type, channel, category, description,
            metadata, blockchain_tx_hash, settlement_id, initiated_at, completed_at,
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
            fee_amount, fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, status_reason, transaction_type, channel, category, description,
            metadata, blockchain_tx_hash, settlement_id, initiated_at, completed_at,
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
            fee_amount, fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, status_reason, transaction_type, channel, category, description,
            metadata, blockchain_tx_hash, settlement_id, initiated_at, completed_at,
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

func (r *TransactionRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.Transaction, error) {
	var txs []*domain.Transaction
	query := `
        SELECT 
            id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
            amount, currency, exchange_rate, converted_amount, converted_currency,
            fee_amount, fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, status_reason, transaction_type, channel, category, description,
            metadata, blockchain_tx_hash, settlement_id, initiated_at, completed_at,
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
            fee_amount, fee_currency, COALESCE(net_amount, converted_amount) AS net_amount,
            status, status_reason, transaction_type, channel, category, description,
            metadata, blockchain_tx_hash, settlement_id, initiated_at, completed_at,
            created_at, updated_at
        FROM customer_schema.transactions 
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
	query := `SELECT * FROM customer_schema.transactions WHERE settlement_id = $1`

	err := r.db.SelectContext(ctx, &txs, query, settlementID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find transactions")
	}

	return txs, nil
}

func (r *TransactionRepository) DeleteByWalletID(ctx context.Context, walletID uuid.UUID) error {
	query := `DELETE FROM customer_schema.transactions WHERE sender_wallet_id = $1 OR receiver_wallet_id = $1`
	_, err := r.db.ExecContext(ctx, query, walletID)
	return errors.Wrap(err, "failed to delete transactions by wallet")
}

// ==============================================================================
// KYC-RELATED USER REPOSITORY EXTENSIONS
// ==============================================================================

// UpdateKYCStatus updates the user's KYC status and optionally the KYC profile submission status
func (r *UserRepository) UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status domain.KYCStatus, profileStatus *domain.KYCSubmissionStatus) error {
	// Start a transaction to update both user and kyc_profile if needed
	tx, err := r.db.Beginx()
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	// Update user kyc_status
	userQuery := `UPDATE customer_schema.users SET kyc_status = $1, updated_at = NOW() WHERE id = $2`
	result, err := tx.ExecContext(ctx, userQuery, status, userID)
	if err != nil {
		return errors.Wrap(err, "failed to update user KYC status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrUserNotFound
	}

	// Optionally update kyc_profile submission_status if provided
	if profileStatus != nil {
		profileQuery := `UPDATE customer_schema.kyc_profiles SET submission_status = $1, updated_at = NOW() WHERE user_id = $2`
		_, err := tx.ExecContext(ctx, profileQuery, *profileStatus, userID)
		if err != nil {
			return errors.Wrap(err, "failed to update KYC profile status")
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	return nil
}

// UpdateKYCStatusTx updates the user's KYC status within a transaction
func (r *UserRepository) UpdateKYCStatusTx(ctx context.Context, tx interface{}, userID uuid.UUID, status domain.KYCStatus, profileStatus *domain.KYCSubmissionStatus) error {
	var db sqlx.ExtContext
	if tx == nil {
		db = r.db
	} else {
		db = tx.(*SQLxTransaction).tx
	}

	// Update user kyc_status
	userQuery := `UPDATE customer_schema.users SET kyc_status = $1, updated_at = NOW() WHERE id = $2`
	result, err := db.ExecContext(ctx, userQuery, status, userID)
	if err != nil {
		return errors.Wrap(err, "failed to update user KYC status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrUserNotFound
	}

	// Optionally update kyc_profile submission_status if provided
	if profileStatus != nil {
		profileQuery := `UPDATE customer_schema.kyc_profiles SET submission_status = $1, updated_at = NOW() WHERE user_id = $2`
		_, err := db.ExecContext(ctx, profileQuery, *profileStatus, userID)
		if err != nil {
			return errors.Wrap(err, "failed to update KYC profile status")
		}
	}

	return nil
}

// UpdateKYCLevelTx updates the user's KYC level within a transaction
func (r *UserRepository) UpdateKYCLevelTx(ctx context.Context, tx interface{}, userID uuid.UUID, level int, updateProfile bool) error {
	var db sqlx.ExtContext
	if tx == nil {
		db = r.db
	} else {
		db = tx.(*SQLxTransaction).tx
	}

	// Update user kyc_level
	userQuery := `UPDATE customer_schema.users SET kyc_level = $1, updated_at = NOW() WHERE id = $2`
	result, err := db.ExecContext(ctx, userQuery, level, userID)
	if err != nil {
		return errors.Wrap(err, "failed to update user KYC level")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrUserNotFound
	}

	// Optionally update kyc_profile kyc_level
	if updateProfile {
		profileQuery := `UPDATE customer_schema.kyc_profiles SET kyc_level = $1, updated_at = NOW() WHERE user_id = $2`
		_, err := db.ExecContext(ctx, profileQuery, level, userID)
		if err != nil {
			return errors.Wrap(err, "failed to update KYC profile level")
		}
	}

	return nil
}

// UpdateUserRiskScoreTx updates the user's risk score within a transaction
func (r *UserRepository) UpdateUserRiskScoreTx(ctx context.Context, tx interface{}, userID uuid.UUID, riskScore decimal.Decimal) error {
	var db sqlx.ExtContext
	if tx == nil {
		db = r.db
	} else {
		db = tx.(*SQLxTransaction).tx
	}

	query := `UPDATE customer_schema.users SET risk_score = $1, updated_at = NOW() WHERE id = $2`

	result, err := db.ExecContext(ctx, query, riskScore, userID)
	if err != nil {
		return errors.Wrap(err, "failed to update user risk score")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrUserNotFound
	}

	return nil
}

// FindByIDTx finds a user by ID within a transaction
func (r *UserRepository) FindByIDTx(ctx context.Context, tx interface{}, id uuid.UUID) (*domain.User, error) {
	var user domain.User
	query := `SELECT * FROM customer_schema.users WHERE id = $1`

	var err error
	if tx == nil {
		err = r.db.GetContext(ctx, &user, query, id)
	} else {
		err = tx.(*SQLxTransaction).tx.GetContext(ctx, &user, query, id)
	}

	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user")
	}

	return &user, nil
}

// UpdateKYCLevel updates the user's KYC level and optionally the KYC profile kyc_level
func (r *UserRepository) UpdateKYCLevel(ctx context.Context, userID uuid.UUID, level int, updateProfile bool) error {
	// Start a transaction
	tx, err := r.db.Beginx()
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	// Update user kyc_level
	userQuery := `UPDATE customer_schema.users SET kyc_level = $1, updated_at = NOW() WHERE id = $2`
	result, err := tx.ExecContext(ctx, userQuery, level, userID)
	if err != nil {
		return errors.Wrap(err, "failed to update user KYC level")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrUserNotFound
	}

	// Optionally update kyc_profile kyc_level
	if updateProfile {
		profileQuery := `UPDATE customer_schema.kyc_profiles SET kyc_level = $1, updated_at = NOW() WHERE user_id = $2`
		_, err := tx.ExecContext(ctx, profileQuery, level, userID)
		if err != nil {
			return errors.Wrap(err, "failed to update KYC profile level")
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	return nil
}

// GetUserKYCProfile retrieves the user's KYC profile along with user information
func (r *UserRepository) GetUserKYCProfile(ctx context.Context, userID uuid.UUID) (*domain.KYCProfile, error) {
	var profile domain.KYCProfile

	// Query to join user and kyc_profile data
	query := `
        SELECT 
            kp.*,
            u.email,
            u.phone,
            u.first_name,
            u.last_name,
            u.user_type,
            u.country_code as user_country_code,
            u.date_of_birth as user_date_of_birth,
            u.business_name
        FROM customer_schema.kyc_profiles kp
        LEFT JOIN customer_schema.users u ON kp.user_id = u.id
        WHERE kp.user_id = $1
    `

	err := r.db.GetContext(ctx, &profile, query, userID)
	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCProfileNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user KYC profile")
	}

	return &profile, nil
}

// GetUserKYCRequirements retrieves the KYC requirements for a user based on their country, user type, and KYC level
func (r *UserRepository) GetUserKYCRequirements(ctx context.Context, userID uuid.UUID) (*domain.KYCRequirements, error) {
	// First get user details needed for requirements lookup
	var user struct {
		CountryCode string `db:"country_code"`
		UserType    string `db:"user_type"`
		KYCLevel    int    `db:"kyc_level"`
	}

	userQuery := `SELECT country_code, user_type, kyc_level FROM customer_schema.users WHERE id = $1`
	err := r.db.GetContext(ctx, &user, userQuery, userID)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user details")
	}

	// Then fetch the KYC requirements
	var requirements domain.KYCRequirements
	reqQuery := `
        SELECT * FROM customer_schema.kyc_requirements 
        WHERE country_code = $1 
        AND user_type = $2 
        AND kyc_level = $3 
        AND is_active = true
        AND (effective_to IS NULL OR effective_to >= CURRENT_DATE)
        ORDER BY version DESC
        LIMIT 1
    `

	err = r.db.GetContext(ctx, &requirements, reqQuery, user.CountryCode, user.UserType, user.KYCLevel)
	if err == sql.ErrNoRows {
		return nil, errors.ErrKYCRequirementsNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get KYC requirements")
	}

	return &requirements, nil
}

// UpdateUserWithKYCProfile performs a transactional update of both user and KYC profile
func (r *UserRepository) UpdateUserWithKYCProfile(ctx context.Context, user *domain.User, profile *domain.KYCProfile) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	// Update user
	userQuery := `
        UPDATE customer_schema.users SET
            email = $1,
            phone = $2,
            first_name = $3,
            last_name = $4,
            kyc_status = $5,
            kyc_level = $6,
            updated_at = $7
        WHERE id = $8
    `

	_, err = tx.ExecContext(ctx, userQuery,
		user.Email,
		user.Phone,
		user.FirstName,
		user.LastName,
		user.KYCStatus,
		user.KYCLevel,
		time.Now(),
		user.ID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to update user")
	}

	// Update KYC profile using named query
	profileQuery := `
        UPDATE customer_schema.kyc_profiles SET
            profile_type = :profile_type,
            date_of_birth = :date_of_birth,
            nationality = :nationality,
            occupation = :occupation,
            annual_income_range = :annual_income_range,
            source_of_funds = :source_of_funds,
            company_name = :company_name,
            company_registration_number = :company_registration_number,
            address_line1 = :address_line1,
            city = :city,
            country_code = :country_code,
            submission_status = :submission_status,
            kyc_level = :kyc_level,
            updated_at = :updated_at
        WHERE user_id = :user_id
    `

	_, err = tx.NamedExecContext(ctx, profileQuery, profile)
	if err != nil {
		return errors.Wrap(err, "failed to update KYC profile")
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	return nil
}

// UserHasKYCProfile checks if a user has a KYC profile
func (r *UserRepository) UserHasKYCProfile(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM customer_schema.kyc_profiles WHERE user_id = $1)`

	err := r.db.GetContext(ctx, &exists, query, userID)
	if err != nil {
		return false, errors.Wrap(err, "failed to check KYC profile existence")
	}

	return exists, nil
}

// GetUserKYCStatus retrieves the current KYC status of a user
func (r *UserRepository) GetUserKYCStatus(ctx context.Context, userID uuid.UUID) (*domain.KYCStatusResponse, error) {
	var response domain.KYCStatusResponse

	query := `
        SELECT 
            u.id as user_id,
            u.email,
            u.kyc_status as user_kyc_status,
            u.kyc_level as user_kyc_level,
            u.country_code,
            u.user_type,
            kp.submission_status as profile_status,
            kp.aml_check_status,
            kp.aml_risk_score,
            kp.kyc_level as profile_kyc_level,
            kp.next_review_date,
            kp.reviewed_at,
            kp.approved_at,
            kp.created_at as profile_created_at,
            kp.updated_at as profile_updated_at
        FROM customer_schema.users u
        LEFT JOIN customer_schema.kyc_profiles kp ON u.id = kp.user_id
        WHERE u.id = $1
    `

	err := r.db.GetContext(ctx, &response, query, userID)
	if err == sql.ErrNoRows {
		return nil, errors.ErrUserNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user KYC status")
	}

	return &response, nil
}

// GetUserTransactionLimits retrieves transaction limits for a user based on KYC level
func (r *UserRepository) GetUserTransactionLimits(ctx context.Context, userID uuid.UUID) (map[string]interface{}, error) {
	// Get user KYC requirements which include transaction limits
	requirements, err := r.GetUserKYCRequirements(ctx, userID)
	if err != nil {
		return nil, err
	}

	limits := map[string]interface{}{
		"daily_transaction_limit":   requirements.DailyTransactionLimit,
		"monthly_transaction_limit": requirements.MonthlyTransactionLimit,
		"max_single_transaction":    requirements.MaxSingleTransaction,
		"kyc_level":                 requirements.KYCLevel,
		"country_code":              requirements.CountryCode,
		"user_type":                 requirements.UserType,
	}

	return limits, nil
}

// UpdateUserRiskScore updates the user's risk score based on AML check results
func (r *UserRepository) UpdateUserRiskScore(ctx context.Context, userID uuid.UUID, riskScore decimal.Decimal) error {
	query := `UPDATE customer_schema.users SET risk_score = $1, updated_at = NOW() WHERE id = $2`

	result, err := r.db.ExecContext(ctx, query, riskScore, userID)
	if err != nil {
		return errors.Wrap(err, "failed to update user risk score")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return errors.ErrUserNotFound
	}

	return nil
}

// GetUsersByKYCStatus retrieves users filtered by KYC status (for admin/reporting)
func (r *UserRepository) GetUsersByKYCStatus(ctx context.Context, status domain.KYCStatus, limit, offset int) ([]*domain.User, error) {
	var users []*domain.User

	query := `
        SELECT * FROM customer_schema.users 
        WHERE kyc_status = $1
        ORDER BY updated_at DESC
        LIMIT $2 OFFSET $3
    `

	err := r.db.SelectContext(ctx, &users, query, status, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get users by KYC status")
	}

	return users, nil
}

// CountUsersByKYCStatus counts users by KYC status
func (r *UserRepository) CountUsersByKYCStatus(ctx context.Context, status domain.KYCStatus) (int, error) {
	var count int

	query := `SELECT COUNT(*) FROM customer_schema.users WHERE kyc_status = $1`

	err := r.db.GetContext(ctx, &count, query, status)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count users by KYC status")
	}

	return count, nil
}
