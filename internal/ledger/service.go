// ==============================================================================
// LEDGER SERVICE - internal/ledger/service.go
// ==============================================================================
package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"kyd/internal/domain"
	"kyd/internal/repository/postgres"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
)

type Service struct {
	db         *sqlx.DB
	ledgerRepo *postgres.LedgerRepository
}

func NewService(db *sqlx.DB, ledgerRepo *postgres.LedgerRepository) *Service {
	return &Service{
		db:         db,
		ledgerRepo: ledgerRepo,
	}
}

// PostTransaction performs double-entry bookkeeping atomically
func (s *Service) PostTransaction(ctx context.Context, posting *LedgerPosting) error {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return errors.Wrap(err, "begin transaction failed")
	}
	defer tx.Rollback()

	// Lock wallets in deterministic order to prevent deadlocks
	walletIDs := []uuid.UUID{posting.DebitWalletID, posting.CreditWalletID}
	if posting.FeeWalletID != nil {
		walletIDs = append(walletIDs, *posting.FeeWalletID)
	}

	// sort by UUID string to lock deterministically
	for i := 0; i < len(walletIDs); i++ {
		for j := i + 1; j < len(walletIDs); j++ {
			if walletIDs[i].String() > walletIDs[j].String() {
				walletIDs[i], walletIDs[j] = walletIDs[j], walletIDs[i]
			}
		}
	}

	for _, walletID := range walletIDs {
		var balance decimal.Decimal
		err := tx.QueryRowContext(ctx,
			`SELECT available_balance FROM customer_schema.wallets WHERE id = $1 FOR UPDATE`,
			walletID,
		).Scan(&balance)
		if err != nil {
			return errors.Wrap(err, "wallet lock failed")
		}
	}

	// Debit sender wallet
	var debitBalanceAfter decimal.Decimal
	err = tx.QueryRowContext(ctx, `
		UPDATE customer_schema.wallets 
		SET 
			available_balance = available_balance - $1,
			ledger_balance = ledger_balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND available_balance >= $1
		RETURNING available_balance
	`, posting.DebitAmount, posting.DebitWalletID).Scan(&debitBalanceAfter)

	if err != nil {
		if err == sql.ErrNoRows {
			return errors.ErrInsufficientBalance
		}
		return errors.Wrap(err, "debit wallet update failed")
	}

	// Credit receiver wallet
	var creditBalanceAfter decimal.Decimal
	err = tx.QueryRowContext(ctx, `
		UPDATE customer_schema.wallets 
		SET 
			available_balance = available_balance + $1,
			ledger_balance = ledger_balance + $1,
			last_transaction_at = NOW(),
			updated_at = NOW()
		WHERE id = $2
		RETURNING available_balance
	`, posting.CreditAmount, posting.CreditWalletID).Scan(&creditBalanceAfter)

	if err != nil {
		return errors.Wrap(err, "credit wallet update failed")
	}

	// --- Debit Ledger Entry ---
	debitEntryID := uuid.New()
	debitTime := time.Now().UTC().Truncate(time.Microsecond)
	prevHashDebit, err := s.getLastHash(ctx, tx, posting.DebitWalletID)
	if err != nil {
		return errors.Wrap(err, "failed to get debit previous hash")
	}
	hashDebit := s.calculateHash(prevHashDebit, debitEntryID, posting.TransactionID, posting.DebitWalletID, "debit", posting.DebitAmount, posting.Currency, debitBalanceAfter, debitTime)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_schema.ledger_entries (
			id, transaction_id, wallet_id, entry_type, 
			amount, currency, balance_after, created_at,
			previous_hash, hash
		) VALUES ($1, $2, $3, 'debit', $4, $5, $6, $7, $8, $9)
	`, debitEntryID, posting.TransactionID, posting.DebitWalletID, posting.DebitAmount, posting.Currency, debitBalanceAfter, debitTime, prevHashDebit, hashDebit)

	if err != nil {
		return errors.Wrap(err, "insert debit ledger entry failed")
	}

	// --- Credit Ledger Entry ---
	creditEntryID := uuid.New()
	creditTime := time.Now().UTC().Truncate(time.Microsecond)
	prevHashCredit, err := s.getLastHash(ctx, tx, posting.CreditWalletID)
	if err != nil {
		return errors.Wrap(err, "failed to get credit previous hash")
	}
	hashCredit := s.calculateHash(prevHashCredit, creditEntryID, posting.TransactionID, posting.CreditWalletID, "credit", posting.CreditAmount, posting.ConvertedCurrency, creditBalanceAfter, creditTime)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_schema.ledger_entries (
			id, transaction_id, wallet_id, entry_type, 
			amount, currency, balance_after, created_at,
			previous_hash, hash
		) VALUES ($1, $2, $3, 'credit', $4, $5, $6, $7, $8, $9)
	`, creditEntryID, posting.TransactionID, posting.CreditWalletID, posting.CreditAmount, posting.ConvertedCurrency, creditBalanceAfter, creditTime, prevHashCredit, hashCredit)

	if err != nil {
		return errors.Wrap(err, "insert credit ledger entry failed")
	}

	// --- Fee credit (optional) ---
	if posting.FeeWalletID != nil && !posting.FeeAmount.IsZero() && posting.FeeAmount.GreaterThan(decimal.Zero) {
		var feeBalanceAfter decimal.Decimal
		err = tx.QueryRowContext(ctx, `
			UPDATE customer_schema.wallets
			SET
				available_balance = available_balance + $1,
				ledger_balance = ledger_balance + $1,
				last_transaction_at = NOW(),
				updated_at = NOW()
			WHERE id = $2
			RETURNING available_balance
		`, posting.FeeAmount, *posting.FeeWalletID).Scan(&feeBalanceAfter)
		if err != nil {
			return errors.Wrap(err, "fee wallet update failed")
		}

		feeEntryID := uuid.New()
		feeTime := time.Now().UTC().Truncate(time.Microsecond)
		prevHashFee, err := s.getLastHash(ctx, tx, *posting.FeeWalletID)
		if err != nil {
			return errors.Wrap(err, "failed to get fee previous hash")
		}
		hashFee := s.calculateHash(prevHashFee, feeEntryID, posting.TransactionID, *posting.FeeWalletID, "credit", posting.FeeAmount, posting.Currency, feeBalanceAfter, feeTime)

		_, err = tx.ExecContext(ctx, `
			INSERT INTO customer_schema.ledger_entries (
				id, transaction_id, wallet_id, entry_type,
				amount, currency, balance_after, created_at,
				previous_hash, hash
			) VALUES ($1, $2, $3, 'credit', $4, $5, $6, $7, $8, $9)
		`, feeEntryID, posting.TransactionID, *posting.FeeWalletID, posting.FeeAmount, posting.Currency, feeBalanceAfter, feeTime, prevHashFee, hashFee)
		if err != nil {
			return errors.Wrap(err, "insert fee ledger entry failed")
		}

		_ = s.ledgerRepo.CreateEntryTx(ctx, tx, posting.TransactionID, "fee", posting.FeeAmount, posting.Currency, "completed")
	}

	// --- Record in Immutable Transaction Ledger ---
	// We record the main transaction event.
	eventType := posting.EventType
	if eventType == "" {
		eventType = "payment"
	}
	err = s.ledgerRepo.CreateEntryTx(ctx, tx, posting.TransactionID, eventType, posting.DebitAmount, posting.Currency, "completed")
	if err != nil {
		return errors.Wrap(err, "failed to create immutable ledger entry")
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "transaction commit failed")
	}
	return nil
}

