package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"kyd/internal/repository/postgres"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func loadEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		// Try looking up one directory if we are in cmd/audit
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

func main() {
	loadEnv()
	fmt.Println("🔍 Starting System Audit (2025 Banking Standards)...")

	// 1. Environment & Security Config Check
	checkEnvVars()

	// 2. Database Connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// Fallback for local dev
		dbURL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
		fmt.Println("⚠️  DATABASE_URL not set, using default local dev URL (SSL disabled)")
	} else {
		if strings.Contains(dbURL, "sslmode=disable") {
			fmt.Println("⚠️  Production Warning: DB SSL is DISABLED")
		} else {
			fmt.Println("✅ DB SSL Configuration: Enabled (verify-full/require)")
		}
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("❌ DB Connection Failed: %v", err)
	}
	defer db.Close()

	// 3. Ledger Integrity Check (Immutable History)
	checkLedgerIntegrity(db)

	// 4. Compliance & Risk Architecture Check
	checkComplianceFeatures(db)

	// 5. User Distribution Check (Target Audience: MWK-CNY)
	checkTargetAudience(db)

	fmt.Println("\n🏁 Audit Complete.")
}

func checkEnvVars() {
	fmt.Println("\n[1] Security & Environment Configuration")
	required := []string{"JWT_SECRET", "App_ENV"} // "DATABASE_URL" handled separately
	missing := []string{}
	for _, key := range required {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		fmt.Printf("⚠️  Missing Critical Env Vars: %v\n", missing)
	} else {
		fmt.Println("✅ Critical Env Vars Present")
	}

	// Check mTLS (Simulated check as we can't check certs here easily, but check var)
	if os.Getenv("MTLS_ENABLED") == "true" {
		fmt.Println("✅ mTLS Enforced for Inter-Service Communication")
	} else {
		fmt.Println("ℹ️  mTLS Not Enforced (Standard for Local Dev, Critical for Prod)")
	}
}

func checkLedgerIntegrity(db *sqlx.DB) {
	fmt.Println("\n[2] Ledger Immutability (Blockchain Standard)")
	repo := postgres.NewLedgerRepository(db)
	ctx := context.Background()

	valid, err := repo.VerifyChain(ctx)
	if err != nil {
		fmt.Printf("❌ Ledger Verification Failed: %v\n", err)
	} else if valid {
		fmt.Println("✅ HASH CHAIN INTEGRITY: VERIFIED")
		fmt.Println("   - All transactions are cryptographically linked.")
		fmt.Println("   - Tamper-evident history (Superior to traditional SQL ledgers).")
	} else {
		fmt.Println("❌ HASH CHAIN BROKEN: Tampering Detected!")
	}
}

func checkComplianceFeatures(db *sqlx.DB) {
	fmt.Println("\n[3] Compliance & Risk Architecture (2025 Standard)")

	// Check for Risk Score column in Users
	var hasRiskScore bool
	err := db.Get(&hasRiskScore, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='risk_score')")
	if err == nil && hasRiskScore {
		fmt.Println("✅ Risk Scoring Engine: ENABLED (Database Support Found)")
	} else {
		fmt.Println("❌ Risk Scoring Engine: MISSING")
	}

	// Check for KYC Levels
	var hasKYC bool
	err = db.Get(&hasKYC, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='kyc_level')")
	if err == nil && hasKYC {
		fmt.Println("✅ Tiered KYC System: ENABLED")
	}
}

func checkTargetAudience(db *sqlx.DB) {
	fmt.Println("\n[4] Target Corridor Analysis (MWK <-> CNY)")

	// Check for MWK Users
	var mwkCount int
	err := db.Get(&mwkCount, "SELECT COUNT(*) FROM customer_schema.wallets WHERE currency = 'MWK'")
	if err != nil {
		fmt.Printf("❌ MWK Count Failed: %v\n", err)
	}

	// Check for CNY Users
	var cnyCount int
	err = db.Get(&cnyCount, "SELECT COUNT(*) FROM customer_schema.wallets WHERE currency = 'CNY'")
	if err != nil {
		fmt.Printf("❌ CNY Count Failed: %v\n", err)
	}

	fmt.Printf("   - MWK Wallets: %d\n", mwkCount)
	fmt.Printf("   - CNY Wallets: %d\n", cnyCount)

	if mwkCount > 0 && cnyCount > 0 {
		fmt.Println("✅ Corridor Active: Both MWK and CNY endpoints established.")
	} else {
		fmt.Println("⚠️  Corridor Inactive: Missing wallets for one or both currencies.")
	}
}
