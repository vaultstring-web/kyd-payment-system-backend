// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"kyd/internal/wallet"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

// WalletHandler manages wallet endpoints.
type WalletHandler struct {
	service   *wallet.Service
	validator *validator.Validator
	logger    logger.Logger
}

// NewWalletHandler creates a WalletHandler.
func NewWalletHandler(service *wallet.Service, val *validator.Validator, log logger.Logger) *WalletHandler {
	return &WalletHandler{
		service:   service,
		validator: val,
		logger:    log,
	}
}

// CreateWallet handles wallet creation.
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req wallet.CreateWalletRequest

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

	userID, ok := r.Context().Value("user_id").(uuid.UUID)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	req.UserID = userID

	if err := h.validator.Validate(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	wallet, err := h.service.CreateWallet(r.Context(), &req)
	if err != nil {
		h.logger.Error("Failed to create wallet", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusCreated, wallet)
}

// GetWallet returns a wallet by ID.
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	walletID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid wallet ID")
		return
	}

	wallet, err := h.service.GetWallet(r.Context(), walletID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Wallet not found")
		return
	}

	h.respondJSON(w, http.StatusOK, wallet)
}

// GetUserWallets lists wallets for the authenticated user.
func (h *WalletHandler) GetUserWallets(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(uuid.UUID)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	wallets, err := h.service.GetUserWallets(r.Context(), userID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to fetch wallets")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"wallets": wallets,
		"count":   len(wallets),
	})
}

// GetBalance returns a wallet balance summary.
func (h *WalletHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	walletID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid wallet ID")
		return
	}

	balance, err := h.service.GetBalance(r.Context(), walletID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Wallet not found")
		return
	}

	h.respondJSON(w, http.StatusOK, balance)
}

// GetTransactionHistory returns transaction history for a wallet (stub).
func (h *WalletHandler) GetTransactionHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	walletID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid wallet ID")
		return
	}

	// TODO: Implement transaction history
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"wallet_id":    walletID,
		"transactions": []interface{}{},
	})
}

func (h *WalletHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *WalletHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
