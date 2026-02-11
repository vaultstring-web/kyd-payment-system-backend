package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"kyd/internal/compliance"
	"kyd/internal/middleware"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type ComplianceHandler struct {
	service *compliance.Service
	logger  logger.Logger
}

func NewComplianceHandler(service *compliance.Service, log logger.Logger) *ComplianceHandler {
	return &ComplianceHandler{
		service: service,
		logger:  log,
	}
}

func (h *ComplianceHandler) SubmitKYC(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
		h.respondError(w, http.StatusBadRequest, "File too large or invalid form")
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	docType := r.FormValue("document_type")
	docNumber := r.FormValue("document_number")
	issuingCountry := r.FormValue("issuing_country")

	if docType == "" || docNumber == "" || issuingCountry == "" {
		h.respondError(w, http.StatusBadRequest, "Missing required fields")
		return
	}

	// Handle file upload
	file, handler, err := r.FormFile("documents") // Frontend sends 'documents'
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Missing document file")
		return
	}
	defer file.Close()

	// Ensure upload directory exists
	uploadDir := "./uploads/kyc"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		h.logger.Error("Failed to create upload directory", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Generate filename
	ext := filepath.Ext(handler.Filename)
	filename := uuid.New().String() + ext
	filePath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(filePath)
	if err != nil {
		h.logger.Error("Failed to create file", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		h.logger.Error("Failed to save file", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Construct request
	// Note: For simplicity, we assume one file for Front Image.
	// In a real scenario, we might handle multiple files or map based on docType.
	// We'll use the same URL for all image fields for now or just Front.
	fileURL := "/uploads/kyc/" + filename

	req := &compliance.SubmitKYCRequest{
		UserID:         userID,
		DocumentType:   docType,
		DocumentNumber: docNumber,
		IssuingCountry: issuingCountry,
		FrontImageURL:  fileURL,
		// BackImageURL:   fileURL, // Optional
		// SelfieImageURL: fileURL, // Optional
	}

	doc, err := h.service.SubmitKYC(r.Context(), req)
	if err != nil {
		h.logger.Error("Failed to submit KYC", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to submit KYC")
		return
	}

	h.respondJSON(w, http.StatusCreated, doc)
}

func (h *ComplianceHandler) GetKYCStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	docs, err := h.service.GetKYCStatus(r.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get KYC status", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to get KYC status")
		return
	}

	h.respondJSON(w, http.StatusOK, docs)
}

func (h *ComplianceHandler) ListApplications(w http.ResponseWriter, r *http.Request) {
	// Admin Check
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
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
	status := r.URL.Query().Get("status")

	apps, total, err := h.service.ListApplications(r.Context(), status, limit, offset)
	if err != nil {
		h.logger.Error("Failed to list kyc applications", map[string]interface{}{"error": err.Error()})
		h.respondError(w, http.StatusInternalServerError, "Failed to list kyc applications")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"applications": apps,
		"total":        total,
		"limit":        limit,
		"offset":       offset,
	})
}

func (h *ComplianceHandler) ReviewApplication(w http.ResponseWriter, r *http.Request) {
	// Admin Check
	ut, ok := middleware.UserTypeFromContext(r.Context())
	if !ok || ut != "admin" {
		h.respondError(w, http.StatusForbidden, "admin access required")
		return
	}

	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Get Admin ID
	adminID, _ := middleware.UserIDFromContext(r.Context())

	if err := h.service.ReviewApplication(r.Context(), id, req.Status, req.Reason, adminID); err != nil {
		h.logger.Error("Failed to review kyc application", map[string]interface{}{"error": err.Error(), "user_id": id})
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "kyc status updated successfully"})
}

// Helpers

func (h *ComplianceHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("json encode failed", map[string]interface{}{"error": err.Error()})
	}
}

func (h *ComplianceHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
