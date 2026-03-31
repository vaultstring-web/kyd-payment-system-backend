package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"kyd/internal/casework"
	"kyd/internal/domain"
	"kyd/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type CasesHandler struct {
	service *casework.Service
}

func NewCasesHandler(service *casework.Service) *CasesHandler {
	return &CasesHandler{service: service}
}

func (h *CasesHandler) List(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	limit, offset := parsePagination(r)

	q := r.URL.Query()
	filter := &casework.Filter{}
	if v := strings.TrimSpace(q.Get("status")); v != "" {
		filter.Status = &v
	}
	if v := strings.TrimSpace(q.Get("priority")); v != "" {
		filter.Priority = &v
	}
	if v := strings.TrimSpace(q.Get("entity_type")); v != "" {
		filter.EntityType = &v
	}
	if v := strings.TrimSpace(q.Get("entity_id")); v != "" {
		filter.EntityID = &v
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		filter.Query = &v
	}
	// If no filters provided, keep nil for simpler SQL.
	if filter.Status == nil && filter.Priority == nil && filter.EntityType == nil && filter.EntityID == nil && filter.Query == nil {
		filter = nil
	}

	items, total, err := h.service.ListCases(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list cases")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items":  items,
		"cases":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *CasesHandler) Create(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}
	actorID, _ := middleware.UserIDFromContext(r.Context())

	var req struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
		Priority    string  `json:"priority"`
		EntityType  string  `json:"entity_type"`
		EntityID    string  `json:"entity_id"`
		AssignedTo  *string `json:"assigned_to"`
		Note        *string `json:"note"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.EntityType = strings.TrimSpace(req.EntityType)
	req.EntityID = strings.TrimSpace(req.EntityID)

	if req.Title == "" || req.EntityType == "" || req.EntityID == "" {
		respondError(w, http.StatusBadRequest, "Missing required fields")
		return
	}

	var assignedUUID *uuid.UUID
	if req.AssignedTo != nil && strings.TrimSpace(*req.AssignedTo) != "" {
		id, err := uuid.Parse(strings.TrimSpace(*req.AssignedTo))
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid assigned_to")
			return
		}
		assignedUUID = &id
	}

	priority := domain.CasePriority(strings.TrimSpace(req.Priority))
	if priority == "" {
		priority = domain.CasePriorityMedium
	}

	now := time.Now()
	c := &domain.Case{
		ID:          uuid.New(),
		Title:       req.Title,
		Description: req.Description,
		Status:      domain.CaseStatusOpen,
		Priority:    priority,
		EntityType:  domain.CaseEntityType(req.EntityType),
		EntityID:    req.EntityID,
		CreatedBy:   &actorID,
		AssignedTo:  assignedUUID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	created, err := h.service.CreateCase(r.Context(), c, req.Note)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create case")
		return
	}

	respondJSON(w, http.StatusCreated, created)
}

func (h *CasesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid case ID")
		return
	}

	c, err := h.service.GetCase(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "Case not found")
		return
	}

	respondJSON(w, http.StatusOK, c)
}

func (h *CasesHandler) Update(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}
	actorID, _ := middleware.UserIDFromContext(r.Context())

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid case ID")
		return
	}

	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
		Priority    *string `json:"priority"`
		AssignedTo  *string `json:"assigned_to"`
		Note        *string `json:"note"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	existing, err := h.service.GetCase(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "Case not found")
		return
	}

	updated := *existing
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t != "" {
			updated.Title = t
		}
	}
	if req.Description != nil {
		updated.Description = req.Description
	}
	if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
		updated.Status = domain.CaseStatus(strings.TrimSpace(*req.Status))
	}
	if req.Priority != nil && strings.TrimSpace(*req.Priority) != "" {
		updated.Priority = domain.CasePriority(strings.TrimSpace(*req.Priority))
	}
	if req.AssignedTo != nil {
		v := strings.TrimSpace(*req.AssignedTo)
		if v == "" {
			updated.AssignedTo = nil
		} else {
			aid, err := uuid.Parse(v)
			if err != nil {
				respondError(w, http.StatusBadRequest, "Invalid assigned_to")
				return
			}
			updated.AssignedTo = &aid
		}
	}

	note := req.Note
	actor := actorID
	res, err := h.service.UpdateCase(r.Context(), &updated, &actor, note)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update case")
		return
	}

	respondJSON(w, http.StatusOK, res)
}

func (h *CasesHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid case ID")
		return
	}
	limit, offset := parsePagination(r)

	items, total, err := h.service.ListCaseEvents(r.Context(), id, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list case events")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items":  items,
		"events": items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

