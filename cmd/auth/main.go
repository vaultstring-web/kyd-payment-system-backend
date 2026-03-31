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

	"github.com/google/uuid"
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
	return u.IsActive, nil
}

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

	// Initialize security crypto service
	cryptoService, err := security.NewCryptoService()
	if err != nil {
		log.Fatal("Failed to initialize crypto service", map[string]interface{}{"error": err.Error()})
	}

	// Initialize repositories
	userRepo := postgres.NewUserRepository(db, cryptoService)
	auditRepo := postgres.NewAuditRepository(db, cryptoService)
	securityRepo := postgres.NewSecurityRepository(db)

	// Initialize token blacklist
	blacklist := middleware.NewRedisTokenBlacklist(redisClient)

	// Initialize services
	authService := auth.NewService(userRepo, blacklist, cfg.JWT.Secret, cfg.JWT.Expiration).WithAdditionalJWTSecrets(cfg.JWT.OldSecrets)
	securityService := security.NewService(securityRepo)

	// Configure email verification and password reset
	m, err := mailer.New(mailer.Config{
		Host:                 cfg.Email.SMTPHost,
		Port:                 cfg.Email.SMTPPort,
		Username:             cfg.Email.SMTPUsername,
		Password:             cfg.Email.SMTPPassword,
		From:                 cfg.Email.SMTPFrom,
		UseTLS:               cfg.Email.SMTPUseTLS,
		GmailAPIEnabled:      cfg.Email.GmailAPIEnabled,
		GmailCredentialsPath: cfg.Email.GmailCredentialsPath,
		GmailTokenPath:       cfg.Email.GmailTokenPath,
	})
	if err != nil {
		log.Fatal("Failed to initialize mailer", map[string]interface{}{"error": err.Error()})
	}

	authService = authService.WithEmailVerification(m, cfg.Verification.BaseURL, cfg.Verification.TokenExpiration, cfg.Verification.BypassEmailVerification)
	authService = authService.WithPasswordReset(cfg.PasswordReset.BaseURL, cfg.PasswordReset.TokenExpiration)

	// Initialize Google OAuth Service
	if cfg.Google.MockMode || (cfg.Google.ClientID != "" && cfg.Google.ClientSecret != "") {
		googleOAuthConfig := &auth.GoogleOAuthConfig{
			ClientID:     cfg.Google.ClientID,
			ClientSecret: cfg.Google.ClientSecret,
			RedirectURI:  cfg.Google.RedirectURI,
			TokenIssuer:  cfg.Google.TokenIssuer,
			MockMode:     cfg.Google.MockMode,
		}
		googleOAuthService, err := auth.NewGoogleOAuthService(googleOAuthConfig, authService)
		if err != nil {
			log.Error("Failed to initialize Google OAuth Service", map[string]interface{}{"error": err.Error()})
		} else {
			authService = authService.WithGoogleOAuth(googleOAuthService)
		}
	}

	// Initialize handlers
	val := validator.New()
	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	if env == "" {
		env = "local"
	}
	cookieSecure := envBool("COOKIE_SECURE", env != "local")
	authHandler := handler.NewAuthHandler(authService, val, log, auditRepo, securityService, cfg.TOTP.Issuer, cfg.TOTP.Period, cfg.TOTP.Digits, cookieSecure)
	usersHandler := handler.NewUsersHandler(authService, val, log, auditRepo, nil, nil, nil)
	enableRateLimiter := envBool("AUTH_RATE_LIMIT_ENABLED", env != "local")

	// Setup router
	r := mux.NewRouter()

	// Middleware
	r.Use(middleware.CORS)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.NewLoggingMiddleware(log).Log)
	if enableRateLimiter {
		// Secure auth rate limit: max 5 attempts per 15 minutes
		rl := middleware.NewRateLimiter(redisClient, 5, 15*time.Minute).WithAdaptive(3, 30*time.Minute)
		r.Use(rl.Limit)
	}
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Recovery)
	r.Use(middleware.BodyLimit(1 << 20))

	// Routes
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/api/v1/auth/health", healthCheck).Methods("GET")
	r.HandleFunc("/api/v1/auth/register", authHandler.Register).Methods("POST")
	r.HandleFunc("/api/v1/auth/login", authHandler.Login).Methods("POST")
	r.HandleFunc("/api/v1/auth/logout", authHandler.Logout).Methods("POST")
	r.HandleFunc("/api/v1/auth/send-verification", authHandler.SendVerification).Methods("POST")
	r.HandleFunc("/api/v1/auth/verify/resend", authHandler.SendVerification).Methods("POST")
	r.HandleFunc("/api/v1/auth/verify", authHandler.VerifyEmail).Methods("POST", "GET")
	r.HandleFunc("/api/v1/auth/forgot-password", authHandler.ForgotPassword).Methods("POST")
	r.HandleFunc("/api/v1/auth/reset-password", authHandler.ResetPassword).Methods("POST")

	// Google OAuth routes
	r.HandleFunc("/api/v1/auth/google/start", authHandler.GoogleAuthStart).Methods("GET")
	r.HandleFunc("/api/v1/auth/google/callback", authHandler.GoogleAuthCallback).Methods("POST")
	r.HandleFunc("/api/v1/auth/google/mock-login", authHandler.GoogleMockLogin).Methods("GET")

	r.HandleFunc("/api/v1/auth/debug", authHandler.DebugUser).Methods("GET")

	// Protected routes
	authMW := middleware.NewAuthMiddlewareWithUserStatus(cfg.JWT.Secret, blacklist, &userStatusChecker{repo: userRepo, log: log})
	auditMW := middleware.NewAuditMiddleware(auditRepo, log)

	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/auth/health", healthCheck).Methods("GET")
	api.Use(auditMW.Audit)
	api.Use(authMW.Authenticate)
	api.HandleFunc("/auth/me", authHandler.Me).Methods("GET")
	api.HandleFunc("/auth/me", usersHandler.UpdateMe).Methods("PUT")
	api.HandleFunc("/auth/me/password", usersHandler.ChangeMyPassword).Methods("POST")
	api.HandleFunc("/auth/totp/setup", authHandler.SetupTOTP).Methods("POST")
	api.HandleFunc("/auth/totp/verify", authHandler.VerifyTOTP).Methods("POST")
	api.HandleFunc("/auth/totp/disable", authHandler.DisableTOTP).Methods("POST")
	api.HandleFunc("/auth/totp/status", authHandler.TOTPStatus).Methods("GET")
	// Admin user management
	api.HandleFunc("/auth/users", usersHandler.List).Methods("GET")
	api.HandleFunc("/auth/users/{id}", usersHandler.Get).Methods("GET")
	api.HandleFunc("/auth/users/{id}", usersHandler.Update).Methods("PUT")
	api.HandleFunc("/auth/users/{id}/block", usersHandler.BlockUser).Methods("POST")
	api.HandleFunc("/auth/users/{id}/unblock", usersHandler.UnblockUser).Methods("POST")

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
