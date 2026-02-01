package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"kyd/internal/domain"
	"kyd/internal/repository/postgres"
	"kyd/internal/security"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
)

func loadEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		// Try looking up one directory if we are in cmd/seed
		content, err = os.ReadFile("../../.env")
		if err != nil {
			return // No .env found, rely on process env
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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	loadEnv()

	dbURL := getenv("DATABASE_URL", "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable")
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		fmt.Printf("Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Initialize security service
	cryptoService, err := security.NewCryptoService()
	if err != nil {
		fmt.Printf("Failed to initialize crypto service: %v\n", err)
		os.Exit(1)
	}

	repo := postgres.NewUserRepository(db, cryptoService)
	ledgerRepo := postgres.NewLedgerRepository(db)

	ctx := context.Background()

	// Verify Ledger Chain Integrity
	valid, err := ledgerRepo.VerifyChain(ctx)
	if err != nil {
		fmt.Printf("⚠️  Ledger Chain Verification Error: %v\n", err)
	} else if valid {
		fmt.Println("✅ Ledger Chain Integrity Verified")
	} else {
		fmt.Println("❌ Ledger Chain Broken!")
	}

	// 1. Create Individual User
	email := "john.doe@example.com"
	exists, err := repo.ExistsByEmail(ctx, email)
	if err != nil {
		fmt.Printf("ExistsByEmail failed: %v\n", err)
		os.Exit(1)
	}

	if !exists {
		hash, err := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("Hash failed: %v\n", err)
			os.Exit(1)
		}
		now := time.Now()
		u := &domain.User{
			ID:           uuid.New(),
			Email:        email,
			Phone:        "+1234567890",
			PasswordHash: string(hash),
			FirstName:    "John",
			LastName:     "Doe",
			UserType:     domain.UserTypeIndividual,
			KYCLevel:     1,
			KYCStatus:    domain.KYCStatusVerified,
			CountryCode:  "MW",
			RiskScore:    decimal.Zero,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := repo.Create(ctx, u); err != nil {
			fmt.Printf("Create failed: %v\n", err)
			os.Exit(1)
		}
		// Mark email as verified
		if err := repo.SetEmailVerified(ctx, u.ID); err != nil {
			fmt.Printf("SetEmailVerified failed: %v\n", err)
		}
		fmt.Println("OK: individual user created")
	} else {
		// Update password if exists
		u, err := repo.FindByEmail(ctx, email)
		if err == nil {
			hash, _ := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
			u.PasswordHash = string(hash)
			if err := repo.Update(ctx, u); err != nil {
				fmt.Printf("Update failed: %v\n", err)
			} else {
				fmt.Println("OK: individual user password updated")
			}
			// Ensure verified
			repo.SetEmailVerified(ctx, u.ID)
			// Ensure country code is MW for John
			if u.CountryCode != "MW" {
				if _, err := db.ExecContext(ctx, `UPDATE customer_schema.users SET country_code = 'MW', updated_at = NOW() WHERE id = $1`, u.ID); err != nil {
					fmt.Printf("Failed to update John Doe country_code to MW: %v\n", err)
				} else {
					fmt.Println("OK: Updated John Doe country_code to MW")
				}
			}
		}
	}

	// 2. Create Merchant User
	merchantEmail := "merchant@example.com"
	mExists, err := repo.ExistsByEmail(ctx, merchantEmail)
	if err != nil {
		fmt.Printf("Merchant ExistsByEmail failed: %v\n", err)
		os.Exit(1)
	}

	if !mExists {
		hash, err := bcrypt.GenerateFromPassword([]byte("MerchantPass123!"), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("Merchant Hash failed: %v\n", err)
			os.Exit(1)
		}
		now := time.Now()
		u := &domain.User{
			ID:           uuid.New(),
			Email:        merchantEmail,
			Phone:        "+1987654321",
			PasswordHash: string(hash),
			FirstName:    "Merchant",
			LastName:     "One",
			UserType:     domain.UserTypeMerchant,
			KYCLevel:     2,
			KYCStatus:    domain.KYCStatusVerified,
			CountryCode:  "US",
			RiskScore:    decimal.Zero,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := repo.Create(ctx, u); err != nil {
			fmt.Printf("Merchant Create failed: %v\n", err)
			os.Exit(1)
		}
		if err := repo.SetEmailVerified(ctx, u.ID); err != nil {
			fmt.Printf("Merchant SetEmailVerified failed: %v\n", err)
		}
		fmt.Println("OK: merchant user created")
	}

	// 3. Create Admin User
	adminEmail := getenv("SEED_ADMIN_EMAIL", "admin@example.com")
	adminPassword := getenv("SEED_ADMIN_PASSWORD", "AdminPassword123") // Removed ! to match user expectation

	adminExists, err := repo.ExistsByEmail(ctx, adminEmail)
	if err != nil {
		fmt.Printf("Admin ExistsByEmail failed: %v\n", err)
		os.Exit(1)
	}

	if !adminExists {
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("Admin Hash failed: %v\n", err)
			os.Exit(1)
		}
		now := time.Now()
		u := &domain.User{
			ID:           uuid.New(),
			Email:        adminEmail,
			Phone:        "+265999999999",
			PasswordHash: string(hash),
			FirstName:    "System",
			LastName:     "Admin",
			UserType:     domain.UserTypeAdmin,
			KYCLevel:     3,
			KYCStatus:    domain.KYCStatusVerified,
			CountryCode:  "MW",
			RiskScore:    decimal.Zero,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := repo.Create(ctx, u); err != nil {
			fmt.Printf("Admin Create failed: %v\n", err)
			os.Exit(1)
		}
		if err := repo.SetEmailVerified(ctx, u.ID); err != nil {
			fmt.Printf("Admin SetEmailVerified failed: %v\n", err)
		}
		fmt.Println("OK: admin user created")
	} else {
		// Update password if exists
		u, err := repo.FindByEmail(ctx, adminEmail)
		if err == nil {
			hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
			if err != nil {
				fmt.Printf("Admin Hash failed: %v\n", err)
				os.Exit(1)
			}
			u.PasswordHash = string(hash)
			if err := repo.Update(ctx, u); err != nil {
				fmt.Printf("Admin Update failed: %v\n", err)
			} else {
				fmt.Println("OK: admin user password updated")
			}
			repo.SetEmailVerified(ctx, u.ID)
		}
	}

	// 4. Create Wang (CNY Receiver)
	wangEmail := "wang.wei@example.com"
	wExists, err := repo.ExistsByEmail(ctx, wangEmail)
	if err != nil {
		fmt.Printf("Wang ExistsByEmail failed: %v\n", err)
		os.Exit(1)
	}

	if !wExists {
		hash, err := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("Wang Hash failed: %v\n", err)
			os.Exit(1)
		}
		now := time.Now()
		u := &domain.User{
			ID:           uuid.New(),
			Email:        wangEmail,
			Phone:        "+8613800138000",
			PasswordHash: string(hash),
			FirstName:    "Wei",
			LastName:     "Wang",
			UserType:     domain.UserTypeIndividual,
			KYCLevel:     2,
			KYCStatus:    domain.KYCStatusVerified,
			CountryCode:  "CN",
			RiskScore:    decimal.Zero,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := repo.Create(ctx, u); err != nil {
			fmt.Printf("Wang Create failed: %v\n", err)
			os.Exit(1)
		}
		if err := repo.SetEmailVerified(ctx, u.ID); err != nil {
			fmt.Printf("Wang SetEmailVerified failed: %v\n", err)
		}
		fmt.Println("OK: wang user created")
	} else {
		// Update password if exists
		u, err := repo.FindByEmail(ctx, wangEmail)
		if err == nil {
			hash, _ := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
			u.PasswordHash = string(hash)
			repo.Update(ctx, u)
			repo.SetEmailVerified(ctx, u.ID)
		}
	}

	// 5. Create Luka (MWK Sender)
	lukaEmail := "luka.banda@example.com"
	lExists, err := repo.ExistsByEmail(ctx, lukaEmail)
	if err != nil {
		fmt.Printf("Luka ExistsByEmail failed: %v\n", err)
		os.Exit(1)
	}

	if !lExists {
		hash, err := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("Luka Hash failed: %v\n", err)
			os.Exit(1)
		}
		now := time.Now()
		u := &domain.User{
			ID:           uuid.New(),
			Email:        lukaEmail,
			Phone:        "+265991234567",
			PasswordHash: string(hash),
			FirstName:    "Luka",
			LastName:     "Banda",
			UserType:     domain.UserTypeIndividual,
			KYCLevel:     2,
			KYCStatus:    domain.KYCStatusVerified,
			CountryCode:  "MW",
			RiskScore:    decimal.Zero,
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := repo.Create(ctx, u); err != nil {
			fmt.Printf("Luka Create failed: %v\n", err)
			os.Exit(1)
		}
		if err := repo.SetEmailVerified(ctx, u.ID); err != nil {
			fmt.Printf("Luka SetEmailVerified failed: %v\n", err)
		}
		fmt.Println("OK: luka user created")
	} else {
		// Update password if exists
		u, err := repo.FindByEmail(ctx, lukaEmail)
		if err == nil {
			hash, _ := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
			u.PasswordHash = string(hash)
			repo.Update(ctx, u)
			repo.SetEmailVerified(ctx, u.ID)
		}
	}

	// 6. Create Wallets
	walletRepo := postgres.NewWalletRepository(db)

	createWallet := func(userEmail string, currency domain.Currency, balance decimal.Decimal) {
		u, err := repo.FindByEmail(ctx, userEmail)
		if err != nil {
			fmt.Printf("Failed to find user %s: %v\n", userEmail, err)
			return
		}

		wallets, err := walletRepo.FindByUserID(ctx, u.ID)
		if err != nil {
			fmt.Printf("Failed to find wallets for %s: %v\n", userEmail, err)
			return
		}

		// Check if wallet for currency exists
		var exists bool
		for _, w := range wallets {
			if w.Currency == currency {
				exists = true
				break
			}
		}

		if !exists {
			// Generate 16-digit wallet number
			n, err := rand.Int(rand.Reader, big.NewInt(10000000000000000))
			if err != nil {
				fmt.Printf("Failed to generate wallet number: %v\n", err)
				return
			}
			walletNum := fmt.Sprintf("%016d", n)

			now := time.Now()
			w := &domain.Wallet{
				ID:               uuid.New(),
				UserID:           u.ID,
				WalletAddress:    &walletNum,
				Currency:         currency,
				AvailableBalance: balance,
				LedgerBalance:    balance,
				ReservedBalance:  decimal.Zero,
				Status:           domain.WalletStatusActive,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := walletRepo.Create(ctx, w); err != nil {
				fmt.Printf("Failed to create %s wallet for %s: %v\n", currency, userEmail, err)
			} else {
				fmt.Printf("OK: Created %s wallet for %s with balance %s\n", currency, userEmail, balance)
			}
		} else {
			fmt.Printf("Info: %s wallet for %s already exists\n", currency, userEmail)
		}
	}

	// Fix for John Doe: Convert USD wallet to MWK if it exists
	jd, err := repo.FindByEmail(ctx, "john.doe@example.com")
	if err == nil {
		// Check if MWK wallet already exists
		_, errMWK := walletRepo.FindByUserAndCurrency(ctx, jd.ID, domain.MWK)
		if errMWK != nil { // MWK not found
			// Check for USD wallet
			usdWallet, errUSD := walletRepo.FindByUserAndCurrency(ctx, jd.ID, domain.USD)
			if errUSD == nil {
				fmt.Println("Converting USD wallet to MWK for John Doe...")
				// Manually update currency since Repository doesn't allow it
				_, err := db.ExecContext(ctx, `
					UPDATE customer_schema.wallets 
					SET currency = 'MWK', 
						available_balance = 500000.00, 
						ledger_balance = 500000.00 
					WHERE id = $1`, usdWallet.ID)

				if err != nil {
					fmt.Printf("Error converting wallet: %v\n", err)
				} else {
					fmt.Println("OK: Wallet converted to MWK")
				}
			}
		}
		// Print wallet currencies after conversion
		ws, err := walletRepo.FindByUserID(ctx, jd.ID)
		if err == nil {
			for _, w := range ws {
				fmt.Printf("John Doe wallet currency: %s\n", w.Currency)
			}
		}
	}

	// createWallet("john.doe@example.com", domain.USD, decimal.NewFromFloat(40500.80))
	createWallet("john.doe@example.com", domain.MWK, decimal.NewFromFloat(500000.00)) // Changed to MWK as per user request
	createWallet("wang.wei@example.com", domain.CNY, decimal.NewFromFloat(100000.00))
	createWallet("luka.banda@example.com", domain.MWK, decimal.NewFromFloat(1500000.00))
}
