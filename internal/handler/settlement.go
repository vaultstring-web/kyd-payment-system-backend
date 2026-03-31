package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"kyd/internal/middleware"
	"kyd/internal/settlement"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type SettlementHandler struct {
	service *settlement.Service
	logger  logger.Logger
}

func NewSettlementHandler(service *settlement.Service, log logger.Logger) *SettlementHandler {
	return &SettlementHandler{
		service: service,
		logger:  log,
	}
}

func (h *SettlementHandler) ListSettlements(w http.ResponseWriter, r *http.Request) {
	// Admin check
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
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

	status := r.URL.Query().Get("status")
	currency := r.URL.Query().Get("currency")
	network := r.URL.Query().Get("network")

	settlements, total, err := h.service.ListSettlementsFiltered(r.Context(), limit, offset, status, currency, network)
	if err != nil {
		h.logger.Error("Failed to fetch settlements", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch settlements")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"settlements": settlements,
		"items":       settlements,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

func (h *SettlementHandler) GetSettlement(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
		return
	}

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid settlement id")
		return
	}

	set, err := h.service.GetSettlementByID(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "settlement not found")
		return
	}
	h.respondJSON(w, http.StatusOK, set)
}

func (h *SettlementHandler) RetrySettlement(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
		return
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid settlement id")
		return
	}
	set, err := h.service.RetrySettlement(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "failed to retry settlement")
		return
	}
	h.respondJSON(w, http.StatusOK, map[string]interface{}{"settlement": set})
}

func (h *SettlementHandler) ReconcileSettlement(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
		return
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid settlement id")
		return
	}
	var req struct {
		ReconciliationID *string `json:"reconciliation_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	set, err := h.service.MarkReconciled(r.Context(), id, req.ReconciliationID)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "failed to reconcile settlement")
		return
	}
	h.respondJSON(w, http.StatusOK, map[string]interface{}{"settlement": set})
}

func (h *SettlementHandler) GetBankAccounts(w http.ResponseWriter, r *http.Request) {
	// Admin check
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
		return
	}

	// Mock data as in main.go
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

	h.respondJSON(w, http.StatusOK, map[string]interface{}{"accounts": accounts})
}

func (h *SettlementHandler) GetPaymentGateways(w http.ResponseWriter, r *http.Request) {
	// Admin check
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
		return
	}

	// Mock data as in main.go
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

	h.respondJSON(w, http.StatusOK, map[string]interface{}{"gateways": gateways})
}

func (h *SettlementHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *SettlementHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
