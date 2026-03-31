package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"

	"kyd/internal/domain"
	"kyd/internal/repository/postgres"
	"kyd/internal/security"
)

func main() {
	startTime := time.Now()
	fmt.Println("🌱 Seeding Database...")

	env := flag.String("env", "development", "Environment for seeding (e.g., development, testing, staging)")
	size := flag.String("size", "small", "Dataset size (small, medium, large)")
	dryRun := flag.Bool("dry-run", false, "Perform a dry run without modifying the database")
	flag.Parse()

	if *dryRun {
		fmt.Println("Performing a dry run. No database changes will be made.")
	}

	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, relying on environment variables")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		switch *env {
		case "testing":
			dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_test?sslmode=disable"
		case "staging":
			dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_staging?sslmode=disable"
		case "development":
			fallthrough
		default:
			dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
		}
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if *dryRun {
		fmt.Println("Dry run: Skipping database connection and truncation.")
		return // Exit early for dry run
	}

	// Init Security & Repos
	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatalf("Failed to init crypto service: %v", err)
	}
	userRepo := postgres.NewUserRepository(db, cryptoService)

	ctx := context.Background()

	// 1. Seed Users
	fmt.Println("Creating Users...")

	var numUsers int
	var numTransactions int

	switch *size {
	case "small":
		numUsers = 5
		numTransactions = 10
	case "medium":
		numUsers = 50
		numTransactions = 200
	case "large":
		numUsers = 500
		numTransactions = 2000
	default:
		log.Fatalf("Invalid size parameter: %s. Must be small, medium, or large.", *size)
	}

	// Fetch core users (seeded by consolidated migration)
	var adminID, providerFeeUserID, customerID, johnID, janeID string
	db.Get(&adminID, "SELECT id FROM customer_schema.users WHERE email = $1", "admin@kyd.com")
	db.Get(&providerFeeUserID, "SELECT id FROM customer_schema.users WHERE email = $1", "fees@kyd.com")
	db.Get(&customerID, "SELECT id FROM customer_schema.users WHERE email = $1", "customer@kyd.com")
	db.Get(&johnID, "SELECT id FROM customer_schema.users WHERE email = $1", "john.doe@example.com")
	db.Get(&janeID, "SELECT id FROM customer_schema.users WHERE email = $1", "jane.smith@example.com")

	// Generate additional users based on size
	var userIDs []string
	userIDs = append(userIDs, adminID, providerFeeUserID, customerID, johnID, janeID)

	for i := 0; i < numUsers-5; i++ { // -5 for the core users already seeded
		email := fmt.Sprintf("user%d@example.com", i)
		firstName := fmt.Sprintf("User%d", i)
		lastName := "Generated"
		country := "MW" // Default country
		if i%2 == 0 {
			country = "CN"
		}
		id := seedUser(ctx, db, userRepo, email, firstName, lastName, domain.UserTypeIndividual, domain.KYCStatusVerified, country)
		userIDs = append(userIDs, id)
	}

	// 2. Seed Wallets
	fmt.Println("Creating Wallets...")
	// Fetch core wallets (seeded by consolidated migration)
	var johnWalletID, janeWalletID, customerWalletID uuid.UUID
	db.Get(&johnWalletID, "SELECT id FROM customer_schema.wallets WHERE user_id = $1 AND currency = $2", johnID, "MWK")
	db.Get(&janeWalletID, "SELECT id FROM customer_schema.wallets WHERE user_id = $1 AND currency = $2", janeID, "CNY")
	db.Get(&customerWalletID, "SELECT id FROM customer_schema.wallets WHERE user_id = $1 AND currency = $2", customerID, "MWK")

	// Generate additional wallets for generated users
	var walletIDs []string
	walletIDs = append(walletIDs, johnWalletID.String(), janeWalletID.String(), customerWalletID.String())

	for _, userID := range userIDs {
		// Skip core users, their wallets are fetched above
		if userID == adminID || userID == providerFeeUserID || userID == johnID || userID == janeID || userID == customerID {
			continue
		}
		walletIDs = append(walletIDs, seedWallet(db, userID, "MWK", 100000.00))
	}

	// 3. Seed Transactions
	fmt.Println("Creating Transactions with Ledger Security...")
	// Initial transactions are now part of the consolidated migration
	// Generate additional transactions based on size
	for i := 0; i < numTransactions; i++ { // numTransactions now represents total additional transactions
		senderID := userIDs[i%len(userIDs)]
		receiverID := userIDs[(i+1)%len(userIDs)]
		senderWallet := walletIDs[i%len(walletIDs)]
		receiverWallet := walletIDs[(i+1)%len(walletIDs)]
		amount := float64(i%1000 + 100) // Random amount
		currency := "MWK"
		if i%3 == 0 {
			currency = "CNY"
		}
		seedTransaction(db, senderID, senderWallet, receiverID, receiverWallet, amount, currency, "completed")
	}

	// 4. Seed Security Data
	fmt.Println("Seeding Security Data...")
	seedBlocklist(db)
	seedSecurityEvents(db, johnID, adminID)
	seedSystemHealth(db)

	// 5. Validate Seeded Data
	fmt.Println("Validating seeded data...")
	validateSeededData(db, numUsers, numTransactions)

	fmt.Println("✅ Seeding Complete!")
	fmt.Printf("Total seeding time: %s\n", time.Since(startTime))
	fmt.Println("\n---------------------------------------------------")
	fmt.Println("🔑 Test Accounts Created:")
	fmt.Println("---------------------------------------------------")
	fmt.Println("Admin User:")
	fmt.Println("  Email:    admin@kyd.com")
	fmt.Println("  Password: password123")
	fmt.Println("  Role:     ADMIN")
	fmt.Println("---------------------------------------------------")
	fmt.Println("Customer User:")
	fmt.Println("  Email:    customer@kyd.com")
	fmt.Println("  Password: password123")
	fmt.Println("  Role:     INDIVIDUAL")
	fmt.Println("---------------------------------------------------")
	fmt.Println("Additional Users:")
	fmt.Println("  - john.doe@example.com (password123)")
	fmt.Println("  - jane.smith@example.com (password123)")
	fmt.Println("---------------------------------------------------")
}

