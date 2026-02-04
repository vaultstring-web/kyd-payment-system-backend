package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"kyd/internal/repository/postgres"
)

func loadEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		content, err = os.ReadFile("../../.env")
		if err != nil {
			return
		}
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}

func main() {
	loadEnv()
	fmt.Println("=========================================================")
	fmt.Println("VERIFYING TRANSACTION QUERY (NULL HANDLING)")
	fmt.Println("=========================================================")

	// 1. Setup Database
	dbURL := "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	if url := os.Getenv("DATABASE_URL"); url != "" {
		dbURL = url
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	txRepo := postgres.NewTransactionRepository(db)
	ctx := context.Background()

	// Jane Smith ID from previous logs
	userID, _ := uuid.Parse("04feda31-75f6-4d3a-a707-fd9f67fe062f")

	fmt.Printf("Fetching transactions for UserID: %s\n", userID)
	txs, err := txRepo.FindByUserID(ctx, userID, 10, 0)
	if err != nil {
		log.Fatalf("[FAIL] Failed to fetch transactions: %v", err)
	}

	fmt.Printf("[PASS] Successfully fetched %d transactions\n", len(txs))
	for i, tx := range txs {
		fmt.Printf("  %d. ID: %s, Fee: %s %s\n", i+1, tx.ID, tx.FeeAmount, tx.FeeCurrency)
		if tx.FeeCurrency == "" && !tx.FeeAmount.IsZero() {
			fmt.Printf("     [WARN] FeeCurrency is empty but FeeAmount is %s\n", tx.FeeAmount)
		}
	}
}
