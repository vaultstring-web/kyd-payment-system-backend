package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"kyd/internal/blockchain"
	"kyd/internal/domain"
	"kyd/internal/ledger"
	"kyd/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type BlockchainHandler struct {
	service      *blockchain.Service
	ledgerService *ledger.Service
}

func NewBlockchainHandler(service *blockchain.Service, ledgerService *ledger.Service) *BlockchainHandler {
	return &BlockchainHandler{service: service, ledgerService: ledgerService}
}

func (h *BlockchainHandler) ListNetworks(w http.ResponseWriter, r *http.Request) {
	// Admin check
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	networks, err := h.service.ListNetworks(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch networks")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"networks": networks,
	})
}

func (h *BlockchainHandler) CreateNetwork(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	var network domain.BlockchainNetworkInfo
	if err := json.NewDecoder(r.Body).Decode(&network); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.service.CreateNetwork(r.Context(), &network); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create network")
		return
	}

	respondJSON(w, http.StatusCreated, network)
}

func (h *BlockchainHandler) UpdateNetwork(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	// 1. Fetch existing network
	existing, err := h.service.GetNetwork(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "Network not found")
		return
	}

	// 2. Decode partial updates
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 3. Apply updates
	if v, ok := updates["name"].(string); ok {
		existing.Name = v
	}
	if v, ok := updates["status"].(string); ok {
		existing.Status = v
	}
	if v, ok := updates["height"].(float64); ok {
		existing.BlockHeight = int64(v)
	}
	if v, ok := updates["peer_count"].(float64); ok {
		existing.PeerCount = int(v)
	}
	if v, ok := updates["channel"].(string); ok {
		existing.Channel = &v
	}
	if v, ok := updates["rpc_url"].(string); ok {
		existing.RpcURL = &v
	}
	if v, ok := updates["chain_id"].(string); ok {
		existing.ChainID = &v
	}
	if v, ok := updates["symbol"].(string); ok {
		existing.Symbol = &v
	}
	if v, ok := updates["last_block_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			existing.LastBlockTime = &t
		}
	}

	existing.ID = id // ensure ID is preserved

	// 4. Save updated network
	if err := h.service.UpdateNetwork(r.Context(), existing); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update network")
		return
	}

	respondJSON(w, http.StatusOK, existing)
}

func (h *BlockchainHandler) GetNetwork(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	network, err := h.service.GetNetwork(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "Network not found")
		return
	}

	respondJSON(w, http.StatusOK, network)
}

func (h *BlockchainHandler) DeleteNetwork(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	if err := h.service.DeleteNetwork(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete network")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// VerifyLedgerChain verifies the hash chain integrity for a given wallet.
func (h *BlockchainHandler) VerifyLedgerChain(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	if h.ledgerService == nil {
		respondError(w, http.StatusServiceUnavailable, "Ledger service not available")
		return
	}

	vars := mux.Vars(r)
	walletIDStr := vars["wallet_id"]
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid wallet ID")
		return
	}

	okChain, err := h.ledgerService.VerifyChainIntegrity(r.Context(), walletID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to verify ledger chain")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"wallet_id": walletIDStr,
		"valid":     okChain,
	})
}

// GetLedgerChainReport returns a data-driven view of hash linkage for the last N ledger entries.
func (h *BlockchainHandler) GetLedgerChainReport(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	if h.ledgerService == nil {
		respondError(w, http.StatusServiceUnavailable, "Ledger service not available")
		return
	}

	vars := mux.Vars(r)
	walletIDStr := vars["wallet_id"]
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid wallet ID")
		return
	}

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	report, err := h.ledgerService.GetChainReport(r.Context(), walletID, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load ledger chain report")
		return
	}

	respondJSON(w, http.StatusOK, report)
}
