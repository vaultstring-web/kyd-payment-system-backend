// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"kyd/internal/auth"
	"kyd/pkg/errors"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	service   *auth.Service
	validator *validator.Validator
	logger    logger.Logger
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(service *auth.Service, val *validator.Validator, log logger.Logger) *AuthHandler {
	return &AuthHandler{
		service:   service,
		validator: val,
		logger:    log,
	}
}

// Register handles user registration.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req auth.RegisterRequest

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

	if err := h.validator.Validate(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	response, err := h.service.Register(r.Context(), &req)
	if err != nil {
		// Handle common errors explicitly so clients get useful feedback.
		if err == errors.ErrUserAlreadyExists {
			h.respondError(w, http.StatusConflict, "User already exists")
			return
		}

		h.logger.Error("Registration failed", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Registration failed")
		return
	}

	h.respondJSON(w, http.StatusCreated, response)
}

// Login authenticates a user and returns tokens.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req auth.LoginRequest

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

	if err := h.validator.Validate(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	response, err := h.service.Login(r.Context(), &req)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	h.respondJSON(w, http.StatusOK, response)
}

func (h *AuthHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *AuthHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
