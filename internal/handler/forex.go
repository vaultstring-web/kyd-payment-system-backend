// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kyd/internal/domain"
	"kyd/internal/forex"
	"kyd/pkg/logger"
	"kyd/pkg/validator"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now (CORS)
	},
}

// ForexHandler manages forex endpoints.
type ForexHandler struct {
	service   *forex.Service
	validator *validator.Validator
	logger    logger.Logger
}

// NewForexHandler creates a ForexHandler.
func NewForexHandler(service *forex.Service, val *validator.Validator, log logger.Logger) *ForexHandler {
	return &ForexHandler{
		service:   service,
		validator: val,
		logger:    log,
	}
}

// GetRate returns a specific FX rate.
func (h *ForexHandler) GetRate(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	from := domain.Currency(vars["from"])
	to := domain.Currency(vars["to"])

	rate, err := h.service.GetRate(r.Context(), from, to)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Rate not found")
		return
	}

	h.respondJSON(w, http.StatusOK, rate)
}

// GetRateQuery returns a specific FX rate using query parameters (?from=...&to=...).
func (h *ForexHandler) GetRateQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fromStr := strings.TrimSpace(q.Get("from"))
	toStr := strings.TrimSpace(q.Get("to"))
	if fromStr == "" || toStr == "" {
		h.respondError(w, http.StatusBadRequest, "from and to query parameters are required")
		return
	}
	from := domain.Currency(fromStr)
	to := domain.Currency(toStr)

	rate, err := h.service.GetRate(r.Context(), from, to)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Rate not found")
		return
	}
	h.respondJSON(w, http.StatusOK, rate)
}

// GetAllRates returns all available FX rates.
func (h *ForexHandler) GetAllRates(w http.ResponseWriter, r *http.Request) {
	pairs := []struct {
		from domain.Currency
		to   domain.Currency
	}{
		{domain.MWK, domain.CNY},
		{domain.CNY, domain.MWK},
		{domain.MWK, domain.USD},
		{domain.USD, domain.MWK},
		{domain.CNY, domain.USD},
		{domain.USD, domain.CNY},
	}

	var rates []*domain.ExchangeRate
	for _, p := range pairs {
		rate, err := h.service.GetRate(r.Context(), p.from, p.to)
		if err != nil {
			continue
		}
		rates = append(rates, rate)
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{"rates": rates})
}

// Calculate computes a conversion for a currency pair.
func (h *ForexHandler) Calculate(w http.ResponseWriter, r *http.Request) {
	var req forex.CalculateRequest

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		if err == io.EOF {
			h.respondError(w, http.StatusBadRequest, "Request body is required")
			return
		}
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if valErrs := h.validator.ValidateStructured(&req); valErrs != nil {
		h.respondValidationErrors(w, valErrs)
		return
	}

	result, err := h.service.Calculate(r.Context(), &req)
	if err != nil {
		h.logger.Error("Forex calculate failed", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Calculation failed")
		return
	}

	h.respondJSON(w, http.StatusOK, result)
}

// GetHistory returns historical FX data (placeholder).
func (h *ForexHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	from := domain.Currency(vars["from"])
	to := domain.Currency(vars["to"])

	limit := 30 // Default limit
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	history, err := h.service.GetHistory(r.Context(), from, to, limit)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch history")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"from":    from,
		"to":      to,
		"history": history,
	})
}

// WebSocketHandler provides real-time FX rates.
func (h *ForexHandler) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", map[string]interface{}{"error": err.Error()})
		return
	}
	defer conn.Close()

	h.logger.Info("WebSocket client connected", nil)

	// Send initial rates
	h.sendRates(r.Context(), conn)

	// Send updates every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := h.sendRates(r.Context(), conn); err != nil {
				h.logger.Error("Failed to send rates", map[string]interface{}{"error": err.Error()})
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *ForexHandler) sendRates(ctx context.Context, conn *websocket.Conn) error {
	pairs := []struct {
		from domain.Currency
		to   domain.Currency
	}{
		{domain.MWK, domain.CNY},
		{domain.CNY, domain.MWK},
		{domain.MWK, domain.USD},
		{domain.USD, domain.MWK},
		{domain.CNY, domain.USD},
		{domain.USD, domain.CNY},
	}

	var rates []*domain.ExchangeRate
	for _, p := range pairs {
		rate, err := h.service.GetRate(ctx, p.from, p.to)
		if err != nil {
			continue
		}
		rates = append(rates, rate)
	}

	return conn.WriteJSON(map[string]interface{}{
		"type":      "rates_update",
		"timestamp": time.Now(),
		"rates":     rates,
	})
}

func (h *ForexHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("json encode failed", map[string]interface{}{"error": err.Error()})
		_, _ = w.Write([]byte(`{"error":"response encoding failed"}`))
	}
}

func (h *ForexHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}

func (h *ForexHandler) respondValidationErrors(w http.ResponseWriter, errors map[string]string) {
	h.respondJSON(w, http.StatusBadRequest, map[string]interface{}{
		"error":             "Validation failed",
		"validation_errors": errors,
	})
}
