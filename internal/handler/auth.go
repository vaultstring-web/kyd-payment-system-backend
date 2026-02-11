// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"kyd/internal/auth"
	"kyd/internal/domain"
	"kyd/internal/middleware"
	"kyd/pkg/errors"
	"kyd/pkg/logger"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

// AuditLogger defines the interface for persisting audit logs.
type AuditLogger interface {
	Create(ctx context.Context, log *domain.AuditLog) error
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	service      *auth.Service
	validator    *validator.Validator
	logger       logger.Logger
	auditLogger  AuditLogger
	totpIssuer   string
	totpPeriod   int
	totpDigits   int
	cookieSecure bool
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(service *auth.Service, val *validator.Validator, log logger.Logger, auditLogger AuditLogger, totpIssuer string, totpPeriod int, totpDigits int, cookieSecure bool) *AuthHandler {
	return &AuthHandler{
		service:      service,
		validator:    val,
		logger:       log,
		auditLogger:  auditLogger,
		totpIssuer:   totpIssuer,
		totpPeriod:   totpPeriod,
		totpDigits:   totpDigits,
		cookieSecure: cookieSecure,
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

	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
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
		return
	}

	h.logger.Info("User registered", map[string]interface{}{
		"event":   "user_registered",
		"user_id": response.User.ID,
		"email":   req.Email,
		"ip":      r.RemoteAddr,
	})

	// Audit Log: User Registered
	_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
		ID:         uuid.New(),
		Action:     "CREATE", // Maps to AuditAction.CREATE
		UserID:     &response.User.ID,
		EntityType: "user",
		EntityID:   response.User.ID.String(),
		UserEmail:  response.User.Email,
		IPAddress:  r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		CreatedAt:  time.Now(),
		Metadata: map[string]interface{}{
			"type": "registration",
		},
	})

	// Set httpOnly cookies for access and refresh tokens
	h.setAuthCookies(w, response)

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

	// Enrich with device info
	req.IPAddress = r.Header.Get("X-Forwarded-For")
	if req.IPAddress == "" {
		req.IPAddress = r.RemoteAddr
	}
	req.DeviceName = r.Header.Get("User-Agent")
	if req.DeviceID == "" {
		req.DeviceID = r.Header.Get("X-Device-ID")
		if req.DeviceID == "" && req.DeviceName != "" {
			// Fallback: Use User-Agent as DeviceID
			req.DeviceID = req.DeviceName
		}
	}

	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
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

		// Audit Log: Login Failed
		_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
			ID:           uuid.New(),
			Action:       "LOGIN_FAILED",
			EntityType:   "user",
			EntityID:     req.Email, // Use email as identifier for failed attempts
			UserEmail:    req.Email,
			IPAddress:    req.IPAddress,
			UserAgent:    req.DeviceName,
			ErrorMessage: err.Error(),
			CreatedAt:    time.Now(),
		})

		h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	h.logger.Info("Login successful", map[string]interface{}{
		"event":   "login_success",
		"user_id": response.User.ID,
		"ip":      r.RemoteAddr,
	})

	// Audit Log: Login Success
	_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
		ID:         uuid.New(),
		Action:     "LOGIN_SUCCESS",
		UserID:     &response.User.ID,
		EntityType: "user",
		EntityID:   response.User.ID.String(),
		UserEmail:  response.User.Email,
		IPAddress:  req.IPAddress,
		UserAgent:  req.DeviceName,
		CreatedAt:  time.Now(),
	})

	// Set httpOnly cookies for access and refresh tokens
	h.setAuthCookies(w, response)

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
	Email string `json:"email" validate:"required,email"`
}

func (h *AuthHandler) SendVerification(w http.ResponseWriter, r *http.Request) {
	var req sendVerificationRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
		return
	}

	if err := h.service.SendVerificationByEmail(r.Context(), req.Email); err != nil {
		h.logger.Error("Failed to send verification email", map[string]interface{}{
			"error": err.Error(),
			"email": req.Email,
		})
		// Do not return error to user to prevent enumeration
	}
	h.respondJSON(w, http.StatusAccepted, map[string]string{"status": "verification email requested"})
}

type verifyEmailRequest struct {
	Code string `json:"code" validate:"required"`
}

// VerifyEmail marks the user's email as verified using a token.
func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var code string

	if r.Method == http.MethodGet {
		code = r.URL.Query().Get("token")
		if code == "" {
			h.respondError(w, http.StatusBadRequest, "Missing token")
			return
		}
	} else {
		var req verifyEmailRequest
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.respondError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if errs := h.validator.ValidateStructured(&req); errs != nil {
			h.respondValidationErrors(w, errs)
			return
		}
		code = req.Code
	}

	if err := h.service.VerifyEmail(r.Context(), code); err != nil {
		h.respondError(w, http.StatusBadRequest, "Verification failed: "+err.Error())
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

func (h *AuthHandler) respondValidationErrors(w http.ResponseWriter, errors map[string]string) {
	h.respondJSON(w, http.StatusBadRequest, map[string]interface{}{
		"error":             "Validation failed",
		"validation_errors": errors,
	})
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

func (h *AuthHandler) setAuthCookies(w http.ResponseWriter, resp *auth.TokenResponse) {
	sameSite := http.SameSiteNoneMode
	if !h.cookieSecure {
		sameSite = http.SameSiteLaxMode
	}

	// Access token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    resp.AccessToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: sameSite,
		Secure:   h.cookieSecure,
		MaxAge:   int(time.Until(resp.ExpiresAt).Seconds()),
	})
	// Refresh token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    resp.RefreshToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: sameSite,
		Secure:   h.cookieSecure,
		MaxAge:   30 * 24 * 60 * 60,
	})
}

