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
		authProxy:       createReverseProxy("http://localhost:3000"),
		paymentProxy:    createReverseProxy("http://localhost:3001"),
		walletProxy:     createReverseProxy("http://localhost:3003"),
		forexProxy:      createReverseProxy("http://localhost:3002"),
		settlementProxy: createReverseProxy("http://localhost:3004"),
		logger:          log,
	}
}

func createReverseProxy(target string) *httputil.ReverseProxy {
	url, _ := url.Parse(target)
	return httputil.NewSingleHostReverseProxy(url)
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Log request
	g.logger.Info("Gateway request", map[string]interface{}{
		"method": r.Method,
		"path":   r.URL.Path,
		"ip":     r.RemoteAddr,
	})

	// Add common headers
	w.Header().Set("X-Gateway-Version", "1.0.0")
	w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))

	// Route to appropriate service
	switch {
	case matchPath(r.URL.Path, "/api/v1/auth"):
		g.authProxy.ServeHTTP(w, r)
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
	}
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
