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
	fmt.Println("KYD PAYMENT SYSTEM - ADMIN FEATURES VERIFICATION")
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
	logObj := logger.New("verify-admin")
	cfg := config.Load()

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

	walletService := wallet.NewService(walletRepo, txRepo, userRepo, logObj)

	ctx := context.Background()

	// 3. Verify System Stats
	fmt.Println("\n--- Verifying GetSystemStats ---")
	stats, err := paymentService.GetSystemStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get system stats: %v", err)
	}
	fmt.Printf("[PASS] System Stats: TotalTx=%d, Volume=%s, Earnings=%s, Pending=%d\n",
		stats.TotalTransactions, stats.TotalVolume, stats.TotalFees, stats.PendingApprovals)

	// 4. Verify GetAuditLogs
	fmt.Println("\n--- Verifying GetAuditLogs ---")
	logs, totalLogs, err := paymentService.GetAuditLogs(ctx, 10, 0)
	if err != nil {
		log.Fatalf("Failed to get audit logs: %v", err)
	}
	fmt.Printf("[PASS] Audit Logs: Count=%d, Total=%d\n", len(logs), totalLogs)
	if len(logs) > 0 {
		fmt.Printf("       Latest Action: %s by User %s\n", logs[0].Action, logs[0].UserID)
	}

	// 5. Verify GetDisputes
	fmt.Println("\n--- Verifying GetDisputes ---")
	disputes, totalDisputes, err := paymentService.GetDisputes(ctx, 10, 0)
	if err != nil {
		log.Fatalf("Failed to get disputes: %v", err)
	}
	fmt.Printf("[PASS] Disputes: Count=%d, Total=%d\n", len(disputes), totalDisputes)

	// 6. Verify GetAllWallets
	fmt.Println("\n--- Verifying GetAllWallets ---")
	wallets, totalWallets, err := walletService.GetAllWallets(ctx, 10, 0)
	if err != nil {
		log.Fatalf("Failed to get all wallets: %v", err)
	}
	fmt.Printf("[PASS] All Wallets: Count=%d, Total=%d\n", len(wallets), totalWallets)
	if len(wallets) > 0 {
		addr := "N/A"
		if wallets[0].WalletAddress != nil {
			addr = *wallets[0].WalletAddress
		}
		fmt.Printf("       Sample Wallet: %s (Balance: %s)\n", addr, wallets[0].AvailableBalance)
	}

	// 7. Verify GetRiskAlerts
	fmt.Println("\n--- Verifying GetRiskAlerts ---")
	alerts, err := paymentService.GetRiskAlerts(ctx, 10, 0)
	if err != nil {
		log.Fatalf("Failed to get risk alerts: %v", err)
	}
	fmt.Printf("[PASS] Risk Alerts: Count=%d\n", len(alerts))

	// 8. Verify GetPendingTransactions
	fmt.Println("\n--- Verifying GetPendingTransactions ---")
	pending, totalPending, err := paymentService.GetPendingTransactions(ctx, 10, 0)
	if err != nil {
		log.Fatalf("Failed to get pending transactions: %v", err)
	}
	fmt.Printf("[PASS] Pending Transactions: Count=%d, Total=%d\n", len(pending), totalPending)

	fmt.Println("\n=========================================================")
	fmt.Println("VERIFICATION COMPLETE - ALL ADMIN FEATURES ACCESSIBLE")
	fmt.Println("=========================================================")
}
