package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"kyd/internal/domain"
	pkgerrors "kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
)

// LedgerRepository implements ledger persistence with hash chaining.
type LedgerRepository struct {
	db *sqlx.DB
}

// NewLedgerRepository creates a new LedgerRepository.
func NewLedgerRepository(db *sqlx.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// CreateEntry adds a new entry to the ledger with hash chaining.
// It handles optimistic locking by retrying if a race condition occurs on previous_hash.
func (r *LedgerRepository) CreateEntry(ctx context.Context, txID uuid.UUID, eventType string, amount decimal.Decimal, currency domain.Currency, status string) error {
	var lastErr error
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		err := r.tryCreateEntry(ctx, txID, eventType, amount, currency, status)
		if err == nil {
			return nil
		}
		lastErr = err
		// If it's not a unique constraint violation, return error
		// In a real app we'd check the specific error code (23505 for unique violation in Postgres)
		// For now, simple retry logic
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("failed to create ledger entry after max retries: %w", lastErr)
}

func (r *LedgerRepository) tryCreateEntry(ctx context.Context, txID uuid.UUID, eventType string, amount decimal.Decimal, currency domain.Currency, status string) error {
	// 1. Get the latest hash
	var previousHash string
	// We handle the case where the table is empty
	queryLast := `SELECT hash FROM customer_schema.transaction_ledger ORDER BY created_at DESC LIMIT 1`
	err := r.db.GetContext(ctx, &previousHash, queryLast)
	if err != nil {
		// Assume empty table if error (or specific no rows error)
		// "0" * 64
		previousHash = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	// 2. Calculate new hash
	now := time.Now().UTC().Truncate(time.Microsecond)
	data := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%d",
		txID.String(), eventType, amount.String(), currency, status, previousHash, now.UnixNano())

	hashBytes := sha256.Sum256([]byte(data))
	hash := hex.EncodeToString(hashBytes[:])

	entry := &domain.TransactionLedger{
		ID:            uuid.New(),
		TransactionID: txID,
		EventType:     eventType,
		Amount:        amount,
		Currency:      currency,
		Status:        status,
		PreviousHash:  previousHash,
		Hash:          hash,
		CreatedAt:     now,
	}

	// 3. Insert
	insertQuery := `
		INSERT INTO customer_schema.transaction_ledger (
			id, transaction_id, event_type, amount, currency, status, previous_hash, hash, created_at
		) VALUES (
			:id, :transaction_id, :event_type, :amount, :currency, :status, :previous_hash, :hash, :created_at
		)
	`

	_, err = r.db.NamedExecContext(ctx, insertQuery, entry)
	if err != nil {
		return err
	}

	return nil
}

// CreateEntryTx adds a new entry to the ledger using an existing transaction.
func (r *LedgerRepository) CreateEntryTx(ctx context.Context, tx *sqlx.Tx, txID uuid.UUID, eventType string, amount decimal.Decimal, currency domain.Currency, status string) error {
	// 1. Get the latest hash with lock
	var previousHash string
	// Lock the last row to ensure sequential processing and valid chaining
	queryLast := `SELECT hash FROM customer_schema.transaction_ledger ORDER BY created_at DESC LIMIT 1 FOR UPDATE`
	err := tx.GetContext(ctx, &previousHash, queryLast)
	if err != nil {
		// Assume empty table if error
		previousHash = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	// 2. Calculate new hash
	now := time.Now().UTC().Truncate(time.Microsecond)
	data := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%d",
		txID.String(), eventType, amount.String(), currency, status, previousHash, now.UnixNano())

	hashBytes := sha256.Sum256([]byte(data))
	hash := hex.EncodeToString(hashBytes[:])

	entry := &domain.TransactionLedger{
		ID:            uuid.New(),
		TransactionID: txID,
		EventType:     eventType,
		Amount:        amount,
		Currency:      currency,
		Status:        status,
		PreviousHash:  previousHash,
		Hash:          hash,
		CreatedAt:     now,
	}

	// 3. Insert
	insertQuery := `
		INSERT INTO customer_schema.transaction_ledger (
			id, transaction_id, event_type, amount, currency, status, previous_hash, hash, created_at
		) VALUES (
			:id, :transaction_id, :event_type, :amount, :currency, :status, :previous_hash, :hash, :created_at
		)
	`

	_, err = tx.NamedExecContext(ctx, insertQuery, entry)
	if err != nil {
		return pkgerrors.Wrap(err, "failed to insert ledger entry")
	}

	return nil
}

// VerifyChain verifies the integrity of the ledger chain.
// Returns true if valid, false and error details if invalid.
func (r *LedgerRepository) VerifyChain(ctx context.Context) (bool, error) {
	var entries []domain.TransactionLedger
	query := `SELECT * FROM customer_schema.transaction_ledger ORDER BY created_at ASC`
	err := r.db.SelectContext(ctx, &entries, query)
	if err != nil {
		return false, pkgerrors.Wrap(err, "failed to read ledger")
	}

	if len(entries) == 0 {
		return true, nil
	}

	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"
	for i, entry := range entries {
		if entry.PreviousHash != prevHash {
			return false, fmt.Errorf("chain broken at index %d: expected prev_hash %s, got %s", i, prevHash, entry.PreviousHash)
		}

		data := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%d",
			entry.TransactionID.String(), entry.EventType, entry.Amount.String(), entry.Currency, entry.Status, entry.PreviousHash, entry.CreatedAt.UnixNano())

		hashBytes := sha256.Sum256([]byte(data))
		calcHash := hex.EncodeToString(hashBytes[:])

		if entry.Hash != calcHash {
			return false, fmt.Errorf("hash mismatch at index %d: expected %s, got %s", i, calcHash, entry.Hash)
		}

		prevHash = entry.Hash
	}

	return true, nil
}
