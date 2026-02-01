package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"kyd/internal/repository/postgres"
	"kyd/pkg/config"
	"kyd/pkg/logger"
)

func main() {
	cfg := config.Load()
	log := logger.New("fix-wallets")

	log.Info("Starting Wallet Address Fix Tool", nil)

	// Database connection
	db, err := sqlx.Connect("postgres", cfg.Database.URL)
	if err != nil {
		log.Fatal("Failed to connect to database", map[string]interface{}{
			"error": err.Error(),
		})
	}
	defer db.Close()

	walletRepo := postgres.NewWalletRepository(db)
	ctx := context.Background()

	// Fetch all wallets
	// We'll paginate to be safe
	limit := 100
	offset := 0
	totalUpdated := 0

	for {
		wallets, err := walletRepo.FindAll(ctx, limit, offset)
		if err != nil {
			log.Fatal("Failed to fetch wallets", map[string]interface{}{"error": err.Error()})
		}

		if len(wallets) == 0 {
			break
		}

		for _, w := range wallets {
			addr := ""
			if w.WalletAddress != nil {
				addr = *w.WalletAddress
			}

			isValid := false
			if len(addr) == 16 {
				_, err := strconv.ParseInt(addr, 10, 64)
				if err == nil {
					isValid = true
				}
			}

			if !isValid {
				// Generate new number
				n, err := rand.Int(rand.Reader, big.NewInt(10000000000000000))
				if err != nil {
					log.Error("Failed to generate random number", map[string]interface{}{"error": err.Error()})
					continue
				}
				newAddr := fmt.Sprintf("%016d", n)

				if err := walletRepo.UpdateWalletAddress(ctx, w.ID, newAddr); err != nil {
					log.Error("Failed to update wallet", map[string]interface{}{
						"id":    w.ID,
						"error": err.Error(),
					})
				} else {
					log.Info("Updated wallet address", map[string]interface{}{
						"id":       w.ID,
						"old_addr": addr,
						"new_addr": newAddr,
					})
					totalUpdated++
				}
			}
		}

		offset += limit
	}

	log.Info("Completed wallet address fix", map[string]interface{}{
		"total_updated": totalUpdated,
	})
}
