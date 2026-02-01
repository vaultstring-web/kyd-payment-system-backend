package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type LedgerEntry struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	WalletID      uuid.UUID
	EntryType     string
	Amount        decimal.Decimal
	Currency      string
	BalanceAfter  decimal.Decimal
	CreatedAt     time.Time
	PreviousHash  sql.NullString
	Hash          sql.NullString
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Fallback for local dev if not set
		dsn = "postgres://postgres:postgres@localhost:5432/kyd_payment?sslmode=disable"
		fmt.Println("DATABASE_URL not set, using default:", dsn)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("DB open error: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("DB ping error: %v", err)
	}

	mode := "verify"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "fix":
		// 1. Ensure columns exist
		ensureColumns(db)
		// 2. Backfill hashes
		backfillHashes(db)
		fmt.Println("Ledger immutability fix complete.")
	case "verify":
		verifyLedger(db)
	default:
		fmt.Println("Usage: go run main.go [verify|fix]")
		os.Exit(1)
	}
}

func verifyLedger(db *sql.DB) {
	fmt.Println("Starting ledger integrity verification...")

	// Get all wallet IDs
	rows, err := db.Query(`SELECT DISTINCT wallet_id FROM customer_schema.ledger_entries`)
	if err != nil {
		log.Fatalf("Failed to fetch wallets: %v", err)
	}
	defer rows.Close()

	var walletIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			log.Fatalf("Failed to scan wallet ID: %v", err)
		}
		walletIDs = append(walletIDs, id)
	}

	fmt.Printf("Verifying ledger for %d wallets...\n", len(walletIDs))

	var totalEntries int
	var tamperedWallets int

	for _, walletID := range walletIDs {
		count, tampered := verifyWallet(db, walletID)
		totalEntries += count
		if tampered {
			tamperedWallets++
		}
	}

	fmt.Println("---------------------------------------------------")
	fmt.Printf("Verification Complete.\n")
	fmt.Printf("Total Entries Verified: %d\n", totalEntries)
	if tamperedWallets > 0 {
		fmt.Printf("WARNING: %d wallets have TAMPERED ledger chains!\n", tamperedWallets)
		os.Exit(1)
	} else {
		fmt.Printf("SUCCESS: All ledger chains are valid.\n")
	}
}

func verifyWallet(db *sql.DB, walletID uuid.UUID) (int, bool) {
	// Fetch entries for this wallet ordered by creation time
	rows, err := db.Query(`
		SELECT id, transaction_id, wallet_id, entry_type, amount, currency, balance_after, created_at, previous_hash, hash
		FROM customer_schema.ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at ASC, id ASC
	`, walletID)
	if err != nil {
		log.Printf("Failed to fetch entries for wallet %s: %v", walletID, err)
		return 0, false
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransactionID, &e.WalletID, &e.EntryType, &e.Amount, &e.Currency, &e.BalanceAfter, &e.CreatedAt, &e.PreviousHash, &e.Hash); err != nil {
			log.Printf("Failed to scan entry: %v", err)
			return 0, false
		}
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		return 0, false
	}

	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"
	tampered := false

	for i, entry := range entries {
		// Check previous hash
		if !entry.PreviousHash.Valid || entry.PreviousHash.String != prevHash {
			fmt.Printf("[FAIL] Wallet %s Entry %d (%s): Previous Hash Mismatch!\n", walletID, i, entry.ID)
			fmt.Printf("       Expected: %s\n", prevHash)
			fmt.Printf("       Found:    %s\n", entry.PreviousHash.String)
			tampered = true
		}

		// Calculate expected hash
		expectedHash := calculateHash(prevHash, entry)

		// Check current hash
		if !entry.Hash.Valid || entry.Hash.String != expectedHash {
			fmt.Printf("[FAIL] Wallet %s Entry %d (%s): Hash Mismatch!\n", walletID, i, entry.ID)
			fmt.Printf("       Expected: %s\n", expectedHash)
			fmt.Printf("       Found:    %s\n", entry.Hash.String)
			tampered = true
		}

		prevHash = expectedHash
	}

	return len(entries), tampered
}

