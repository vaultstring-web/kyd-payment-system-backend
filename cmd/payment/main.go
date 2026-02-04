// ==============================================================================
// COMPLETE PAYMENT SERVICE MAIN - cmd/payment/main.go
// ==============================================================================
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"kyd/internal/blockchain/ripple"
	"kyd/internal/blockchain/stellar"
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
	auditRepo := postgres.NewAuditRepository(db)
	ledgerRepo := postgres.NewLedgerRepository(db)
	securityRepo := postgres.NewSecurityRepository(db)

	// Initialize services
	ledgerService := ledger.NewService(db, ledgerRepo)
	securityService := security.NewService(securityRepo)

	// Initialize blockchain connectors
	stellarConnector, err := stellar.NewConnector(
		cfg.Stellar.NetworkURL,
		cfg.Stellar.SecretKey,
		true, // Use simulation mode for now
	)
	if err != nil {
		log.Fatal("Failed to initialize Stellar connector", map[string]interface{}{"error": err.Error()})
	}

	rippleConnector, err := ripple.NewConnector(
		cfg.Ripple.ServerURL,
		cfg.Ripple.SecretKey,
	)
	if err != nil {
		log.Fatal("Failed to initialize Ripple connector", map[string]interface{}{"error": err.Error()})
	}

	// Initialize Settlement Service (Background Worker)
	_ = settlement.NewService(
		settlementRepo,
		txRepo,
		stellarConnector,
		rippleConnector,
		log,
	)

	// Initialize forex providers
	forexProviders := []forex.RateProvider{
		forex.NewMockRateProvider(),
		forex.NewExchangeRateAPIProvider(),
	}

	// Initialize Notification Service
	notificationService := notification.NewService(log, auditRepo)

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
	forexHandler := handler.NewForexHandler(forexService, val, log)

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
				Value:      dbLatency,
				RecordedAt: time.Now(),
			})

			// Redis ping latency
			start = time.Now()
			_ = redisClient.Ping(context.Background()).Err()
			redisLatency := time.Since(start).Seconds()
			_ = securityService.RecordHealthSnapshot(context.Background(), &domain.SystemHealthMetric{
				MetricName: "redis_ping_latency_seconds",
				Value:      redisLatency,
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

	blacklist := middleware.NewRedisTokenBlacklist(redisClient)
	authMW := middleware.NewAuthMiddleware(cfg.JWT.Secret, blacklist)
	idemMW := middleware.NewIdempotencyMiddleware(redisClient, 24*time.Hour)
	auditMW := middleware.NewAuditMiddleware(auditRepo, log)

	// Health check routes (no auth)
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/ready", readyCheck(db)).Methods("GET")

	// Protected routes
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(auditMW.Audit) // Audit logs for all API requests
	api.Use(authMW.Authenticate)
	api.Use(idemMW.Require) // Enforce Idempotency-Key
	api.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).WithAdaptive(5, 15*time.Minute).Limit)

	api.HandleFunc("/wallets", walletHandler.GetUserWallets).Methods("GET")
	api.HandleFunc("/wallets/lookup", walletHandler.LookupWallet).Methods("GET")
	api.HandleFunc("/wallets/search", walletHandler.SearchWallets).Methods("GET")
	api.HandleFunc("/wallets/{id}/transactions", walletHandler.GetTransactionHistory).Methods("GET")
	api.HandleFunc("/payments", paymentHandler.InitiatePayment).Methods("POST")
	api.HandleFunc("/payments/initiate", paymentHandler.InitiatePayment).Methods("POST") // Add explicit route
	api.HandleFunc("/payments", paymentHandler.GetTransactions).Methods("GET")
	api.HandleFunc("/transactions/{id}/receipt", paymentHandler.GetReceipt).Methods("GET")
	api.HandleFunc("/disputes", paymentHandler.InitiateDispute).Methods("POST")

	// Forex routes
	api.HandleFunc("/forex/rates", forexHandler.GetAllRates).Methods("GET")
	api.HandleFunc("/forex/calculate", forexHandler.Calculate).Methods("POST")

	// Admin routes
	admin := api.PathPrefix("/admin").Subrouter()
	admin.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).WithAdaptive(5, 15*time.Minute).Limit)
	// Admin: Transaction Management
	admin.HandleFunc("/transactions", paymentHandler.GetAllTransactions).Methods("GET")
	admin.HandleFunc("/transactions/pending", paymentHandler.GetPendingTransactions).Methods("GET")
	admin.HandleFunc("/transactions/{id}/review", paymentHandler.ReviewTransaction).Methods("POST")
	admin.HandleFunc("/stats", paymentHandler.GetSystemStats).Methods("GET")
	admin.HandleFunc("/analytics/volume", paymentHandler.GetTransactionVolume).Methods("GET")
	admin.HandleFunc("/risk/alerts", paymentHandler.GetRiskAlerts).Methods("GET")
	admin.HandleFunc("/audit-logs", paymentHandler.GetAuditLogs).Methods("GET")
	admin.HandleFunc("/disputes", paymentHandler.GetDisputes).Methods("GET")
	admin.HandleFunc("/disputes/resolve", paymentHandler.ResolveDispute).Methods("POST")

	// Admin: Compliance
	admin.HandleFunc("/compliance/kyc", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		status := r.URL.Query().Get("status")
		var users []*domain.User
		var total int
		var err error

		if status != "" {
			users, err = userRepo.FindAllByKYCStatus(r.Context(), status, limit, offset)
			if err == nil {
				total, _ = userRepo.CountAllByKYCStatus(r.Context(), status)
			}
		} else {
			// If no status specified, fetch all users (filtering for non-empty KYC could be added here)
			// For now, fetching all users to populate the queue
			users, err = userRepo.FindAll(r.Context(), limit, offset, "")
			if err == nil {
				total, _ = userRepo.CountAll(r.Context(), "")
			}
		}

		if err != nil {
			log.Error("Failed to fetch kyc applications", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Map Users to KYCApplication
		type KYCApplication struct {
			ID          uuid.UUID   `json:"id"`
			UserID      uuid.UUID   `json:"user_id"`
			Status      string      `json:"status"`
			SubmittedAt time.Time   `json:"submitted_at"`
			ReviewedAt  *time.Time  `json:"reviewed_at,omitempty"`
			ReviewerID  *uuid.UUID  `json:"reviewer_id,omitempty"`
			Documents   interface{} `json:"documents,omitempty"`
			Name        string      `json:"name"`  // Added for frontend convenience
			Email       string      `json:"email"` // Added for frontend convenience
		}

		apps := make([]KYCApplication, len(users))
		for i, u := range users {
			// Use CreatedAt or UpdatedAt as proxy for submission time
			submitted := u.UpdatedAt
			if submitted.IsZero() {
				submitted = u.CreatedAt
			}

			apps[i] = KYCApplication{
				ID:          u.ID, // Using UserID as ApplicationID for simplicity 1:1
				UserID:      u.ID,
				Status:      string(u.KYCStatus),
				SubmittedAt: submitted,
				Name:        u.FirstName + " " + u.LastName,
				Email:       u.Email,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"applications": apps,
			"total":        total,
			"limit":        limit,
			"offset":       offset,
		})
	}).Methods("GET")

	admin.HandleFunc("/compliance/reports", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		// Generate dynamic reports based on system state
		type ComplianceReport struct {
			ID          string    `json:"id"`
			ReportType  string    `json:"report_type"`
			Period      string    `json:"period"`
			GeneratedAt time.Time `json:"generated_at"`
			Status      string    `json:"status"`
			FileURL     string    `json:"file_url,omitempty"`
		}

		// Create a "Daily Reconciliation" report
		reports := []ComplianceReport{
			{
				ID:          uuid.New().String(),
				ReportType:  "Daily Reconciliation",
				Period:      time.Now().Format("2006-01-02"),
				GeneratedAt: time.Now(),
				Status:      "ready",
				FileURL:     "/api/v1/admin/reports/download/daily-recon.pdf",
			},
			{
				ID:          uuid.New().String(),
				ReportType:  "KYC Summary",
				Period:      time.Now().Format("2006-01"),
				GeneratedAt: time.Now().Add(-24 * time.Hour),
				Status:      "ready",
				FileURL:     "/api/v1/admin/reports/download/kyc-summary.pdf",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"reports": reports,
			"total":   len(reports),
		})
	}).Methods("GET")

	// Admin: Security Center
	admin.HandleFunc("/security/events", securityHandler.GetSecurityEvents).Methods("GET")
	admin.HandleFunc("/security/events/{id}", securityHandler.UpdateSecurityEvent).Methods("PATCH")
	admin.HandleFunc("/security/blocklist", securityHandler.GetBlocklist).Methods("GET")
	admin.HandleFunc("/security/blocklist", securityHandler.AddToBlocklist).Methods("POST")
	admin.HandleFunc("/security/blocklist/{id}", securityHandler.RemoveFromBlocklist).Methods("DELETE")
	admin.HandleFunc("/security/health", securityHandler.GetSystemHealth).Methods("GET")

	admin.HandleFunc("/wallets", walletHandler.GetAllWallets).Methods("GET")
	admin.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 100
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		kycStatus := r.URL.Query().Get("kyc_status")
		userType := r.URL.Query().Get("user_type")

		var users []*domain.User
		var total int
		var err error

		if kycStatus != "" {
			users, err = userRepo.FindAllByKYCStatus(r.Context(), kycStatus, limit, offset)
			if err == nil {
				total, _ = userRepo.CountAllByKYCStatus(r.Context(), kycStatus)
			}
		} else {
			users, err = userRepo.FindAll(r.Context(), limit, offset, userType)
			if err == nil {
				total, _ = userRepo.CountAll(r.Context(), userType)
			}
		}

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to fetch users"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type Resp struct {
			Users  []*domain.User `json:"users"`
			Total  int            `json:"total"`
			Limit  int            `json:"limit"`
			Offset int            `json:"offset"`
		}
		b, _ := json.Marshal(Resp{Users: users, Total: total, Limit: limit, Offset: offset})
		w.Write(b)
	}).Methods("GET")

	admin.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid user id"}`))
			return
		}

		var req struct {
			Password      string `json:"password,omitempty"`
			FirstName     string `json:"first_name,omitempty"`
			LastName      string `json:"last_name,omitempty"`
			Email         string `json:"email,omitempty"`
			Phone         string `json:"phone,omitempty"`
			AccountStatus string `json:"account_status,omitempty"` // active/blocked
			Role          string `json:"role,omitempty"`           // user/admin/support
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid request body"}`))
			return
		}

		user, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"user not found"}`))
			return
		}

		changes := make(domain.Metadata)

		if req.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			user.PasswordHash = string(hash)
			changes["password"] = "updated"
		}
		if req.FirstName != "" {
			user.FirstName = req.FirstName
			changes["first_name"] = req.FirstName
		}
		if req.LastName != "" {
			user.LastName = req.LastName
			changes["last_name"] = req.LastName
		}
		if req.Email != "" {
			user.Email = req.Email
			changes["email"] = req.Email
		}
		if req.Phone != "" {
			user.Phone = req.Phone
			changes["phone"] = req.Phone
		}
		if req.AccountStatus != "" {
			isActive := req.AccountStatus == "active"
			user.IsActive = isActive
			changes["is_active"] = isActive
		}
		if req.Role != "" {
			user.UserType = domain.UserType(req.Role)
			changes["role"] = req.Role
		}

		if err := userRepo.Update(r.Context(), user); err != nil {
			log.Error("Failed to update user", map[string]interface{}{"error": err.Error(), "user_id": id})
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to update user"}`))
			return
		}

		// Audit Log
		entityType := "user"
		ip := r.RemoteAddr
		ua := r.UserAgent()
		changesBytes, _ := json.Marshal(changes)

		auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID:     &id,
			Action:     "ADMIN_UPDATE_USER",
			EntityID:   id.String(),
			EntityType: entityType,
			NewValues:  json.RawMessage(changesBytes),
			IPAddress:  ip,
			UserAgent:  ua,
		})

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"user updated successfully"}`))
	}).Methods("PATCH")

	admin.HandleFunc("/users/{id}/kyc", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid user id"}`))
			return
		}

		var req struct {
			KYCStatus string `json:"kyc_status"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid request body"}`))
			return
		}

		user, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"user not found"}`))
			return
		}

		// Update KYC Status
		oldStatus := user.KYCStatus
		user.KYCStatus = domain.KYCStatus(req.KYCStatus)

		if err := userRepo.Update(r.Context(), user); err != nil {
			log.Error("Failed to update user kyc", map[string]interface{}{"error": err.Error(), "user_id": id})
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to update user kyc"}`))
			return
		}

		// Audit Log
		entityType := "user"
		ip := r.RemoteAddr
		ua := r.UserAgent()
		changes := domain.Metadata{
			"kyc_status_old": string(oldStatus),
			"kyc_status_new": req.KYCStatus,
			"reason":         req.Reason,
		}
		changesBytes, _ := json.Marshal(changes)

		auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID:     &id,
			Action:     "ADMIN_UPDATE_KYC",
			EntityID:   id.String(),
			EntityType: entityType,
			NewValues:  json.RawMessage(changesBytes),
			IPAddress:  ip,
			UserAgent:  ua,
		})

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"kyc status updated successfully"}`))
	}).Methods("PATCH")

	admin.HandleFunc("/blockchain/networks", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		type BlockchainNetwork struct {
			NetworkID     string `json:"network_id"`
			Name          string `json:"name"`
			Status        string `json:"status"`
			Height        int    `json:"height"`
			PeerCount     int    `json:"peer_count"`
			LastBlockTime string `json:"last_block_time"`
			Channel       string `json:"channel"`
		}

		// Mocked networks
		networks := []BlockchainNetwork{
			{
				NetworkID:     "net-stellar-test",
				Name:          "Stellar Testnet",
				Status:        "active",
				Height:        123456,
				PeerCount:     15,
				LastBlockTime: time.Now().Format(time.RFC3339),
				Channel:       "public",
			},
			{
				NetworkID:     "net-hyperledger-kyd",
				Name:          "Hyperledger Fabric KYD",
				Status:        "active",
				Height:        5678,
				PeerCount:     4,
				LastBlockTime: time.Now().Add(-2 * time.Second).Format(time.RFC3339),
				Channel:       "kyd-channel",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"networks": networks})
	}).Methods("GET")

	admin.HandleFunc("/blockchain/transactions", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		txs, err := txRepo.FindAll(r.Context(), limit, offset)
		if err != nil {
			log.Error("Failed to fetch blockchain transactions", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		total, _ := txRepo.CountAll(r.Context())

		type BlockchainTransaction struct {
			TxID        uuid.UUID `json:"tx_id"`
			BlockNumber int       `json:"block_number,omitempty"`
			Status      string    `json:"status"`
			Timestamp   time.Time `json:"timestamp"`
			Channel     string    `json:"channel,omitempty"`
			Chaincode   string    `json:"chaincode,omitempty"`
		}

		bTxs := make([]BlockchainTransaction, len(txs))
		for i, tx := range txs {
			bTxs[i] = BlockchainTransaction{
				TxID:      tx.ID,
				Status:    string(tx.Status),
				Timestamp: tx.CreatedAt,
				Channel:   "kyd-channel",
			}
			if tx.BlockchainTxHash != "" {
				// Mock block number for display
				bTxs[i].BlockNumber = 1000 + i
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"transactions": bTxs, "total": total})
	}).Methods("GET")

	admin.HandleFunc("/analytics/metrics", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		stats, err := txRepo.GetSystemStats(r.Context())
		if err != nil {
			log.Error("Failed to fetch system stats", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}).Methods("GET")

	admin.HandleFunc("/analytics/volume", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		months := 6
		if v := r.URL.Query().Get("months"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				months = n
			}
		}

		volumes, err := txRepo.GetTransactionVolume(r.Context(), months)
		if err != nil {
			log.Error("Failed to fetch transaction volume", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"volumes": volumes})
	}).Methods("GET")

	admin.HandleFunc("/analytics/earnings", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		earnings, _ := txRepo.SumEarnings(r.Context())
		txCount, _ := txRepo.CountAll(r.Context())

		avg := 0.0
		if txCount > 0 {
			avg = earnings.InexactFloat64() / float64(txCount)
		}

		type EarningsReport struct {
			Period                        string  `json:"period"`
			TotalEarnings                 float64 `json:"total_earnings"`
			TransactionCount              int     `json:"transaction_count"`
			AverageEarningsPerTransaction float64 `json:"average_earnings_per_transaction"`
			Currency                      string  `json:"currency"`
		}

		// Return a single "All Time" report for now
		report := EarningsReport{
			Period:                        "All Time",
			TotalEarnings:                 earnings.InexactFloat64(),
			TransactionCount:              txCount,
			AverageEarningsPerTransaction: avg,
			Currency:                      "MWK",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"reports": []EarningsReport{report}})
	}).Methods("GET")

	admin.HandleFunc("/compliance/kyc", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		status := r.URL.Query().Get("status")
		limit := 50
		offset := 0
		// Parsing limit/offset omitted for brevity, using defaults if err

		var users []*domain.User
		var total int
		var err error

		if status != "" {
			users, total, err = userRepo.FindByKYCStatus(r.Context(), status, limit, offset)
		} else {
			users, err = userRepo.FindAll(r.Context(), limit, offset, "")
			total, _ = userRepo.CountAll(r.Context(), "")
		}

		if err != nil {
			log.Error("Failed to fetch kyc applications", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		type KYCApplication struct {
			ID           string        `json:"id"`
			CustomerID   string        `json:"customerId"`
			CustomerName string        `json:"customerName"`
			CustomerType string        `json:"customerType"`
			SubmittedAt  time.Time     `json:"submittedAt"`
			Status       string        `json:"status"`
			RiskLevel    string        `json:"riskLevel"`
			RiskScore    float64       `json:"riskScore"`
			Documents    []interface{} `json:"documents"`
		}

		apps := make([]KYCApplication, len(users))
		for i, u := range users {
			name := u.FirstName + " " + u.LastName
			if u.BusinessName != nil && *u.BusinessName != "" {
				name = *u.BusinessName
			}

			score := u.RiskScore.InexactFloat64()
			riskLevel := "low"
			if score > 80 {
				riskLevel = "critical"
			} else if score > 60 {
				riskLevel = "high"
			} else if score > 30 {
				riskLevel = "medium"
			}

			apps[i] = KYCApplication{
				ID:           u.ID.String(),
				CustomerID:   u.ID.String(),
				CustomerName: name,
				CustomerType: string(u.UserType),
				SubmittedAt:  u.CreatedAt,
				Status:       string(u.KYCStatus),
				RiskLevel:    riskLevel,
				RiskScore:    score,
				Documents:    []interface{}{},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"applications": apps, "total": total})
	}).Methods("GET")

	admin.HandleFunc("/compliance/kyc/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var req struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		user, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		user.KYCStatus = domain.KYCStatus(req.Status)
		if err := userRepo.Update(r.Context(), user); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Log audit
		entityType := "user"
		newVals := domain.Metadata{"status": req.Status, "reason": req.Reason}
		newValsJSON, _ := json.Marshal(newVals)
		auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID:     &id,
			Action:     "KYC_UPDATE",
			EntityType: entityType,
			EntityID:   id.String(),
			NewValues:  newValsJSON,
		})

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"status updated"}`))
	}).Methods("PATCH")

	// User KYC Status Update (Direct User Endpoint)
	admin.HandleFunc("/users/{id}/kyc", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var req struct {
			KYCStatus string `json:"kyc_status"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		user, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		user.KYCStatus = domain.KYCStatus(req.KYCStatus)
		if err := userRepo.Update(r.Context(), user); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Log audit
		entityType := "user"
		newVals := domain.Metadata{"status": req.KYCStatus, "reason": req.Reason}
		newValsJSON, _ := json.Marshal(newVals)
		auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID:     &id,
			Action:     "KYC_UPDATE",
			EntityType: entityType,
			EntityID:   id.String(),
			NewValues:  newValsJSON,
		})

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"kyc status updated"}`))
	}).Methods("PATCH")

	// User Role Update
	admin.HandleFunc("/users/{id}/role", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var req struct {
			UserType string `json:"user_type"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		user, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		user.UserType = domain.UserType(req.UserType)
		if err := userRepo.Update(r.Context(), user); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Log audit
		entityType := "user"
		newVals := domain.Metadata{"user_type": req.UserType, "reason": req.Reason}
		newValsJSON, _ := json.Marshal(newVals)
		auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID:     &id,
			Action:     "ROLE_UPDATE",
			EntityType: entityType,
			EntityID:   id.String(),
			NewValues:  newValsJSON,
		})

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"user role updated"}`))
	}).Methods("PATCH")

	// General User Profile Update (Admin)
	admin.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var req struct {
			FirstName   string `json:"first_name"`
			LastName    string `json:"last_name"`
			Email       string `json:"email"`
			Phone       string `json:"phone"`
			CountryCode string `json:"country_code"`
			Password    string `json:"password"` // Optional
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		user, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Update fields if provided
		if req.FirstName != "" {
			user.FirstName = req.FirstName
		}
		if req.LastName != "" {
			user.LastName = req.LastName
		}
		if req.Email != "" {
			user.Email = req.Email
		}
		if req.Phone != "" {
			user.Phone = req.Phone
		}
		if req.CountryCode != "" {
			user.CountryCode = req.CountryCode
		}
		if req.Password != "" {
			hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			user.PasswordHash = string(hashed)
		}

		user.UpdatedAt = time.Now()

		if err := userRepo.Update(r.Context(), user); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Log audit
		entityType := "user"
		newVals := domain.Metadata{
			"first_name": req.FirstName,
			"last_name":  req.LastName,
			"email":      req.Email,
			"phone":      req.Phone,
			"country":    req.CountryCode,
			"password":   "*****", // masked
		}
		newValsJSON, _ := json.Marshal(newVals)
		auditRepo.Create(r.Context(), &domain.AuditLog{
			UserID:     &id,
			Action:     "PROFILE_UPDATE",
			EntityType: entityType,
			EntityID:   id.String(),
			NewValues:  newValsJSON,
		})

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"profile updated"}`))
	}).Methods("PATCH")

	// User Activity
	admin.HandleFunc("/users/{id}/activity", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		logs, err := auditRepo.FindByUserID(r.Context(), id, limit, offset)
		if err != nil {
			log.Error("Failed to fetch user activity", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Format logs for UI
		type ActivityLog struct {
			ID        uuid.UUID `json:"id"`
			Action    string    `json:"action"`
			Timestamp time.Time `json:"timestamp"`
			Details   string    `json:"details,omitempty"`
		}

		uiLogs := make([]ActivityLog, len(logs))
		for i, l := range logs {
			details := ""
			if l.NewValues != nil {
				var meta map[string]interface{}
				if err := json.Unmarshal(l.NewValues, &meta); err == nil {
					if reason, ok := meta["reason"].(string); ok {
						details = reason
					}
				}
			}
			uiLogs[i] = ActivityLog{
				ID:        l.ID,
				Action:    l.Action,
				Timestamp: l.CreatedAt,
				Details:   details,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"logs": uiLogs, "total": len(uiLogs)}) // Total logic requires CountByUserID if needed
	}).Methods("GET")

	// User Profile Update
	// Get All Users
	admin.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		users, err := userRepo.FindAll(r.Context(), limit, offset, "")
		if err != nil {
			log.Error("Failed to fetch users", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		total, _ := userRepo.CountAll(r.Context(), "")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"users": users, "total": total})
	}).Methods("GET")

	// Banking Endpoints
	admin.HandleFunc("/banking/accounts", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// Mock Data
		accounts := []map[string]interface{}{
			{
				"id": "ba-001", "bank_name": "Standard Bank", "account_number": "****1234",
				"account_holder": "VaultString Ltd", "currency": "MWK", "balance": 5000000.00,
				"status": "active", "connected_at": time.Now().Add(-2400 * time.Hour),
			},
			{
				"id": "ba-002", "bank_name": "NBS Bank", "account_number": "****5678",
				"account_holder": "VaultString Ops", "currency": "MWK", "balance": 1250000.50,
				"status": "active", "connected_at": time.Now().Add(-1200 * time.Hour),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"accounts": accounts})
	}).Methods("GET")

	admin.HandleFunc("/banking/gateways", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// Mock Data
		gateways := []map[string]interface{}{
			{
				"id": "pg-001", "name": "Airtel Money", "provider": "airtel",
				"status": "active", "last_sync": time.Now().Format(time.RFC3339),
			},
			{
				"id": "pg-002", "name": "Mpamba", "provider": "tnm",
				"status": "active", "last_sync": time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"gateways": gateways})
	}).Methods("GET")

	admin.HandleFunc("/banking/settlements", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			n, _ := strconv.Atoi(v)
			if n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			n, _ := strconv.Atoi(v)
			if n >= 0 {
				offset = n
			}
		}

		settlements, err := settlementRepo.FindAll(r.Context(), limit, offset)
		if err != nil {
			log.Error("Failed to fetch settlements", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		total, _ := settlementRepo.CountAll(r.Context())

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"settlements": settlements, "total": total})
	}).Methods("GET")

	admin.HandleFunc("/compliance/reports", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		// Mock reports
		type ComplianceReport struct {
			ID          string    `json:"id"`
			ReportType  string    `json:"report_type"`
			Period      string    `json:"period"`
			GeneratedAt time.Time `json:"generated_at"`
			Status      string    `json:"status"`
		}

		reports := []ComplianceReport{
			{ID: "rep-001", ReportType: "AML_DAILY", Period: "2024-01-24", GeneratedAt: time.Now(), Status: "ready"},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"reports": reports, "total": 1})
	}).Methods("GET")

	admin.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid user id"}`))
			return
		}
		u, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"user not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		b, _ := json.Marshal(u)
		w.Write(b)
	}).Methods("GET")

	// Update KYC Status
	admin.HandleFunc("/users/{id}/kyc", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid user id"}`))
			return
		}

		var req struct {
			Status domain.KYCStatus `json:"kyc_status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		u, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		u.KYCStatus = req.Status
		u.UpdatedAt = time.Now()

		if err := userRepo.Update(r.Context(), u); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to update kyc status"}`))
			return
		}

		// Log audit
		entityType := "user"
		auditRepo.Create(r.Context(), &domain.AuditLog{
			ID:         uuid.New(),
			UserID:     &id, // Subject
			Action:     "UPDATE_KYC",
			EntityType: entityType,
			EntityID:   id.String(),
			CreatedAt:  time.Now(),
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(u)
	}).Methods("PATCH")

	// Delete User
	admin.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid user id"}`))
			return
		}

		u, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		u.IsActive = false
		u.UpdatedAt = time.Now()

		if err := userRepo.Update(r.Context(), u); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to delete user"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"user deleted successfully"}`))
	}).Methods("DELETE")

	// Update User Status
	admin.HandleFunc("/users/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid user id"}`))
			return
		}

		var req struct {
			IsActive bool   `json:"is_active"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		u, err := userRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		u.IsActive = req.IsActive
		u.UpdatedAt = time.Now()

		if err := userRepo.Update(r.Context(), u); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to update user status"}`))
			return
		}

		// Audit log
		action := "DEACTIVATE_USER"
		if req.IsActive {
			action = "ACTIVATE_USER"
		}
		entityType := "user"
		auditRepo.Create(r.Context(), &domain.AuditLog{
			ID:         uuid.New(),
			UserID:     &id,
			Action:     action,
			EntityType: entityType,
			EntityID:   id.String(),
			CreatedAt:  time.Now(),
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"user status updated"}`))
	}).Methods("PATCH")

	// Get User Audit Logs
	admin.HandleFunc("/users/{id}/activity", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid user id"}`))
			return
		}

		logs, err := auditRepo.FindByUserID(r.Context(), id, 100, 0)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to fetch audit logs"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type Resp struct {
			Logs []*domain.AuditLog `json:"logs"`
		}
		json.NewEncoder(w).Encode(Resp{Logs: logs})
	}).Methods("GET")

	// Get All Audit Logs
	admin.HandleFunc("/audit/logs", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 100
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		logs, err := auditRepo.FindAll(r.Context(), limit, offset)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to fetch audit logs"}`))
			return
		}
		total, _ := auditRepo.CountAll(r.Context())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type Resp struct {
			Logs  []*domain.AuditLog `json:"logs"`
			Total int                `json:"total"`
		}
		json.NewEncoder(w).Encode(Resp{Logs: logs, Total: total})
	}).Methods("GET")

	// Flag Transaction
	admin.HandleFunc("/transactions/{id}/flag", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid transaction id"}`))
			return
		}

		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := txRepo.Flag(r.Context(), id, req.Reason); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to flag transaction"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"transaction flagged"}`))
	}).Methods("POST")

	// Fix Wallet Addresses
	admin.HandleFunc("/wallets/fix-addresses", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		// Fetch all wallets (limit 10000 for safety)
		wallets, err := walletRepo.FindAll(r.Context(), 10000, 0)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to fetch wallets"}`))
			return
		}

		count := 0
		for _, w := range wallets {
			addr := ""
			if w.WalletAddress != nil {
				addr = *w.WalletAddress
			}

			// Check if address is valid 16-digit number
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
					continue
				}
				newAddr := fmt.Sprintf("%016d", n)

				if err := walletRepo.UpdateWalletAddress(r.Context(), w.ID, newAddr); err == nil {
					count++
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"message": fmt.Sprintf("Updated %d wallets", count)})
	}).Methods("POST")

	// Compliance Reports (Mock)
	admin.HandleFunc("/compliance/reports", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}

		// Mock reports
		type Report struct {
			ID          string    `json:"id"`
			Type        string    `json:"type"`
			Status      string    `json:"status"`
			GeneratedAt time.Time `json:"generated_at"`
			URL         string    `json:"url"`
		}
		reports := []Report{
			{ID: "rep-001", Type: "AML", Status: "completed", GeneratedAt: time.Now().Add(-24 * time.Hour), URL: "#"},
			{ID: "rep-002", Type: "KYC", Status: "pending", GeneratedAt: time.Now(), URL: "#"},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"reports": reports})
	}).Methods("GET")

	admin.HandleFunc("/transactions", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 100
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		txs, err := txRepo.FindAll(r.Context(), limit, offset)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to fetch transactions"}`))
			return
		}
		total, _ := txRepo.CountAll(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type AdminTx struct {
			ID                   uuid.UUID                `json:"id"`
			TransactionID        *uuid.UUID               `json:"transaction_id,omitempty"`
			SenderID             uuid.UUID                `json:"sender_id"`
			ReceiverID           uuid.UUID                `json:"receiver_id"`
			SenderUserType       domain.UserType          `json:"sender_user_type"`
			ReceiverUserType     domain.UserType          `json:"receiver_user_type"`
			SenderName           *string                  `json:"sender_name,omitempty"`
			SenderEmail          *string                  `json:"sender_email,omitempty"`
			SenderWalletNumber   *string                  `json:"sender_wallet_number,omitempty"`
			ReceiverName         *string                  `json:"receiver_name,omitempty"`
			ReceiverEmail        *string                  `json:"receiver_email,omitempty"`
			ReceiverWalletNumber *string                  `json:"receiver_wallet_number,omitempty"`
			Amount               domain.Money             `json:"amount"`
			Fee                  domain.Money             `json:"fee"`
			NetAmount            domain.Money             `json:"net_amount"`
			Status               domain.TransactionStatus `json:"status"`
			TransactionType      domain.TransactionType   `json:"transaction_type"`
			StatusReason         string                   `json:"status_reason,omitempty"`
			Reference            string                   `json:"reference"`
			CreatedAt            time.Time                `json:"created_at"`
			UpdatedAt            time.Time                `json:"updated_at"`
			Flagged              bool                     `json:"flagged"`
		}
		ids := make([]uuid.UUID, 0, len(txs)*2)
		idSeen := make(map[uuid.UUID]struct{})
		walletIDs := make([]uuid.UUID, 0, len(txs)*2)
		walletSeen := make(map[uuid.UUID]struct{})

		for _, t := range txs {
			if _, ok := idSeen[t.SenderID]; !ok {
				ids = append(ids, t.SenderID)
				idSeen[t.SenderID] = struct{}{}
			}
			if _, ok := idSeen[t.ReceiverID]; !ok {
				ids = append(ids, t.ReceiverID)
				idSeen[t.ReceiverID] = struct{}{}
			}
			if t.SenderWalletID != nil {
				if _, ok := walletSeen[*t.SenderWalletID]; !ok {
					walletIDs = append(walletIDs, *t.SenderWalletID)
					walletSeen[*t.SenderWalletID] = struct{}{}
				}
			}
			if t.ReceiverWalletID != nil {
				if _, ok := walletSeen[*t.ReceiverWalletID]; !ok {
					walletIDs = append(walletIDs, *t.ReceiverWalletID)
					walletSeen[*t.ReceiverWalletID] = struct{}{}
				}
			}
		}
		users, _ := userRepo.FindByIDs(r.Context(), ids)
		userMap := make(map[uuid.UUID]*domain.User, len(users))
		for _, u := range users {
			userMap[u.ID] = u
		}

		wallets, _ := walletRepo.FindByIDs(r.Context(), walletIDs)
		walletMap := make(map[uuid.UUID]*domain.Wallet, len(wallets))
		for _, w := range wallets {
			walletMap[w.ID] = w
		}

		out := make([]AdminTx, 0, len(txs))
		for _, t := range txs {
			var sender *domain.User = userMap[t.SenderID]
			var receiver *domain.User = userMap[t.ReceiverID]
			var sName *string
			var sEmail *string
			var rName *string
			var rEmail *string
			var sType domain.UserType
			var rType domain.UserType
			var sWalletNum *string
			var rWalletNum *string

			if sender != nil {
				name := sender.FirstName + " " + sender.LastName
				sName = &name
				email := sender.Email
				sEmail = &email
				sType = sender.UserType
			}
			if receiver != nil {
				name := receiver.FirstName + " " + receiver.LastName
				rName = &name
				email := receiver.Email
				rEmail = &email
				rType = receiver.UserType
			}

			if t.SenderWalletID != nil {
				if w, ok := walletMap[*t.SenderWalletID]; ok {
					sWalletNum = w.WalletAddress
				}
			}
			if t.ReceiverWalletID != nil {
				if w, ok := walletMap[*t.ReceiverWalletID]; ok {
					rWalletNum = w.WalletAddress
				}
			}

			amt := domain.Money{Amount: t.Amount, Currency: t.Currency}
			fee := domain.Money{Amount: t.FeeAmount, Currency: t.FeeCurrency}
			net := domain.Money{Amount: t.NetAmount, Currency: t.ConvertedCurrency}

			flagged := false
			if val, ok := t.Metadata["flagged"]; ok {
				if b, ok := val.(bool); ok {
					flagged = b
				}
			}

			out = append(out, AdminTx{
				ID:                   t.ID,
				SenderID:             t.SenderID,
				ReceiverID:           t.ReceiverID,
				SenderUserType:       sType,
				ReceiverUserType:     rType,
				SenderName:           sName,
				SenderEmail:          sEmail,
				SenderWalletNumber:   sWalletNum,
				ReceiverName:         rName,
				ReceiverEmail:        rEmail,
				ReceiverWalletNumber: rWalletNum,
				Amount:               amt,
				Fee:                  fee,
				NetAmount:            net,
				Status:               t.Status,
				TransactionType:      t.TransactionType,
				StatusReason:         t.StatusReason,
				Reference:            t.Reference,
				CreatedAt:            t.CreatedAt,
				UpdatedAt:            t.UpdatedAt,
				Flagged:              flagged,
			})
		}
		type Resp struct {
			Transactions []AdminTx `json:"transactions"`
			Total        int       `json:"total"`
			Limit        int       `json:"limit"`
			Offset       int       `json:"offset"`
		}
		b, _ := json.Marshal(Resp{Transactions: out, Total: total, Limit: limit, Offset: offset})
		w.Write(b)
	}).Methods("GET")

	// Get Transaction By ID
	admin.HandleFunc("/transactions/{id}", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		vars := mux.Vars(r)
		id, err := uuid.Parse(vars["id"])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid transaction id"}`))
			return
		}

		t, err := txRepo.FindByID(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Enrich with User/Wallet info
		var sName, sEmail, rName, rEmail, sWalletNum, rWalletNum *string
		var sType, rType domain.UserType

		sender, _ := userRepo.FindByID(r.Context(), t.SenderID)
		if sender != nil {
			name := sender.FirstName + " " + sender.LastName
			sName = &name
			email := sender.Email
			sEmail = &email
			sType = sender.UserType
		}

		receiver, _ := userRepo.FindByID(r.Context(), t.ReceiverID)
		if receiver != nil {
			name := receiver.FirstName + " " + receiver.LastName
			rName = &name
			email := receiver.Email
			rEmail = &email
			rType = receiver.UserType
		}

		var sWallet *domain.Wallet
		if t.SenderWalletID != nil {
			sWallet, _ = walletRepo.FindByID(r.Context(), *t.SenderWalletID)
		}
		if sWallet != nil {
			sWalletNum = sWallet.WalletAddress
		}

		var rWallet *domain.Wallet
		if t.ReceiverWalletID != nil {
			rWallet, _ = walletRepo.FindByID(r.Context(), *t.ReceiverWalletID)
		}
		if rWallet != nil {
			rWalletNum = rWallet.WalletAddress
		}

		amt := domain.Money{Amount: t.Amount, Currency: t.Currency}
		fee := domain.Money{Amount: t.FeeAmount, Currency: t.FeeCurrency}
		net := domain.Money{Amount: t.NetAmount, Currency: t.ConvertedCurrency}

		flagged := false
		if val, ok := t.Metadata["flagged"]; ok {
			if b, ok := val.(bool); ok {
				flagged = b
			}
		}

		// Define AdminTx struct locally or reuse?
		// Reusing struct definition is tricky inside func.
		// I'll define a response struct.
		type AdminTx struct {
			ID                   uuid.UUID                `json:"id"`
			TransactionID        *uuid.UUID               `json:"transaction_id,omitempty"`
			SenderID             uuid.UUID                `json:"sender_id"`
			ReceiverID           uuid.UUID                `json:"receiver_id"`
			SenderUserType       domain.UserType          `json:"sender_user_type"`
			ReceiverUserType     domain.UserType          `json:"receiver_user_type"`
			SenderName           *string                  `json:"sender_name,omitempty"`
			SenderEmail          *string                  `json:"sender_email,omitempty"`
			SenderWalletNumber   *string                  `json:"sender_wallet_number,omitempty"`
			ReceiverName         *string                  `json:"receiver_name,omitempty"`
			ReceiverEmail        *string                  `json:"receiver_email,omitempty"`
			ReceiverWalletNumber *string                  `json:"receiver_wallet_number,omitempty"`
			Amount               domain.Money             `json:"amount"`
			Fee                  domain.Money             `json:"fee"`
			NetAmount            domain.Money             `json:"net_amount"`
			Status               domain.TransactionStatus `json:"status"`
			TransactionType      domain.TransactionType   `json:"transaction_type"`
			StatusReason         string                   `json:"status_reason,omitempty"`
			Reference            string                   `json:"reference"`
			CreatedAt            time.Time                `json:"created_at"`
			UpdatedAt            time.Time                `json:"updated_at"`
			Flagged              bool                     `json:"flagged"`
		}

		resp := AdminTx{
			ID:                   t.ID,
			SenderID:             t.SenderID,
			ReceiverID:           t.ReceiverID,
			SenderUserType:       sType,
			ReceiverUserType:     rType,
			SenderName:           sName,
			SenderEmail:          sEmail,
			SenderWalletNumber:   sWalletNum,
			ReceiverName:         rName,
			ReceiverEmail:        rEmail,
			ReceiverWalletNumber: rWalletNum,
			Amount:               amt,
			Fee:                  fee,
			NetAmount:            net,
			Status:               t.Status,
			TransactionType:      t.TransactionType,
			StatusReason:         t.StatusReason,
			Reference:            t.Reference,
			CreatedAt:            t.CreatedAt,
			UpdatedAt:            t.UpdatedAt,
			Flagged:              flagged,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}).Methods("GET")

	admin.HandleFunc("/banking/settlements", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		s, err := settlementRepo.FindAll(r.Context(), limit, offset)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to fetch settlements"}`))
			return
		}
		total, _ := settlementRepo.CountAll(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type Resp struct {
			Settlements []domain.Settlement `json:"settlements"`
			Total       int                 `json:"total"`
		}
		b, _ := json.Marshal(Resp{Settlements: s, Total: total})
		w.Write(b)
	}).Methods("GET")
	admin.HandleFunc("/banking/accounts", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type BankAccount struct {
			ID            string    `json:"id"`
			BankName      string    `json:"bank_name"`
			AccountNumber string    `json:"account_number"`
			AccountHolder string    `json:"account_holder"`
			Currency      string    `json:"currency"`
			Balance       float64   `json:"balance"`
			Status        string    `json:"status"`
			ConnectedAt   time.Time `json:"connected_at"`
		}
		accounts := []BankAccount{
			{
				ID:            "mwk-primary",
				BankName:      "National Bank of Malawi",
				AccountNumber: "000123456789",
				AccountHolder: "KYD Operations",
				Currency:      "MWK",
				Balance:       12500000,
				Status:        "active",
				ConnectedAt:   time.Now().Add(-48 * time.Hour),
			},
			{
				ID:            "cny-settlement",
				BankName:      "Bank of China",
				AccountNumber: "987654321000",
				AccountHolder: "KYD Operations",
				Currency:      "CNY",
				Balance:       3200000,
				Status:        "active",
				ConnectedAt:   time.Now().Add(-72 * time.Hour),
			},
		}
		type Resp struct {
			Accounts []BankAccount `json:"accounts"`
		}
		b, _ := json.Marshal(Resp{Accounts: accounts})
		w.Write(b)
	}).Methods("GET")
	admin.HandleFunc("/banking/gateways", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type PaymentGateway struct {
			ID         string    `json:"id"`
			Name       string    `json:"name"`
			Provider   string    `json:"provider"`
			Status     string    `json:"status"`
			APIKey     string    `json:"api_key"`
			WebhookURL string    `json:"webhook_url"`
			LastSync   time.Time `json:"last_sync"`
		}
		gateways := []PaymentGateway{
			{
				ID:         "mwk-bank",
				Name:       "Malawi Bank Transfer",
				Provider:   "LocalBank",
				Status:     "active",
				APIKey:     "masked",
				WebhookURL: "https://api.localbank.example/webhook",
				LastSync:   time.Now().Add(-30 * time.Minute),
			},
			{
				ID:         "cny-unionpay",
				Name:       "UnionPay",
				Provider:   "UnionPay",
				Status:     "active",
				APIKey:     "masked",
				WebhookURL: "https://api.unionpay.example/webhook",
				LastSync:   time.Now().Add(-2 * time.Hour),
			},
		}
		type Resp struct {
			Gateways []PaymentGateway `json:"gateways"`
		}
		b, _ := json.Marshal(Resp{Gateways: gateways})
		w.Write(b)
	}).Methods("GET")

	// Security Routes
	admin.HandleFunc("/security/events", securityHandler.GetSecurityEvents).Methods("GET")
	admin.HandleFunc("/security/events/{id}", securityHandler.UpdateSecurityEvent).Methods("PATCH")
	admin.HandleFunc("/security/blocklist", securityHandler.GetBlocklist).Methods("GET")
	admin.HandleFunc("/security/blocklist", securityHandler.AddToBlocklist).Methods("POST")
	admin.HandleFunc("/security/blocklist/{id}", securityHandler.RemoveFromBlocklist).Methods("DELETE")
	admin.HandleFunc("/security/health", securityHandler.GetSystemHealth).Methods("GET")

	admin.HandleFunc("/blockchain/wallets", func(w http.ResponseWriter, r *http.Request) {
		ut, _ := middleware.UserTypeFromContext(r.Context())
		if ut != "admin" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admin access required"}`))
			return
		}
		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		type WalletAddr struct {
			ID            uuid.UUID           `json:"id"`
			UserID        uuid.UUID           `json:"user_id"`
			WalletAddress *string             `json:"wallet_address,omitempty"`
			Currency      domain.Currency     `json:"currency"`
			Status        domain.WalletStatus `json:"status"`
			CreatedAt     time.Time           `json:"created_at"`
		}

		var wallets []*domain.Wallet
		var total int
		var err error

		var uidPtr *uuid.UUID
		userIDStr := r.URL.Query().Get("user_id")
		if userIDStr != "" {
			uid, err := uuid.Parse(userIDStr)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid user_id"}`))
				return
			}
			uidPtr = &uid
		}

		wallets, err = walletRepo.FindAllWithFilter(r.Context(), limit, offset, uidPtr)
		if err != nil {
			log.Error("Failed to fetch wallets", map[string]interface{}{"error": err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		total, err = walletRepo.CountWithFilter(r.Context(), uidPtr)
		if err != nil {
			log.Error("Failed to count wallets", map[string]interface{}{"error": err.Error()})
		}

		addresses := make([]WalletAddr, len(wallets))
		for i, w := range wallets {
			addresses[i] = WalletAddr{
				ID:            w.ID,
				UserID:        w.UserID,
				WalletAddress: w.WalletAddress,
				Currency:      w.Currency,
				Status:        w.Status,
				CreatedAt:     w.CreatedAt,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type Resp struct {
			Addresses []WalletAddr `json:"addresses"`
			Total     int          `json:"total"`
		}
		b, _ := json.Marshal(Resp{Addresses: addresses, Total: total})
		w.Write(b)
	}).Methods("GET")

	payments := api.PathPrefix("/payments").Subrouter()
	// payments.Use(idemMW.Require) - Removed redundant middleware (already on api)
	payments.HandleFunc("/initiate", paymentHandler.InitiatePayment).Methods("POST")
	// payments.HandleFunc("/receiver-info", paymentHandler.GetReceiverInfo).Methods("GET") // Removed, use /wallets/lookup or /wallets/search
	payments.HandleFunc("/{id}/receipt", paymentHandler.GetReceipt).Methods("GET")
	payments.HandleFunc("", paymentHandler.GetTransactions).Methods("GET") // Changed GetUserTransactions to GetTransactions
	// payments.HandleFunc("/{id}", paymentHandler.GetTransaction).Methods("GET") // Commented out until implemented
	// payments.HandleFunc("/{id}/cancel", paymentHandler.CancelPayment).Methods("POST") // Commented out until implemented
	// payments.HandleFunc("/bulk", paymentHandler.BulkPayment).Methods("POST") // Commented out until implemented

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
