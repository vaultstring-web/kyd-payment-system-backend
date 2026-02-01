// ==============================================================================
// SERVICE MAIN ENTRY POINTS - cmd/
// ==============================================================================

// AUTH SERVICE - cmd/auth/main.go
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

	"github.com/gorilla/mux"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"kyd/internal/auth"
	"kyd/internal/handler"
	"kyd/internal/middleware"
	"kyd/internal/repository/postgres"
	"kyd/internal/security"
	"kyd/pkg/config"
	"kyd/pkg/logger"
	"kyd/pkg/mailer"
	"kyd/pkg/validator"
)

func loadEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		// Try looking up one directory if we are in cmd/auth
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

// simple env bool parsing (duplicate to avoid exporting internal helpers)
func envBool(key string, def bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func main() {
	loadEnv()
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

	// Initialize security service
	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatal("Failed to initialize crypto service", map[string]interface{}{"error": err.Error()})
	}

	// Initialize repositories
	userRepo := postgres.NewUserRepository(db, cryptoService)
	auditRepo := postgres.NewAuditRepository(db)

	// Initialize token blacklist
	blacklist := middleware.NewRedisTokenBlacklist(redisClient)

	// Initialize services
	authService := auth.NewService(userRepo, blacklist, cfg.JWT.Secret, cfg.JWT.Expiration)
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
	// Cookie secure defaults to true; can be disabled in local/dev via COOKIE_SECURE=false or ENV=local
	cookieSecure := envBool("COOKIE_SECURE", strings.ToLower(os.Getenv("ENV")) != "local")
	authHandler := handler.NewAuthHandler(authService, val, log, cfg.TOTP.Issuer, cfg.TOTP.Period, cfg.TOTP.Digits, cookieSecure)
	usersHandler := handler.NewUsersHandler(authService, val, log)

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	r.Use(middleware.NewRateLimiter(redisClient, 60, time.Minute).WithAdaptive(5, 15*time.Minute).Limit)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.BodyLimit(1 << 20))

	// Routes
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/api/v1/auth/register", authHandler.Register).Methods("POST")
	r.HandleFunc("/api/v1/auth/login", authHandler.Login).Methods("POST")
	r.HandleFunc("/api/v1/auth/logout", authHandler.Logout).Methods("POST")
	r.HandleFunc("/api/v1/auth/send-verification", authHandler.SendVerification).Methods("POST")
	r.HandleFunc("/api/v1/auth/verify", authHandler.VerifyEmail).Methods("GET")
	r.HandleFunc("/api/v1/auth/debug", authHandler.DebugUser).Methods("GET")

	// Protected routes
	authMW := middleware.NewAuthMiddleware(cfg.JWT.Secret, blacklist)
	auditMW := middleware.NewAuditMiddleware(auditRepo, log)

	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(auditMW.Audit)
	api.Use(authMW.Authenticate)
	api.HandleFunc("/auth/me", authHandler.Me).Methods("GET")
	api.HandleFunc("/auth/me", usersHandler.UpdateMe).Methods("PUT")
	api.HandleFunc("/auth/me/password", usersHandler.ChangeMyPassword).Methods("POST")
	api.HandleFunc("/auth/totp/setup", authHandler.SetupTOTP).Methods("POST")
	api.HandleFunc("/auth/totp/verify", authHandler.VerifyTOTP).Methods("POST")
	api.HandleFunc("/auth/totp/status", authHandler.TOTPStatus).Methods("GET")
	// Admin user management
	api.HandleFunc("/auth/users", usersHandler.List).Methods("GET")
	api.HandleFunc("/auth/users/{id}", usersHandler.Get).Methods("GET")
	api.HandleFunc("/auth/users/{id}", usersHandler.Update).Methods("PUT")

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	if cfg.Server.UseTLS {
		// Load CA cert for client auth
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
		log.Info("Auth service starting", map[string]interface{}{
			"port": cfg.Server.Port,
			"tls":  cfg.Server.UseTLS,
		})

		var err error
		if cfg.Server.UseTLS {
			err = srv.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile)
		} else {
			err = srv.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
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
