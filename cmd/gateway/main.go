// ==============================================================================
// API GATEWAY - cmd/gateway/main.go
// ==============================================================================
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"kyd/internal/middleware"
	"kyd/pkg/config"
	"kyd/pkg/logger"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
)

type Gateway struct {
	authProxy       *httputil.ReverseProxy
	paymentProxy    *httputil.ReverseProxy
	walletProxy     *httputil.ReverseProxy
	forexProxy      *httputil.ReverseProxy
	settlementProxy *httputil.ReverseProxy
	logger          logger.Logger
	redisClient     *redis.Client
	rateLimiter     *middleware.RateLimiter
	jwtSecret       string
	signingSecret   string
	requireSigning  bool
	signatureTTL    time.Duration
}

func isAdminToken(tokenStr string, secret string) bool {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false
	}
	ut, _ := claims["user_type"].(string)
	return ut == "admin"
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func NewGateway(log logger.Logger, redisClient *redis.Client, cfg *config.Config) *Gateway {
	rl := middleware.NewRateLimiter(redisClient, 100, time.Minute).WithAdaptive(10, 30*time.Minute)

	var tlsConfig *tls.Config
	if cfg.Server.UseTLS {
		// Load Client Cert for mTLS
		cert, err := os.ReadFile(cfg.Server.CertFile)
		if err != nil {
			log.Fatal("Failed to read client cert", map[string]interface{}{"error": err.Error()})
		}
		key, err := os.ReadFile(cfg.Server.KeyFile)
		if err != nil {
			log.Fatal("Failed to read client key", map[string]interface{}{"error": err.Error()})
		}

		keyPair, err := tls.X509KeyPair(cert, key)
		if err != nil {
			log.Fatal("Failed to load client keypair", map[string]interface{}{"error": err.Error()})
		}

		// Load CA to verify backend server
		caCert, err := os.ReadFile(cfg.Server.CAFile)
		if err != nil {
			log.Fatal("Failed to read CA file", map[string]interface{}{"error": err.Error()})
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{keyPair},
			RootCAs:      caCertPool,
			MinVersion:   tls.VersionTLS12,
		}
	}

	return &Gateway{
		authProxy:       createReverseProxy(getEnv("AUTH_SERVICE_URL", "http://127.0.0.1:3000"), tlsConfig),
		paymentProxy:    createReverseProxy(getEnv("PAYMENT_SERVICE_URL", "http://127.0.0.1:3001"), tlsConfig),
		walletProxy:     createReverseProxy(getEnv("WALLET_SERVICE_URL", "http://127.0.0.1:3003"), tlsConfig),
		forexProxy:      createReverseProxy(getEnv("FOREX_SERVICE_URL", "http://127.0.0.1:3002"), tlsConfig),
		settlementProxy: createReverseProxy(getEnv("SETTLEMENT_SERVICE_URL", "http://127.0.0.1:3004"), tlsConfig),
		logger:          log,
		redisClient:     redisClient,
		rateLimiter:     rl,
		jwtSecret:       cfg.JWT.Secret,
		signingSecret:   cfg.Security.SigningSecret,
		requireSigning:  cfg.Security.RequireSigning,
		signatureTTL:    cfg.Security.SignatureTTL,
	}
}

