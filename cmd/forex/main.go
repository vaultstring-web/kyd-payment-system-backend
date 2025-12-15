// ==============================================================================
// FOREX SERVICE MAIN - cmd/forex/main.go
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
	"kyd/internal/middleware"
	"kyd/internal/repository/postgres"
	"kyd/pkg/config"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

func main() {
	cfg := config.Load()
	log := logger.New("forex-service")

	if err := cfg.ValidateCore(); err != nil {
		log.Fatal("Invalid configuration", map[string]interface{}{"error": err.Error()})
	}

	log.Info("Starting Forex Service", map[string]interface{}{
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

	// Initialize repositories
	forexRepo := postgres.NewForexRepository(db)

	// Initialize rate providers (real provider first, mock as fallback)
	providers := []forex.RateProvider{
		forex.NewExchangeRateAPIProvider(),
		forex.NewMockRateProvider(),
	}

	// Initialize services
	// Wrap redis client with the RateCache adapter
	rateCache := forex.NewRedisRateCache(redisClient)
	forexService := forex.NewService(forexRepo, rateCache, providers, log)

	// Initialize handlers
	val := validator.New()
	forexHandler := handler.NewForexHandler(forexService, val, log)

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.BodyLimit(1 << 20)) // 1MB global cap
	r.Use(middleware.NewRateLimiter(redisClient, 100, time.Minute).Limit)

	// Routes
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/ready", readyCheck(db)).Methods("GET")

	// Public routes
	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/forex/rates", forexHandler.GetAllRates).Methods("GET")
	api.HandleFunc("/forex/rate/{from}/{to}", forexHandler.GetRate).Methods("GET")
	api.HandleFunc("/forex/rate", forexHandler.GetRateQuery).Methods("GET")
	api.HandleFunc("/forex/calculate", forexHandler.Calculate).Methods("POST")
	api.HandleFunc("/forex/history/{from}/{to}", forexHandler.GetHistory).Methods("GET")

	// WebSocket for real-time rates
	api.HandleFunc("/forex/ws", forexHandler.WebSocketHandler)

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		log.Info("Forex service started", map[string]interface{}{
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

	log.Info("Shutting down forex service...", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Forex service forced to shutdown", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("Forex service stopped gracefully", nil)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","service":"forex"}`))
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
		w.Write([]byte(`{"status":"ready","service":"forex"}`))
	}
}
