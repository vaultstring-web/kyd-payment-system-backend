// ==============================================================================
// COMPLETE PAYMENT SERVICE MAIN - cmd/payment/main.go
// ==============================================================================
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"kyd/internal/domain"
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
	userRepo := postgres.NewUserRepository(db)
	settlementRepo := postgres.NewSettlementRepository(db)

	// Initialize services
	ledgerService := ledger.NewService(db.DB)

	// Initialize forex providers
	forexProviders := []forex.RateProvider{
		forex.NewExchangeRateAPIProvider(),
		forex.NewMockRateProvider(),
	}

	// Wrap redis client with RateCache adapter
	rateCache := forex.NewRedisRateCache(redisClient)
	forexService := forex.NewService(forexRepo, rateCache, forexProviders, log)

	paymentService := payment.NewService(txRepo, walletRepo, forexService, ledgerService, userRepo, log)

	// Initialize handlers
	val := validator.New()
	paymentHandler := handler.NewPaymentHandler(paymentService, val, log)

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.BodyLimit(1 << 20)) // 1MB global cap
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

	// Admin routes
	admin := api.PathPrefix("/admin").Subrouter()
	admin.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).Limit)
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
		users, err := userRepo.FindAll(r.Context(), limit, offset)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"failed to fetch users"}`))
			return
		}
		total, _ := userRepo.CountAll(r.Context())
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
			ID               uuid.UUID                `json:"id"`
			TransactionID    *uuid.UUID               `json:"transaction_id,omitempty"`
			SenderID         uuid.UUID                `json:"sender_id"`
			ReceiverID       uuid.UUID                `json:"receiver_id"`
			SenderUserType   domain.UserType          `json:"sender_user_type"`
			ReceiverUserType domain.UserType          `json:"receiver_user_type"`
			SenderName       *string                  `json:"sender_name,omitempty"`
			SenderEmail      *string                  `json:"sender_email,omitempty"`
			ReceiverName     *string                  `json:"receiver_name,omitempty"`
			ReceiverEmail    *string                  `json:"receiver_email,omitempty"`
			Amount           domain.Money             `json:"amount"`
			Status           domain.TransactionStatus `json:"status"`
			TransactionType  domain.TransactionType   `json:"transaction_type"`
			StatusReason     *string                  `json:"status_reason,omitempty"`
			Reference        string                   `json:"reference"`
			CreatedAt        time.Time                `json:"created_at"`
			UpdatedAt        time.Time                `json:"updated_at"`
			Flagged          bool                     `json:"flagged"`
		}
		ids := make([]uuid.UUID, 0, len(txs)*2)
		idSeen := make(map[uuid.UUID]struct{})
		for _, t := range txs {
			if _, ok := idSeen[t.SenderID]; !ok {
				ids = append(ids, t.SenderID)
				idSeen[t.SenderID] = struct{}{}
			}
			if _, ok := idSeen[t.ReceiverID]; !ok {
				ids = append(ids, t.ReceiverID)
				idSeen[t.ReceiverID] = struct{}{}
			}
		}
		users, _ := userRepo.FindByIDs(r.Context(), ids)
		userMap := make(map[uuid.UUID]*domain.User, len(users))
		for _, u := range users {
			userMap[u.ID] = u
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
			amt := domain.Money{Amount: t.Amount, Currency: t.Currency}
			out = append(out, AdminTx{
				ID:               t.ID,
				SenderID:         t.SenderID,
				ReceiverID:       t.ReceiverID,
				SenderUserType:   sType,
				ReceiverUserType: rType,
				SenderName:       sName,
				SenderEmail:      sEmail,
				ReceiverName:     rName,
				ReceiverEmail:    rEmail,
				Amount:           amt,
				Status:           t.Status,
				TransactionType:  t.TransactionType,
				StatusReason:     t.StatusReason,
				Reference:        t.Reference,
				CreatedAt:        t.CreatedAt,
				UpdatedAt:        t.UpdatedAt,
				Flagged:          t.Status == domain.TransactionStatusFailed,
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
		all := make([]WalletAddr, 0, limit)
		users, _ := userRepo.FindAll(r.Context(), 1000, 0)
		for _, u := range users {
			ws, _ := walletRepo.FindByUserID(r.Context(), u.ID)
			for _, w := range ws {
				all = append(all, WalletAddr{
					ID:            w.ID,
					UserID:        w.UserID,
					WalletAddress: w.WalletAddress,
					Currency:      w.Currency,
					Status:        w.Status,
					CreatedAt:     w.CreatedAt,
				})
			}
		}
		if offset > len(all) {
			offset = len(all)
		}
		end := offset + limit
		if end > len(all) {
			end = len(all)
		}
		page := all[offset:end]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		type Resp struct {
			Addresses []WalletAddr `json:"addresses"`
			Total     int          `json:"total"`
		}
		b, _ := json.Marshal(Resp{Addresses: page, Total: len(all)})
		w.Write(b)
	}).Methods("GET")

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
