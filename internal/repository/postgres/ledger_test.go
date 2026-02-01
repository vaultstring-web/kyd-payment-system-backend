package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string {
	return &s
}

func TestLedgerRepository_HashChaining(t *testing.T) {
	// Skip if no DB available
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skip("Skipping integration test: database not available")
	}
	defer db.Close()

	repo := NewLedgerRepository(db)
	txRepo := NewTransactionRepository(db)
	ctx := context.Background()

	// Cleanup previous test runs
	_, err = db.ExecContext(ctx, "TRUNCATE TABLE customer_schema.transaction_ledger CASCADE")
	require.NoError(t, err)

	// 0. Setup: Need a valid user and wallet to create transactions
	// Fetch an existing wallet
	var wallet domain.Wallet
	err = db.GetContext(ctx, &wallet, "SELECT * FROM customer_schema.wallets LIMIT 1")
	if err != nil {
		t.Skip("Skipping test: no wallets found in DB (run seed first)")
	}

	// 1. Create first transaction & entry
	tx1, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)

	txID1 := uuid.New()
	amount := decimal.NewFromFloat(100.0)
	now := time.Now().UTC()

	// Insert Transaction 1
	domainTx1 := &domain.Transaction{
		ID:                txID1,
		Reference:         uuid.New().String(),
		SenderID:          wallet.UserID,
		ReceiverID:        wallet.UserID, // Self-transfer for test
		SenderWalletID:    wallet.ID,
		ReceiverWalletID:  wallet.ID,
		Amount:            amount,
		Currency:          "USD",
		ExchangeRate:      decimal.NewFromFloat(1.0),
		ConvertedAmount:   amount,
		ConvertedCurrency: "USD",
		FeeAmount:         decimal.Zero,
		FeeCurrency:       "USD",
		NetAmount:         amount,
		Status:            domain.TransactionStatusCompleted,
		TransactionType:   domain.TransactionTypeTransfer,
		Channel:           strPtr("api"),
		Category:          strPtr("test"),
		Description:       strPtr("Test Tx 1"),
		InitiatedAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	err = txRepo.Create(ctx, domainTx1)
	require.NoError(t, err)

	// Create Ledger Entry 1
	err = repo.CreateEntryTx(ctx, tx1, txID1, "test_event", amount, "USD", "completed")
	require.NoError(t, err)

	err = tx1.Commit()
	require.NoError(t, err)

	// Fetch the first entry to get its hash
	var entry1 domain.TransactionLedger
	err = db.GetContext(ctx, &entry1, "SELECT * FROM customer_schema.transaction_ledger WHERE transaction_id = $1", txID1)
	require.NoError(t, err)
	assert.NotEmpty(t, entry1.Hash)

	// 2. Create second transaction & entry
	tx2, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)

	txID2 := uuid.New()

	// Insert Transaction 2
	domainTx2 := &domain.Transaction{
		ID:                txID2,
		Reference:         uuid.New().String(),
		SenderID:          wallet.UserID,
		ReceiverID:        wallet.UserID,
		SenderWalletID:    wallet.ID,
		ReceiverWalletID:  wallet.ID,
		Amount:            amount,
		Currency:          "USD",
		ExchangeRate:      decimal.NewFromFloat(1.0),
		ConvertedAmount:   amount,
		ConvertedCurrency: "USD",
		FeeAmount:         decimal.Zero,
		FeeCurrency:       "USD",
		NetAmount:         amount,
		Status:            domain.TransactionStatusCompleted,
		TransactionType:   domain.TransactionTypeTransfer,
		Channel:           strPtr("api"),
		Category:          strPtr("test"),
		Description:       strPtr("Test Tx 2"),
		InitiatedAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	err = txRepo.Create(ctx, domainTx2)
	require.NoError(t, err)

	err = repo.CreateEntryTx(ctx, tx2, txID2, "test_event_2", amount, "USD", "completed")
	require.NoError(t, err)

	err = tx2.Commit()
	require.NoError(t, err)

	// Fetch the second entry
	var entry2 domain.TransactionLedger
	err = db.GetContext(ctx, &entry2, "SELECT * FROM customer_schema.transaction_ledger WHERE transaction_id = $1", txID2)
	require.NoError(t, err)

	// 3. Verify Chaining
	assert.NotEmpty(t, entry2.PreviousHash)

	// Check if the chain is valid overall
	valid, err := repo.VerifyChain(ctx)
	assert.NoError(t, err)
	assert.True(t, valid, "Ledger chain should be valid")

	fmt.Printf("Entry 1 Hash: %s\n", entry1.Hash)
	fmt.Printf("Entry 2 PrevHash: %s\n", entry2.PreviousHash)
	fmt.Printf("Entry 2 Hash: %s\n", entry2.Hash)

	// 4. Test Tampering
	// Tamper with the amount of the first entry
	_, err = db.ExecContext(ctx, "UPDATE customer_schema.transaction_ledger SET amount = 9999 WHERE id = $1", entry1.ID)
	require.NoError(t, err)

	valid, err = repo.VerifyChain(ctx)
	assert.False(t, valid, "Ledger chain should be INVALID after tampering")
	assert.Error(t, err)
	fmt.Printf("Tampering Detected: %v\n", err)
}
