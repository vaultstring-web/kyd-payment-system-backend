// Package main is the entry point for the payment service.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"kyd/internal/analytics"
	"kyd/internal/auth"
	"kyd/internal/blockchain"
	"kyd/internal/blockchain/ripple"
	"kyd/internal/blockchain/stellar"
	"kyd/internal/casework"
	"kyd/internal/compliance"
	"kyd/internal/domain"
	"kyd/internal/forex"
	"kyd/internal/handler"
	"kyd/internal/ledger"
	"kyd/internal/middleware"
	"kyd/internal/notification"
	"kyd/internal/payment"
	"kyd/internal/repository/postgres"
	"kyd/internal/security"
	"kyd/internal/settlement"
	"kyd/internal/wallet"
	"kyd/pkg/config"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

func loadEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		// Try looking up one directory if we are in cmd/payment
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

type userStatusChecker struct {
	repo *postgres.UserRepository
	log  logger.Logger
}

func (c *userStatusChecker) IsUserActive(ctx context.Context, id uuid.UUID) (bool, error) {
	u, err := c.repo.FindByID(ctx, id)
	if err != nil {
		if c.log != nil {
			c.log.Error("IsUserActive: FindByID failed", map[string]interface{}{
				"user_id": id.String(),
				"error":   err.Error(),
			})
		}
		return false, err
	}
	if u.UserType == domain.UserTypeAdmin {
		return true, nil
	}
	return u.IsActive, nil
}

