// ==============================================================================
// DATABASE SEED - cmd/seed/main.go
// ==============================================================================
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"

	"kyd/internal/domain"
	"kyd/internal/repository/postgres"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := sqlx.Connect("postgres", databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	log.Println("🌱 Seeding database...")

	// Seed users
	userRepo := postgres.NewUserRepository(db)
	walletRepo := postgres.NewWalletRepository(db)
	forexRepo := postgres.NewForexRepository(db)

	// Create test users
	users := []*domain.User{
		createUser("john.doe@example.com", "John", "Doe", "MW", domain.UserTypeIndividual),
		createUser("jane.smith@example.com", "Jane", "Smith", "MW", domain.UserTypeMerchant),
		createUser("wang.wei@example.com", "Wang", "Wei", "CN", domain.UserTypeMerchant),
	}

	for _, user := range users {
		if err := userRepo.Create(ctx, user); err != nil {
			log.Printf("Failed to create user %s: %v", user.Email, err)
			continue
		}
		log.Printf("✅ Created user: %s", user.Email)

		// Create wallets for each user
		currencies := []domain.Currency{domain.MWK, domain.CNY, domain.USD}
		for _, currency := range currencies {
			wallet := &domain.Wallet{
				ID:               uuid.New(),
				UserID:           user.ID,
				Currency:         currency,
				AvailableBalance: decimal.NewFromInt(10000),
				LedgerBalance:    decimal.NewFromInt(10000),
				ReservedBalance:  decimal.Zero,
				Status:           domain.WalletStatusActive,
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			}

			if err := walletRepo.Create(ctx, wallet); err != nil {
				log.Printf("Failed to create wallet for %s: %v", user.Email, err)
				continue
			}
			log.Printf("✅ Created %s wallet for %s", currency, user.Email)
		}
	}

	// Seed exchange rates
	rates := []*domain.ExchangeRate{
		{
			ID:             uuid.New(),
			BaseCurrency:   domain.MWK,
			TargetCurrency: domain.CNY,
			Rate:           decimal.NewFromFloat(0.0085),
			BuyRate:        decimal.NewFromFloat(0.008628),
			SellRate:       decimal.NewFromFloat(0.008373),
			Source:         "Market",
			Spread:         decimal.NewFromFloat(0.015),
			ValidFrom:      time.Now(),
			CreatedAt:      time.Now(),
		},
		{
			ID:             uuid.New(),
			BaseCurrency:   domain.CNY,
			TargetCurrency: domain.MWK,
			Rate:           decimal.NewFromFloat(117.65),
			BuyRate:        decimal.NewFromFloat(119.41),
			SellRate:       decimal.NewFromFloat(115.89),
			Source:         "Market",
			Spread:         decimal.NewFromFloat(0.015),
			ValidFrom:      time.Now(),
			CreatedAt:      time.Now(),
		},
	}

	for _, rate := range rates {
		if err := forexRepo.CreateRate(ctx, rate); err != nil {
			log.Printf("Failed to create rate: %v", err)
			continue
		}
		log.Printf("✅ Created exchange rate: %s -> %s", rate.BaseCurrency, rate.TargetCurrency)
	}

	log.Println("✅ Database seeding completed!")
}

func createUser(email, firstName, lastName, countryCode string, userType domain.UserType) *domain.User {
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("Password123"), bcrypt.DefaultCost)

	return &domain.User{
		ID:           uuid.New(),
		Email:        email,
		Phone:        "+265991234567",
		PasswordHash: string(passwordHash),
		FirstName:    firstName,
		LastName:     lastName,
		UserType:     userType,
		KYCLevel:     1,
		KYCStatus:    domain.KYCStatusVerified,
		CountryCode:  countryCode,
		RiskScore:    decimal.Zero,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}
