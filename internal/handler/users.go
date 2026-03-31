package handler

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"

	"kyd/internal/auth"
	"kyd/internal/middleware"
	"kyd/internal/payment"
	"kyd/internal/security"
	"kyd/internal/wallet"
	"kyd/pkg/domain"
	"kyd/pkg/validator"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

type UsersHandler struct {
	service     *auth.Service
	validator   *validator.Validator
	logger      Logger
	auditRepo   AuditRepository
	walletSvc   *wallet.Service
	paymentSvc  *payment.Service
	securitySvc *security.Service
}

type AuditRepository interface {
	Create(ctx context.Context, log *domain.AuditLog) error
	FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error)
	FindAll(ctx context.Context, limit, offset int) ([]*domain.AuditLog, error)
	CountAll(ctx context.Context) (int, error)
}

type Logger interface {
	Info(msg string, fields map[string]interface{})
	Warn(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
}

func NewUsersHandler(service *auth.Service, val *validator.Validator, log Logger, audit AuditRepository, walletSvc *wallet.Service, paymentSvc *payment.Service, securitySvc *security.Service) *UsersHandler {
	return &UsersHandler{
		service:     service,
		validator:   val,
		logger:      log,
		auditRepo:   audit,
		walletSvc:   walletSvc,
		paymentSvc:  paymentSvc,
		securitySvc: securitySvc,
	}
}

type listUsersResponse struct {
	Users  []*domain.User `json:"users"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit,omitempty"`
	Offset int            `json:"offset,omitempty"`
	Items  []*domain.User `json:"items,omitempty"`
}

func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 && n <= 200 {
			limit = int(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			offset = int(n)
		}
	}
	qp := r.URL.Query()
	// Backwards compatible: accept both `type` and `user_type`
	userType := qp.Get("user_type")
	if userType == "" {
		userType = qp.Get("type")
	}
	kycStatus := qp.Get("kyc_status")

	users, total, err := h.service.ListUsersAdmin(r.Context(), limit, offset, userType, kycStatus)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list users")
		return
	}
	respondJSON(w, http.StatusOK, listUsersResponse{
		Users:  users,
		Items:  users,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (h *UsersHandler) Get(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}
	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}
	user, err := h.service.GetUserByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

type updateUserRequest struct {
	Email                *string            `json:"email" validate:"omitempty,email"`
	Phone                *string            `json:"phone" validate:"omitempty,phone_by_country"`
	FirstName            *string            `json:"first_name" validate:"omitempty,min=1,max=64"`
	LastName             *string            `json:"last_name" validate:"omitempty,min=1,max=64"`
	UserType             *domain.UserType   `json:"user_type"`
	KYCLevel             *int               `json:"kyc_level"`
	KYCStatus            *domain.KYCStatus  `json:"kyc_status"`
	CountryCode          *string            `json:"country_code" validate:"omitempty,len=2"`
	BusinessName         *string            `json:"business_name" validate:"omitempty,max=128"`
	BusinessRegistration *string            `json:"business_registration" validate:"omitempty,max=128"`
	IsActive             *bool              `json:"is_active"`
	UserStatus           *domain.UserStatus `json:"user_status"`
	Password             *string            `json:"password" validate:"omitempty,min=8"`
	Bio                  *string            `json:"bio" validate:"omitempty,max=500"`
	City                 *string            `json:"city" validate:"omitempty,max=100"`
	PostalCode           *string            `json:"postal_code" validate:"omitempty,max=20"`
	TaxID                *string            `json:"tax_id" validate:"omitempty,max=50"`
}

func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}
	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}
	user, err := h.service.GetUserByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}
	var req updateUserRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if errs := h.validator.ValidateStructured(&req); errs != nil {
		respondValidationErrors(w, errs)
		return
	}
	// Apply updates
	now := time.Now()
	user.UpdatedAt = now
	if req.Email != nil {
		user.Email = *req.Email
	}
	if req.Phone != nil {
		user.Phone = *req.Phone
	}
	if req.FirstName != nil {
		user.FirstName = *req.FirstName
	}
	if req.LastName != nil {
		user.LastName = *req.LastName
	}
	if req.UserType != nil {
		user.UserType = *req.UserType
	}
	if req.KYCLevel != nil {
		user.KYCLevel = *req.KYCLevel
	}
	if req.KYCStatus != nil {
		user.KYCStatus = *req.KYCStatus
	}
	if req.CountryCode != nil {
		user.CountryCode = *req.CountryCode
	}
	if req.BusinessName != nil {
		user.BusinessName = req.BusinessName
	}
	if req.BusinessRegistration != nil {
		user.BusinessRegistration = req.BusinessRegistration
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	if req.UserStatus != nil {
		user.UserStatus = *req.UserStatus
	}
	if req.Bio != nil {
		user.Bio = *req.Bio
	}
	if req.City != nil {
		user.City = *req.City
	}
	if req.PostalCode != nil {
		user.PostalCode = *req.PostalCode
	}
	if req.TaxID != nil {
		user.TaxID = *req.TaxID
	}
	// Sanitize user fields
	auth.SanitizeUserInput(user)
	// Handle password update
	if req.Password != nil && *req.Password != "" {
		if err := h.service.ChangePassword(r.Context(), user, *req.Password); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		if err := h.service.UpdateUser(r.Context(), user); err != nil {
			h.logger.Error("Failed to update user", map[string]interface{}{
				"user_id": user.ID,
				"error":   err.Error(),
			})
			respondError(w, http.StatusInternalServerError, "Failed to update user")
			return
		}
	}
	respondJSON(w, http.StatusOK, user)
}

