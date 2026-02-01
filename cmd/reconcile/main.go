package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
)

func main() {
	// Database Connection
	connStr := "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	fmt.Println("=========================================================")
	fmt.Printf("KYD PAYMENT SYSTEM - DAILY RECONCILIATION REPORT\n")
	fmt.Printf("Time: %s\n", time.Now().Format(time.RFC3339))
	fmt.Println("=========================================================")

	ctx := context.Background()

	// 1. Wallet Liability Check (Sum of all User Balances)
	fmt.Println("\n[1] Total Wallet Liabilities (User Funds)")
	rows, err := db.QueryContext(ctx, `
		SELECT currency, SUM(ledger_balance) 
		FROM customer_schema.wallets 
		GROUP BY currency
	`)
	if err != nil {
		log.Fatalf("Failed to query wallet totals: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var currency string
		var total decimal.Decimal
		if err := rows.Scan(&currency, &total); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		fmt.Printf("    - %s: %s\n", currency, total.String())
	}

	// 2. Negative Balance Check (Invariant Violation)
	fmt.Println("\n[2] Negative Balance Check")
	negRows, err := db.QueryContext(ctx, `
		SELECT id, user_id, ledger_balance, currency 
		FROM customer_schema.wallets 
		WHERE ledger_balance < 0 OR available_balance < 0
	`)
	if err != nil {
		log.Fatalf("Failed to query negative balances: %v", err)
	}
	defer negRows.Close()

	foundNegative := false
	for negRows.Next() {
		var id, userID, currency string
		var bal decimal.Decimal
		negRows.Scan(&id, &userID, &bal, &currency)
		fmt.Printf("    [ALERT] Wallet %s (User: %s) has NEGATIVE balance: %s %s\n", id, userID, bal.String(), currency)
		foundNegative = true
	}

	if !foundNegative {
		fmt.Println("    [PASS] No negative balances detected.")
	} else {
		fmt.Println("    [FAIL] Negative balances exist! Immediate investigation required.")
	}

	// 3. Orphaned Transactions Check (Transactions without Wallets)
	// This is just a placeholder logic, normally we'd check FK integrity
	// but SQL constraints usually handle this.
	// Instead, let's check for 'pending' transactions older than 24 hours (Stuck)
	fmt.Println("\n[3] Stuck Transactions Check (>24h Pending)")
	stuckRows, err := db.QueryContext(ctx, `
		SELECT id, status, created_at, amount, currency 
		FROM customer_schema.transactions 
		WHERE status = 'pending' 
		AND created_at < NOW() - INTERVAL '24 hours'
	`)
	if err != nil {
		log.Printf("Failed to query stuck transactions: %v", err) // Log but don't fail script
	} else {
		defer stuckRows.Close()
		foundStuck := false
		for stuckRows.Next() {
			var id, status, curr string
			var created time.Time
			var amt decimal.Decimal
			stuckRows.Scan(&id, &status, &created, &amt, &curr)
			fmt.Printf("    [WARN] Transaction %s is stuck in %s since %s (%s %s)\n", id, status, created.Format(time.RFC3339), amt.String(), curr)
			foundStuck = true
		}
		if !foundStuck {
			fmt.Println("    [PASS] No stuck transactions detected.")
		}
	}

	fmt.Println("\n=========================================================")
	fmt.Println("RECONCILIATION COMPLETE")
}