type LedgerPosting struct {
	TransactionID     uuid.UUID
	DebitWalletID     uuid.UUID
	CreditWalletID    uuid.UUID
	FeeWalletID       *uuid.UUID
	DebitAmount       decimal.Decimal
	CreditAmount      decimal.Decimal
	Currency          domain.Currency
	ConvertedCurrency domain.Currency
	ExchangeRate      decimal.Decimal
	FeeAmount         decimal.Decimal
	Reference         string
	EventType         string
	Description       string
}

func (s *Service) getLastHash(ctx context.Context, tx *sqlx.Tx, walletID uuid.UUID) (string, error) {
	var hash string
	err := tx.QueryRowContext(ctx, `
		SELECT hash FROM customer_schema.ledger_entries 
		WHERE wallet_id = $1 
		ORDER BY created_at DESC, id DESC 
		LIMIT 1`, walletID).Scan(&hash)

	if err == sql.ErrNoRows {
		return "0000000000000000000000000000000000000000000000000000000000000000", nil
	}
	if err != nil {
		return "", err
	}
	return hash, nil
}

func (s *Service) calculateHash(prevHash string, id, txID, walletID uuid.UUID, entryType string, amount decimal.Decimal, currency domain.Currency, balanceAfter decimal.Decimal, createdAt time.Time) string {
	// Hash format: SHA256(prevHash + ID + TransactionID + WalletID + EntryType + Amount + Currency + BalanceAfter + CreatedAt)
	data := fmt.Sprintf("%s%s%s%s%s%s%s%s%s",
		prevHash,
		id.String(),
		txID.String(),
		walletID.String(),
		entryType,
		amount.String(),
		string(currency),
		balanceAfter.String(),
		createdAt.UTC().Format(time.RFC3339Nano),
	)

	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyChainIntegrity checks if the hash chain for a specific wallet is valid
func (s *Service) VerifyChainIntegrity(ctx context.Context, walletID uuid.UUID) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, transaction_id, wallet_id, entry_type, amount, currency, balance_after, created_at, previous_hash, hash
		FROM customer_schema.ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at ASC, id ASC
	`, walletID)
	if err != nil {
		return false, errors.Wrap(err, "failed to query ledger entries")
	}
	defer rows.Close()

	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"

	for rows.Next() {
		var id, txID, wID uuid.UUID
		var entryType, currency, pHash, storedHash string
		var amount, balanceAfter decimal.Decimal
		var createdAt time.Time

		if err := rows.Scan(&id, &txID, &wID, &entryType, &amount, &currency, &balanceAfter, &createdAt, &pHash, &storedHash); err != nil {
			return false, errors.Wrap(err, "failed to scan ledger entry")
		}

		if pHash != prevHash {
			return false, fmt.Errorf("broken chain at entry %s: expected previous hash %s, got %s", id, prevHash, pHash)
		}

		calculatedHash := s.calculateHash(prevHash, id, txID, wID, entryType, amount, domain.Currency(currency), balanceAfter, createdAt)
		if calculatedHash != storedHash {
			return false, fmt.Errorf("integrity failure at entry %s: hash mismatch", id)
		}

		prevHash = storedHash
	}

	return true, nil
}

type ChainEntryReport struct {
	ID                 uuid.UUID       `json:"id"`
	TransactionID       uuid.UUID       `json:"transaction_id"`
	WalletID            uuid.UUID       `json:"wallet_id"`
	EntryType           string          `json:"entry_type"`
	Amount              decimal.Decimal `json:"amount"`
	Currency            string          `json:"currency"`
	BalanceAfter        decimal.Decimal `json:"balance_after"`
	CreatedAt           time.Time       `json:"created_at"`
	PreviousHash        string          `json:"previous_hash"`
	Hash               string          `json:"hash"`
	ExpectedPreviousHash string         `json:"expected_previous_hash"`
	CalculatedHash      string          `json:"calculated_hash"`
	LinkOK              bool            `json:"link_ok"`
	HashOK              bool            `json:"hash_ok"`
}

type ChainReportResponse struct {
	WalletID uuid.UUID          `json:"wallet_id"`
	Valid    bool               `json:"valid"`
	Count    int                `json:"count"`
	Entries  []ChainEntryReport `json:"entries"`
}

// GetChainReport returns the last N ledger entries plus per-row integrity checks.
func (s *Service) GetChainReport(ctx context.Context, walletID uuid.UUID, limit int) (*ChainReportResponse, error) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}

	// Pull most recent entries, then reverse so we can validate forward.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, transaction_id, wallet_id, entry_type, amount, currency, balance_after, created_at, previous_hash, hash
		FROM customer_schema.ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, walletID, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query ledger entries")
	}
	defer rows.Close()

	type raw struct {
		id          uuid.UUID
		txID        uuid.UUID
		wID         uuid.UUID
		entryType   string
		amount      decimal.Decimal
		currency    string
		balanceAfter decimal.Decimal
		createdAt   time.Time
		prevHash    string
		hash        string
	}

	var rawRows []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.id, &r.txID, &r.wID, &r.entryType, &r.amount, &r.currency, &r.balanceAfter, &r.createdAt, &r.prevHash, &r.hash); err != nil {
			return nil, errors.Wrap(err, "failed to scan ledger entry")
		}
		rawRows = append(rawRows, r)
	}

	// Reverse into chronological order.
	for i, j := 0, len(rawRows)-1; i < j; i, j = i+1, j-1 {
		rawRows[i], rawRows[j] = rawRows[j], rawRows[i]
	}

	prevExpected := "0000000000000000000000000000000000000000000000000000000000000000"
	entries := make([]ChainEntryReport, 0, len(rawRows))
	valid := true

	for _, rr := range rawRows {
		calculated := s.calculateHash(prevExpected, rr.id, rr.txID, rr.wID, rr.entryType, rr.amount, domain.Currency(rr.currency), rr.balanceAfter, rr.createdAt)
		linkOK := rr.prevHash == prevExpected
		hashOK := calculated == rr.hash
		if !linkOK || !hashOK {
			valid = false
		}

		entries = append(entries, ChainEntryReport{
			ID:                  rr.id,
			TransactionID:        rr.txID,
			WalletID:             rr.wID,
			EntryType:            rr.entryType,
			Amount:               rr.amount,
			Currency:             rr.currency,
			BalanceAfter:         rr.balanceAfter,
			CreatedAt:            rr.createdAt,
			PreviousHash:         rr.prevHash,
			Hash:                rr.hash,
			ExpectedPreviousHash: prevExpected,
			CalculatedHash:       calculated,
			LinkOK:              linkOK,
			HashOK:              hashOK,
		})

		prevExpected = rr.hash
	}

	return &ChainReportResponse{
		WalletID: walletID,
		Valid:    valid,
		Count:    len(entries),
		Entries:  entries,
	}, nil
}

// ==============================================================================
