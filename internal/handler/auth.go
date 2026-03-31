// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"kyd/internal/auth"
	"kyd/internal/domain"
	"kyd/internal/middleware"
	"kyd/internal/security"
	"kyd/pkg/errors"
	"kyd/pkg/logger"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"golang.org/x/oauth2"
)

// AuditLogger defines the interface for persisting audit logs.
type AuditLogger interface {
	Create(ctx context.Context, log *domain.AuditLog) error
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	service         *auth.Service
	validator       *validator.Validator
	logger          logger.Logger
	auditLogger     AuditLogger
	securityService *security.Service
	totpIssuer      string
	totpPeriod      int
	totpDigits      int
	cookieSecure    bool
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(service *auth.Service, val *validator.Validator, log logger.Logger, auditLogger AuditLogger, securityService *security.Service, totpIssuer string, totpPeriod int, totpDigits int, cookieSecure bool) *AuthHandler {
	return &AuthHandler{
		service:         service,
		validator:       val,
		logger:          log,
		auditLogger:     auditLogger,
		securityService: securityService,
		totpIssuer:      totpIssuer,
		totpPeriod:      totpPeriod,
		totpDigits:      totpDigits,
		cookieSecure:    cookieSecure,
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
		// #region agent log
		h.logger.Error("Login decode failed", map[string]interface{}{
			"event":        "login_decode_failed",
			"decoder_error": err.Error(),
			"content_type": r.Header.Get("Content-Type"),
			"content_length": r.ContentLength,
			"content_encoding": r.Header.Get("Content-Encoding"),
			"path":          r.URL.Path,
			"method":        r.Method,
		})
		// #endregion
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

	// Check blocklist by email before attempting login
	if h.securityService != nil {
		isBlocked, err := h.securityService.IsBlacklisted(r.Context(), req.Email)
		if err != nil {
			h.respondError(w, http.StatusServiceUnavailable, "Authentication service unavailable")
			return
		}
		if isBlocked {
			h.respondError(w, http.StatusForbidden, "Account is blocked")
			return
		}
	}

	response, err := h.service.Login(r.Context(), &req)
	if err != nil {
		ip := req.IPAddress
		if ip == "" {
			ip = r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
		}
		ua := req.DeviceName
		if ua == "" {
			ua = r.UserAgent()
		}

		h.logger.Warn("Login failed", map[string]interface{}{
			"event": "login_failed",
			"email": req.Email,
			"error": err.Error(),
			"ip":    ip,
			"ua":    ua,
		})

		_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
			ID:           uuid.New(),
			Action:       "LOGIN_FAILED",
			EntityType:   "user",
			EntityID:     req.Email, // Use email as identifier for failed attempts
			UserEmail:    req.Email,
			IPAddress:    ip,
			UserAgent:    ua,
			ErrorMessage: err.Error(),
			CreatedAt:    time.Now(),
		})

		if h.securityService != nil {
			go func(req auth.LoginRequest, ip, ua, errMsg string) {
				riskScore := 50
				if req.DeviceID == "" {
					riskScore += 20
				}
				if req.CountryCode != "" {
					riskScore += 10
				}

				severity := domain.SecuritySeverityMedium
				if riskScore >= 80 {
					severity = domain.SecuritySeverityHigh
				}

				description := fmt.Sprintf("Login failed for %s from %s (%s)", req.Email, ip, req.CountryCode)

				_ = h.securityService.LogSecurityEvent(context.Background(), &domain.SecurityEvent{
					Type:        domain.SecurityEventTypeAdminLoginFailed,
					Severity:    severity,
					Description: description,
					UserID:      nil,
					IPAddress:   ip,
					Location:    req.CountryCode,
					Status:      domain.SecurityEventStatusOpen,
					Metadata: domain.Metadata{
						"email":        req.Email,
						"device_id":    req.DeviceID,
						"device_name":  req.DeviceName,
						"user_agent":   ua,
						"risk_score":   riskScore,
						"error":        errMsg,
						"country_code": req.CountryCode,
					},
					CreatedAt: time.Now(),
				})
			}(req, ip, ua, err.Error())
		}

		h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	ip := req.IPAddress
	if ip == "" {
		ip = r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
	}
	ua := req.DeviceName
	if ua == "" {
		ua = r.UserAgent()
	}

	h.logger.Info("Login successful", map[string]interface{}{
		"event":   "login_success",
		"user_id": response.User.ID,
		"ip":      ip,
		"ua":      ua,
	})

	_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
		ID:         uuid.New(),
		Action:     "LOGIN_SUCCESS",
		UserID:     &response.User.ID,
		EntityType: "user",
		EntityID:   response.User.ID.String(),
		UserEmail:  response.User.Email,
		IPAddress:  ip,
		UserAgent:  ua,
		CreatedAt:  time.Now(),
	})

	if h.securityService != nil && response.User != nil {
		go func(user *domain.User, req auth.LoginRequest, ip, ua string) {
			riskScore := 10
			if req.CountryCode != "" && !strings.EqualFold(req.CountryCode, user.CountryCode) {
				riskScore += 60
			}
			if req.DeviceID == "" {
				riskScore += 20
			}

			if riskScore >= 60 {
				severity := domain.SecuritySeverityMedium
				if riskScore >= 80 {
					severity = domain.SecuritySeverityHigh
				}

				description := fmt.Sprintf(
					"Suspicious login for user %s from country %s (ip=%s, device=%s, risk=%d)",
					user.Email, req.CountryCode, ip, req.DeviceName, riskScore,
				)

				_ = h.securityService.LogSecurityEvent(context.Background(), &domain.SecurityEvent{
					Type:        domain.SecurityEventTypeSuspiciousIP,
					Severity:    severity,
					Description: description,
					UserID:      &user.ID,
					IPAddress:   ip,
					Location:    req.CountryCode,
					Status:      domain.SecurityEventStatusOpen,
					Metadata: domain.Metadata{
						"user_email":   user.Email,
						"device_id":    req.DeviceID,
						"device_name":  req.DeviceName,
						"user_agent":   ua,
						"risk_score":   riskScore,
						"country_code": req.CountryCode,
					},
					CreatedAt: time.Now(),
				})
			}
		}(response.User, req, ip, ua)
	}

	// Set httpOnly cookies for access and refresh tokens
	h.setAuthCookies(w, response)

	h.respondJSON(w, http.StatusOK, response)
}

