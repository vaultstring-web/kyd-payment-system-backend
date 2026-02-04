package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"kyd/internal/domain"
	"kyd/internal/middleware"
	"kyd/internal/security"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type SecurityHandler struct {
	service   *security.Service
	validator *validator.Validator
}

func NewSecurityHandler(service *security.Service, val *validator.Validator) *SecurityHandler {
	return &SecurityHandler{service: service, validator: val}
}

func (h *SecurityHandler) GetSecurityEvents(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	limit, offset := parsePagination(r)
	events, total, err := h.service.GetSecurityEvents(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch security events")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"total":  total,
	})
}

func (h *SecurityHandler) UpdateSecurityEvent(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid event ID")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	adminID, _ := middleware.UserIDFromContext(r.Context())

	if err := h.service.UpdateSecurityEventStatus(r.Context(), id, req.Status, &adminID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update event status")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *SecurityHandler) GetBlocklist(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	entries, err := h.service.GetBlocklist(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch blocklist")
		return
	}

	respondJSON(w, http.StatusOK, entries)
}

func (h *SecurityHandler) AddToBlocklist(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	var req struct {
		Type      string     `json:"type"`
		Value     string     `json:"value"`
		Reason    string     `json:"reason"`
		ExpiresAt *time.Time `json:"expires_at"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	adminID, _ := middleware.UserIDFromContext(r.Context())

	entry := &domain.BlocklistEntry{
		Type:      req.Type,
		Value:     req.Value,
		Reason:    req.Reason,
		ExpiresAt: req.ExpiresAt,
		AddedBy:   adminID,
		CreatedAt: time.Now(),
	}

	if err := h.service.AddToBlocklist(r.Context(), entry); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to add to blocklist")
		return
	}

	respondJSON(w, http.StatusCreated, entry)
}

func (h *SecurityHandler) RemoveFromBlocklist(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid entry ID")
		return
	}

	if err := h.service.RemoveFromBlocklist(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to remove from blocklist")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *SecurityHandler) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	metrics, err := h.service.GetSystemHealth(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch system health")
		return
	}

	respondJSON(w, http.StatusOK, metrics)
}
