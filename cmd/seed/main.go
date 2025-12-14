// Simple seeding tool to create/update a default user for auth login
// Usage (env overrides):
//
//	SEED_EMAIL=john.doe@example.com SEED_PASSWORD=Password123
//
// Reads DATABASE_URL and other core config via kyd/pkg/config
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"

	"kyd/internal/repository/postgres"
	"kyd/pkg/config"
	"kyd/pkg/domain"
	"kyd/pkg/logger"
)

func main() {
	log := logger.New("seed-user")

	cfg := config.Load()
	if err := cfg.ValidateCore(); err != nil {
		log.Fatal("Invalid configuration", map[string]interface{}{"error": err.Error()})
	}

	email := getenv("SEED_EMAIL", "john.doe@example.com")
	password := getenv("SEED_PASSWORD", "Password123")
	phone := getenv("SEED_PHONE", "+265991234567")
	first := getenv("SEED_FIRST", "John")
	last := getenv("SEED_LAST", "Doe")
	country := getenv("SEED_COUNTRY", "MW")

	db, err := sqlx.Connect("postgres", cfg.Database.URL)
	if err != nil {
		log.Fatal("Failed to connect to database", map[string]interface{}{"error": err.Error()})
	}
	defer db.Close()

	userRepo := postgres.NewUserRepository(db)
	walletRepo := postgres.NewWalletRepository(db)
	ctx := context.Background()

	// Ensure John exists and has MWK wallet funded
	johnID := ensureUser(ctx, userRepo, log, email, password, phone, first, last, country, domain.UserTypeIndividual, domain.KYCStatusVerified, 1)
	ensureWallet(ctx, walletRepo, log, johnID, domain.MWK, decimal.NewFromInt(1_000_000))

	// Ensure Wang exists and has CNY wallet funded
	wEmail := getenv("SEED_WANG_EMAIL", "wang.wei@example.com")
	wPass := getenv("SEED_WANG_PASSWORD", "Password123")
	wPhone := getenv("SEED_WANG_PHONE", "+86 13800138000")
	wFirst := getenv("SEED_WANG_FIRST", "Wang")
	wLast := getenv("SEED_WANG_LAST", "Wei")
	wCountry := getenv("SEED_WANG_COUNTRY", "CN")
	wangID := ensureUser(ctx, userRepo, log, wEmail, wPass, wPhone, wFirst, wLast, wCountry, domain.UserTypeMerchant, domain.KYCStatusPending, 0)
	ensureWallet(ctx, walletRepo, log, wangID, domain.CNY, decimal.NewFromInt(10_000))

	fmt.Println("OK: users and wallets seeded")
}

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func ensureUser(ctx context.Context, repo *postgres.UserRepository, log logger.Logger, email, password, phone, first, last, country string, userType domain.UserType, kyc domain.KYCStatus, kycLevel int) uuid.UUID {
	exists, err := repo.ExistsByEmail(ctx, email)
	if err != nil {
		log.Fatal("ExistsByEmail failed", map[string]interface{}{"error": err.Error()})
	}
	now := time.Now()
	if !exists {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal("Hash failed", map[string]interface{}{"error": err.Error()})
		}
		id := uuid.New()
		u := &domain.User{
			ID:           id,
			Email:        email,
			Phone:        phone,
			PasswordHash: string(hash),
			FirstName:    first,
			LastName:     last,
			UserType:     userType,
			KYCLevel:     kycLevel,
			KYCStatus:    kyc,
			CountryCode:  country,
			RiskScore:    decimal.Zero,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := repo.Create(ctx, u); err != nil {
			log.Fatal("Create failed", map[string]interface{}{"error": err.Error()})
		}
		log.Info("User created", map[string]interface{}{"email": email})
		return id
	}
	user, err := repo.FindByEmail(ctx, email)
	if err != nil {
		log.Fatal("FindByEmail failed", map[string]interface{}{"error": err.Error()})
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal("Hash failed", map[string]interface{}{"error": err.Error()})
	}
	user.PasswordHash = string(hash)
	user.UpdatedAt = now
	if err := repo.Update(ctx, user); err != nil {
		log.Fatal("Update failed", map[string]interface{}{"error": err.Error()})
	}
	log.Info("User password updated", map[string]interface{}{"email": email})
	return user.ID
}

func ensureWallet(ctx context.Context, repo *postgres.WalletRepository, log logger.Logger, userID uuid.UUID, currency domain.Currency, initialBalance decimal.Decimal) {
	w, err := repo.FindByUserAndCurrency(ctx, userID, currency)
	now := time.Now()
	if err == nil && w != nil {
		w.AvailableBalance = initialBalance
		w.LedgerBalance = initialBalance
		w.UpdatedAt = now
		if err := repo.Update(ctx, w); err != nil {
			log.Fatal("Update wallet failed", map[string]interface{}{"error": err.Error()})
		}
		log.Info("Wallet updated", map[string]interface{}{"user_id": userID, "currency": currency})
		return
	}
	wallet := &domain.Wallet{
		ID:               uuid.New(),
		UserID:           userID,
		Currency:         currency,
		AvailableBalance: initialBalance,
		LedgerBalance:    initialBalance,
		ReservedBalance:  decimal.Zero,
		Status:           domain.WalletStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.Create(ctx, wallet); err != nil {
		log.Fatal("Create wallet failed", map[string]interface{}{"error": err.Error()})
	}
	log.Info("Wallet created", map[string]interface{}{"user_id": userID, "currency": currency})
}
