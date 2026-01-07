// ==============================================================================
// API GATEWAY - cmd/gateway/main.go
// ==============================================================================
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kyd/pkg/config"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type Gateway struct {
	authProxy       *httputil.ReverseProxy
	paymentProxy    *httputil.ReverseProxy
	walletProxy     *httputil.ReverseProxy
	forexProxy      *httputil.ReverseProxy
	settlementProxy *httputil.ReverseProxy
	logger          logger.Logger
}

func NewGateway(log logger.Logger) *Gateway {
	return &Gateway{
		authProxy:       createReverseProxy("http://127.0.0.1:3000"),
		paymentProxy:    createReverseProxy("http://127.0.0.1:3001"),
		walletProxy:     createReverseProxy("http://127.0.0.1:3003"),
		forexProxy:      createReverseProxy("http://127.0.0.1:3002"),
		settlementProxy: createReverseProxy("http://127.0.0.1:3004"),
		logger:          log,
	}
}

func createReverseProxy(target string) *httputil.ReverseProxy {
	url, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(url)

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
			if req.Header.Get("Idempotency-Key") == "" {
				req.Header.Set("Idempotency-Key", uuid.NewString())
			}
		}
	}

	// Handle proxy errors by adding CORS headers
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		origin := r.Header.Get("Origin")
		allowedOrigins := []string{
			"http://localhost:3012",
			"http://localhost:3016",
			"http://127.0.0.1:3012",
			"http://127.0.0.1:3016",
		}

		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if origin == allowedOrigin {
				allowed = true
				break
			}
		}

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

		allowedOrigins := []string{
			"http://localhost:3012",
			"http://localhost:3016",
			"http://127.0.0.1:3012",
			"http://127.0.0.1:3016",
		}

		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if origin == allowedOrigin {
				allowed = true
				break
			}
		}

		// Set CORS headers
		if allowed {
			resp.Header.Set("Access-Control-Allow-Origin", origin)
			resp.Header.Set("Access-Control-Allow-Credentials", "true")
		} else if origin != "" {
			resp.Header.Set("Access-Control-Allow-Origin", "*")
		}

		resp.Header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		resp.Header.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-Request-ID")
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
	// Handle OPTIONS preflight requests
	if r.Method == "OPTIONS" {
		g.handleCORS(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Log request
	g.logger.Info("Gateway request", map[string]interface{}{
		"method": r.Method,
		"path":   r.URL.Path,
		"ip":     r.RemoteAddr,
	})

	// Add common headers
	w.Header().Set("X-Gateway-Version", "1.0.0")
	w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))

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
}

func (g *Gateway) handleCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	allowedOrigins := []string{
		"http://localhost:3012",
		"http://localhost:3016",
		"http://127.0.0.1:3012",
		"http://127.0.0.1:3016",
	}

	allowed := false
	for _, allowedOrigin := range allowedOrigins {
		if origin == allowedOrigin {
			allowed = true
			break
		}
	}

	if allowed {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	} else if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-Request-ID")
	w.Header().Set("Access-Control-Max-Age", "3600")
}

func matchPath(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}

func main() {
	cfg := config.Load()
	log := logger.New("api-gateway")

	log.Info("Starting API Gateway", map[string]interface{}{
		"port": cfg.Server.Port,
	})

	gateway := NewGateway(log)

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