// Me returns the authenticated user's profile based on JWT.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.logger.Debug("Me: UserIDFromContext returned !ok - context missing user ID", map[string]interface{}{
			"path":   r.URL.Path,
			"method": r.Method,
		})
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	h.logger.Debug("Me: Successfully retrieved user ID from context", map[string]interface{}{
		"user_id": userID.String(),
	})
	user, err := h.service.GetUserByID(r.Context(), userID)
	if err != nil {
		h.logger.Debug("Me: GetUserByID failed", map[string]interface{}{
			"user_id": userID.String(),
			"error":   err.Error(),
		})
		h.respondError(w, http.StatusNotFound, "User not found")
		return
	}
	h.logger.Debug("Me: Successfully retrieved user", map[string]interface{}{
		"user_id": user.ID.String(),
	})
	h.respondJSON(w, http.StatusOK, user)
}

// GoogleAuthRequest represents a Google OAuth authentication request
type GoogleAuthRequest struct {
	Code    string `json:"code"`     // Authorization code from Google
	IDToken string `json:"id_token"` // ID token from Google (alternative to code)
	State   string `json:"state"`    // State parameter for CSRF protection
}

// GoogleAuthStart initiates Google OAuth flow by returning the auth URL
func (h *AuthHandler) GoogleAuthStart(w http.ResponseWriter, r *http.Request) {
	if h.service.GoogleOAuth == nil {
		h.respondError(w, http.StatusNotImplemented, "Google OAuth is not configured")
		return
	}

	// Generate state parameter for CSRF protection
	state, err := generateRandomToken(32)
	if err != nil {
		h.logger.Error("Failed to generate state token", map[string]interface{}{
			"error": err.Error(),
			"ip":    r.RemoteAddr,
		})
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Store state in session or cookie for validation later
	// For now, we'll return it to the client to include in the redirect
	authURL := h.service.GoogleOAuth.GetAuthURL(state)

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"auth_url": authURL,
		"state":    state,
	})
}

