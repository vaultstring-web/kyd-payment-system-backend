// ==============================================================================
// WALLET SERVICE MAIN - cmd/wallet/main.go
// ==============================================================================
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"kyd/internal/handler"
	"kyd/internal/middleware"
	"kyd/internal/repository/postgres"
	"kyd/internal/wallet"
	"kyd/pkg/config"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

func main() {
	cfg := config.Load()
	log := logger.New("wallet-service")

	if err := cfg.ValidateCore(); err != nil {
		log.Fatal("Invalid configuration", map[string]interface{}{"error": err.Error()})
	}

	log.Info("Starting Wallet Service", map[string]interface{}{
		"port": cfg.Server.Port,
	})

	// Database connection
	db, err := sqlx.Connect("postgres", cfg.Database.URL)
	if err != nil {
		log.Fatal("Failed to connect to database", map[string]interface{}{
			"error": err.Error(),
		})
	}
	defer db.Close()

	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	log.Info("Database connected", nil)

	// Redis connection
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.URL,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("Redis connected", nil)

	// Initialize repositories
	walletRepo := postgres.NewWalletRepository(db)
	userRepo := postgres.NewUserRepository(db)

	// Initialize services
	walletService := wallet.NewService(walletRepo, userRepo, log)

	// Initialize handlers
	val := validator.New()
	walletHandler := handler.NewWalletHandler(walletService, val, log)

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.BodyLimit(1 << 20)) // 1MB global cap
	r.Use(middleware.NewRateLimiter(redisClient, 120, time.Minute).Limit)

	authMW := middleware.NewAuthMiddleware(cfg.JWT.Secret)

	// Routes
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/ready", readyCheck(db)).Methods("GET")

	// Protected routes
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(authMW.Authenticate)
	api.Use(middleware.NewRateLimiter(redisClient, 80, time.Minute).Limit)

	api.HandleFunc("/wallets", walletHandler.CreateWallet).Methods("POST")
	api.HandleFunc("/wallets", walletHandler.GetUserWallets).Methods("GET")
	api.HandleFunc("/wallets/{id}", walletHandler.GetWallet).Methods("GET")
	api.HandleFunc("/wallets/{id}/balance", walletHandler.GetBalance).Methods("GET")
	api.HandleFunc("/wallets/{id}/history", walletHandler.GetTransactionHistory).Methods("GET")

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	go func() {
		log.Info("Wallet service started", map[string]interface{}{
			"address": srv.Addr,
		})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed to start", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down wallet service...", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Wallet service forced to shutdown", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("Wallet service stopped gracefully", nil)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","service":"wallet"}`))
}

func readyCheck(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r
		if err := db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not ready","reason":"database unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready","service":"wallet"}`))
	}
}
