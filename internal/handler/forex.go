// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"kyd/internal/domain"
	"kyd/internal/forex"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

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

// GetAllRates returns all available FX rates.
func (h *ForexHandler) GetAllRates(w http.ResponseWriter, r *http.Request) {
	_ = r
	// TODO: Implement get all rates
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"rates": []interface{}{},
	})
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

	if err := h.validator.Validate(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.service.Calculate(r.Context(), &req)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, result)
}

// GetHistory returns historical FX data (placeholder).
func (h *ForexHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	from := domain.Currency(vars["from"])
	to := domain.Currency(vars["to"])

	// TODO: Implement history
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"from":    from,
		"to":      to,
		"history": []interface{}{},
	})
}

// WebSocketHandler provides real-time FX rates (not implemented).
func (h *ForexHandler) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	_ = r
	// TODO: Implement WebSocket for real-time rates
	h.respondError(w, http.StatusNotImplemented, "WebSocket not implemented yet")
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