// GoogleAuthCallback handles the Google OAuth callback and exchanges code for tokens
func (h *AuthHandler) GoogleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if h.service.GoogleOAuth == nil {
		h.respondError(w, http.StatusNotImplemented, "Google OAuth is not configured")
		return
	}

	var req GoogleAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Code == "" && req.IDToken == "" {
		h.respondError(w, http.StatusBadRequest, "Either code or id_token is required")
		return
	}

	var userInfo *auth.GoogleUserInfo
	var googleToken *oauth2.Token
	var err error

	if req.Code != "" {
		// Exchange authorization code for tokens and user info
		userInfo, googleToken, err = h.service.GoogleOAuth.ExchangeCode(r.Context(), req.Code)
	} else {
		// Validate ID token directly
		userInfo, err = h.service.GoogleOAuth.ValidateIDToken(r.Context(), req.IDToken)
		// For ID token auth, we don't have a full oauth2.Token unless passed in req
	}

	if err != nil {
		h.logger.Error("Google OAuth authentication failed", map[string]interface{}{
			"error": err.Error(),
			"ip":    r.RemoteAddr,
		})
		h.respondError(w, http.StatusUnauthorized, "Google authentication failed: "+err.Error())
		return
	}

	// Handle Google sign-in (creates user if doesn't exist, logs in if exists)
	tokenResponse, err := h.service.GoogleOAuth.HandleGoogleSignIn(r.Context(), userInfo, googleToken)
	if err != nil {
		h.logger.Error("Google sign-in failed", map[string]interface{}{
			"error": err.Error(),
			"email": userInfo.Email,
			"ip":    r.RemoteAddr,
		})
		h.respondError(w, http.StatusInternalServerError, "Failed to complete sign-in: "+err.Error())
		return
	}

	h.logger.Info("Google OAuth login successful", map[string]interface{}{
		"event":   "google_oauth_login",
		"user_id": tokenResponse.User.ID.String(),
		"email":   userInfo.Email,
		"ip":      r.RemoteAddr,
	})

	// Audit log
	_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
		ID:        uuid.New(),
		UserID:    &tokenResponse.User.ID,
		Action:    "LOGIN_GOOGLE",
		Status:    "SUCCESS",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		CreatedAt: time.Now(),
	})

	// Set authentication cookies
	h.setAuthCookies(w, tokenResponse)

	// Return token response
	h.respondJSON(w, http.StatusOK, tokenResponse)
}

// GoogleMockLogin simulates a Google login for development/testing
func (h *AuthHandler) GoogleMockLogin(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		h.respondError(w, http.StatusBadRequest, "Missing state parameter")
		return
	}

	// For mock login, we redirect back to the frontend with a mock code
	// or directly to the callback if we want to skip the redirect
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3012"
	}

	// Create a mock user info
	mockUserInfo := &auth.GoogleUserInfo{
		ID:            "mock-google-id-" + uuid.NewString()[:8],
		Email:         "mock.user@example.com",
		VerifiedEmail: true,
		Name:          "Mock Google User",
		GivenName:     "Mock",
		FamilyName:    "User",
		Picture:       "https://ui-avatars.com/api/?name=Mock+User",
		Locale:        "en",
	}

	// In a real flow, this would be a redirect. For simplicity in mock,
	// we just handle the sign-in directly and redirect to dashboard.
	mockToken := &oauth2.Token{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
		Expiry:       time.Now().Add(1 * time.Hour),
	}
	tokenResponse, err := h.service.GoogleOAuth.HandleGoogleSignIn(r.Context(), mockUserInfo, mockToken)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to complete mock sign-in: "+err.Error())
		return
	}

	// Set authentication cookies
	h.setAuthCookies(w, tokenResponse)

	// Audit log
	_ = h.auditLogger.Create(r.Context(), &domain.AuditLog{
		ID:        uuid.New(),
		UserID:    &tokenResponse.User.ID,
		Action:    "LOGIN_GOOGLE_MOCK",
		Status:    "SUCCESS",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
		CreatedAt: time.Now(),
	})

	// Redirect to frontend callback with mock code
	http.Redirect(w, r, frontendURL+"/google-callback?code=mock-code&state="+state, http.StatusFound)
}

// generateRandomToken generates a cryptographically secure random token
func generateRandomToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
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
		h.logger.Error("Email verification failed", map[string]interface{}{
			"error": err.Error(),
			"ip":    r.RemoteAddr,
		})
		h.respondError(w, http.StatusBadRequest, "Verification failed: "+err.Error())
		return
	}

	h.logger.Info("Email verified", map[string]interface{}{
		"event": "email_verified",
		"ip":    r.RemoteAddr,
	})

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "email verified"})
}

// ForgotPasswordRequest captures the email for password reset request.
type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// ForgotPassword handles the initial request for a password reset.
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
		return
	}

	if err := h.service.RequestPasswordReset(r.Context(), req.Email); err != nil {
		h.logger.Error("Password reset request failed", map[string]interface{}{
			"error": err.Error(),
			"email": req.Email,
		})
		// Do not return error to prevent enumeration
	}

	h.respondJSON(w, http.StatusAccepted, map[string]string{"message": "If the email exists, a reset link has been sent"})
}

// ResetPasswordRequest captures the token and new password.
type ResetPasswordRequest struct {
	Token       string `json:"token" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8"`
}

// ResetPassword handles the password update using a reset token.
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if errs := h.validator.ValidateStructured(&req); errs != nil {
		h.respondValidationErrors(w, errs)
		return
	}

	if err := h.service.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		h.logger.Error("Password reset failed", map[string]interface{}{
			"error": err.Error(),
			"ip":    r.RemoteAddr,
		})
		h.respondError(w, http.StatusBadRequest, "Reset failed: "+err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
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
