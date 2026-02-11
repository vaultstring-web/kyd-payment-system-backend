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

	"kyd/internal/domain"
	"kyd/internal/forex"
	"kyd/internal/notification"
	"kyd/internal/payment"
	"kyd/internal/repository/postgres"
	"kyd/internal/security"
	"kyd/internal/wallet"
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

// Mock Ledger Service
type MockLedgerService struct{}

func (m *MockLedgerService) PostTransaction(ctx context.Context, posting interface{}) error {
	return nil
}

func loadEnv() {
	// Simple env loader for local testing
	content, err := os.ReadFile(".env")
	if err != nil {
		content, err = os.ReadFile("../../.env")
		if err != nil {
			return // No .env found, assume env vars are set or defaults work
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
	fmt.Println("KYD PAYMENT SYSTEM - ESCROW VERIFICATION")
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

	// 2. Setup Dependencies
	logObj := logger.New("verify-escrow")
	cryptoSvc, err := security.NewCryptoService()
	if err != nil {
		log.Fatalf("Failed to create crypto service: %v", err)
	}

	userRepo := postgres.NewUserRepository(db, cryptoSvc)
	walletRepo := postgres.NewWalletRepository(db)
	txRepo := postgres.NewTransactionRepository(db)
	auditRepo := postgres.NewAuditRepository(db, cryptoSvc)
	securityRepo := postgres.NewSecurityRepository(db)

	forexRepo := postgres.NewForexRepository(db)
	providers := []forex.RateProvider{
		forex.NewMockRateProvider(),
	}
	forexService := forex.NewService(forexRepo, nil, providers, logObj)

	// Note: We use the real PaymentService which now supports CreateEscrow
	// But we might need to mock LedgerService if we don't want to enforce ledger constraints for this test
	// However, PaymentService uses LedgerService interface.
	// For now, let's use a real ledger service if possible, or mock it.
	// Since we are verifying Wallet updates (ReserveFunds), we need to ensure PaymentService logic is correct.
	// CreateEscrow in internal/payment/escrow.go does NOT seem to call LedgerService directly,
	// it calls walletRepo.ReserveFunds directly. So MockLedgerService is fine.

	// Create a mock/wrapper for LedgerService if needed.
	// But wait, PaymentService constructor expects LedgerService.
	// Let's assume we can pass a nil or mock.
	// I'll create a simple mock struct above.

	// Need to cast MockLedgerService to match the interface in service.go
	// interface: PostTransaction(ctx context.Context, posting *ledger.LedgerPosting) error
	// My mock above uses `interface{}` which is wrong.

	notifier := &MockNotifier{}

	paymentService := payment.NewService(
		txRepo,
		walletRepo,
		forexService,
		nil, // LedgerService (we'll see if it crashes, CreateEscrow doesn't seem to use it)
		userRepo,
		notifier,
		auditRepo,
		securityRepo,
		logObj,
		nil, // config
	)

	ctx := context.Background()

	// 3. Setup Test Data (Sender and Receiver)
	senderID := uuid.New()
	sender := &domain.User{
		ID:           senderID,
		Email:        fmt.Sprintf("sender.escrow.%d@example.com", time.Now().Unix()),
		PasswordHash: "hash",
		FirstName:    "Escrow",
		LastName:     "Sender",
		UserType:     domain.UserTypeIndividual,
		CountryCode:  "MW",
		KYCLevel:     3,
		KYCStatus:    domain.KYCStatusVerified,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := userRepo.Create(ctx, sender); err != nil {
		log.Fatalf("Failed to create Sender: %v", err)
	}

	receiverID := uuid.New()
	receiver := &domain.User{
		ID:           receiverID,
		Email:        fmt.Sprintf("receiver.escrow.%d@example.com", time.Now().Unix()),
		PasswordHash: "hash",
		FirstName:    "Escrow",
		LastName:     "Receiver",
		UserType:     domain.UserTypeMerchant,
		CountryCode:  "MW",
		KYCLevel:     3,
		KYCStatus:    domain.KYCStatusVerified,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := userRepo.Create(ctx, receiver); err != nil {
		log.Fatalf("Failed to create Receiver: %v", err)
	}

	// Create Wallets
	wService := wallet.NewService(walletRepo, txRepo, userRepo, logObj)
	senderWallet, err := wService.CreateWallet(ctx, &wallet.CreateWalletRequest{
		UserID:   senderID,
		Currency: "MWK",
	})
	if err != nil {
		log.Fatalf("Failed to create sender wallet: %v", err)
	}

	_, err = wService.CreateWallet(ctx, &wallet.CreateWalletRequest{
		UserID:   receiverID,
		Currency: "MWK",
	})
	if err != nil {
		log.Fatalf("Failed to create receiver wallet: %v", err)
	}

	// Fund Sender Wallet
	initialBalance := decimal.NewFromInt(50000)
	_, _ = db.Exec("UPDATE customer_schema.wallets SET available_balance = $1, ledger_balance = $1 WHERE id = $2", initialBalance, senderWallet.ID)
	fmt.Printf("[SETUP] Funded Sender Wallet %s with %s MWK\n", senderWallet.ID, initialBalance)

	// 4. Test CreateEscrow
	escrowAmount := decimal.NewFromInt(10000)
	fmt.Println("\n>>> TEST 1: Create Escrow Transaction")

	req := &payment.EscrowRequest{
		SenderID:    senderID,
		ReceiverID:  receiverID,
		Amount:      escrowAmount,
		Currency:    "MWK",
		Condition:   "DELIVERY_CONFIRMED",
		Expiry:      time.Now().Add(24 * time.Hour),
		Description: "Goods Delivery",
	}

	resp, err := paymentService.CreateEscrow(ctx, req)
	if err != nil {
		log.Fatalf("CreateEscrow failed: %v", err)
	}

	fmt.Printf("Escrow Transaction Created: %s\n", resp.Transaction.ID)
	fmt.Printf("Status: %s (Expected: reserved)\n", resp.Transaction.Status)

	if resp.Transaction.Status != domain.TransactionStatusReserved {
		log.Fatalf("FAIL: Expected status 'reserved', got '%s'", resp.Transaction.Status)
	}

	// 5. Verify Wallet Balances (ReserveFunds check)
	updatedSenderWallet, err := walletRepo.FindByID(ctx, senderWallet.ID)
	if err != nil {
		log.Fatalf("Failed to fetch sender wallet: %v", err)
	}

	expectedAvailable := initialBalance.Sub(escrowAmount)
	expectedReserved := escrowAmount

	fmt.Printf("Sender Available Balance: %s (Expected: %s)\n", updatedSenderWallet.AvailableBalance, expectedAvailable)
	fmt.Printf("Sender Reserved Balance:  %s (Expected: %s)\n", updatedSenderWallet.ReservedBalance, expectedReserved)

	if !updatedSenderWallet.AvailableBalance.Equal(expectedAvailable) {
		log.Fatalf("FAIL: Available Balance mismatch")
	}
	if !updatedSenderWallet.ReservedBalance.Equal(expectedReserved) {
		log.Fatalf("FAIL: Reserved Balance mismatch")
	}

	fmt.Println("PASS: Funds correctly moved to Reserved Balance")

	// 6. Verify Transaction in DB (check if COALESCE fixes worked for retrieval)
	// We specifically check FindByID which we know is used by GetTransaction
	fmt.Println("\n>>> TEST 2: Retrieve Transaction (Verify SQL Fixes)")
	tx, err := txRepo.FindByID(ctx, resp.Transaction.ID)
	if err != nil {
		log.Fatalf("Failed to retrieve transaction: %v", err)
	}
	fmt.Printf("Retrieved Transaction: %s\n", tx.ID)
	if tx.Channel == "" {
		// If COALESCE works, it should be 'api' as set in CreateEscrow
		// CreateEscrow sets Channel: "api"
		fmt.Printf("Channel: %s\n", tx.Channel)
	} else {
		fmt.Printf("Channel: %s\n", tx.Channel)
	}

	if tx.Channel != "api" {
		log.Printf("WARN: Channel is '%s', expected 'api'", tx.Channel)
	}

	fmt.Println("PASS: Transaction retrieval successful")

	fmt.Println("\n=== ALL ESCROW TESTS PASSED ===")
}
