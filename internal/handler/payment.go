// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"kyd/internal/domain"
	"kyd/internal/middleware"
	"kyd/internal/payment"
	"kyd/pkg/errors"
	"kyd/pkg/logger"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/shopspring/decimal"
)

// PaymentHandler manages payment-related endpoints.
type PaymentHandler struct {
	service   *payment.Service
	validator *validator.Validator
	logger    logger.Logger
}

// NewPaymentHandler creates a PaymentHandler.
func NewPaymentHandler(service *payment.Service, val *validator.Validator, log logger.Logger) *PaymentHandler {
	return &PaymentHandler{
		service:   service,
		validator: val,
		logger:    log,
	}
}

// InitiatePayment starts a payment flow.
func (h *PaymentHandler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	req, userID, err := h.decodeInitiatePaymentRequest(w, r)
	if err != nil {
		return
	}

	// Ensure sender_id is set from authenticated user
	req.SenderID = userID

	response, err := h.service.InitiatePayment(r.Context(), &req)
	if err != nil {
		h.logger.Error("Payment failed", map[string]interface{}{
			"error":     err.Error(),
			"sender_id": userID,
		})

		// Handle specific errors
		if stdErrors.Is(err, errors.ErrInsufficientBalance) {
			h.respondError(w, http.StatusBadRequest, "Insufficient balance")
			return
		}
		if stdErrors.Is(err, errors.ErrWalletNotFound) {
			h.respondError(w, http.StatusNotFound, "Wallet not found")
			return
		}

		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Payment processing failed: %s", err.Error()))
		return
	}

	h.respondJSON(w, http.StatusCreated, response)
}

func (h *PaymentHandler) decodeInitiatePaymentRequest(w http.ResponseWriter, r *http.Request) (payment.InitiatePaymentRequest, uuid.UUID, error) {
	var req payment.InitiatePaymentRequest

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Request body is required")
		return req, uuid.Nil, err
	}
	if len(bodyBytes) == 0 {
		h.respondError(w, http.StatusBadRequest, "Request body is required")
		return req, uuid.Nil, fmt.Errorf("empty body")
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		h.logger.Error("Decode initiate payment failed", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %s", err.Error()))
		return req, uuid.Nil, err
	}

	// Parse fields manually to tolerate number/string types and provide clearer errors
	if v, ok := raw["receiver_wallet_id"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			if id, err := uuid.Parse(s); err == nil {
				req.ReceiverWalletID = id
			} else {
				h.respondError(w, http.StatusBadRequest, "Invalid receiver_wallet_id format")
				return req, uuid.Nil, err
			}
		}
	}
	if v, ok := raw["receiver_id"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			if id, err := uuid.Parse(s); err == nil {
				req.ReceiverID = id
			} else {
				h.respondError(w, http.StatusBadRequest, "Invalid receiver_id format")
				return req, uuid.Nil, err
			}
		}
	}
	if v, ok := raw["amount"]; ok && v != nil {
		switch vv := v.(type) {
		case float64:
			req.Amount = decimal.NewFromFloat(vv)
		case string:
			d, derr := decimal.NewFromString(vv)
			if derr != nil {
				h.respondError(w, http.StatusBadRequest, "Invalid amount")
				return req, uuid.Nil, derr
			}
			req.Amount = d
		default:
			h.respondError(w, http.StatusBadRequest, "Invalid amount type")
			return req, uuid.Nil, fmt.Errorf("invalid amount type")
		}
	}
	if v, ok := raw["currency"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			req.Currency = domain.Currency(s)
		}
	}
	if v, ok := raw["description"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.Description = s
		}
	}
	if v, ok := raw["channel"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.Channel = s
		}
	}
	if v, ok := raw["category"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.Category = s
		}
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return req, uuid.Nil, fmt.Errorf("unauthorized")
	}
	// Attach authenticated user as sender
	req.SenderID = userID

	// Basic validation (manual to allow either receiver_id or receiver_wallet_id)
	if req.Amount.IsZero() || req.Amount.IsNegative() {
		h.respondError(w, http.StatusBadRequest, "Amount must be greater than 0")
		return req, uuid.Nil, fmt.Errorf("invalid amount")
	}
	if req.ReceiverID == uuid.Nil && req.ReceiverWalletID == uuid.Nil {
		h.respondError(w, http.StatusBadRequest, "Provide receiver_id or receiver_wallet_id")
		return req, uuid.Nil, fmt.Errorf("missing receiver")
	}

	// Run validator for required fields except conditional ones
	if err := h.validator.Validate(&req); err != nil {
		h.logger.Error("Validation failed for initiate payment", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusBadRequest, err.Error())
		return req, uuid.Nil, err
	}

	return req, userID, nil
}

// GetTransaction returns a single transaction by ID.
func (h *PaymentHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	tx, err := h.service.GetTransaction(r.Context(), txID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Transaction not found")
		return
	}

	// Verify user has access to this transaction
	userID, _ := middleware.UserIDFromContext(r.Context())
	if tx.SenderID != userID && tx.ReceiverID != userID {
		h.respondError(w, http.StatusForbidden, "Access denied")
		return
	}

	h.respondJSON(w, http.StatusOK, tx)
}

// GetUserTransactions lists transactions for the authenticated user.
func (h *PaymentHandler) GetUserTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	limit, offset := parsePagination(r)

	txs, total, err := h.service.GetUserTransactions(r.Context(), userID, limit, offset)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch transactions")
		return
	}

	count := len(txs)
	hasMore := offset+count < total
	nextOffset := offset + count
	if nextOffset > total {
		nextOffset = total
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"limit":        limit,
		"offset":       offset,
		"count":        count,
		"total":        total,
		"has_more":     hasMore,
		"next_offset":  nextOffset,
	})
}

// CancelPayment cancels a pending transaction.
func (h *PaymentHandler) CancelPayment(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid transaction ID")
		return
	}

	userID, _ := middleware.UserIDFromContext(r.Context())

	if err := h.service.CancelTransaction(r.Context(), txID, userID); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{
		"message": "Transaction cancelled successfully",
	})
}

// BulkPayment processes multiple payments in one request.
func (h *PaymentHandler) BulkPayment(w http.ResponseWriter, r *http.Request) {
	var req payment.BulkPaymentRequest

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

	userID, _ := middleware.UserIDFromContext(r.Context())
	req.SenderID = userID

	if err := h.validator.Validate(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	results, err := h.service.BulkPayment(r.Context(), &req)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Bulk payment failed")
		return
	}

	h.respondJSON(w, http.StatusCreated, results)
}

func parsePagination(r *http.Request) (int, int) {
	limit := 10
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	return limit, offset
}

func (h *PaymentHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("json encode failed", map[string]interface{}{"error": err.Error()})
		_, _ = w.Write([]byte(`{"error":"response encoding failed"}`))
	}
}

func (h *PaymentHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