func createReverseProxy(target string, tlsConfig *tls.Config) *httputil.ReverseProxy {
	url, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(url)

	if tlsConfig != nil {
		proxy.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}

	// Store original Director to capture request
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Store origin header in a custom header that gets forwarded to backend
		// This allows ModifyResponse to access it even if backend modifies headers
		if origin := req.Header.Get("Origin"); origin != "" {
			req.Header.Set("X-Gateway-Origin", origin)
		}
		// Inject Idempotency-Key for unsafe methods if missing
		if req.Method == http.MethodPost || req.Method == http.MethodPut || req.Method == http.MethodPatch || req.Method == http.MethodDelete {
			existingKey := req.Header.Get("Idempotency-Key")
			if existingKey != "" {
				fmt.Printf("[DEBUG-GATEWAY] Forwarding Existing Idempotency-Key: %s for %s %s\n", existingKey, req.Method, req.URL.Path)
			} else {
				newKey := uuid.NewString()
				req.Header.Set("Idempotency-Key", newKey)
				fmt.Printf("[DEBUG-GATEWAY] Injected NEW Idempotency-Key: %s for %s %s\n", newKey, req.Method, req.URL.Path)
			}
		}
	}

	// Handle proxy errors by adding CORS headers
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		origin := r.Header.Get("Origin")
		allowedOrigins := getAllowedOrigins()

		allowed := isOriginAllowed(origin, allowedOrigins)

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.WriteHeader(http.StatusBadGateway)
		// Return a JSON error to be more API friendly
		w.Write([]byte(fmt.Sprintf(`{"error": "Bad Gateway", "message": "%v"}`, err)))
	}

	// Modify response to handle CORS properly
	originalModifyResponse := proxy.ModifyResponse
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Remove CORS headers that backend services might set
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")
		resp.Header.Del("Access-Control-Allow-Credentials")
		resp.Header.Del("Access-Control-Max-Age")

		// Get origin from request (stored in custom header by Director)
		origin := resp.Request.Header.Get("X-Gateway-Origin")
		if origin == "" {
			origin = resp.Request.Header.Get("Origin")
		}

		allowedOrigins := getAllowedOrigins()

		allowed := isOriginAllowed(origin, allowedOrigins)

		// Set CORS headers
		if allowed {
			resp.Header.Set("Access-Control-Allow-Origin", origin)
			resp.Header.Set("Access-Control-Allow-Credentials", "true")
		} else if origin != "" {
			resp.Header.Set("Access-Control-Allow-Origin", "*")
		}

		resp.Header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		resp.Header.Set("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization, Idempotency-Key, X-Request-ID, X-CSRF-Token, X-Signature, X-Signature-Timestamp")
		resp.Header.Set("Access-Control-Max-Age", "3600")

		// Call original ModifyResponse if it exists
		if originalModifyResponse != nil {
			return originalModifyResponse(resp)
		}
		return nil
	}

	return proxy
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Security headers
	secureHeaders(w)
	// Apply CORS headers early so browser sees them even on error paths
	applyCORSHeaders(w, r)

	// Issue CSRF cookie for safe methods
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if c, err := r.Cookie("csrf_token"); err != nil || c.Value == "" {
			token := uuid.NewString()
			http.SetCookie(w, &http.Cookie{
				Name:     "csrf_token",
				Value:    token,
				Path:     "/",
				HttpOnly: false, // double-submit cookie
				SameSite: http.SameSiteLaxMode,
				Secure:   false,
				MaxAge:   3600,
			})
		}
	}

	// Ensure request ID
	if r.Header.Get("X-Request-ID") == "" {
		rid := uuid.NewString()
		r.Header.Set("X-Request-ID", rid)
		w.Header().Set("X-Request-ID", rid)
	}

	// Enforce JSON on mutating methods
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		ct := r.Header.Get("Content-Type")
		if ct == "" || (ct != "application/json" && ct != "application/json; charset=utf-8") {
			applyCORSHeaders(w, r)
			w.WriteHeader(http.StatusUnsupportedMediaType)
			w.Write([]byte(`{"error":"unsupported_media_type","message":"Content-Type must be application/json"}`))
			return
		}
		// CSRF check (double-submit cookie), exempt login/register
		path := r.URL.Path
		if !(matchPath(path, "/api/v1/auth/login") || matchPath(path, "/api/v1/auth/register") || matchPath(path, "/api/v1/forex")) {
			csrfCookie, _ := r.Cookie("csrf_token")
			csrfHeader := r.Header.Get("X-CSRF-Token")
			if csrfCookie == nil || csrfCookie.Value == "" || csrfHeader == "" || csrfCookie.Value != csrfHeader {
				applyCORSHeaders(w, r)
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"csrf_failed","message":"Invalid CSRF token"}`))
				return
			}
		}
	}

	// Limit body size to 1MB for safety
	const maxBody = int64(1 << 20) // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)

	if g.requireSigning && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		if requiresGatewaySigning(r.URL.Path) {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				applyCORSHeaders(w, r)
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid_body","message":"Unable to read request body"}`))
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			if !g.verifySignature(r, bodyBytes) {
				applyCORSHeaders(w, r)
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"invalid_signature","message":"Invalid or missing request signature"}`))
				return
			}
		}
	}

	// Define downstream handler with routing logic
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle OPTIONS preflight requests
		if r.Method == "OPTIONS" {
			g.handleCORS(w, r)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Log request basic info
		g.logger.Info("Gateway request", map[string]interface{}{
			"method": r.Method,
			"path":   r.URL.Path,
			"ip":     r.RemoteAddr,
			"rid":    r.Header.Get("X-Request-ID"),
		})

		// Add common headers
		w.Header().Set("X-Gateway-Version", "1.0.0")
		w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))

		// If Authorization missing, inject from secure cookie access_token
		if r.Header.Get("Authorization") == "" {
			if c, err := r.Cookie("access_token"); err == nil && c.Value != "" {
				r.Header.Set("Authorization", "Bearer "+c.Value)
			}
		}

		// RBAC: admin routes must have user_type=admin in JWT
		if matchPath(r.URL.Path, "/api/v1/admin") {
			authz := r.Header.Get("Authorization")
			if authz == "" {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			// parse "Bearer xxx"
			// simple split
			tokenStr := authz
			if len(tokenStr) > 7 && tokenStr[:7] == "Bearer " {
				tokenStr = tokenStr[7:]
			}
			// Validate token and claims
			if !isAdminToken(tokenStr, g.jwtSecret) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		// Route to appropriate service (CORS handled in ModifyResponse)
		switch {
		case matchPath(r.URL.Path, "/api/v1/auth"):
			g.authProxy.ServeHTTP(w, r)
		case matchPath(r.URL.Path, "/api/v1/admin"):
			// Admin endpoints are handled by payment service
			g.paymentProxy.ServeHTTP(w, r)
		case matchPath(r.URL.Path, "/api/v1/payments"):
			g.paymentProxy.ServeHTTP(w, r)
		case matchPath(r.URL.Path, "/api/v1/wallets"):
			g.walletProxy.ServeHTTP(w, r)
		case matchPath(r.URL.Path, "/api/v1/forex"):
			g.forexProxy.ServeHTTP(w, r)
		case matchPath(r.URL.Path, "/api/v1/settlements"):
			g.settlementProxy.ServeHTTP(w, r)
		default:
			http.Error(w, "Service not found", http.StatusNotFound)
			return
		}
	})

	// Execute Rate Limiter
	g.rateLimiter.Limit(next).ServeHTTP(w, r)
}
func requiresGatewaySigning(path string) bool {
	if matchPath(path, "/api/v1/payments") {
		return true
	}
	if matchPath(path, "/api/v1/wallets") {
		return true
	}
	return false
}

func (g *Gateway) verifySignature(r *http.Request, body []byte) bool {
	if g.signingSecret == "" {
		return false
	}
	sig := r.Header.Get("X-Signature")
	ts := r.Header.Get("X-Signature-Timestamp")
	if sig == "" || ts == "" {
		return false
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	ttl := int64(g.signatureTTL.Seconds())
	if ttl <= 0 {
		ttl = int64((5 * time.Minute).Seconds())
	}
	if now-tsInt > ttl || tsInt-now > ttl {
		return false
	}
	mac := hmac.New(sha256.New, []byte(g.signingSecret))
	mac.Write([]byte(r.Method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(r.URL.Path))
	mac.Write([]byte("\n"))
	mac.Write([]byte(ts))
	mac.Write([]byte("\n"))
	mac.Write(body)
	expected := mac.Sum(nil)
	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, sigBytes)
}

func (g *Gateway) handleCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	allowedOrigins := getAllowedOrigins()

	allowed := isOriginAllowed(origin, allowedOrigins)

	if allowed {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	} else if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization, Idempotency-Key, X-Request-ID, X-CSRF-Token, X-Signature, X-Signature-Timestamp")
	w.Header().Set("Access-Control-Max-Age", "3600")
}

func secureHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
	w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=()")
	w.Header().Set("X-DNS-Prefetch-Control", "off")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
}

func matchPath(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}

func applyCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	allowedOrigins := getAllowedOrigins()
	if isOriginAllowed(origin, allowedOrigins) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	} else if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization, Idempotency-Key, X-Request-ID, X-CSRF-Token, X-Signature, X-Signature-Timestamp")
	w.Header().Set("Access-Control-Max-Age", "3600")
}
func getAllowedOrigins() []string {
	if v, ok := os.LookupEnv("ALLOWED_ORIGINS"); ok && strings.TrimSpace(v) != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			t := strings.TrimSpace(p)
			if t != "" {
				out = append(out, t)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{
		"http://localhost:3012",
		"http://localhost:3016",
		"http://127.0.0.1:3012",
		"http://127.0.0.1:3016",
	}
}

func isOriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return false
	}
	for _, o := range allowed {
		if origin == o {
			return true
		}
	}
	// In local env, be permissive: allow any http/https origin
	env := strings.ToLower(os.Getenv("ENV"))
	if env == "" {
		env = "local"
	}
	if env == "local" {
		if strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
			return true
		}
	}
	return false
}

func main() {
	cfg := config.Load()
	log := logger.New("api-gateway")

	log.Info("Starting API Gateway", map[string]interface{}{
		"port": cfg.Server.Port,
	})

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.URL,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Failed to connect to Redis", map[string]interface{}{"error": err.Error()})
	}

	gateway := NewGateway(log, redisClient, cfg)

	r := mux.NewRouter()

	// Health check
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","service":"gateway"}`))
	}).Methods("GET")

	// Route all other requests through gateway
	r.PathPrefix("/").HandlerFunc(gateway.ServeHTTP)

	// Start server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		log.Info("API Gateway started", map[string]interface{}{
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

	log.Info("Shutting down API Gateway...", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("API Gateway forced to shutdown", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("API Gateway stopped gracefully", nil)
}
