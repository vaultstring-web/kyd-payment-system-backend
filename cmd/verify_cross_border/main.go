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
	fmt.Println("KYD PAYMENT SYSTEM - CROSS BORDER TRANSFER VERIFICATION")
	fmt.Println("=========================================================")
	fmt.Println("Scenario: Jane Smith (MWK) -> International (CNY)")
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

	// 2. Setup Services
	logObj := logger.New("cross-border-sim")
	cfg := config.Load()

	// Set reasonable limits
	cfg.Risk.MaxDailyLimit = 1000000000
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

	// Use MockRateProvider for verification script
	providers := []forex.RateProvider{
		forex.NewMockRateProvider(),
	}
	forexService := forex.NewService(forexRepo, nil, providers, logObj)
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

	// 3. Find or Create Jane Smith
	ctx := context.Background()
	var janeSmith *domain.User
	janeSmithEmail := "jane.smith@example.com"

	janeSmith, err = userRepo.FindByEmail(ctx, janeSmithEmail)
	if err != nil {
		// Create Jane Smith
		fmt.Println("Creating User: Jane Smith")
		newID := uuid.New()
		js := &domain.User{
			ID:           newID,
			Email:        janeSmithEmail,
			PasswordHash: "hash",
			FirstName:    "Jane",
			LastName:     "Smith",
			UserType:     domain.UserTypeIndividual,
			CountryCode:  "MW",
			KYCLevel:     3,
			KYCStatus:    domain.KYCStatusVerified,
			IsActive:     true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := userRepo.Create(ctx, js); err != nil {
			log.Fatalf("Failed to create Jane Smith: %v", err)
		}
		janeSmith = js
	} else {
		fmt.Printf("Found User: Jane Smith (%s)\n", janeSmith.ID)
	}

	// 4. Ensure Jane Smith has MWK Wallet with Funds
	wService := wallet.NewService(walletRepo, txRepo, userRepo, logObj)
	mwkWallet, err := walletRepo.FindByUserAndCurrency(ctx, janeSmith.ID, "MWK")
	if err != nil {
		// Create Wallet
		fmt.Println("Creating MWK Wallet for Jane Smith")
		mwkWallet, err = wService.CreateWallet(ctx, &wallet.CreateWalletRequest{
			UserID:   janeSmith.ID,
			Currency: "MWK",
		})
		if err != nil {
			log.Fatalf("Failed to create MWK wallet: %v", err)
		}
	}
	// Fund it
	fmt.Println("Funding Jane Smith's MWK Wallet with 1,000,000 MWK")
	_, err = db.Exec("UPDATE customer_schema.wallets SET available_balance = available_balance + 1000000, ledger_balance = ledger_balance + 1000000 WHERE id = $1", mwkWallet.ID)
	if err != nil {
		log.Fatalf("Failed to fund wallet: %v", err)
	}

	// Ensure Device is Trusted
	fmt.Println("Ensuring Device 'jane-laptop' is Trusted")
	deviceName := "Jane's Laptop"
	device := &domain.UserDevice{
		UserID:     janeSmith.ID,
		DeviceHash: "jane-laptop",
		DeviceName: &deviceName,
		IsTrusted:  true,
		CreatedAt:  time.Now(),
		LastSeenAt: time.Now(),
	}
	if err := userRepo.AddDevice(ctx, device); err != nil {
		log.Fatalf("Failed to add trusted device: %v", err)
	}
	// Force update is_trusted in case it already existed as untrusted
	_, err = db.Exec("UPDATE customer_schema.user_devices SET is_trusted = true WHERE user_id = $1 AND device_hash = 'jane-laptop'", janeSmith.ID)
	if err != nil {
		log.Fatalf("Failed to force update trusted device: %v", err)
	}

	// 5. Find or Create Receiver (International Supplier - CNY)
	supplierEmail := "supplier.cny@example.com"
	var supplier *domain.User
	supplier, err = userRepo.FindByEmail(ctx, supplierEmail)
	if err != nil {
		fmt.Println("Creating International Supplier (CNY)")
		newID := uuid.New()
		sup := &domain.User{
			ID:           newID,
			Email:        supplierEmail,
			PasswordHash: "hash",
			FirstName:    "China",
			LastName:     "Supplier",
			UserType:     domain.UserTypeMerchant,
			CountryCode:  "CN",
			KYCLevel:     3,
			KYCStatus:    domain.KYCStatusVerified,
			IsActive:     true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := userRepo.Create(ctx, sup); err != nil {
			log.Fatalf("Failed to create Supplier: %v", err)
		}
		supplier = sup
	}

	// Ensure Supplier has CNY Wallet
	_, err = walletRepo.FindByUserAndCurrency(ctx, supplier.ID, "CNY")
	if err != nil {
		fmt.Println("Creating CNY Wallet for Supplier")
		_, err = wService.CreateWallet(ctx, &wallet.CreateWalletRequest{
			UserID:   supplier.ID,
			Currency: "CNY",
		})
		if err != nil {
			log.Fatalf("Failed to create CNY wallet: %v", err)
		}
	}

	// 6. Initiate Cross-Border Payment (MWK -> CNY)
	fmt.Println("\n>>> INITIATING CROSS-BORDER TRANSFER")
	fmt.Println("Sender: Jane Smith (MWK)")
	fmt.Println("Receiver: China Supplier (CNY)")
	fmt.Println("Amount: 50,000 MWK")

	req := &payment.InitiatePaymentRequest{
		SenderID:            janeSmith.ID,
		ReceiverID:          supplier.ID,
		Amount:              decimal.NewFromInt(50000),
		Currency:            "MWK",
		DestinationCurrency: "CNY", // Explicitly requesting conversion
		Description:         "Import Payment for Electronics",
		Channel:             "web",
		Reference:           uuid.New().String(),
		DeviceID:            "jane-laptop",
	}

	resp, err := paymentService.InitiatePayment(ctx, req)
	if err != nil {
		log.Fatalf("Failed to initiate payment: %v", err)
	}

	fmt.Printf("[PASS] Transaction Created: %s\n", resp.Transaction.ID)
	fmt.Printf("  Status:            %s\n", resp.Transaction.Status)
	fmt.Printf("  Source Amount:     %s %s\n", resp.Transaction.Amount, resp.Transaction.Currency)
	fmt.Printf("  Converted Amount:  %s %s\n", resp.Transaction.ConvertedAmount, resp.Transaction.ConvertedCurrency)
	fmt.Printf("  Exchange Rate:     %s\n", resp.Transaction.ExchangeRate)
	fmt.Printf("  Fee:               %s %s\n", resp.Transaction.FeeAmount, resp.Transaction.FeeCurrency)

	if resp.Transaction.ConvertedCurrency != "CNY" {
		log.Fatalf("[FAIL] Expected ConvertedCurrency CNY, got %s", resp.Transaction.ConvertedCurrency)
	}

	if resp.Transaction.Status != domain.TransactionStatusPendingSettlement && resp.Transaction.Status != domain.TransactionStatusCompleted {
		fmt.Printf("[WARN] Transaction Status is %s (Expected pending_settlement or completed)\n", resp.Transaction.Status)
	}

	fmt.Println("=========================================================")
	fmt.Println("CROSS-BORDER VERIFICATION COMPLETE")
	fmt.Println("=========================================================")
}
