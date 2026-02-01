package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"

	"kyd/internal/blockchain/ripple"
	"kyd/internal/blockchain/stellar"
	"kyd/internal/domain"
	"kyd/internal/repository/postgres"
	"kyd/internal/settlement"
	"kyd/pkg/logger"
)

func loadEnv() {
	// Simple env loader for local dev
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
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	fmt.Println("=========================================================")
	fmt.Println("KYD PAYMENT SYSTEM - SETTLEMENT VERIFICATION")
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

	// 2. Setup Repositories
	txRepo := postgres.NewTransactionRepository(db)
	settlementRepo := postgres.NewSettlementRepository(db)
	logObj := logger.New("verify-settlement")

	// 3. Setup Blockchain Connectors
	stellarConn, err := stellar.NewConnector("", "", false)
	if err != nil {
		log.Fatalf("Failed to create Stellar connector: %v", err)
	}
	rippleConn, err := ripple.NewConnector("", "")
	if err != nil {
		log.Fatalf("Failed to create Ripple connector: %v", err)
	}

	// 4. Setup Settlement Service
	// Note: NewService starts a worker in the background, but we will also trigger manually.
	svc := settlement.NewService(settlementRepo, txRepo, stellarConn, rippleConn, logObj)

	// --- STEP 0: Create a Fresh Transaction to Settle ---
	fmt.Println("--- Creating Fresh Transaction for Verification ---")

	// Fetch real user/wallet IDs to satisfy FK constraints
	var senderID, receiverID, senderWalletID, receiverWalletID uuid.UUID

	// Get Sender with a wallet
	err = db.QueryRow(`
		SELECT u.id, w.id 
		FROM customer_schema.users u 
		JOIN customer_schema.wallets w ON u.id = w.user_id 
		LIMIT 1
	`).Scan(&senderID, &senderWalletID)
	if err != nil {
		log.Fatalf("Failed to get sender with wallet: %v", err)
	}

	// Get Receiver (different one preferably)
	err = db.QueryRow(`
		SELECT u.id, w.id 
		FROM customer_schema.users u 
		JOIN customer_schema.wallets w ON u.id = w.user_id 
		WHERE u.id != $1 
		LIMIT 1
	`, senderID).Scan(&receiverID, &receiverWalletID)
	if err != nil {
		// Fallback to same user if only 1 exists
		receiverID = senderID
		receiverWalletID = senderWalletID
	}

	fmt.Printf("Using Sender: %s, Receiver: %s\n", senderID, receiverID)

	desc := "Settlement Verification Tx"
	channel := "web"
	category := "transfer"

	freshTx := &domain.Transaction{
		ID:                uuid.New(),
		Reference:         fmt.Sprintf("settle-test-%d", time.Now().UnixNano()),
		SenderID:          senderID,
		ReceiverID:        receiverID,
		SenderWalletID:    senderWalletID,
		ReceiverWalletID:  receiverWalletID,
		Amount:            decimal.NewFromInt(5000),
		Currency:          "MWK",
		ConvertedAmount:   decimal.NewFromFloat(19.5),
		ConvertedCurrency: "CNY",
		Status:            domain.TransactionStatusPendingSettlement,
		TransactionType:   domain.TransactionTypePayment,
		Channel:           &channel,
		Category:          &category,
		Description:       &desc,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	// We need to insert this directly using sqlx since we don't have full service stack here
	// Or use txRepo if it has Create (it usually does or Update)
	// TransactionRepository usually has Create. Let's check postgres/transaction.go or just use raw SQL.
	_, err = db.Exec(`
		INSERT INTO customer_schema.transactions (
			id, reference, sender_id, receiver_id, sender_wallet_id, receiver_wallet_id,
			amount, currency, converted_amount, converted_currency, net_amount, status, transaction_type, 
			channel, category, description, created_at, updated_at, 
			exchange_rate, fee_amount, fee_currency
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $9, $11, $12, $13, $14, $15, $16, $17, 0.0039, 0, 'MWK'
		)`,
		freshTx.ID, freshTx.Reference, freshTx.SenderID, freshTx.ReceiverID,
		freshTx.SenderWalletID, freshTx.ReceiverWalletID,
		freshTx.Amount, freshTx.Currency, freshTx.ConvertedAmount, freshTx.ConvertedCurrency,
		freshTx.Status, freshTx.TransactionType, freshTx.Channel, freshTx.Category,
		freshTx.Description, freshTx.CreatedAt, freshTx.UpdatedAt,
	)
	if err != nil {
		log.Fatalf("Failed to insert fresh transaction: %v", err)
	}
	fmt.Printf("Created Transaction: %s\n", freshTx.ID)

	// 5. Check Pending Transactions BEFORE
	ctx := context.Background()
	pending, err := txRepo.FindPendingSettlement(ctx, 100)
	if err != nil {
		log.Fatalf("Failed to find pending transactions: %v", err)
	}
	fmt.Printf("Pending Transactions (Before): %d\n", len(pending))

	if len(pending) == 0 {
		fmt.Println("No pending transactions to settle. Please run 'test_duplicate' or 'seed_admin_data' first.")
		// We can exit or create one. Let's assume the user wants to see it work on existing data.
		// If 0, we could create a dummy transaction, but simpler to ask user to seed.
		// Actually, let's just proceed, maybe the worker already picked them up?
	} else {
		// 6. Trigger Settlement Manually
		fmt.Println("--- Triggering ProcessPendingSettlements ---")
		if err := svc.ProcessPendingSettlements(ctx); err != nil {
			log.Fatalf("Settlement process failed: %v", err)
		}

		// 7. Wait for async confirmation (monitorSettlement runs in goroutine)
		// Service uses 10s interval, so we need to wait at least that long.
		fmt.Println("Waiting for blockchain confirmation (up to 30s)...")

		deadline := time.Now().Add(30 * time.Second)
		var allCompleted bool

		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)

			// Check status of all pending txs
			allCompleted = true
			for _, tx := range pending {
				updatedTx, err := txRepo.FindByID(ctx, tx.ID)
				if err != nil {
					fmt.Printf("Error fetching tx %s: %v\n", tx.ID, err)
					allCompleted = false
					continue
				}

				// Also check settlement status for debugging
				var settStatus string
				if updatedTx.SettlementID != nil {
					s, err := settlementRepo.FindByID(ctx, *updatedTx.SettlementID)
					if err == nil {
						settStatus = string(s.Status)
					} else {
						settStatus = fmt.Sprintf("Error: %v", err)
					}
				}

				fmt.Printf("Tx %s Status: %s, Settlement: %s\n", updatedTx.ID, updatedTx.Status, settStatus)

				if updatedTx.Status != domain.TransactionStatusCompleted {
					allCompleted = false
				}
			}

			if allCompleted {
				break
			}
			fmt.Println("---")
		}
		fmt.Println()
	}

	// 8. Verify Transactions AFTER
	// They should be status 'completed'
	if len(pending) > 0 {
		fmt.Println("--- Verifying Status Updates ---")
		for _, tx := range pending {
			updatedTx, err := txRepo.FindByID(ctx, tx.ID)
			if err != nil {
				log.Printf("Failed to fetch tx %s: %v", tx.ID, err)
				continue
			}
			fmt.Printf("Tx %s Status: %s (Expected: %s)\n", updatedTx.ID, updatedTx.Status, domain.TransactionStatusCompleted)

			if updatedTx.Status != domain.TransactionStatusCompleted {
				log.Printf("[FAIL] Transaction %s did not complete!", tx.ID)
			} else {
				fmt.Println("[PASS] Transaction settled successfully.")
			}
		}
	}

	// 9. Verify Settlement Record
	submitted, err := settlementRepo.FindSubmitted(ctx) // Should be empty if confirmed
	if err != nil {
		log.Printf("Error checking submitted settlements: %v", err)
	}
	if len(submitted) > 0 {
		fmt.Printf("Warning: %d settlements still in 'submitted' state (not confirmed yet).\n", len(submitted))
	} else {
		fmt.Println("All settlements confirmed (or none existed).")
	}

	fmt.Println("=========================================================")
	fmt.Println("SETTLEMENT VERIFICATION COMPLETE")
	fmt.Println("=========================================================")
}
