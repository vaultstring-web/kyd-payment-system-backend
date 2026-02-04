package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"

	"kyd/internal/repository/postgres"
	"kyd/internal/security"
	"kyd/internal/wallet"
	"kyd/pkg/logger"
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
	fmt.Println("KYD PAYMENT SYSTEM - API VERIFICATION (Direct Service Call)")
	fmt.Println("=========================================================")

	// 1. Setup Database Connection
	// Using hardcoded credentials for simplicity in this verification script
	dbURL := "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	if url := os.Getenv("DATABASE_URL"); url != "" {
		dbURL = url
	}

	fmt.Printf("Connecting to database: %s\n", dbURL)
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	fmt.Println("[PASS] Database Connected")

	// 2. Initialize Repositories and Services
	logObj := logger.New("verify-api")

	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatalf("Failed to initialize crypto service: %v", err)
	}

	walletRepo := postgres.NewWalletRepository(db)
	txRepo := postgres.NewTransactionRepository(db)
	userRepo := postgres.NewUserRepository(db, cryptoService)

	walletService := wallet.NewService(walletRepo, txRepo, userRepo, logObj)

	// 3. Find John Doe
	ctx := context.Background()
	johnDoeEmail := "john.doe@example.com"
	johnDoe, err := userRepo.FindByEmail(ctx, johnDoeEmail)
	if err != nil {
		log.Fatalf("Failed to find user %s: %v", johnDoeEmail, err)
	}
	fmt.Printf("[PASS] Found User: %s %s (ID: %s, Country: %s)\n",
		johnDoe.FirstName, johnDoe.LastName, johnDoe.ID, johnDoe.CountryCode)

	// 4. Verify GetUserWallets (simulating GET /api/v1/wallets)
	fmt.Println("\n--- Verifying GetUserWallets (GET /api/v1/wallets) ---")
	wallets, err := walletService.GetUserWallets(ctx, johnDoe.ID)
	if err != nil {
		log.Fatalf("Failed to get user wallets: %v", err)
	}

	if len(wallets) == 0 {
		fmt.Println("[FAIL] No wallets found for John Doe!")
	} else {
		fmt.Printf("[PASS] Found %d wallets for John Doe:\n", len(wallets))
		for _, w := range wallets {
			fmt.Printf("  - WalletID: %s\n", w.WalletID)
			fmt.Printf("    Currency: %s\n", w.Currency)
			fmt.Printf("    Balance:  %s\n", w.AvailableBalance.String())
			fmt.Printf("    Address:  %s\n", w.FormattedWalletAddress)
			fmt.Printf("    Status:   %s\n", w.Status)

			// Verification Logic
			if w.Currency == "MWK" {
				fmt.Println("    [CHECK] Currency is MWK: PASS")
				if w.AvailableBalance.GreaterThan(decimal.Zero) {
					fmt.Println("    [CHECK] Balance > 0: PASS")
				} else {
					fmt.Println("    [CHECK] Balance > 0: FAIL (Is this expected?)")
				}
			} else {
				fmt.Printf("    [CHECK] Currency is %s (Non-MWK)\n", w.Currency)
			}
			fmt.Println("")
		}
	}

	// 5. Verify GetUserTransactions (simulating GET /api/v1/transactions)
	fmt.Println("\n--- Verifying GetUserTransactions (GET /api/v1/transactions) ---")
	txs, err := txRepo.FindByUserID(ctx, johnDoe.ID, 10, 0)
	if err != nil {
		log.Fatalf("Failed to get user transactions: %v", err)
	}

	if len(txs) == 0 {
		fmt.Println("[WARN] No transactions found for John Doe (This might be expected if new DB)")
	} else {
		fmt.Printf("[PASS] Found %d transactions for John Doe:\n", len(txs))
		for _, tx := range txs {
			fmt.Printf("  - Ref: %s | Amount: %s %s | Fee: %s %s | Status: %s\n",
				tx.Reference, tx.Amount, tx.Currency, tx.FeeAmount, tx.FeeCurrency, tx.Status)
		}
	}

	fmt.Println("=========================================================")
	fmt.Println("VERIFICATION COMPLETE")
	fmt.Println("=========================================================")
}
