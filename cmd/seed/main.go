// Simple seeding tool to create/update a default user for auth login
// Usage (env overrides):
//   SEED_EMAIL=john.doe@example.com SEED_PASSWORD=Password123
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

    "kyd/pkg/config"
    "kyd/pkg/logger"
    "kyd/internal/repository/postgres"
    "kyd/pkg/domain"
)

func main() {
    log := logger.New("seed-user")

    cfg := config.Load()
    if err := cfg.ValidateCore(); err != nil {
        log.Fatal("Invalid configuration", map[string]interface{}{"error": err.Error()})
    }

    // Defaults; allow override via env
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

    repo := postgres.NewUserRepository(db)
    ctx := context.Background()

    // Check if user exists
    exists, err := repo.ExistsByEmail(ctx, email)
    if err != nil {
        log.Fatal("ExistsByEmail failed", map[string]interface{}{"error": err.Error()})
    }

    if !exists {
        // Create
        hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
        if err != nil {
            log.Fatal("Hash failed", map[string]interface{}{"error": err.Error()})
        }
        now := time.Now()
        u := &domain.User{
            ID:           uuid.New(),
            Email:        email,
            Phone:        phone,
            PasswordHash: string(hash),
            FirstName:    first,
            LastName:     last,
            UserType:     domain.UserTypeIndividual,
            KYCLevel:     0,
            KYCStatus:    domain.KYCStatusPending,
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
        fmt.Println("OK: user created")
        return
    }

    // Update password for existing user (non-destructive update)
    user, err := repo.FindByEmail(ctx, email)
    if err != nil {
        log.Fatal("FindByEmail failed", map[string]interface{}{"error": err.Error()})
    }
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        log.Fatal("Hash failed", map[string]interface{}{"error": err.Error()})
    }
    user.PasswordHash = string(hash)
    now := time.Now()
    user.UpdatedAt = now
    if err := repo.Update(ctx, user); err != nil {
        log.Fatal("Update failed", map[string]interface{}{"error": err.Error()})
    }
    log.Info("User password updated", map[string]interface{}{"email": email})
    fmt.Println("OK: user password updated")
}

func getenv(key, def string) string {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    return v
}
