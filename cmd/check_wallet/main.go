package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// Default to local dev
		dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// List all users
	query := `
		SELECT u.id, u.first_name, w.currency, w.available_balance::text
		FROM customer_schema.users u
		JOIN customer_schema.wallets w ON u.id = w.user_id
	`

	type Result struct {
		ID       string `db:"id"`
		Name     string `db:"first_name"` // Note: This will be encrypted
		Currency string `db:"currency"`
		Balance  string `db:"available_balance"`
	}

	var results []Result
	if err := db.Select(&results, query); err != nil {
		log.Fatalf("Query failed: %v", err)
	}

	for _, r := range results {
		fmt.Printf("User: %s | Currency: %s | Balance: %s\n", r.ID, r.Currency, r.Balance)
	}
}
