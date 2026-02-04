package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
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
	fmt.Println("🌱 Seeding Database...")

	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, relying on environment variables")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Init Security & Repos
	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatalf("Failed to init crypto service: %v", err)
	}
	userRepo := postgres.NewUserRepository(db, cryptoService)

	ctx := context.Background()

	// 1. Seed Users
	fmt.Println("Cleaning old data...")
	// Explicitly truncate child tables first to avoid FK issues if cascade isn't perfect or to be explicit
	db.Exec("TRUNCATE TABLE customer_schema.transaction_ledger CASCADE")
	db.Exec("TRUNCATE TABLE customer_schema.transactions CASCADE")
	db.Exec("TRUNCATE TABLE customer_schema.wallets CASCADE")
	db.Exec("TRUNCATE TABLE customer_schema.users CASCADE")

	// Admin tables
	db.Exec("TRUNCATE TABLE admin_schema.security_events CASCADE")
	db.Exec("TRUNCATE TABLE admin_schema.blocklist CASCADE")
	db.Exec("TRUNCATE TABLE admin_schema.system_health_snapshots CASCADE")

	fmt.Println("Creating Users...")

	adminID := seedUser(ctx, db, userRepo, "admin@kyd.com", "Admin", "User", domain.UserTypeAdmin, domain.KYCStatusVerified, "US")
	customerID := seedUser(ctx, db, userRepo, "customer@kyd.com", "Customer", "User", domain.UserTypeIndividual, domain.KYCStatusVerified, "MW")
	johnID := seedUser(ctx, db, userRepo, "john.doe@example.com", "John", "Doe", domain.UserTypeIndividual, domain.KYCStatusVerified, "MW")
	janeID := seedUser(ctx, db, userRepo, "jane.smith@example.com", "Jane", "Smith", domain.UserTypeIndividual, domain.KYCStatusVerified, "CN")

	// 2. Seed Wallets
	fmt.Println("Creating Wallets...")
	johnWalletID := seedWallet(db, johnID, "MWK", 500000.00)
	janeWalletID := seedWallet(db, janeID, "CNY", 1000.00)
	customerWalletID := seedWallet(db, customerID, "MWK", 250000.00)

	// 3. Seed Transactions
	fmt.Println("Creating Transactions with Ledger Security...")
	seedTransaction(db, johnID, johnWalletID, janeID, janeWalletID, 1000.00, "MWK", "completed")
	seedTransaction(db, janeID, janeWalletID, johnID, johnWalletID, 50.00, "CNY", "completed")
	seedTransaction(db, johnID, johnWalletID, janeID, janeWalletID, 50000.00, "MWK", "pending")
	seedTransaction(db, johnID, johnWalletID, janeID, janeWalletID, 2000000.00, "MWK", "pending")
	seedTransaction(db, customerID, customerWalletID, johnID, johnWalletID, 15000.00, "MWK", "completed")

	// 4. Seed Security Data
	fmt.Println("Seeding Security Data...")
	seedBlocklist(db)
	seedSecurityEvents(db, johnID, adminID)
	seedSystemHealth(db)

	fmt.Println("✅ Seeding Complete!")
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

func cleanUser(db *sqlx.DB, crypto *security.CryptoService, email string) {
	blindIndex := crypto.BlindIndex(email)
	_, err := db.Exec("DELETE FROM customer_schema.users WHERE email_hash = $1", blindIndex)
	if err != nil {
		log.Printf("Failed to clean user %s: %v", email, err)
	}
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

func seedWallet(db *sqlx.DB, userID, currency string, balance float64) string {
	var id string
	err := db.Get(&id, "SELECT id FROM customer_schema.wallets WHERE user_id = $1 AND currency = $2", userID, currency)
	if err == nil {
		return id
	}

	id = uuid.New().String()
	walletNumber := generateWalletNumber()
	_, err = db.Exec(`
		INSERT INTO customer_schema.wallets (id, user_id, wallet_address, currency, available_balance, ledger_balance, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $5, 'active', NOW())
	`, id, userID, walletNumber, currency, balance)

	if err != nil {
		log.Printf("Failed to create wallet for %s: %v", userID, err)
	}
	return id
}

func generateWalletNumber() string {
	// Generate 12 random digits
	max := new(big.Int).SetInt64(999999999999)
	n, _ := rand.Int(rand.Reader, max)
	// Pad with leading zeros to ensure 12 digits
	return fmt.Sprintf("4000%012d", n)
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