func (h *AuthHandler) clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   h.cookieSecure,
		MaxAge:   0,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   h.cookieSecure,
		MaxAge:   0,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Extract token from header to blacklist it
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if err := h.service.Logout(r.Context(), token); err != nil {
			h.logger.Error("Failed to blacklist token", map[string]interface{}{"error": err.Error()})
			// Continue to clear cookies anyway
		}
	}

	h.clearAuthCookies(w)

	// Audit Log: Logout
	// Note: We might not have user ID here if token was invalid, but we should log the attempt if possible.
	// But Logout is usually called with a valid token or just clears cookies.
	// If middleware extracted UserID, we can use it. But this handler might not be behind AuthMiddleware?
	// Usually Logout IS behind AuthMiddleware.
	userID, ok := middleware.UserIDFromContext(r.Context())
	if ok {
		_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
			ID:         uuid.New(),
			Action:     "LOGOUT",
			UserID:     &userID,
			EntityType: "user",
			EntityID:   userID.String(),
			IPAddress:  r.RemoteAddr,
			UserAgent:  r.UserAgent(),
			CreatedAt:  time.Now(),
		})
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

type totpSetupResponse struct {
	OTPURL string `json:"otp_url"`
}

type totpVerifyRequest struct {
	Code string `json:"code" validate:"required"`
}

type totpStatusResponse struct {
	Enabled bool `json:"enabled"`
}

func (h *AuthHandler) SetupTOTP(w http.ResponseWriter, r *http.Request) {
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
	// Allow all users to setup TOTP
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      h.totpIssuer,
		AccountName: user.Email,
	})
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "TOTP setup failed")
		return
	}

	// Save secret to DB (encrypted by repo)
	secret := key.Secret()
	user.TOTPSecret = &secret
	user.IsTOTPEnabled = false
	if err := h.service.UpdateUser(r.Context(), user); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to save TOTP secret")
		return
	}

	h.respondJSON(w, http.StatusOK, totpSetupResponse{OTPURL: key.URL()})
}

func (h *AuthHandler) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
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
	// Allow all users to verify TOTP
	/*
		if user.UserType != domain.UserTypeAdmin {
			h.respondError(w, http.StatusForbidden, "Forbidden")
			return
		}
	*/
	var req totpVerifyRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
		return
	}

	if user.TOTPSecret == nil || *user.TOTPSecret == "" {
		h.respondError(w, http.StatusBadRequest, "TOTP not set up")
		return
	}

	if !totp.Validate(req.Code, *user.TOTPSecret) {
		h.respondError(w, http.StatusUnauthorized, "Invalid code")
		return
	}

	user.IsTOTPEnabled = true
	if err := h.service.UpdateUser(r.Context(), user); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to enable TOTP")
		return
	}

	// Audit Log: TOTP Enabled
	_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
		ID:         uuid.New(),
		Action:     "UPDATE",
		UserID:     &user.ID,
		EntityType: "user",
		EntityID:   user.ID.String(),
		UserEmail:  user.Email,
		IPAddress:  r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		CreatedAt:  time.Now(),
		Metadata: map[string]interface{}{
			"type": "totp_enabled",
		},
	})

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "totp_verified"})
}

func (h *AuthHandler) TOTPStatus(w http.ResponseWriter, r *http.Request) {
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
	// Removed admin-only restriction
	h.respondJSON(w, http.StatusOK, totpStatusResponse{Enabled: user.IsTOTPEnabled})
}

// DisableTOTP disables TOTP for the user.
func (h *AuthHandler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
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

	// Disable TOTP
	user.IsTOTPEnabled = false
	user.TOTPSecret = nil // Optional: Clear the secret or keep it for re-enabling without new setup? Better to clear.

	if err := h.service.UpdateUser(r.Context(), user); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to disable TOTP")
		return
	}

	// Audit Log: TOTP Disabled
	_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
		ID:         uuid.New(),
		Action:     "UPDATE",
		UserID:     &user.ID,
		EntityType: "user",
		EntityID:   user.ID.String(),
		UserEmail:  user.Email,
		IPAddress:  r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		CreatedAt:  time.Now(),
		Metadata: map[string]interface{}{
			"type": "totp_disabled",
		},
	})

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "totp_disabled"})
}

func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}
	name := parts[0]
	if len(name) <= 2 {
		return "***@" + parts[1]
	}
	return name[:2] + "***@" + parts[1]
}
