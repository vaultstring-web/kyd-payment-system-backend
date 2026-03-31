package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"kyd/internal/domain"
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

// GetTransaction returns a single transaction by ID (for admin).
func (h *PaymentHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	tx, err := h.service.GetTransaction(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Transaction not found")
		return
	}

	h.respondJSON(w, http.StatusOK, tx)
}

// GetTransactionForUser returns a single transaction by ID for the authenticated user (sender or receiver).
func (h *PaymentHandler) GetTransactionForUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	tx, err := h.service.GetTransaction(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Transaction not found")
		return
	}

	// Only sender or receiver can view
	if tx.SenderID != userID && tx.ReceiverID != userID {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	h.respondJSON(w, http.StatusOK, tx)
}

// CancelPayment cancels a pending transaction (sender only).
func (h *PaymentHandler) CancelPayment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	if err := h.service.CancelTransaction(r.Context(), id, userID); err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "unauthorized"):
			h.respondError(w, http.StatusForbidden, "Unauthorized to cancel this transaction")
			return
		case strings.Contains(msg, "only pending"):
			h.respondError(w, http.StatusBadRequest, "Only pending transactions can be cancelled")
			return
		default:
			h.respondError(w, http.StatusNotFound, "Transaction not found")
			return
		}
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Transaction cancelled"})
}

// BulkPayment handles bulk payment initiation.
func (h *PaymentHandler) BulkPayment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req payment.BulkPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.SenderID = userID

	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
		return
	}

	result, err := h.service.BulkPayment(r.Context(), &req)
	if err != nil {
		h.logger.Error("Bulk payment failed", map[string]interface{}{"error": err.Error(), "sender_id": userID})
		h.respondError(w, http.StatusInternalServerError, "Bulk payment failed")
		return
	}

	h.respondJSON(w, http.StatusOK, result)
}

// FlagTransaction flags a transaction for review.
func (h *PaymentHandler) FlagTransaction(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	var req struct {
		Reason string `json:"reason" validate:"required"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.service.FlagTransaction(r.Context(), id, req.Reason); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to flag transaction")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Transaction flagged"})
}

// ReverseTransaction reverses a transaction (admin-only).
func (h *PaymentHandler) ReverseTransaction(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}
	adminID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.service.ReverseTransactionAdmin(r.Context(), id, adminID, req.Reason); err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "not eligible"):
			h.respondError(w, http.StatusBadRequest, "Transaction is not eligible for reversal")
		case strings.Contains(msg, "not found"):
			h.respondError(w, http.StatusNotFound, "Transaction not found")
		default:
			h.respondError(w, http.StatusInternalServerError, "Failed to reverse transaction")
		}
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Transaction reversed"})
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

	var walletID *uuid.UUID
	if v := r.URL.Query().Get("wallet_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			h.respondError(w, http.StatusBadRequest, "Invalid wallet ID")
			return
		}
		walletID = &id
	}

	txs, total, err := h.service.GetUserTransactions(r.Context(), userID, walletID, limit, offset)
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

	status := r.URL.Query().Get("status")
	currency := r.URL.Query().Get("currency")

	txs, total, err := h.service.GetAllTransactionsFiltered(r.Context(), limit, offset, status, currency)
	if err != nil {
		h.logger.Error("Failed to fetch all transactions", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch transactions")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"items":        txs,
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

// GetTransactionVolume returns transaction volume analytics (for admin).
func (h *PaymentHandler) GetTransactionVolume(w http.ResponseWriter, r *http.Request) {
	// 1. Authorization Check (Admin only)
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	// 2. Parse query parameters
	months := 6
	if v := r.URL.Query().Get("months"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			months = n
		}
	}

	// 3. Call Service
	volumes, err := h.service.GetTransactionVolume(r.Context(), months)
	if err != nil {
		h.logger.Error("Failed to fetch transaction volume", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch transaction volume")
		return
	}

	// 4. Respond
	h.respondJSON(w, http.StatusOK, volumes)
}

func (h *PaymentHandler) GetRiskUsageMetrics(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	metrics, err := h.service.GetRiskUsageMetrics(r.Context())
	if err != nil {
		h.logger.Error("Failed to fetch risk usage metrics", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch risk usage metrics")
		return
	}

	h.respondJSON(w, http.StatusOK, metrics)
}

// GetSystemStats returns system-wide statistics (for admin).
func (h *PaymentHandler) GetSystemStats(w http.ResponseWriter, r *http.Request) {
	// 1. Authorization Check (Admin only)
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	// 2. Call Service
	stats, err := h.service.GetSystemStats(r.Context())
	if err != nil {
		h.logger.Error("Failed to fetch system stats", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch system stats")
		return
	}

	// 3. Respond
	h.respondJSON(w, http.StatusOK, stats)
}

// GetRiskAlerts returns flagged transactions (for admin/risk).
func (h *PaymentHandler) GetRiskAlerts(w http.ResponseWriter, r *http.Request) {
	// 1. Authorization Check (Admin only)
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
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

	alerts, total, err := h.service.GetRiskAlerts(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch risk alerts", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch risk alerts")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"alerts": alerts,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetAuditLogs returns system audit logs (for admin).
func (h *PaymentHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	// 1. Authorization Check (Admin only)
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		h.respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	// 2. Pagination
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

	// 3. Call Service
	logs, total, err := h.service.GetAuditLogs(r.Context(), limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch audit logs", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch audit logs")
		return
	}

	// 4. Respond
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
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
		h.logger.Error("Failed to resolve dispute", map[string]interface{}{"error": err.Error(), "dispute_id": req.DisputeID})
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Dispute resolved successfully"})
}

// Removed duplicate GetRiskAlerts

// Duplicate of GetAuditLogs exists above with admin authorization; removing duplicate.

// decodeInitiatePaymentRequest decodes the payment initiation request.
func (h *PaymentHandler) decodeInitiatePaymentRequest(w http.ResponseWriter, r *http.Request) (payment.InitiatePaymentRequest, uuid.UUID, error) {
	var req payment.InitiatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return req, uuid.Nil, err
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return req, uuid.Nil, http.ErrNoCookie // Or appropriate error
	}

	// Enrich with device info from headers
	req.DeviceID = r.Header.Get("X-Device-ID")
	if req.DeviceID == "" {
		// Fallback to User-Agent if Device-ID is missing, similar to auth handler
		req.DeviceID = r.Header.Get("User-Agent")
	}

	// Enrich with Idempotency Key from headers if not in body
	if req.Reference == "" {
		req.Reference = r.Header.Get("Idempotency-Key")
	}

	return req, userID, nil
}

// respondJSON responds with JSON.
func (h *PaymentHandler) respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// respondError responds with an error message.
func (h *PaymentHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}

// respondValidationErrors responds with validation errors.
func (h *PaymentHandler) respondValidationErrors(w http.ResponseWriter, errs map[string]string) {
	h.respondJSON(w, http.StatusBadRequest, map[string]interface{}{"errors": errs})
}