func seedUser(ctx context.Context, db *sqlx.DB, repo *postgres.UserRepository, email, first, last string, role domain.UserType, kyc domain.KYCStatus, country string) string {
	hashedPwd, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)

	existing, err := repo.FindByEmail(ctx, email)
	if err == nil && existing != nil {
		fmt.Printf("User %s already exists (ID: %s). Updating...\n", email, existing.ID)
		// Force update role, password, kyc
		_, err := db.ExecContext(ctx, "UPDATE customer_schema.users SET password_hash = $1, user_type = $2, kyc_status = $3 WHERE id = $4",
			string(hashedPwd), role, kyc, existing.ID)
		if err != nil {
			log.Printf("Failed to update user %s: %v", email, err)
		}
		return existing.ID.String()
	}

	user := &domain.User{
		ID:           uuid.New(),
		Email:        email,
		Phone:        fmt.Sprintf("+%s%d", "1", time.Now().UnixNano()), // Random phone
		PasswordHash: string(hashedPwd),
		FirstName:    first,
		LastName:     last,
		UserType:     role,
		KYCLevel:     1,
		KYCStatus:    kyc,
		CountryCode:  country,
		RiskScore:    decimal.NewFromFloat(0),
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := repo.Create(ctx, user); err != nil {
		log.Printf("Failed to create user %s: %v", email, err)
		return user.ID.String()
	}

	fmt.Printf("Created user %s (%s)\n", email, user.ID)
	return user.ID.String()
}

