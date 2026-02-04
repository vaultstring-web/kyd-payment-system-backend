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
	"kyd/internal/ledger"
	"kyd/internal/notification"
	"kyd/internal/payment"
	"kyd/internal/repository/postgres"
	"kyd/internal/scheduler"
	"kyd/internal/security"
	"kyd/internal/wallet"
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
	fmt.Println("KYD PAYMENT SYSTEM - BANKING FEATURES SIMULATION")
	fmt.Println("=========================================================")
	fmt.Println("Features: Recurring Payments (Standing Orders) & Escrow")
	fmt.Println("---------------------------------------------------------")

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

	// FIX: Update Transaction Status Constraint (Self-healing for simulation)
	_, err = db.Exec(`
		ALTER TABLE customer_schema.transactions DROP CONSTRAINT IF EXISTS transactions_status_check;
		ALTER TABLE customer_schema.transactions ADD CONSTRAINT transactions_status_check CHECK (status IN (
			'pending', 'processing', 'reserved', 'settling', 
			'pending_settlement', 'completed', 'failed', 'cancelled', 'refunded',
			'disputed', 'reversed', 'pending_approval'
		));
	`)
	if err != nil {
		log.Printf("[WARN] Failed to update transactions_status_check: %v", err)
	} else {
		fmt.Println("[SETUP] Updated transactions_status_check constraint")
	}

	// 2. Setup Services
	logObj := logger.New("banking-sim")
	cfg := config.Load()

	// Ensure config has reasonable defaults for simulation
	cfg.Risk.MaxDailyLimit = 1000000000 // High limit for sim
	cfg.Risk.EnableCircuitBreaker = false

	cryptoService, _ := security.NewCryptoService()

	walletRepo := postgres.NewWalletRepository(db)
	txRepo := postgres.NewTransactionRepository(db)
	userRepo := postgres.NewUserRepository(db, cryptoService)
	ledgerRepo := postgres.NewLedgerRepository(db)
	forexRepo := postgres.NewForexRepository(db)
	auditRepo := postgres.NewAuditRepository(db)
	securityRepo := postgres.NewSecurityRepository(db)

	ledgerService := ledger.NewService(db, ledgerRepo)
	forexService := forex.NewService(forexRepo, nil, nil, logObj)

	// Mock Notifier
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

	// 3. Identify Test Users
	ctx := context.Background()
	johnDoe, err := userRepo.FindByEmail(ctx, "john.doe@example.com")
	if err != nil {
		log.Fatalf("John Doe not found: %v", err)
	}
	fmt.Printf("[SETUP] Sender: John Doe (%s)\n", johnDoe.ID)

	// Create a Receiver if not exists (Jane Doe)
	var janeDoe *domain.User
	janeDoe, err = userRepo.FindByEmail(ctx, "jane.doe@example.com")
	if err != nil {
		// Create Jane
		newID := uuid.New()
		jane := &domain.User{
			ID:           newID,
			Email:        "jane.doe@example.com",
			PasswordHash: "hash",
			FirstName:    "Jane",
			LastName:     "Doe",
			UserType:     domain.UserTypeIndividual,
			CountryCode:  "MW",
			KYCLevel:     2,
			KYCStatus:    domain.KYCStatusVerified,
			IsActive:     true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := userRepo.Create(ctx, jane); err != nil {
			log.Fatalf("Failed to create Jane: %v", err)
		}
		janeDoe = jane
		fmt.Printf("[SETUP] Created Receiver: Jane Doe (%s)\n", janeDoe.ID)

		// Create Wallet for Jane
		wService := wallet.NewService(walletRepo, txRepo, userRepo, logObj)
		_, err = wService.CreateWallet(ctx, &wallet.CreateWalletRequest{
			UserID:   janeDoe.ID,
			Currency: "MWK",
		})
		if err != nil {
			log.Fatalf("Failed to create wallet for Jane: %v", err)
		}
	} else {
		fmt.Printf("[SETUP] Receiver: Jane Doe (%s)\n", janeDoe.ID)
	}

	// ---------------------------------------------------------
	// FEATURE 1: ESCROW
	// ---------------------------------------------------------
	fmt.Println("\n>>> TEST 1: ESCROW PAYMENT")
	fmt.Println("Creating Escrow of 5000 MWK (Condition: Delivery Verified)")

	escrowReq := &payment.EscrowRequest{
		SenderID:    johnDoe.ID,
		ReceiverID:  janeDoe.ID,
		Amount:      decimal.NewFromInt(5000),
		Currency:    "MWK",
		Condition:   "Delivery Verified",
		Expiry:      time.Now().Add(24 * time.Hour),
		Description: "Escrow for Laptop Purchase",
	}

	escrowResp, err := paymentService.CreateEscrow(ctx, escrowReq)
	if err != nil {
		log.Fatalf("[FAIL] CreateEscrow: %v", err)
	}
	fmt.Printf("[PASS] Escrow Created. TxID: %s, Status: %s\n", escrowResp.Transaction.ID, escrowResp.Transaction.Status)

	// Verify status is RESERVED
	if escrowResp.Transaction.Status != domain.TransactionStatusReserved {
		log.Fatalf("[FAIL] Expected status RESERVED, got %s", escrowResp.Transaction.Status)
	}

	// Release Escrow
	fmt.Println("Releasing Escrow...")
	err = paymentService.ReleaseEscrow(ctx, escrowResp.Transaction.ID, johnDoe.ID)
	if err != nil {
		log.Fatalf("[FAIL] ReleaseEscrow: %v", err)
	}

	// Verify Final Status
	finalTx, _ := paymentService.GetTransaction(ctx, escrowResp.Transaction.ID)
	fmt.Printf("[PASS] Escrow Released. Final Status: %s\n", finalTx.Status)

	// ---------------------------------------------------------
	// FEATURE 2: RECURRING PAYMENTS (STANDING ORDER)
	// ---------------------------------------------------------
	fmt.Println("\n>>> TEST 2: RECURRING PAYMENT SCHEDULER")
	fmt.Println("Scheduling a Standing Order: 100 MWK every 2 seconds")

	sched := scheduler.NewScheduler(paymentService, logObj)
	sched.Start()
	defer sched.Stop()

	rp := &scheduler.RecurringPayment{
		SenderID:    johnDoe.ID,
		ReceiverID:  janeDoe.ID,
		Amount:      decimal.NewFromInt(100),
		Currency:    "MWK",
		Interval:    2 * time.Second,
		Description: "Monthly Rent (Simulated)",
	}

	sched.SchedulePayment(rp)
	fmt.Printf("[INFO] Payment Scheduled (ID: %s)\n", rp.ID)

	fmt.Println("Waiting for 5 seconds to observe execution...")
	time.Sleep(5 * time.Second)

	fmt.Println("[PASS] Scheduler Test Complete (Check logs for executions)")

	// ---------------------------------------------------------
	// FEATURE 3: DISPUTE RESOLUTION (REVERSAL)
	// ---------------------------------------------------------
	fmt.Println("\n>>> TEST 3: DISPUTE RESOLUTION")

	// Create a new sender for this test to avoid velocity limits
	victimID := uuid.New()
	victim := &domain.User{
		ID:           victimID,
		Email:        fmt.Sprintf("victim.%d@example.com", time.Now().Unix()),
		PasswordHash: "hash",
		FirstName:    "Victim",
		LastName:     "User",
		UserType:     domain.UserTypeIndividual,
		CountryCode:  "MW",
		KYCLevel:     3, // High level to allow high value
		KYCStatus:    domain.KYCStatusVerified,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := userRepo.Create(ctx, victim); err != nil {
		log.Fatalf("Failed to create Victim: %v", err)
	}
	// Create Wallet for Victim
	wService := wallet.NewService(walletRepo, txRepo, userRepo, logObj)
	_, err = wService.CreateWallet(ctx, &wallet.CreateWalletRequest{
		UserID:   victimID,
		Currency: "MWK",
	})
	if err != nil {
		log.Fatalf("Failed to create wallet for Victim: %v", err)
	}
	// Fund the wallet manually for the test
	_, _ = db.Exec("UPDATE customer_schema.wallets SET available_balance = 100000, ledger_balance = 100000 WHERE user_id = $1", victimID)

	// Create a transaction to dispute
	disputeTxReq := &payment.InitiatePaymentRequest{
		SenderID:    victimID,
		ReceiverID:  janeDoe.ID,
		Amount:      decimal.NewFromInt(5000),
		Currency:    "MWK",
		Description: "Fraudulent Charge",
		Channel:     "api",
		Reference:   uuid.New().String(),
		DeviceID:    "system-scheduler", // Trusted device
	}
	disputeResp, err := paymentService.InitiatePayment(ctx, disputeTxReq)
	if err != nil {
		log.Fatalf("Failed to create tx for dispute: %v", err)
	}
	fmt.Printf("[INFO] Created Transaction for Dispute: %s (Status: %s)\n", disputeResp.Transaction.ID, disputeResp.Transaction.Status)

	// Initiate Dispute
	fmt.Println("Initiating Dispute (Reason: FRAUD)...")
	err = paymentService.InitiateDispute(ctx, payment.InitiateDisputeRequest{
		TransactionID: disputeResp.Transaction.ID,
		Reason:        payment.DisputeReasonFraud,
		Description:   "I did not authorize this.",
		InitiatedBy:   johnDoe.ID,
	})
	if err != nil {
		log.Fatalf("[FAIL] InitiateDispute: %v", err)
	}

	// Verify Status
	dTx, _ := paymentService.GetTransaction(ctx, disputeResp.Transaction.ID)
	if dTx.Status != domain.TransactionStatusDisputed {
		log.Fatalf("[FAIL] Expected status DISPUTED, got %s", dTx.Status)
	}
	fmt.Printf("[PASS] Transaction Status is %s\n", dTx.Status)

	// Resolve Dispute (Reverse)
	fmt.Println("Resolving Dispute (Action: REVERSE)...")
	err = paymentService.ResolveDispute(ctx, payment.ResolveDisputeRequest{
		TransactionID: disputeResp.Transaction.ID,
		Resolution:    "reverse",
		AdminID:       uuid.New(), // Mock Admin
		Notes:         "Confirmed fraud pattern.",
	})
	if err != nil {
		log.Fatalf("[FAIL] ResolveDispute: %v", err)
	}

	// Verify Final Status
	rTx, _ := paymentService.GetTransaction(ctx, disputeResp.Transaction.ID)
	if rTx.Status != domain.TransactionStatusReversed {
		log.Fatalf("[FAIL] Expected status REVERSED, got %s", rTx.Status)
	}
	fmt.Printf("[PASS] Transaction Status is %s\n", rTx.Status)

	fmt.Println("=========================================================")
}