type updateMeRequest struct {
	Phone       *string `json:"phone" validate:"omitempty,phone_by_country"`
	FirstName   *string `json:"first_name" validate:"omitempty,min=1,max=64"`
	LastName    *string `json:"last_name" validate:"omitempty,min=1,max=64"`
	CountryCode *string `json:"country_code" validate:"omitempty,len=2"`
	Bio         *string `json:"bio" validate:"omitempty,max=500"`
	City        *string `json:"city" validate:"omitempty,max=100"`
	PostalCode  *string `json:"postal_code" validate:"omitempty,max=20"`
	TaxID       *string `json:"tax_id" validate:"omitempty,max=50"`
}

func (h *UsersHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	user, err := h.service.GetUserByID(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}
	var req updateMeRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if errs := h.validator.ValidateStructured(&req); errs != nil {
		respondValidationErrors(w, errs)
		return
	}
	if req.Phone != nil {
		user.Phone = *req.Phone
	}
	if req.FirstName != nil {
		user.FirstName = *req.FirstName
	}
	if req.LastName != nil {
		user.LastName = *req.LastName
	}
	if req.CountryCode != nil {
		user.CountryCode = *req.CountryCode
	}
	if req.Bio != nil {
		user.Bio = *req.Bio
	}
	if req.City != nil {
		user.City = *req.City
	}
	if req.PostalCode != nil {
		user.PostalCode = *req.PostalCode
	}
	if req.TaxID != nil {
		user.TaxID = *req.TaxID
	}
	auth.SanitizeUserInput(user)
	user.UpdatedAt = time.Now()
	if err := h.service.UpdateUser(r.Context(), user); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update profile")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

type changePasswordRequest struct {
	Current string `json:"current_password" validate:"required"`
	New     string `json:"new_password" validate:"required,min=8"`
}

func (h *UsersHandler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	user, err := h.service.GetUserByID(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}
	var req changePasswordRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if errs := h.validator.ValidateStructured(&req); errs != nil {
		respondValidationErrors(w, errs)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Current)); err != nil {
		respondError(w, http.StatusUnauthorized, "Current password is incorrect")
		return
	}
	if err := h.service.ChangePassword(r.Context(), user, req.New); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}

func (h *UsersHandler) BlockUser(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.service.GetUserByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}

	// Parse optional reason from request body
	var reason string
	if r.Body != nil && r.ContentLength > 0 {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		reason = body.Reason
	}

	// Block the user
	user.UserStatus = domain.UserStatusBlocked
	user.IsActive = false
	user.UpdatedAt = time.Now()

	if err := h.service.UpdateUser(r.Context(), user); err != nil {
		h.logger.Error("Failed to block user", map[string]interface{}{
			"user_id": user.ID,
			"error":   err.Error(),
		})
		respondError(w, http.StatusInternalServerError, "Failed to block user")
		return
	}

	// Audit trail: record admin action with timestamp
	if actorID, ok := middleware.UserIDFromContext(r.Context()); ok {
		h.writeAdminAudit(r.Context(), r, "USER_BLOCKED", id, actorID, reason, nil)
	}

	h.logger.Info("User blocked", map[string]interface{}{
		"user_id":    user.ID,
		"blocked_by": func() string { uid, _ := middleware.UserIDFromContext(r.Context()); return uid.String() }(),
	})

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "user_blocked",
		"user_id": user.ID.String(),
	})
}

func (h *UsersHandler) UnblockUser(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.service.GetUserByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}

	// Parse optional reason from request body
	var reason string
	if r.Body != nil && r.ContentLength > 0 {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		reason = body.Reason
	}

	// Unblock the user
	user.UserStatus = domain.UserStatusActive
	user.IsActive = true
	user.UpdatedAt = time.Now()

	if err := h.service.UpdateUser(r.Context(), user); err != nil {
		h.logger.Error("Failed to unblock user", map[string]interface{}{
			"user_id": user.ID,
			"error":   err.Error(),
		})
		respondError(w, http.StatusInternalServerError, "Failed to unblock user")
		return
	}

	// Audit trail: record admin action with timestamp
	if actorID, ok := middleware.UserIDFromContext(r.Context()); ok {
		h.writeAdminAudit(r.Context(), r, "USER_UNBLOCKED", id, actorID, reason, nil)
	}

	h.logger.Info("User unblocked", map[string]interface{}{
		"user_id":      user.ID,
		"unblocked_by": func() string { uid, _ := middleware.UserIDFromContext(r.Context()); return uid.String() }(),
	})

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "user_unblocked",
		"user_id": user.ID.String(),
	})
}

