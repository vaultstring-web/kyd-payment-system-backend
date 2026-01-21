// Package handler provides HTTP handlers for the KYD services.
// internal/handler/auth.go
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"kyd/internal/auth"
	"kyd/internal/middleware"
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

		h.respondError(w, http.StatusInternalServerError, "Registration failed")
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Registration failed: %v", err))
		return
	}

	h.logger.Info("User registered", map[string]interface{}{
		"event":   "user_registered",
		"user_id": response.User.ID,
		"email":   req.Email,
		"ip":      r.RemoteAddr,
	})

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
		h.logger.Warn("Login failed", map[string]interface{}{
			"event": "login_failed",
			"email": req.Email,
			"error": err.Error(),
			"ip":    r.RemoteAddr,
			"ua":    r.UserAgent(),
		})
		h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	h.logger.Info("Login successful", map[string]interface{}{
		"event":   "login_success",
		"user_id": response.User.ID,
		"ip":      r.RemoteAddr,
	})

	h.respondJSON(w, http.StatusOK, response)
}

// Me returns the authenticated user's profile based on JWT.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	user, err := h.service.GetUserByID(r.Context(), userID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "User not found")
		return
	}
	h.respondJSON(w, http.StatusOK, user)
}

// SendVerification triggers an email verification email to the given address.
type sendVerificationRequest struct {
	Email string `json:"email"`
}

func (h *AuthHandler) SendVerification(w http.ResponseWriter, r *http.Request) {
	var req sendVerificationRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Email == "" {
		h.respondError(w, http.StatusBadRequest, "Email is required")
		return
	}
	_ = h.service.SendVerificationByEmail(r.Context(), req.Email)
	h.respondJSON(w, http.StatusAccepted, map[string]string{"status": "verification email requested"})
}

// VerifyEmail marks the user's email as verified using a token.
func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.respondError(w, http.StatusBadRequest, "Token is required")
		return
	}
	if err := h.service.VerifyEmail(r.Context(), token); err != nil {
		if err == errors.ErrInvalidCredentials {
			h.respondError(w, http.StatusBadRequest, "Invalid or expired token")
			return
		}
		h.respondError(w, http.StatusInternalServerError, "Verification failed")
		return
	}

	h.logger.Info("Email verified", map[string]interface{}{
		"event": "email_verified",
		"ip":    r.RemoteAddr,
	})

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "email verified"})
}

func (h *AuthHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("json encode failed", map[string]interface{}{"error": err.Error()})
		_, _ = w.Write([]byte(`{"error":"response encoding failed"}`))
	}
}

func (h *AuthHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}

// DebugUser returns basic user info for a given email in development.
func (h *AuthHandler) DebugUser(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		h.respondError(w, http.StatusBadRequest, "Email is required")
		return
	}
	user, err := h.service.DebugFindByEmail(r.Context(), email)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "User not found")
		return
	}
	resp := map[string]interface{}{
		"id":              user.ID.String(),
		"email":           user.Email,
		"password_hash":   user.PasswordHash,
		"failed_attempts": user.FailedLoginAttempts,
		"locked_until":    user.LockedUntil,
	}
	h.respondJSON(w, http.StatusOK, resp)
}
