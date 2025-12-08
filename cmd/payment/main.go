// ==============================================================================
// COMPLETE PAYMENT SERVICE MAIN - cmd/payment/main.go
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

	"kyd/internal/forex"
	"kyd/internal/handler"
	"kyd/internal/ledger"
	"kyd/internal/middleware"
	"kyd/internal/payment"
	"kyd/internal/repository/postgres"
	"kyd/pkg/config"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

func main() {
	cfg := config.Load()
	log := logger.New("payment-service")

	if err := cfg.ValidateCore(); err != nil {
		log.Fatal("Invalid configuration", map[string]interface{}{"error": err.Error()})
	}

	log.Info("Starting Payment Service", map[string]interface{}{
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

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatal("Database ping failed", map[string]interface{}{
			"error": err.Error(),
		})
	}

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
	txRepo := postgres.NewTransactionRepository(db)
	walletRepo := postgres.NewWalletRepository(db)
	forexRepo := postgres.NewForexRepository(db)

	// Initialize services
	ledgerService := ledger.NewService(db.DB)

	// Initialize forex providers
	forexProviders := []forex.RateProvider{
		forex.NewMockRateProvider(), // Add real providers here
	}

	// Wrap redis client with RateCache adapter
	rateCache := forex.NewRedisRateCache(redisClient)
	forexService := forex.NewService(forexRepo, rateCache, forexProviders, log)

	paymentService := payment.NewService(txRepo, walletRepo, forexService, ledgerService, log)

	// Initialize handlers
	val := validator.New()
	paymentHandler := handler.NewPaymentHandler(paymentService, val, log)

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.NewRateLimiter(redisClient, 150, time.Minute).Limit)

	authMW := middleware.NewAuthMiddleware(cfg.JWT.Secret)
	idemMW := middleware.NewIdempotencyMiddleware(redisClient, 24*time.Hour)

	// Health check routes (no auth)
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/ready", readyCheck(db)).Methods("GET")

	// Protected routes
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(authMW.Authenticate)
	api.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).Limit)

	payments := api.PathPrefix("/payments").Subrouter()
	payments.Use(idemMW.Require)
	payments.HandleFunc("/initiate", paymentHandler.InitiatePayment).Methods("POST")
	payments.HandleFunc("/{id}", paymentHandler.GetTransaction).Methods("GET")
	payments.HandleFunc("", paymentHandler.GetUserTransactions).Methods("GET")
	payments.HandleFunc("/{id}/cancel", paymentHandler.CancelPayment).Methods("POST")
	payments.HandleFunc("/bulk", paymentHandler.BulkPayment).Methods("POST")

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
		log.Info("Payment service started", map[string]interface{}{
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

	log.Info("Shutting down payment service...", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Payment service forced to shutdown", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("Payment service stopped gracefully", nil)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","service":"payment","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
}

func readyCheck(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r
		if err := db.Ping(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not ready","reason":"database unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready","service":"payment"}`))
	}
}
