// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"kyd/internal/middleware"
	"kyd/internal/payment"
	"kyd/pkg/errors"
	"kyd/pkg/logger"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
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

	response, err := h.service.InitiatePayment(r.Context(), &req)
	if err != nil {
		h.logger.Error("Payment failed", map[string]interface{}{
			"error":     err.Error(),
			"sender_id": userID,
		})

		// Handle specific errors
		if err == errors.ErrInsufficientBalance {
			h.respondError(w, http.StatusBadRequest, "Insufficient balance")
			return
		}
		if err == errors.ErrWalletNotFound {
			h.respondError(w, http.StatusNotFound, "Wallet not found")
			return
		}

		h.respondError(w, http.StatusInternalServerError, "Payment failed")
		return
	}

	h.respondJSON(w, http.StatusCreated, response)
}

func (h *PaymentHandler) decodeInitiatePaymentRequest(w http.ResponseWriter, r *http.Request) (payment.InitiatePaymentRequest, uuid.UUID, error) {
	var req payment.InitiatePaymentRequest

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		if err == io.EOF {
			h.respondError(w, http.StatusBadRequest, "Request body is required")
			return req, uuid.Nil, err
		}
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return req, uuid.Nil, err
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return req, uuid.Nil, fmt.Errorf("unauthorized")
	}
	req.SenderID = userID

	if err := h.validator.Validate(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return req, uuid.Nil, err
	}

	if req.Amount.IsZero() || req.Amount.IsNegative() {
		h.respondError(w, http.StatusBadRequest, "Amount must be greater than 0")
		return req, uuid.Nil, fmt.Errorf("invalid amount")
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

	txs, err := h.service.GetUserTransactions(r.Context(), userID, limit, offset)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch transactions")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"limit":        limit,
		"offset":       offset,
		"count":        len(txs),
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
	limit := 20
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