func ensureColumns(db *sql.DB) {
	cols := []string{"previous_hash", "hash"}
	for _, col := range cols {
		var exists bool
		query := fmt.Sprintf(`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns 
			WHERE table_schema = 'customer_schema' AND table_name = 'ledger_entries' AND column_name = '%s'
		)`, col)

		if err := db.QueryRow(query).Scan(&exists); err != nil {
			log.Fatalf("Column check failed for %s: %v", col, err)
		}

		if !exists {
			fmt.Printf("Adding column %s...\n", col)
			_, err := db.Exec(fmt.Sprintf(`ALTER TABLE customer_schema.ledger_entries ADD COLUMN %s VARCHAR(64)`, col))
			if err != nil {
				log.Fatalf("Failed to add column %s: %v", col, err)
			}
		} else {
			fmt.Printf("Column %s already exists.\n", col)
		}
	}
}

func backfillHashes(db *sql.DB) {
	fmt.Println("Starting hash backfill...")

	// Get all wallet IDs
	rows, err := db.Query(`SELECT DISTINCT wallet_id FROM customer_schema.ledger_entries`)
	if err != nil {
		log.Fatalf("Failed to fetch wallets: %v", err)
	}
	defer rows.Close()

	var walletIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			log.Fatalf("Failed to scan wallet ID: %v", err)
		}
		walletIDs = append(walletIDs, id)
	}

	fmt.Printf("Found %d wallets with ledger entries.\n", len(walletIDs))

	for _, walletID := range walletIDs {
		processWallet(db, walletID)
	}
}

func processWallet(db *sql.DB, walletID uuid.UUID) {
	// Fetch entries for this wallet ordered by creation time
	rows, err := db.Query(`
		SELECT id, transaction_id, wallet_id, entry_type, amount, currency, balance_after, created_at
		FROM customer_schema.ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at ASC, id ASC
	`, walletID)
	if err != nil {
		log.Printf("Failed to fetch entries for wallet %s: %v", walletID, err)
		return
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransactionID, &e.WalletID, &e.EntryType, &e.Amount, &e.Currency, &e.BalanceAfter, &e.CreatedAt); err != nil {
			log.Printf("Failed to scan entry: %v", err)
			return
		}
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		return
	}

	// Calculate chain
	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"

	tx, err := db.Begin()
	if err != nil {
		log.Printf("Failed to begin transaction: %v", err)
		return
	}

	for _, entry := range entries {
		hash := calculateHash(prevHash, entry)

		_, err := tx.Exec(`
			UPDATE customer_schema.ledger_entries 
			SET previous_hash = $1, hash = $2 
			WHERE id = $3
		`, prevHash, hash, entry.ID)

		if err != nil {
			tx.Rollback()
			log.Printf("Failed to update entry %s: %v", entry.ID, err)
			return
		}

		prevHash = hash
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit wallet %s: %v", walletID, err)
	} else {
		// fmt.Printf("Processed wallet %s (%d entries)\n", walletID, len(entries))
	}
}

func calculateHash(prevHash string, entry LedgerEntry) string {
	// Hash format: SHA256(prevHash + ID + TransactionID + WalletID + EntryType + Amount + Currency + BalanceAfter + CreatedAt)
	// Using standard string representation for consistency
	data := fmt.Sprintf("%s%s%s%s%s%s%s%s%s",
		prevHash,
		entry.ID.String(),
		entry.TransactionID.String(),
		entry.WalletID.String(),
		entry.EntryType,
		entry.Amount.String(),
		entry.Currency,
		entry.BalanceAfter.String(),
		entry.CreatedAt.UTC().Format(time.RFC3339Nano),
	)

	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
