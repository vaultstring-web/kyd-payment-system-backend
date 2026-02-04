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
	"github.com/shopspring/decimal"

	"kyd/internal/domain"
	"kyd/internal/forex"
	"kyd/internal/ledger"
	"kyd/internal/notification"
	"kyd/internal/payment"
	"kyd/internal/repository/postgres"
	"kyd/internal/security"
	"kyd/pkg/config"
	"kyd/pkg/logger"
)

// Mock Notification Service
type MockNotifier struct{}

func (m *MockNotifier) Notify(ctx context.Context, userID uuid.UUID, eventType string, data map[string]interface{}) error {
	fmt.Printf("[NOTIFICATION] User: %s, Event: %s, Data: %v\n", userID, eventType, data)
	return nil
}
func (m *MockNotifier) SendRaw(ctx context.Context, n *notification.Notification) error {
	return nil
}

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
	fmt.Println("KYD PAYMENT SYSTEM - VERIFY FIX (Cross-Border)")
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

	// 2. Setup Services
	logObj := logger.New("verify-fix")
	cfg := config.Load()
	cfg.Risk.MaxDailyLimit = 1000000000 // High limit
	cfg.Risk.EnableCircuitBreaker = false

	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatalf("Failed to init crypto service: %v", err)
	}

	walletRepo := postgres.NewWalletRepository(db)
	txRepo := postgres.NewTransactionRepository(db)
	userRepo := postgres.NewUserRepository(db, cryptoService)
	ledgerRepo := postgres.NewLedgerRepository(db)
	forexRepo := postgres.NewForexRepository(db)
	auditRepo := postgres.NewAuditRepository(db)
	securityRepo := postgres.NewSecurityRepository(db)
	ledgerService := ledger.NewService(db, ledgerRepo)
	forexService := forex.NewService(forexRepo, nil, nil, logObj)
	notifier := &MockNotifier{}

	paymentService := payment.NewService(
		txRepo,
		walletRepo,
		forexService,
		ledgerService,
		userRepo,
		notifier,
		auditRepo,
		securityRepo,
		logObj,
		cfg,
	)

	ctx := context.Background()

	// 3. Find Users
	johnDoe, err := userRepo.FindByEmail(ctx, "john.doe@example.com")
	if err != nil {
		log.Fatalf("John Doe not found: %v", err)
	}
	janeDoe, err := userRepo.FindByEmail(ctx, "jane.smith@example.com")
	if err != nil {
		log.Fatalf("Jane Smith not found: %v", err)
	}

	fmt.Printf("Sender: %s %s (ID: %s, Currency: MWK)\n", johnDoe.FirstName, johnDoe.LastName, johnDoe.ID)
	fmt.Printf("Receiver: %s %s (ID: %s, Currency: CNY)\n", janeDoe.FirstName, janeDoe.LastName, janeDoe.ID)

	// Fetch Receiver Wallet to get Address
	janeWallet, err := walletRepo.FindByUserAndCurrency(ctx, janeDoe.ID, "CNY")
	if err != nil {
		log.Fatalf("Jane's CNY wallet not found: %v", err)
	}
	fmt.Printf("Receiver Wallet Address: %s\n", *janeWallet.WalletAddress)

	// 4. Initiate Payment
	req := &payment.InitiatePaymentRequest{
		SenderID:              johnDoe.ID,
		ReceiverID:            janeDoe.ID,
		ReceiverWalletAddress: *janeWallet.WalletAddress,
		Amount:                decimal.NewFromInt(100),
		Currency:              "MWK",
		Description:           "Cross-border verification MWK -> CNY",
		Channel:               "api",
		Reference:             uuid.New().String(),
		DeviceID:              "system-scheduler",
	}

	fmt.Println("\nInitiating Payment of 100 MWK...")
	resp, err := paymentService.InitiatePayment(ctx, req)
	if err != nil {
		log.Fatalf("Payment Failed: %v", err)
	}
	fmt.Printf("Payment Success! TxID: %s, Status: %s\n", resp.Transaction.ID, resp.Transaction.Status)

	if resp.Transaction.Status != domain.TransactionStatusCompleted {
		fmt.Printf("WARNING: Transaction status is %s, expected completed.\n", resp.Transaction.Status)
	}

	// 5. Verify Ledger Balance Update
	fmt.Println("\nVerifying Ledger Balances in DB...")
	var ledgerBal decimal.Decimal
	var availBal decimal.Decimal

	// Check Sender
	err = db.QueryRow("SELECT available_balance, ledger_balance FROM customer_schema.wallets WHERE user_id = $1 AND currency = 'MWK'", johnDoe.ID).Scan(&availBal, &ledgerBal)
	if err != nil {
		log.Printf("Failed to query John's balance: %v", err)
	} else {
		fmt.Printf("John's MWK Balance: Available=%s, Ledger=%s\n", availBal, ledgerBal)
		if availBal.Equal(ledgerBal) {
			fmt.Println("[PASS] John's Available and Ledger balances match.")
		} else {
			fmt.Println("[WARN] John's balances do NOT match (might be expected if pending).")
		}
	}

	// Check Receiver
	err = db.QueryRow("SELECT available_balance, ledger_balance FROM customer_schema.wallets WHERE user_id = $1 AND currency = 'CNY'", janeDoe.ID).Scan(&availBal, &ledgerBal)
	if err != nil {
		log.Printf("Failed to query Jane's balance: %v", err)
	} else {
		fmt.Printf("Jane's CNY Balance: Available=%s, Ledger=%s\n", availBal, ledgerBal)
	}

	fmt.Println("=========================================================")
	fmt.Println("VERIFICATION COMPLETE")
	fmt.Println("=========================================================")
}