func seedUserWithFixedID(ctx context.Context, db *sqlx.DB, repo *postgres.UserRepository, idStr, email, first, last string, role domain.UserType, kyc domain.KYCStatus, country string) string {
	hashedPwd, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	fixedID, err := uuid.Parse(idStr)
	if err != nil {
		fixedID = uuid.New()
	}

	existing, err := repo.FindByEmail(ctx, email)
	if err == nil && existing != nil {
		_, _ = db.ExecContext(ctx, "UPDATE customer_schema.users SET password_hash = $1, user_type = $2, kyc_status = $3 WHERE id = $4",
			string(hashedPwd), role, kyc, existing.ID)
		return existing.ID.String()
	}

	user := &domain.User{
		ID:           fixedID,
		Email:        email,
		Phone:        fmt.Sprintf("+%s%d", "1", time.Now().UnixNano()),
		PasswordHash: string(hashedPwd),
		FirstName:    first,
		LastName:     last,
		UserType:     role,
		KYCLevel:     1,
		KYCStatus:    kyc,
		CountryCode:  country,
		RiskScore:    decimal.NewFromFloat(0),
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := repo.Create(ctx, user); err != nil {
		log.Printf("Failed to create user %s: %v", email, err)
		return user.ID.String()
	}
	fmt.Printf("Created user %s (%s)\n", email, user.ID)
	return user.ID.String()
}

func seedWallet(db *sqlx.DB, userID string, currency string, balance float64) string {
	walletID := uuid.New().String()
	_, err := db.Exec(`
		INSERT INTO customer_schema.wallets (id, user_id, wallet_address, currency, available_balance, ledger_balance, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, walletID, userID, fmt.Sprintf("WALLET-%s-%s", currency, walletID[:8]), currency, balance, balance, "active", time.Now())
	if err != nil {
		log.Fatalf("Failed to seed wallet for user %s: %v", userID, err)
	}
	return walletID
}

func seedWalletWithAddress(db *sqlx.DB, userID, currency string, balance float64, address string) string {
	var id string
	err := db.Get(&id, "SELECT id FROM customer_schema.wallets WHERE user_id = $1 AND currency = $2", userID, currency)
	if err == nil {
		_, _ = db.Exec(`UPDATE customer_schema.wallets SET wallet_address = $1 WHERE id = $2`, address, id)
		return id
	}

	id = uuid.New().String()
	_, err = db.Exec(`
		INSERT INTO customer_schema.wallets (id, user_id, wallet_address, currency, available_balance, ledger_balance, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $5, 'active', NOW())
	`, id, userID, address, currency, balance)
	if err != nil {
		log.Printf("Failed to create wallet for %s: %v", userID, err)
	}
	return id
}

func generateWalletNumber() string {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatalf("Failed to generate random bytes for wallet number: %v", err)
	}
	return fmt.Sprintf("W%s", hex.EncodeToString(b))
}

func validateSeededData(db *sqlx.DB, expectedUsers, expectedTransactions int) {
	var userCount int
	err := db.Get(&userCount, "SELECT COUNT(*) FROM customer_schema.users")
	if err != nil {
		log.Fatalf("Failed to count users: %v", err)
	}
	if userCount < expectedUsers {
		log.Fatalf("Validation failed: Expected at least %d users, got %d", expectedUsers, userCount)
	}
	fmt.Printf("✅ User count validation passed: %d users\n", userCount)

	var walletCount int
	err = db.Get(&walletCount, "SELECT COUNT(*) FROM customer_schema.wallets")
	if err != nil {
		log.Fatalf("Failed to count wallets: %v", err)
	}
	if walletCount < expectedUsers { // At least one wallet per user
		log.Fatalf("Validation failed: Expected at least %d wallets, got %d", expectedUsers, walletCount)
	}
	fmt.Printf("✅ Wallet count validation passed: %d wallets\n", walletCount)

	var transactionCount int
	err = db.Get(&transactionCount, "SELECT COUNT(*) FROM customer_schema.transactions")
	if err != nil {
		log.Fatalf("Failed to count transactions: %v", err)
	}
	if transactionCount < expectedTransactions {
		log.Fatalf("Validation failed: Expected at least %d transactions, got %d", expectedTransactions, transactionCount)
	}
	fmt.Printf("✅ Transaction count validation passed: %d transactions\n", transactionCount)

	// Basic integrity check: ensure all wallets belong to existing users
	var orphanedWallets int
	err = db.Get(&orphanedWallets, `
		SELECT COUNT(*) FROM customer_schema.wallets w
		LEFT JOIN customer_schema.users u ON w.user_id = u.id
		WHERE u.id IS NULL
	`)
	if err != nil {
		log.Fatalf("Failed to check for orphaned wallets: %v", err)
	}
	if orphanedWallets > 0 {
		log.Fatalf("Validation failed: Found %d orphaned wallets", orphanedWallets)
	}
	fmt.Println("✅ Wallet-to-user integrity check passed")

	// Basic integrity check: ensure all transactions link to existing wallets
	var invalidTransactions int
	err = db.Get(&invalidTransactions, `
		SELECT COUNT(*) FROM customer_schema.transactions t
		LEFT JOIN customer_schema.wallets sw ON t.sender_wallet_id = sw.id
		LEFT JOIN customer_schema.wallets rw ON t.receiver_wallet_id = rw.id
		WHERE sw.id IS NULL OR rw.id IS NULL
	`)
	if err != nil {
		log.Fatalf("Failed to check for invalid transactions: %v", err)
	}
	if invalidTransactions > 0 {
		log.Fatalf("Validation failed: Found %d transactions with invalid wallet IDs", invalidTransactions)
	}
	fmt.Println("✅ Transaction-to-wallet integrity check passed")

	fmt.Println("✅ All data validation checks passed!")
}
func seedTransaction(db *sqlx.DB, senderID, senderWalletID, receiverID, receiverWalletID string, amount float64, currency, status string) {
	id := uuid.New().String()
	if senderID == "" {
		senderID = uuid.Nil.String()
	}

	txTime := time.Now().UTC()

	// 1. Insert Transaction
	_, err := db.Exec(`
		INSERT INTO customer_schema.transactions (
			id, sender_id, sender_wallet_id, receiver_id, receiver_wallet_id,
			amount, currency, exchange_rate, converted_amount, converted_currency, 
            fee_amount, net_amount, status, transaction_type, reference, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 1.0, $6, $7, 0, $6, $8, 'transfer', $9, $10)
	`, id, senderID, senderWalletID, receiverID, receiverWalletID, amount, currency, status, uuid.New().String(), txTime)

	if err != nil {
		log.Printf("Failed to create transaction: %v", err)
		return
	}

	// 2. Create Ledger Entry (Blockchain)
	// Get previous hash
	var previousHash string
	err = db.Get(&previousHash, "SELECT hash FROM customer_schema.transaction_ledger ORDER BY created_at DESC LIMIT 1")
	if err != nil {
		// Assume genesis
		previousHash = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	// Calculate Hash
	// Format: txID:eventType:amount:currency:status:previousHash:timestamp
	eventType := "transfer" // Mapping transaction_type
	amountDecimal := decimal.NewFromFloat(amount)

	data := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%d",
		id, eventType, amountDecimal.String(), currency, status, previousHash, txTime.UnixNano())

	hashBytes := sha256.Sum256([]byte(data))
	hash := hex.EncodeToString(hashBytes[:])

	// Insert Ledger
	_, err = db.Exec(`
		INSERT INTO customer_schema.transaction_ledger (
			id, transaction_id, event_type, amount, currency, status, previous_hash, hash, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, uuid.New(), id, eventType, amountDecimal, currency, status, previousHash, hash, txTime)

	if err != nil {
		log.Printf("Failed to create ledger entry for tx %s: %v", id, err)
	} else {
		fmt.Printf("⛓️  Secured Tx %s on Ledger (Hash: %s...)\n", id[:8], hash[:8])
	}
}

func seedBlocklist(db *sqlx.DB) {
	entries := []struct {
		Type   string
		Value  string
		Reason string
	}{
		{"ip", "192.168.1.100", "Suspicious activity detected"},
		{"email", "spammer@example.com", "Confirmed spammer"},
		{"wallet", "0x1234567890abcdef", "Sanctioned entity"},
		{"device", "device_id_8822", "Stolen device reported"},
	}

	for _, e := range entries {
		var count int
		db.Get(&count, "SELECT count(*) FROM admin_schema.blocklist WHERE value = $1", e.Value)
		if count > 0 {
			continue
		}

		_, err := db.Exec(`
			INSERT INTO admin_schema.blocklist (id, type, value, reason, is_active, created_at)
			VALUES ($1, $2, $3, $4, true, NOW())
		`, uuid.New(), e.Type, e.Value, e.Reason)
		if err != nil {
			log.Printf("Failed to seed blocklist %s: %v", e.Value, err)
		}
	}
}

func seedSecurityEvents(db *sqlx.DB, userID, adminID string) {
	events := []struct {
		Type     string
		Severity string
		Status   string
		UserID   string
		Details  string
	}{
		{"brute_force_attempt", "high", "open", userID, `{"ip": "203.0.113.45", "attempts": 5}`},
		{"suspicious_ip", "medium", "investigating", userID, `{"ip": "198.51.100.23", "location": "Unknown"}`},
		{"admin_login_failed", "low", "resolved", adminID, `{"ip": "192.168.1.5"}`},
		{"velocity_limit_exceeded", "critical", "open", userID, `{"amount": "2000000.00", "currency": "MWK"}`},
	}

	for _, e := range events {
		_, err := db.Exec(`
			INSERT INTO admin_schema.security_events (
				id, event_type, severity, user_id, details, status, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		`, uuid.New(), e.Type, e.Severity, e.UserID, e.Details, e.Status)
		if err != nil {
			log.Printf("Failed to seed event %s: %v", e.Type, err)
		}
	}
}

func seedSystemHealth(db *sqlx.DB) {
	metrics := []struct {
		Metric string
		Value  string
		Status string
		Change string
	}{
		{"System Status", "99.9%", "healthy", "+0.1%"},
		{"Active Users", "1,250", "healthy", "+12%"},
		{"Transaction Vol", "15.4K", "healthy", "+5%"},
		{"Error Rate", "0.05%", "healthy", "-0.01%"},
	}

	for _, m := range metrics {
		_, err := db.Exec(`
			INSERT INTO admin_schema.system_health_snapshots (
				id, metric, value, status, change, recorded_at
			) VALUES ($1, $2, $3, $4, $5, NOW())
		`, uuid.New(), m.Metric, m.Value, m.Status, m.Change)

		if err != nil {
			log.Printf("Failed to seed health metric %s: %v", m.Metric, err)
		}
	}
}
