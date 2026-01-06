package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Println("DATABASE_URL not set")
		os.Exit(1)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Printf("DB open error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns 
		WHERE table_schema = 'customer_schema' AND table_name = 'ledger_entries' AND column_name = 'wallet_id'
	)`).Scan(&exists); err != nil {
		fmt.Printf("Column check failed: %v\n", err)
		os.Exit(1)
	}

	if !exists {
		fmt.Println("Adding wallet_id column to customer_schema.ledger_entries ...")
		_, err = db.Exec(`ALTER TABLE customer_schema.ledger_entries 
			ADD COLUMN wallet_id UUID REFERENCES customer_schema.wallets(id)`)
		if err != nil {
			fmt.Printf("ALTER TABLE failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Creating index on ledger_entries(wallet_id) ...")
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON customer_schema.ledger_entries(wallet_id)`)
	} else {
		fmt.Println("wallet_id column already exists on ledger_entries")
	}

	fmt.Println("Fix complete")
}
