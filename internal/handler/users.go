package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"kyd/internal/auth"
	"kyd/internal/domain"
	"kyd/internal/middleware"
	"kyd/pkg/validator"

	"strconv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

type UsersHandler struct {
	service   *auth.Service
	validator *validator.Validator
	logger    Logger
}

type Logger interface {
	Info(msg string, fields map[string]interface{})
	Warn(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
}

func NewUsersHandler(service *auth.Service, val *validator.Validator, log Logger) *UsersHandler {
	return &UsersHandler{service: service, validator: val, logger: log}
}

type listUsersResponse struct {
	Users []*domain.User `json:"users"`
	Total int            `json:"total"`
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
	userType := r.URL.Query().Get("type")
	users, total, err := h.service.ListUsers(r.Context(), limit, offset, userType)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list users")
		return
	}
	respondJSON(w, http.StatusOK, listUsersResponse{Users: users, Total: total})
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
	Email                *string           `json:"email" validate:"omitempty,email"`
	Phone                *string           `json:"phone" validate:"omitempty,phone_by_country"`
	FirstName            *string           `json:"first_name" validate:"omitempty,min=1,max=64"`
	LastName             *string           `json:"last_name" validate:"omitempty,min=1,max=64"`
	UserType             *domain.UserType  `json:"user_type"`
	KYCLevel             *int              `json:"kyc_level"`
	KYCStatus            *domain.KYCStatus `json:"kyc_status"`
	CountryCode          *string           `json:"country_code" validate:"omitempty,len=2"`
	BusinessName         *string           `json:"business_name" validate:"omitempty,max=128"`
	BusinessRegistration *string           `json:"business_registration" validate:"omitempty,max=128"`
	IsActive             *bool             `json:"is_active"`
	Password             *string           `json:"password" validate:"omitempty,min=8"`
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