func (h *UsersHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.service.GetUserByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}

	// Perform soft delete by setting UserStatus to Deleted and IsActive to false
	user.UserStatus = domain.UserStatusDeleted
	user.IsActive = false
	user.UpdatedAt = time.Now()

	if err := h.service.UpdateUser(r.Context(), user); err != nil {
		h.logger.Error("Failed to delete user", map[string]interface{}{
			"user_id": user.ID,
			"error":   err.Error(),
		})
		respondError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	h.logger.Info("User soft-deleted", map[string]interface{}{
		"user_id":    user.ID,
		"deleted_by": func() string { uid, _ := middleware.UserIDFromContext(r.Context()); return uid.String() }(),
	})

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "user_soft_deleted",
		"user_id": user.ID.String(),
	})
}

// writeAdminAudit records an admin action (block/unblock/delete) in the audit trail.
// targetUserID is the user who was acted upon; actorID is the admin who performed the action.
func (h *UsersHandler) writeAdminAudit(ctx context.Context, r *http.Request, action string, targetUserID uuid.UUID, actorID uuid.UUID, reason string, extra map[string]interface{}) {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	ua := r.UserAgent()

	newVals := map[string]interface{}{
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"target_user_id": targetUserID.String(),
	}
	if reason != "" {
		newVals["reason"] = reason
	}
	for k, v := range extra {
		newVals[k] = v
	}
	newValBytes, _ := json.Marshal(newVals)

	auditLog := &domain.AuditLog{
		ID:         uuid.New(),
		UserID:     &actorID,
		Action:     action,
		EntityType: "user",
		EntityID:   targetUserID.String(),
		IPAddress:  ip,
		UserAgent:  ua,
		StatusCode: 200,
		NewValues:  newValBytes,
		CreatedAt:  time.Now(),
	}
	if err := h.auditRepo.Create(ctx, auditLog); err != nil {
		h.logger.Error("Failed to write audit log", map[string]interface{}{"action": action, "error": err.Error()})
	}
}

func (h *UsersHandler) GetActivity(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	logs, err := h.auditRepo.FindByUserID(r.Context(), id, limit, offset)
	if err != nil {
		h.logger.Error("Failed to fetch user activity", map[string]interface{}{
			"user_id": id,
			"error":   err.Error(),
		})
		respondError(w, http.StatusInternalServerError, "Failed to fetch activity logs")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   logs,
		"items":  logs,
		"total":  len(logs),
		"limit":  limit,
		"offset": offset,
	})
}

func (h *UsersHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != string(domain.UserTypeAdmin) {
		respondError(w, http.StatusForbidden, "Forbidden")
		return
	}

	idStr := mux.Vars(r)["id"]
	userID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.service.GetUserByID(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusNotFound, "User not found")
		return
	}

	// Wallets (balances)
	wallets := []*wallet.BalanceResponse{}
	if h.walletSvc != nil {
		if ws, _, err := h.walletSvc.GetWalletsWithFilter(r.Context(), 1000, 0, &userID); err == nil {
			wallets = ws
		}
	}

	// Recent transactions
	recentTxs := []*payment.TransactionDetail{}
	if h.paymentSvc != nil {
		// Reuse payment service user transaction list; no wallet filter here.
		// Fetch most recent 50 for admin overview.
		if txs, _, err := h.paymentSvc.GetUserTransactions(r.Context(), userID, nil, 50, 0); err == nil {
			recentTxs = txs
		}
	}

	// Recent audit logs
	audit := []*domain.AuditLog{}
	if h.auditRepo != nil {
		if logs, err := h.auditRepo.FindByUserID(r.Context(), userID, 50, 0); err == nil {
			audit = logs
		}
	}

	// Recent security events for this user
	securityEvents := []domain.SecurityEvent{}
	if h.securitySvc != nil {
		filter := &security.EventFilter{UserID: &userID}
		if events, _, err := h.securitySvc.GetSecurityEvents(r.Context(), filter, 50, 0); err == nil {
			securityEvents = events
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"user": user,
		"wallets": map[string]interface{}{
			"items":  wallets,
			"total":  len(wallets),
			"limit":  1000,
			"offset": 0,
		},
		"transactions": map[string]interface{}{
			"items":  recentTxs,
			"total":  len(recentTxs),
			"limit":  50,
			"offset": 0,
		},
		"audit_logs": map[string]interface{}{
			"items":  audit,
			"total":  len(audit),
			"limit":  50,
			"offset": 0,
		},
		"security_events": map[string]interface{}{
			"items":  securityEvents,
			"total":  len(securityEvents),
			"limit":  50,
			"offset": 0,
		},
		"risk": map[string]interface{}{
			"risk_score":            user.RiskScore,
			"kyc_level":             user.KYCLevel,
			"kyc_status":            user.KYCStatus,
			"is_active":             user.IsActive,
			"last_login":            user.LastLogin,
			"failed_login_attempts": user.FailedLoginAttempts,
		},
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

func respondValidationErrors(w http.ResponseWriter, errors map[string]string) {
	respondJSON(w, http.StatusBadRequest, map[string]interface{}{
		"error":             "Validation failed",
		"validation_errors": errors,
	})
}
