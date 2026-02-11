package handler

import (
	"encoding/json"
	"net/http"

	"kyd/internal/auth"
	"kyd/internal/middleware"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type APIKeyHandler struct {
	service *auth.APIKeyService
	logger  logger.Logger
}

func NewAPIKeyHandler(service *auth.APIKeyService, log logger.Logger) *APIKeyHandler {
	return &APIKeyHandler{
		service: service,
		logger:  log,
	}
}

type CreateAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

func (h *APIKeyHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	// Ensure user is admin (this check might be redundant if middleware handles it, but good for safety)
	// We'll rely on middleware for AuthZ, but we need UserID for 'CreatedBy'
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		h.respondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	key, rawKey, err := h.service.CreateKey(r.Context(), req.Name, req.Scopes, userID)
	if err != nil {
		h.logger.Error("Failed to create API key", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	// Return the raw key only once
	response := map[string]interface{}{
		"api_key": key,
		"secret":  rawKey,
	}

	h.respondJSON(w, http.StatusCreated, response)
}

func (h *APIKeyHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.service.ListKeys(r.Context())
	if err != nil {
		h.logger.Error("Failed to list API keys", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to list API keys")
		return
	}

	h.respondJSON(w, http.StatusOK, keys)
}

func (h *APIKeyHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	if err := h.service.RevokeKey(r.Context(), id); err != nil {
		h.logger.Error("Failed to revoke API key", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to revoke API key")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// Helpers

func (h *APIKeyHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("json encode failed", map[string]interface{}{"error": err.Error()})
	}
}

func (h *APIKeyHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
