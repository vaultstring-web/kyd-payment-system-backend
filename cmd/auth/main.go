// ==============================================================================
// SERVICE MAIN ENTRY POINTS - cmd/
// ==============================================================================

// AUTH SERVICE - cmd/auth/main.go
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

	"kyd/internal/auth"
	"kyd/internal/handler"
	"kyd/internal/middleware"
	"kyd/internal/repository/postgres"
	"kyd/pkg/config"
	"kyd/pkg/logger"
	"kyd/pkg/mailer"
	"kyd/pkg/validator"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Validate required configuration
	// Initialize logger
	log := logger.New("auth-service")

	if err := cfg.ValidateCore(); err != nil {
		log.Fatal("Invalid configuration", map[string]interface{}{"error": err.Error()})
	}

	// Connect to database
	db, err := sqlx.Connect("postgres", cfg.Database.URL)
	if err != nil {
		log.Fatal("Failed to connect to database", map[string]interface{}{"error": err.Error()})
	}
	defer db.Close()

	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.URL,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis", map[string]interface{}{"error": err.Error()})
	}

	// Initialize repositories
	userRepo := postgres.NewUserRepository(db)

	// Initialize services
	authService := auth.NewService(userRepo, cfg.JWT.Secret, cfg.JWT.Expiration)
	// Configure email verification
	m := mailer.New(mailer.Config{
		Host:     cfg.Email.SMTPHost,
		Port:     cfg.Email.SMTPPort,
		Username: cfg.Email.SMTPUsername,
		Password: cfg.Email.SMTPPassword,
		From:     cfg.Email.SMTPFrom,
		UseTLS:   cfg.Email.SMTPUseTLS,
	})
	authService = authService.WithEmailVerification(m, cfg.Verification.BaseURL, cfg.Verification.TokenExpiration)

	// Initialize handlers
	val := validator.New()
	authHandler := handler.NewAuthHandler(authService, val, log)

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).Limit)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.BodyLimit(1 << 20))

	// Routes
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/api/v1/auth/register", authHandler.Register).Methods("POST")
	r.HandleFunc("/api/v1/auth/login", authHandler.Login).Methods("POST")
	r.HandleFunc("/api/v1/auth/send-verification", authHandler.SendVerification).Methods("POST")
	r.HandleFunc("/api/v1/auth/verify", authHandler.VerifyEmail).Methods("GET")

	// Protected routes
	authMW := middleware.NewAuthMiddleware(cfg.JWT.Secret)
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(authMW.Authenticate)
	api.HandleFunc("/auth/me", authHandler.Me).Methods("GET")

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
		log.Info("Auth service starting", map[string]interface{}{"port": cfg.Server.Port})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed", map[string]interface{}{"error": err.Error()})
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown", map[string]interface{}{"error": err.Error()})
	}

	log.Info("Server stopped", nil)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","service":"auth"}`))
}
