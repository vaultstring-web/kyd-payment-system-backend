// ==============================================================================
// SETTLEMENT SERVICE MAIN - cmd/settlement/main.go
// ==============================================================================
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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

	"kyd/internal/blockchain/ripple"
	"kyd/internal/blockchain/stellar"
	"kyd/internal/middleware"
	"kyd/internal/repository/postgres"
	"kyd/internal/settlement"
	"kyd/pkg/config"
	"kyd/pkg/logger"
)

func main() {
	cfg := config.Load()
	log := logger.New("settlement-service")

	log.Info("Starting Settlement Service", map[string]interface{}{
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

	// Initialize blockchain connectors
	stellarConnector, err := stellar.NewConnector(
		cfg.Stellar.NetworkURL,
		cfg.Stellar.SecretKey,
		true, // testnet
	)
	if err != nil {
		log.Fatal("Failed to initialize Stellar connector", map[string]interface{}{
			"error": err.Error(),
		})
	}

	rippleConnector, err := ripple.NewConnector(
		cfg.Ripple.ServerURL,
		cfg.Ripple.SecretKey,
	)
	if err != nil {
		log.Fatal("Failed to initialize Ripple connector", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("Blockchain connectors initialized", nil)

	// Initialize repositories
	settlementRepo := postgres.NewSettlementRepository(db)
	txRepo := postgres.NewTransactionRepository(db)

	// Initialize settlement service
	settlementService := settlement.NewService(
		settlementRepo,
		txRepo,
		stellarConnector,
		rippleConnector,
		log,
	)

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.BodyLimit(1 << 20))
	r.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).WithAdaptive(5, 15*time.Minute).Limit)

	// Auth Middleware
	blacklist := middleware.NewRedisTokenBlacklist(redisClient)
	authMW := middleware.NewAuthMiddleware(cfg.JWT.Secret, blacklist)

	// Routes
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/ready", readyCheck(db)).Methods("GET")

	// Admin routes for manual settlement triggers
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(authMW.Authenticate)
	api.HandleFunc("/settlements/process", func(w http.ResponseWriter, r *http.Request) {
		// Check for admin role
		userType, ok := middleware.UserTypeFromContext(r.Context())
		if !ok || userType != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if err := settlementService.ProcessPendingSettlements(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"processing"}`))
	}).Methods("POST")

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Configure mTLS if enabled
	if cfg.Server.UseTLS {
		// Load CA cert to verify clients (Gateway)
		caCert, err := os.ReadFile(cfg.Server.CAFile)
		if err != nil {
			log.Fatal("Failed to read CA file", map[string]interface{}{"error": err.Error()})
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		srv.TLSConfig = &tls.Config{
			ClientCAs:  caCertPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
			MinVersion: tls.VersionTLS12,
		}
	}

	go func() {
		log.Info("Settlement service started", map[string]interface{}{
			"address": srv.Addr,
			"tls":     cfg.Server.UseTLS,
		})

		var err error
		if cfg.Server.UseTLS {
			err = srv.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile)
		} else {
			err = srv.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed to start", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down settlement service...", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Settlement service forced to shutdown", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("Settlement service stopped gracefully", nil)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","service":"settlement"}`))
}

func readyCheck(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not ready","reason":"database unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready","service":"settlement"}`))
	}
}
