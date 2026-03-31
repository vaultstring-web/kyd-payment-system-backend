package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"kyd/internal/domain"
	"kyd/internal/middleware"
	"kyd/internal/risk"
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
	filter, err := parseSecurityEventFilter(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	events, total, err := h.service.GetSecurityEvents(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch security events")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"items":  events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func parseSecurityEventFilter(r *http.Request) (*security.EventFilter, error) {
	q := r.URL.Query()
	filter := &security.EventFilter{}

	if v := q.Get("type"); v != "" {
		filter.Type = &v
	}
	if v := q.Get("severity"); v != "" {
		filter.Severity = &v
	}
	if v := q.Get("status"); v != "" {
		filter.Status = &v
	}
	if v := q.Get("ip"); v != "" {
		filter.IPAddress = &v
	}
	if v := q.Get("q"); v != "" {
		filter.Query = &v
	}
	if v := q.Get("user_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, errors.New("Invalid user_id")
		}
		filter.UserID = &id
	}

	// If everything is empty, return nil to preserve the original behavior.
	if filter.Type == nil && filter.Severity == nil && filter.Status == nil && filter.UserID == nil && filter.IPAddress == nil && filter.Query == nil {
		return nil, nil
	}
	return filter, nil
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

func (h *SecurityHandler) GetRiskConfig(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	engine := risk.GetDefaultRiskEngine()
	if engine == nil {
		respondError(w, http.StatusServiceUnavailable, "Risk engine not initialized")
		return
	}

	cfg := engine.GetConfig()
	respondJSON(w, http.StatusOK, cfg)
}

func (h *SecurityHandler) GetRiskStatus(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	engine := risk.GetDefaultRiskEngine()
	if engine == nil {
		respondError(w, http.StatusServiceUnavailable, "Risk engine not initialized")
		return
	}

	status := engine.GetStatus()
	respondJSON(w, http.StatusOK, status)
}

func (h *SecurityHandler) UpdateRiskConfig(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	engine := risk.GetDefaultRiskEngine()
	if engine == nil {
		respondError(w, http.StatusServiceUnavailable, "Risk engine not initialized")
		return
	}

	var req struct {
		GlobalSystemPause       *bool    `json:"global_system_pause,omitempty"`
		MaxDailyLimit           *int64   `json:"max_daily_limit,omitempty"`
		HighValueThreshold      *int64   `json:"high_value_threshold,omitempty"`
		MaxVelocityPerHour      *int     `json:"max_velocity_per_hour,omitempty"`
		MaxVelocityPerDay       *int     `json:"max_velocity_per_day,omitempty"`
		SuspiciousLocationAlert *string  `json:"suspicious_location_alert,omitempty"`
		AdminApprovalThreshold  *int64   `json:"admin_approval_threshold,omitempty"`
		RestrictedCountries     []string `json:"restricted_countries,omitempty"`
		EnableDisputeResolution *bool    `json:"enable_dispute_resolution,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.GlobalSystemPause != nil {
		engine.SetGlobalSystemPause(*req.GlobalSystemPause)
	}
	if req.MaxDailyLimit != nil {
		engine.SetMaxDailyLimit(*req.MaxDailyLimit)
	}
	if req.HighValueThreshold != nil {
		engine.SetHighValueThreshold(*req.HighValueThreshold)
	}
	if req.MaxVelocityPerHour != nil {
		engine.SetMaxVelocityPerHour(*req.MaxVelocityPerHour)
	}
	if req.MaxVelocityPerDay != nil {
		engine.SetMaxVelocityPerDay(*req.MaxVelocityPerDay)
	}
	if req.SuspiciousLocationAlert != nil {
		engine.SetSuspiciousLocationAlert(*req.SuspiciousLocationAlert)
	}
	if req.AdminApprovalThreshold != nil {
		engine.SetAdminApprovalThreshold(*req.AdminApprovalThreshold)
	}
	if req.RestrictedCountries != nil {
		engine.SetRestrictedCountries(req.RestrictedCountries)
	}
	if req.EnableDisputeResolution != nil {
		engine.SetEnableDisputeResolution(*req.EnableDisputeResolution)
	}

	respondJSON(w, http.StatusOK, engine.GetStatus())
}