func main() {
	loadEnv()
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

	// Initialize security service
	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatal("Failed to initialize crypto service", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Initialize repositories
	txRepo := postgres.NewTransactionRepository(db)
	walletRepo := postgres.NewWalletRepository(db)
	forexRepo := postgres.NewForexRepository(db)
	userRepo := postgres.NewUserRepository(db, cryptoService)
	settlementRepo := postgres.NewSettlementRepository(db)
	auditRepo := postgres.NewAuditRepository(db, cryptoService)
	ledgerRepo := postgres.NewLedgerRepository(db)
	securityRepo := postgres.NewSecurityRepository(db)
	blockchainRepo := postgres.NewBlockchainNetworkRepository(db)
	kycRepo := postgres.NewKYCRepository(db)
	apiKeyRepo := postgres.NewAPIKeyRepository(db)

	// Initialize services
	ledgerService := ledger.NewService(db, ledgerRepo)
	securityService := security.NewService(securityRepo)
	blockchainService := blockchain.NewService(blockchainRepo)
	complianceService := compliance.NewService(kycRepo, userRepo, auditRepo)
	apiKeyService := auth.NewAPIKeyService(apiKeyRepo)

	blacklist := middleware.NewRedisTokenBlacklist(redisClient)
	authService := auth.NewService(userRepo, blacklist, cfg.JWT.Secret, 24*time.Hour)

	// Initialize blockchain connectors
	stellarConnector, err := stellar.NewConnector(
		"", // force local-only connector (no external network)
		cfg.Stellar.SecretKey,
		true, // always simulate locally
	)
	if err != nil {
		log.Fatal("Failed to initialize Stellar connector", map[string]interface{}{"error": err.Error()})
	}

	rippleConnector, err := ripple.NewConnector(
		"", // force local-only connector (no external network)
		cfg.Ripple.SecretKey,
	)
	if err != nil {
		log.Fatal("Failed to initialize Ripple connector", map[string]interface{}{"error": err.Error()})
	}

	// Initialize Settlement Service (Background Worker)
	settlementService := settlement.NewService(
		settlementRepo,
		txRepo,
		stellarConnector,
		rippleConnector,
		log,
	)

	// Initialize forex providers
	forexProviders := []forex.RateProvider{
		forex.NewGoogleFinanceProvider(), // Try Google Finance first
		forex.NewMockRateProvider(),
		forex.NewExchangeRateAPIProvider(),
	}

	// Initialize Notification Service (persisted notifications + audit trail)
	notificationRepo := postgres.NewNotificationRepository(db)
	notificationService := notification.NewService(log, auditRepo, notificationRepo)

	// Case management (admin operations)
	caseRepo := postgres.NewCaseRepository(db)
	caseService := casework.NewService(caseRepo)

	// Wrap redis client with RateCache adapter
	rateCache := forex.NewRedisRateCache(redisClient)
	forexService := forex.NewService(forexRepo, rateCache, forexProviders, log)

	paymentService := payment.NewService(txRepo, walletRepo, forexService, ledgerService, userRepo, notificationService, auditRepo, securityRepo, log, cfg)
	walletService := wallet.NewService(walletRepo, txRepo, userRepo, log)

	// Initialize handlers
	val := validator.New()
	paymentHandler := handler.NewPaymentHandler(paymentService, val, log)
	walletHandler := handler.NewWalletHandler(walletService, val, log)
	securityHandler := handler.NewSecurityHandler(securityService, val)
	settlementHandler := handler.NewSettlementHandler(settlementService, log)
	forexHandler := handler.NewForexHandler(forexService, val, log)
	blockchainHandler := handler.NewBlockchainHandler(blockchainService, ledgerService)
	complianceHandler := handler.NewComplianceHandler(complianceService, log)
	apiKeyHandler := handler.NewAPIKeyHandler(apiKeyService, log)
	notificationHandler := handler.NewNotificationHandler(notificationService, notificationRepo, log)
	systemHandler := handler.NewSystemHandler(db, redisClient, auditRepo, notificationRepo, log)
	usersHandler := handler.NewUsersHandler(authService, val, log, auditRepo, walletService, paymentService, securityService)
	casesHandler := handler.NewCasesHandler(caseService)

	// Initialize analytics
	analyticsEngine := analytics.NewAnalyticsEngine()
	analyticsHandler := handler.NewAnalyticsHandler(analyticsEngine, log)

	// Setup router
	r := mux.NewRouter()

	// Background: System Health Collector
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			// DB ping latency
			start := time.Now()
			_ = db.Ping()
			dbLatency := time.Since(start).Seconds()
			_ = securityService.RecordHealthSnapshot(context.Background(), &domain.SystemHealthMetric{
				MetricName: "db_ping_latency_seconds",
				Value:      fmt.Sprintf("%.3f", dbLatency),
				Status:     "healthy",
				RecordedAt: time.Now(),
			})

			// Redis ping latency
			start = time.Now()
			_ = redisClient.Ping(context.Background()).Err()
			redisLatency := time.Since(start).Seconds()
			_ = securityService.RecordHealthSnapshot(context.Background(), &domain.SystemHealthMetric{
				MetricName: "redis_ping_latency_seconds",
				Value:      fmt.Sprintf("%.3f", redisLatency),
				Status:     "healthy",
				RecordedAt: time.Now(),
			})
		}
	}()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.BodyLimit(1 << 20)) // 1MB global cap
	r.Use(middleware.NewRateLimiter(redisClient, 150, time.Minute).WithAdaptive(10, 30*time.Minute).Limit)

	authMW := middleware.NewAuthMiddlewareWithUserStatus(cfg.JWT.Secret, blacklist, &userStatusChecker{repo: userRepo, log: log})
	idemMW := middleware.NewIdempotencyMiddleware(redisClient, 24*time.Hour)
	auditMW := middleware.NewAuditMiddleware(auditRepo, log)

	// Health check routes (no auth)
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/ready", readyCheck(db)).Methods("GET")

	// Protected routes
	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/auth/health", healthCheck).Methods("GET")
	api.HandleFunc("/payments/health", healthCheck).Methods("GET")
	api.HandleFunc("/wallets/health", healthCheck).Methods("GET")
	api.HandleFunc("/forex/health", healthCheck).Methods("GET")
	api.HandleFunc("/settlements/health", healthCheck).Methods("GET")

	api.Use(auditMW.Audit) // Audit logs for all API requests
	api.Use(authMW.Authenticate)
	api.Use(idemMW.Require) // Enforce Idempotency-Key
	api.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).WithAdaptive(5, 15*time.Minute).Limit)

	api.HandleFunc("/wallets", walletHandler.GetUserWallets).Methods("GET")
	api.HandleFunc("/wallets", walletHandler.CreateWallet).Methods("POST")
	api.HandleFunc("/wallets/lookup", walletHandler.LookupWallet).Methods("GET")
	api.HandleFunc("/wallets/search", walletHandler.SearchWallets).Methods("GET")
	api.HandleFunc("/wallets/{id}/deposit", walletHandler.Deposit).Methods("POST")
	api.HandleFunc("/wallets/{id}/transactions", walletHandler.GetTransactionHistory).Methods("GET")
	api.HandleFunc("/payments", paymentHandler.InitiatePayment).Methods("POST")
	api.HandleFunc("/payments/initiate", paymentHandler.InitiatePayment).Methods("POST") // Add explicit route
	api.HandleFunc("/payments", paymentHandler.GetTransactions).Methods("GET")
	api.HandleFunc("/transactions/{id}/receipt", paymentHandler.GetReceipt).Methods("GET")
	api.HandleFunc("/disputes", paymentHandler.InitiateDispute).Methods("POST")

	// Compliance
	api.HandleFunc("/compliance/kyc/submit", complianceHandler.SubmitKYC).Methods("POST")
	api.HandleFunc("/compliance/kyc/status", complianceHandler.GetKYCStatus).Methods("GET")

	// Notifications
	api.HandleFunc("/notifications", notificationHandler.List).Methods("GET")
	api.HandleFunc("/notifications/{id}/read", notificationHandler.MarkRead).Methods("POST")
	api.HandleFunc("/notifications/{id}", notificationHandler.Archive).Methods("DELETE")

	// Static files for KYC Documents (Served via API to ensure Auth)
	// Maps /api/v1/uploads/kyc/... to ./uploads/kyc/...
	// Note: We use a raw handler here, wrapping it manually if needed,
	// but since it's on 'api' subrouter, it inherits authMW.
	api.PathPrefix("/uploads/kyc/").Handler(
		http.StripPrefix("/api/v1/uploads/kyc/", http.FileServer(http.Dir("./uploads/kyc"))),
	).Methods("GET")

	// Forex routes
	api.HandleFunc("/forex/rates", forexHandler.GetAllRates).Methods("GET")
	api.HandleFunc("/forex/history", forexHandler.GetHistory).Methods("GET")
	api.HandleFunc("/forex/calculate", forexHandler.Calculate).Methods("POST")

	// Admin routes
	admin := api.PathPrefix("/admin").Subrouter()
	admin.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).WithAdaptive(5, 15*time.Minute).Limit)

	// Admin: User Management
	admin.HandleFunc("/users", usersHandler.List).Methods("GET")
	admin.HandleFunc("/users/{id}", usersHandler.Get).Methods("GET")
	admin.HandleFunc("/users/{id}", usersHandler.Update).Methods("PATCH")
	admin.HandleFunc("/users/{id}", usersHandler.DeleteUser).Methods("DELETE")
	admin.HandleFunc("/users/{id}/block", usersHandler.BlockUser).Methods("POST")
	admin.HandleFunc("/users/{id}/unblock", usersHandler.UnblockUser).Methods("POST")
	admin.HandleFunc("/users/{id}/activity", usersHandler.GetActivity).Methods("GET")
	admin.HandleFunc("/users/{id}/overview", usersHandler.GetOverview).Methods("GET")

	// Admin: Analytics
	admin.HandleFunc("/analytics/metrics", paymentHandler.GetSystemStats).Methods("GET")
	admin.HandleFunc("/analytics/earnings", analyticsHandler.GetEarningsReport).Methods("GET")
	admin.HandleFunc("/analytics/volume", paymentHandler.GetTransactionVolume).Methods("GET")

	// Admin: API Keys
	admin.HandleFunc("/api-keys", apiKeyHandler.ListAPIKeys).Methods("GET")
	admin.HandleFunc("/api-keys", apiKeyHandler.CreateAPIKey).Methods("POST")
	admin.HandleFunc("/api-keys/{id}", apiKeyHandler.RevokeAPIKey).Methods("DELETE")

	// Admin: Compliance
	admin.HandleFunc("/compliance/applications", complianceHandler.ListApplications).Methods("GET")
	admin.HandleFunc("/compliance/applications/{id}/review", complianceHandler.ReviewApplication).Methods("POST")
	admin.HandleFunc("/compliance/kyc", complianceHandler.ListApplications).Methods("GET")
	admin.HandleFunc("/compliance/kyc/{id}", complianceHandler.ReviewApplication).Methods("PATCH")
	admin.HandleFunc("/compliance/reports", complianceHandler.GetComplianceReports).Methods("GET")

	// Admin: Transaction Management
	admin.HandleFunc("/transactions", paymentHandler.GetAllTransactions).Methods("GET")
	admin.HandleFunc("/transactions/pending", paymentHandler.GetPendingTransactions).Methods("GET")
	admin.HandleFunc("/transactions/{id}", paymentHandler.GetTransaction).Methods("GET")
	admin.HandleFunc("/transactions/{id}/review", paymentHandler.ReviewTransaction).Methods("POST")
	admin.HandleFunc("/transactions/{id}/flag", paymentHandler.FlagTransaction).Methods("POST")
	admin.HandleFunc("/transactions/{id}/reverse", paymentHandler.ReverseTransaction).Methods("POST")

	// Admin: Risk & Disputes
	admin.HandleFunc("/risk/alerts", paymentHandler.GetRiskAlerts).Methods("GET")
	admin.HandleFunc("/risk/metrics", paymentHandler.GetRiskUsageMetrics).Methods("GET")
	admin.HandleFunc("/disputes", paymentHandler.GetDisputes).Methods("GET")
	admin.HandleFunc("/disputes/resolve", paymentHandler.ResolveDispute).Methods("POST")

	// Admin: System & Security
	admin.HandleFunc("/system/status", systemHandler.GetSystemStatus).Methods("GET")
	admin.HandleFunc("/audit-logs", systemHandler.GetAuditLogs).Methods("GET")
	admin.HandleFunc("/audit/logs", paymentHandler.GetAuditLogs).Methods("GET")
	admin.HandleFunc("/security/events", securityHandler.GetSecurityEvents).Methods("GET")
	admin.HandleFunc("/security/events/{id}", securityHandler.UpdateSecurityEvent).Methods("PATCH")
	admin.HandleFunc("/cases", casesHandler.List).Methods("GET")
	admin.HandleFunc("/cases", casesHandler.Create).Methods("POST")
	admin.HandleFunc("/cases/{id}", casesHandler.Get).Methods("GET")
	admin.HandleFunc("/cases/{id}", casesHandler.Update).Methods("PATCH")
	admin.HandleFunc("/cases/{id}/events", casesHandler.ListEvents).Methods("GET")
	admin.HandleFunc("/security/blocklist", securityHandler.GetBlocklist).Methods("GET")
	admin.HandleFunc("/security/blocklist", securityHandler.AddToBlocklist).Methods("POST")
	admin.HandleFunc("/security/blocklist/{id}", securityHandler.RemoveFromBlocklist).Methods("DELETE")
	admin.HandleFunc("/security/health", securityHandler.GetSystemHealth).Methods("GET")
	admin.HandleFunc("/notifications", systemHandler.GetNotifications).Methods("GET")
	admin.HandleFunc("/notifications/read-all", systemHandler.MarkAllNotificationsRead).Methods("POST")
	admin.HandleFunc("/notifications/{id}/read", systemHandler.MarkNotificationRead).Methods("POST")
	admin.HandleFunc("/notifications/{id}", systemHandler.ArchiveNotification).Methods("DELETE")

	// Admin: Wallets
	admin.HandleFunc("/wallets", walletHandler.GetAllWallets).Methods("GET")
	admin.HandleFunc("/wallets/fix-addresses", walletHandler.FixWalletAddresses).Methods("POST")
	admin.HandleFunc("/wallets/{id}/transactions", walletHandler.GetTransactionHistoryAdmin).Methods("GET")
	admin.HandleFunc("/blockchain/wallets", walletHandler.GetBlockchainWallets).Methods("GET")

	// Admin: Blockchain Network Management
	admin.HandleFunc("/blockchain/networks", blockchainHandler.ListNetworks).Methods("GET")
	admin.HandleFunc("/blockchain/networks", blockchainHandler.CreateNetwork).Methods("POST")
	admin.HandleFunc("/blockchain/networks/{id}", blockchainHandler.GetNetwork).Methods("GET")
	admin.HandleFunc("/blockchain/networks/{id}", blockchainHandler.UpdateNetwork).Methods("PUT", "PATCH")
	admin.HandleFunc("/blockchain/networks/{id}", blockchainHandler.DeleteNetwork).Methods("DELETE")
	admin.HandleFunc("/blockchain/wallets/{wallet_id}/verify-ledger", blockchainHandler.VerifyLedgerChain).Methods("GET")
	admin.HandleFunc("/blockchain/wallets/{wallet_id}/ledger-chain", blockchainHandler.GetLedgerChainReport).Methods("GET")

	// Admin: Banking
	admin.HandleFunc("/banking/settlements", settlementHandler.ListSettlements).Methods("GET")
	admin.HandleFunc("/banking/settlements/{id}", settlementHandler.GetSettlement).Methods("GET")
	admin.HandleFunc("/banking/settlements/{id}/retry", settlementHandler.RetrySettlement).Methods("POST")
	admin.HandleFunc("/banking/settlements/{id}/reconcile", settlementHandler.ReconcileSettlement).Methods("POST")
	admin.HandleFunc("/banking/accounts", settlementHandler.GetBankAccounts).Methods("GET")
	admin.HandleFunc("/banking/gateways", settlementHandler.GetPaymentGateways).Methods("GET")

	// Cleaned up redundant code blocks

	payments := api.PathPrefix("/payments").Subrouter()
	// payments.Use(idemMW.Require) - Removed redundant middleware (already on api)
	payments.HandleFunc("/initiate", paymentHandler.InitiatePayment).Methods("POST")
	// payments.HandleFunc("/receiver-info", paymentHandler.GetReceiverInfo).Methods("GET") // Removed, use /wallets/lookup or /wallets/search
	payments.HandleFunc("/{id}/receipt", paymentHandler.GetReceipt).Methods("GET")
	payments.HandleFunc("/{id}", paymentHandler.GetTransactionForUser).Methods("GET")
	payments.HandleFunc("/{id}/cancel", paymentHandler.CancelPayment).Methods("POST")
	payments.HandleFunc("/bulk", paymentHandler.BulkPayment).Methods("POST")
	payments.HandleFunc("", paymentHandler.GetTransactions).Methods("GET")

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

	// Graceful shutdown
	go func() {
		log.Info("Payment service started", map[string]interface{}{
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
