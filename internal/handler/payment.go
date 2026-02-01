package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"kyd/internal/middleware"
	"kyd/internal/payment"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type PaymentHandler struct {
	service   *payment.Service
	validator *validator.Validator
	logger    Logger
}

func NewPaymentHandler(service *payment.Service, val *validator.Validator, log Logger) *PaymentHandler {
	return &PaymentHandler{service: service, validator: val, logger: log}
}

// InitiatePayment handles payment initiation requests.
func (h *PaymentHandler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	req, userID, err := h.decodeInitiatePaymentRequest(w, r)
	if err != nil {
		return
	}

	req.SenderID = userID

	// Validate struct
	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
		return
	}

	resp, err := h.service.InitiatePayment(r.Context(), &req)
	if err != nil {
		h.logger.Error("Payment initiation failed", map[string]interface{}{"error": err.Error(), "sender_id": userID})
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, resp)
}

// GetTransactions returns paginated transactions for the authenticated user.
func (h *PaymentHandler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
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

	txs, total, err := h.service.GetUserTransactions(r.Context(), userID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch user transactions", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch transactions")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"total":        total,
		"limit":        limit,
		"offset":       offset,
	})
}

// GetAllTransactions returns all transactions (for admin).
func (h *PaymentHandler) GetAllTransactions(w http.ResponseWriter, r *http.Request) {
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

	txs, total, err := h.service.GetAllTransactions(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch all transactions", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch transactions")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"total":        total,
		"limit":        limit,
		"offset":       offset,
	})
}

// GetPendingTransactions returns pending transactions (for admin).
func (h *PaymentHandler) GetPendingTransactions(w http.ResponseWriter, r *http.Request) {
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

	txs, total, err := h.service.GetPendingTransactions(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch pending transactions", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch pending transactions")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"total":        total,
		"limit":        limit,
		"offset":       offset,
	})
}

// ReviewTransaction handles admin approval/rejection of transactions.
func (h *PaymentHandler) ReviewTransaction(w http.ResponseWriter, r *http.Request) {
	adminID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	vars := mux.Vars(r)
	txID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	var req struct {
		Action string `json:"action" validate:"required,oneof=approve reject"`
		Reason string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.service.ReviewTransaction(r.Context(), txID, adminID, req.Action, req.Reason); err != nil {
		h.logger.Error("Failed to review transaction", map[string]interface{}{"error": err.Error(), "tx_id": txID})
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Transaction reviewed successfully"})
}

// GetReceipt returns a transaction receipt.
func (h *PaymentHandler) GetReceipt(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	vars := mux.Vars(r)
	txID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	receipt, err := h.service.GetReceipt(r.Context(), txID, userID)
	if err != nil {
		h.logger.Error("Failed to fetch receipt", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusNotFound, "Receipt not found")
		return
	}

	h.respondJSON(w, http.StatusOK, receipt)
}

// InitiateDispute allows a user to dispute a transaction.
func (h *PaymentHandler) InitiateDispute(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		TransactionID uuid.UUID `json:"transaction_id" validate:"required"`
		Reason        string    `json:"reason" validate:"required"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	disputeReq := payment.InitiateDisputeRequest{
		TransactionID: req.TransactionID,
		Reason:        payment.DisputeReason(req.Reason),
		Description:   req.Reason, // Use reason as description for now if not separate
		InitiatedBy:   userID,
	}

	if err := h.service.InitiateDispute(r.Context(), disputeReq); err != nil {
		h.logger.Error("Failed to initiate dispute", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Dispute initiated successfully"})
}

// GetDisputes returns all disputes (for admin).
func (h *PaymentHandler) GetDisputes(w http.ResponseWriter, r *http.Request) {
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

	disputes, total, err := h.service.GetDisputes(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch disputes", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch disputes")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"disputes": disputes,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// ResolveDispute handles admin resolution of disputes.
func (h *PaymentHandler) ResolveDispute(w http.ResponseWriter, r *http.Request) {
	adminID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		DisputeID uuid.UUID `json:"dispute_id" validate:"required"`
		Action    string    `json:"action" validate:"required,oneof=uphold reject"`
		Reason    string    `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var resolution string
	if req.Action == "uphold" {
		resolution = "reverse"
	} else {
		resolution = "dismiss"
	}

	resolveReq := payment.ResolveDisputeRequest{
		TransactionID: req.DisputeID, // DisputeID in request is actually TransactionID
		Resolution:    resolution,
		AdminID:       adminID,
		Notes:         req.Reason,
	}

	if err := h.service.ResolveDispute(r.Context(), resolveReq); err != nil {
		h.logger.Error("Failed to resolve dispute", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Dispute resolved successfully"})
}

// GetSystemStats returns system statistics (for admin).
func (h *PaymentHandler) GetSystemStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.GetSystemStats(r.Context())
	if err != nil {
		h.logger.Error("Failed to fetch system stats", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch stats")
		return
	}

	h.respondJSON(w, http.StatusOK, stats)
}

// GetRiskAlerts returns risk alerts (for admin).
func (h *PaymentHandler) GetRiskAlerts(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	alerts, err := h.service.GetRiskAlerts(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch risk alerts", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch alerts")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"alerts": alerts,
		"limit":  limit,
		"offset": offset,
	})
}

// GetAuditLogs returns audit logs (for admin).
func (h *PaymentHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	logs, total, err := h.service.GetAuditLogs(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch audit logs", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch audit logs")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// Helper methods

func (h *PaymentHandler) decodeInitiatePaymentRequest(w http.ResponseWriter, r *http.Request) (payment.InitiatePaymentRequest, uuid.UUID, error) {
	var req payment.InitiatePaymentRequest

	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return req, uuid.Nil, err
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return req, uuid.Nil, strconv.ErrSyntax // Just a dummy error
	}

	return req, userID, nil
}

func (h *PaymentHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *PaymentHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}

func (h *PaymentHandler) respondValidationErrors(w http.ResponseWriter, errors map[string]string) {
	h.respondJSON(w, http.StatusBadRequest, map[string]interface{}{
		"error":             "Validation failed",
		"validation_errors": errors,
	})
}
