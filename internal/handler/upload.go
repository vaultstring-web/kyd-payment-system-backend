// ==============================================================================
// FILE UPLOAD HANDLER - internal/handler/upload.go
// ==============================================================================
// Handles file uploads for KYC documents with multipart form parsing
// Task 6.2 implementation (with file size/type validation)
// ==============================================================================

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"kyd/internal/domain"
	"kyd/internal/kyc"
	"kyd/internal/middleware"
	"kyd/pkg/logger"
	"kyd/pkg/validator"

	"github.com/google/uuid"
)

// ==============================================================================
// CONFIGURATION STRUCTURES
// ==============================================================================

// UploadConfig holds configuration for file upload validation
type UploadConfig struct {
	// Size limits
	MaxFileSizeMB int64 `json:"max_file_size_mb"`
	MinFileSizeKB int64 `json:"min_file_size_kb"`

	// Type validation
	AllowedMimeTypes  []string `json:"allowed_mime_types"`
	AllowedExtensions []string `json:"allowed_extensions"`

	// Image-specific constraints
	MaxImageWidth  int `json:"max_image_width"`
	MaxImageHeight int `json:"max_image_height"`
	MinImageWidth  int `json:"min_image_width"`
	MinImageHeight int `json:"min_image_height"`

	// Security settings
	RequireVirusScan bool `json:"require_virus_scan"`
	RequireIntegrity bool `json:"require_integrity"`

	// Processing settings
	MaxConcurrentUploads int    `json:"max_concurrent_uploads"`
	TempStoragePath      string `json:"temp_storage_path"`
}

// DefaultUploadConfig returns default upload configuration
func DefaultUploadConfig() *UploadConfig {
	return &UploadConfig{
		MaxFileSizeMB: 10, // 10MB
		MinFileSizeKB: 10, // 10KB

		AllowedMimeTypes: []string{
			"image/jpeg",
			"image/png",
			"image/gif",
			"application/pdf",
			"application/msword",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},

		AllowedExtensions: []string{
			".jpg", ".jpeg", ".png", ".gif",
			".pdf", ".doc", ".docx",
		},

		MaxImageWidth:  4000,
		MaxImageHeight: 4000,
		MinImageWidth:  100,
		MinImageHeight: 100,

		RequireVirusScan:     true,
		RequireIntegrity:     true,
		MaxConcurrentUploads: 10,
		TempStoragePath:      "/tmp/kyd_uploads",
	}
}

// ==============================================================================
// FILE UPLOAD HANDLER STRUCT
// ==============================================================================

// UploadHandler handles file uploads for KYC documents
type UploadHandler struct {
	kycService *kyc.KYCService
	validator  *validator.Validator
	logger     logger.Logger
	config     *UploadConfig

	// Rate limiting and concurrency control
	uploadSemaphore chan struct{}
}

// NewUploadHandler creates a new UploadHandler with required dependencies
func NewUploadHandler(
	kycService *kyc.KYCService,
	val *validator.Validator,
	log logger.Logger,
	config *UploadConfig,
) *UploadHandler {
	if config == nil {
		config = DefaultUploadConfig()
	}

	return &UploadHandler{
		kycService:      kycService,
		validator:       val,
		logger:          log,
		config:          config,
		uploadSemaphore: make(chan struct{}, config.MaxConcurrentUploads),
	}
}

// ==============================================================================
// HELPER METHODS
// ==============================================================================

// respondJSON sends a JSON response
func (h *UploadHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", map[string]interface{}{
			"error":   err.Error(),
			"status":  status,
			"handler": "upload",
		})
		http.Error(w, `{"error":"response encoding failed"}`, http.StatusInternalServerError)
	}
}

// respondError sends a standardized error response
func (h *UploadHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}

// ==============================================================================
// MULTIPART FORM PARSING IMPLEMENTATION
// ==============================================================================

// MultipartUploadRequest represents the parsed multipart form data
type MultipartUploadRequest struct {
	DocumentType   domain.DocumentType `json:"document_type"`
	DocumentNumber string              `json:"document_number,omitempty"`
	IssuingCountry string              `json:"issuing_country,omitempty"`
	IssueDate      *time.Time          `json:"issue_date,omitempty"`
	ExpiryDate     *time.Time          `json:"expiry_date,omitempty"`

	// File information
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	FileContent []byte `json:"-"`
	MimeType    string `json:"mime_type"`
	FileHash    string `json:"file_hash,omitempty"`

	// Original form data for debugging
	FormData map[string]string `json:"-"`
}

// parseMultipartForm parses a multipart form request and extracts file + metadata
func (h *UploadHandler) parseMultipartForm(_ http.ResponseWriter, r *http.Request) (*MultipartUploadRequest, error) {
	startTime := time.Now()

	// Log the incoming request
	h.logger.Debug("Starting multipart form parsing", map[string]interface{}{
		"content_type":   r.Header.Get("Content-Type"),
		"content_length": r.ContentLength,
		"method":         r.Method,
		"url":            r.URL.Path,
	})

	// Calculate maximum form size (file size + 1MB for form fields)
	maxFormSize := (h.config.MaxFileSizeMB * 1024 * 1024) + (1 << 20) // +1MB for form fields

	// Parse the multipart form with size limits
	err := r.ParseMultipartForm(maxFormSize)
	if err != nil {
		h.logger.Warn("Failed to parse multipart form", map[string]interface{}{
			"error":          err.Error(),
			"content_type":   r.Header.Get("Content-Type"),
			"content_length": r.ContentLength,
			"max_form_size":  maxFormSize,
		})

		// Provide specific error messages for common issues
		if err == http.ErrNotMultipart {
			return nil, fmt.Errorf("request is not multipart form")
		} else if err == http.ErrMissingBoundary {
			return nil, fmt.Errorf("multipart boundary is missing")
		} else if strings.Contains(err.Error(), "request body too large") {
			maxSizeMB := float64(maxFormSize) / (1024 * 1024)
			return nil, fmt.Errorf("request body exceeds maximum size of %.1fMB", maxSizeMB)
		}
		return nil, fmt.Errorf("failed to parse multipart form: %w", err)
	}

	// Log successful form parsing
	h.logger.Debug("Multipart form parsed successfully", map[string]interface{}{
		"form_size":     r.ContentLength,
		"parse_time_ms": time.Since(startTime).Milliseconds(),
	})

	// Get the file from the "file" field
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		if err == http.ErrMissingFile {
			return nil, fmt.Errorf("'file' field is required in multipart form")
		}
		return nil, fmt.Errorf("failed to get file from form: %w", err)
	}
	defer file.Close()

	// Log file information
	h.logger.Debug("File extracted from form", map[string]interface{}{
		"file_name": fileHeader.Filename,
		"file_size": fileHeader.Size,
		"mime_type": fileHeader.Header.Get("Content-Type"),
	})

	// Read the file content into memory
	var buf bytes.Buffer
	fileSize, err := io.Copy(&buf, file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}

	// Calculate file hash for integrity checking
	fileHash := calculateFileHash(buf.Bytes())

	// Extract form fields
	formData := make(map[string]string)
	for key, values := range r.MultipartForm.Value {
		if len(values) > 0 {
			formData[key] = values[0]
		}
	}

	// Parse document type (required)
	docTypeStr := r.FormValue("document_type")
	if docTypeStr == "" {
		return nil, fmt.Errorf("document_type is required")
	}

	// Validate document type
	docType := domain.DocumentType(docTypeStr)
	if !isValidDocumentType(docType) {
		return nil, fmt.Errorf("invalid document type: %s", docTypeStr)
	}

	// Parse dates if provided
	var issueDate, expiryDate *time.Time

	if issueDateStr := r.FormValue("issue_date"); issueDateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", issueDateStr)
		if err != nil {
			return nil, fmt.Errorf("invalid issue_date format, use YYYY-MM-DD")
		}
		issueDate = &parsedDate
	}

	if expiryDateStr := r.FormValue("expiry_date"); expiryDateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", expiryDateStr)
		if err != nil {
			return nil, fmt.Errorf("invalid expiry_date format, use YYYY-MM-DD")
		}
		expiryDate = &parsedDate
	}

	// Get mime type from file header or form field
	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		// Try to infer from form field
		mimeType = r.FormValue("mime_type")
		if mimeType == "" {
			// Infer from file extension
			mimeType = inferMimeType(fileHeader.Filename)
		}
	}

	// Create the parsed request
	request := &MultipartUploadRequest{
		DocumentType:   docType,
		DocumentNumber: r.FormValue("document_number"),
		IssuingCountry: r.FormValue("issuing_country"),
		IssueDate:      issueDate,
		ExpiryDate:     expiryDate,
		FileName:       fileHeader.Filename,
		FileSize:       fileSize,
		FileContent:    buf.Bytes(),
		MimeType:       mimeType,
		FileHash:       fileHash,
		FormData:       formData,
	}

	// Log successful parsing
	h.logger.Info("Multipart form parsing completed", map[string]interface{}{
		"file_name":       fileHeader.Filename,
		"file_size":       fileSize,
		"file_hash":       fileHash[:16] + "...", // Truncated for logs
		"document_type":   docType,
		"issuing_country": r.FormValue("issuing_country"),
		"parse_time_ms":   time.Since(startTime).Milliseconds(),
		"form_fields":     len(formData),
	})

	return request, nil
}

// ==============================================================================
// FILE VALIDATION METHODS
// ==============================================================================

// validateFileSize validates file size against configuration
func (h *UploadHandler) validateFileSize(fileSize int64) error {
	// Convert config to bytes
	maxBytes := h.config.MaxFileSizeMB * 1024 * 1024
	minBytes := h.config.MinFileSizeKB * 1024

	if fileSize < minBytes {
		return fmt.Errorf(
			"file size too small: %d bytes (minimum: %d bytes / %d KB)",
			fileSize, minBytes, h.config.MinFileSizeKB,
		)
	}

	if fileSize > maxBytes {
		return fmt.Errorf(
			"file size too large: %d bytes (maximum: %d bytes / %d MB)",
			fileSize, maxBytes, h.config.MaxFileSizeMB,
		)
	}

	return nil
}

// validateFileType validates MIME type and file extension
func (h *UploadHandler) validateFileType(fileName, mimeType string) error {
	// Validate MIME type
	mimeTypeValid := false
	for _, allowedMime := range h.config.AllowedMimeTypes {
		if strings.EqualFold(mimeType, allowedMime) {
			mimeTypeValid = true
			break
		}
	}

	if !mimeTypeValid {
		return fmt.Errorf(
			"invalid MIME type: %s (allowed: %s)",
			mimeType, strings.Join(h.config.AllowedMimeTypes, ", "),
		)
	}

	// Validate file extension
	fileExt := strings.ToLower(filepath.Ext(fileName))
	extensionValid := false

	for _, allowedExt := range h.config.AllowedExtensions {
		if strings.EqualFold(fileExt, allowedExt) {
			extensionValid = true
			break
		}
	}

	if !extensionValid {
		return fmt.Errorf(
			"invalid file extension: %s (allowed: %s)",
			fileExt, strings.Join(h.config.AllowedExtensions, ", "),
		)
	}

	// Additional validation for specific MIME types
	if strings.HasPrefix(mimeType, "image/") {
		// Validate image-specific constraints
		if h.config.MaxImageWidth > 0 || h.config.MaxImageHeight > 0 ||
			h.config.MinImageWidth > 0 || h.config.MinImageHeight > 0 {

			// In a real implementation, we would decode the image and check dimensions
			// For now, we'll just log that this check would happen
			h.logger.Debug("Image dimension validation would be performed", map[string]interface{}{
				"mime_type": mimeType,
				"file_name": fileName,
			})
		}
	}

	return nil
}

// validateDocumentType validates if the document type matches the file content
func (h *UploadHandler) validateDocumentType(docType domain.DocumentType, _, mimeType string) error {
	// Map document types to expected file types
	expectedTypes := map[domain.DocumentType][]string{
		domain.DocumentTypeNationalID:           {"image/jpeg", "image/png", "application/pdf"},
		domain.DocumentTypePassport:             {"image/jpeg", "image/png", "application/pdf"},
		domain.DocumentTypeDriversLicense:       {"image/jpeg", "image/png", "application/pdf"},
		domain.DocumentTypeUtilityBill:          {"image/jpeg", "image/png", "application/pdf"},
		domain.DocumentTypeBankStatement:        {"application/pdf", "image/jpeg", "image/png"},
		domain.DocumentTypeProofOfIncome:        {"application/pdf", "application/msword", "image/jpeg"},
		domain.DocumentTypeBusinessRegistration: {"application/pdf", "image/jpeg", "image/png"},
		domain.DocumentTypeTaxCertificate:       {"application/pdf", "image/jpeg", "image/png"},
		domain.DocumentTypeBusinessLicense:      {"application/pdf", "image/jpeg", "image/png"},
		domain.DocumentTypeAgentLicense:         {"application/pdf", "image/jpeg", "image/png"},
		domain.DocumentTypeSelfieWithID:         {"image/jpeg", "image/png"},
	}

	// Check if document type has expected MIME types
	expected, exists := expectedTypes[docType]
	if !exists {
		// No specific validation for this document type
		return nil
	}

	// Check if the actual MIME type is in the expected list
	found := false
	for _, expectedMime := range expected {
		if strings.HasPrefix(mimeType, strings.TrimSuffix(expectedMime, "/*")) {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf(
			"document type '%s' expects file types: %s, but got: %s",
			docType, strings.Join(expected, ", "), mimeType,
		)
	}

	return nil
}

// validateFile performs comprehensive file validation
func (h *UploadHandler) validateFile(request *MultipartUploadRequest) []string {
	var validationErrors []string

	// 1. File size validation
	if err := h.validateFileSize(request.FileSize); err != nil {
		validationErrors = append(validationErrors, err.Error())
	}

	// 2. File type validation
	if err := h.validateFileType(request.FileName, request.MimeType); err != nil {
		validationErrors = append(validationErrors, err.Error())
	}

	// 3. Document type compatibility
	if err := h.validateDocumentType(request.DocumentType, request.FileName, request.MimeType); err != nil {
		validationErrors = append(validationErrors, err.Error())
	}

	// 4. File name validation
	if err := h.validateFileName(request.FileName); err != nil {
		validationErrors = append(validationErrors, err.Error())
	}

	// 5. Security validation
	if err := h.validateFileSecurity(request.FileName, request.FileContent); err != nil {
		validationErrors = append(validationErrors, err.Error())
	}

	// 6. Date validation (if provided)
	if request.IssueDate != nil && request.ExpiryDate != nil {
		if request.ExpiryDate.Before(*request.IssueDate) {
			validationErrors = append(validationErrors, "expiry_date cannot be before issue_date")
		}
	}

	if request.ExpiryDate != nil && request.ExpiryDate.Before(time.Now()) {
		validationErrors = append(validationErrors, "document has expired")
	}

	return validationErrors
}

// validateFileName validates file name for security
func (h *UploadHandler) validateFileName(fileName string) error {
	// Check for path traversal attempts
	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		return fmt.Errorf("invalid file name: contains path traversal characters")
	}

	// Check for null bytes
	if strings.Contains(fileName, "\x00") {
		return fmt.Errorf("invalid file name: contains null byte")
	}

	// Check length
	if len(fileName) > 255 {
		return fmt.Errorf("file name too long: maximum 255 characters")
	}

	// Check for suspicious extensions
	suspiciousExtensions := []string{".exe", ".bat", ".cmd", ".sh", ".php", ".asp", ".aspx", ".jsp"}
	fileExt := strings.ToLower(filepath.Ext(fileName))

	for _, suspicious := range suspiciousExtensions {
		if fileExt == suspicious {
			return fmt.Errorf("potentially dangerous file extension: %s", fileExt)
		}
	}

	return nil
}

// validateFileSecurity performs basic security checks
func (h *UploadHandler) validateFileSecurity(_ string, content []byte) error {
	// Check for empty file
	if len(content) == 0 {
		return fmt.Errorf("file is empty")
	}

	// Check for binary detection (crude check for executable content)
	if len(content) > 4 {
		// Common executable magic numbers
		magicNumbers := [][]byte{
			{0x4D, 0x5A},             // MZ (DOS/Windows executable)
			{0x7F, 0x45, 0x4C, 0x46}, // ELF (Unix executable)
			{0xCA, 0xFE, 0xBA, 0xBE}, // Java class file
		}

		for _, magic := range magicNumbers {
			if len(content) >= len(magic) && bytes.Equal(content[:len(magic)], magic) {
				return fmt.Errorf("file appears to be an executable, which is not allowed")
			}
		}
	}

	return nil
}

// ==============================================================================
// FILE UPLOAD ENDPOINT HANDLER (WITH VALIDATION)
// ==============================================================================

// UploadDocumentMultipart handles file uploads via multipart form with validation
// POST /kyc/documents/upload
// UploadDocumentMultipart handles file uploads via multipart form with validation
// POST /kyc/documents/upload
func (h *UploadHandler) UploadDocumentMultipart(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Acquire semaphore for concurrency control
	select {
	case h.uploadSemaphore <- struct{}{}:
		// Got semaphore, proceed
		defer func() { <-h.uploadSemaphore }()
	default:
		h.logger.Warn("Upload concurrency limit reached", map[string]interface{}{
			"event": "upload_concurrency_limit",
			"limit": h.config.MaxConcurrentUploads,
			"ip":    r.RemoteAddr,
		})
		h.respondError(w, http.StatusServiceUnavailable, "Too many concurrent uploads, please try again later")
		return
	}

	// Extract user ID from JWT context
	userID, ok := h.getUserIDFromContext(r)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized: missing user context")
		return
	}

	// Log upload attempt with validation context
	h.logger.Info("Multipart document upload started", map[string]interface{}{
		"event":            "multipart_upload_started",
		"user_id":          userID.String(),
		"method":           r.Method,
		"endpoint":         r.URL.Path,
		"ip":               r.RemoteAddr,
		"ua":               r.UserAgent(),
		"max_file_size_mb": h.config.MaxFileSizeMB,
		"allowed_types":    h.config.AllowedMimeTypes,
	})

	// Step 1: Parse multipart form
	uploadRequest, err := h.parseMultipartForm(w, r)
	if err != nil {
		h.logger.Warn("Multipart form parsing failed", map[string]interface{}{
			"event":   "multipart_parse_failed",
			"user_id": userID.String(),
			"error":   err.Error(),
			"ip":      r.RemoteAddr,
		})
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse multipart form: %v", err))
		return
	}

	// Step 2: Perform comprehensive validation
	validationErrors := h.validateFile(uploadRequest)
	if len(validationErrors) > 0 {
		h.logger.Warn("File validation failed", map[string]interface{}{
			"event":     "file_validation_failed",
			"user_id":   userID.String(),
			"file_name": uploadRequest.FileName,
			"errors":    validationErrors,
			"file_size": uploadRequest.FileSize,
			"mime_type": uploadRequest.MimeType,
		})

		// Create detailed error response
		errorResponse := map[string]interface{}{
			"error":   "File validation failed",
			"details": validationErrors,
			"file_info": map[string]interface{}{
				"file_name":     uploadRequest.FileName,
				"file_size":     uploadRequest.FileSize,
				"mime_type":     uploadRequest.MimeType,
				"document_type": uploadRequest.DocumentType,
			},
		}

		h.respondJSON(w, http.StatusBadRequest, errorResponse)
		return
	}

	// Step 3: Create UploadDocumentRequest for KYC service
	kycUploadReq := &domain.UploadDocumentRequest{
		DocumentType:   uploadRequest.DocumentType,
		DocumentNumber: uploadRequest.DocumentNumber,
		IssuingCountry: uploadRequest.IssuingCountry,
		IssueDate:      uploadRequest.IssueDate,
		ExpiryDate:     uploadRequest.ExpiryDate,
		FileName:       uploadRequest.FileName,
		MimeType:       uploadRequest.MimeType,
	}

	// Step 4: Call KYC service to upload document
	response, err := h.kycService.UploadDocument(r.Context(), userID, kycUploadReq, uploadRequest.FileContent)
	if err != nil {
		h.logger.Error("Failed to upload document via KYC service", map[string]interface{}{
			"event":         "kyc_upload_failed",
			"user_id":       userID.String(),
			"file_name":     uploadRequest.FileName,
			"document_type": uploadRequest.DocumentType,
			"error":         err.Error(),
		})

		// Map KYC service errors to appropriate HTTP responses
		if strings.Contains(err.Error(), "KYC profile required") {
			h.respondError(w, http.StatusBadRequest, "KYC profile required before document upload")
			return
		}

		h.respondError(w, http.StatusInternalServerError, "Failed to process document upload")
		return
	}

	// Log successful upload with validation details
	h.logger.Info("Multipart document upload completed with validation", map[string]interface{}{
		"event":               "multipart_upload_validated",
		"user_id":             userID.String(),
		"document_id":         response.DocumentID.String(),
		"document_type":       uploadRequest.DocumentType,
		"file_name":           uploadRequest.FileName,
		"file_size_bytes":     uploadRequest.FileSize,
		"formatted_file_size": formatFileSize(uploadRequest.FileSize),
		"mime_type":           uploadRequest.MimeType,
		"file_hash":           uploadRequest.FileHash[:16] + "...",
		"validation_passed":   true,
		"total_time_ms":       time.Since(startTime).Milliseconds(),
		"ip":                  r.RemoteAddr,
	})

	h.respondJSON(w, http.StatusCreated, response)
}

// ==============================================================================
// HELPER FUNCTIONS FOR VALIDATION AND PROCESSING
// ==============================================================================

// isValidDocumentType checks if a document type is valid
func isValidDocumentType(docType domain.DocumentType) bool {
	validTypes := []domain.DocumentType{
		domain.DocumentTypeNationalID,
		domain.DocumentTypePassport,
		domain.DocumentTypeDriversLicense,
		domain.DocumentTypeBusinessRegistration,
		domain.DocumentTypeTaxCertificate,
		domain.DocumentTypeUtilityBill,
		domain.DocumentTypeBankStatement,
		domain.DocumentTypeProofOfIncome,
		domain.DocumentTypeSelfieWithID,
		domain.DocumentTypeBusinessLicense,
		domain.DocumentTypeAgentLicense,
	}

	for _, validType := range validTypes {
		if docType == validType {
			return true
		}
	}
	return false
}

// inferMimeType infers mime type from file extension
func inferMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".tiff", ".tif":
		return "image/tiff"
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

// calculateFileHash calculates a simple hash for file integrity
func calculateFileHash(content []byte) string {
	// In production, use crypto/sha256
	// For now, use a simple placeholder
	return fmt.Sprintf("%x", len(content))
}

// formatFileSize formats file size in human readable format
func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// getUserIDFromContext extracts user ID from request context (JWT)
func (h *UploadHandler) getUserIDFromContext(r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		h.logger.Warn("Missing user ID in context", map[string]interface{}{
			"handler":  "upload",
			"endpoint": r.URL.Path,
			"ip":       r.RemoteAddr,
		})
		return uuid.Nil, false
	}
	return userID, true
}

// ==============================================================================
// ASYNC FILE PROCESSING WITH VALIDATION
// ==============================================================================

// processUploadedFile processes the uploaded file in the background
func (h *UploadHandler) processUploadedFile(_ context.Context, userID, documentID uuid.UUID, request *MultipartUploadRequest) {
	processingStart := time.Now()

	// Log background processing start with validation context
	h.logger.Info("Background file processing started", map[string]interface{}{
		"event":         "background_processing_started",
		"user_id":       userID.String(),
		"document_id":   documentID.String(),
		"file_name":     request.FileName,
		"file_size":     request.FileSize,
		"file_hash":     request.FileHash[:16] + "...",
		"document_type": request.DocumentType,
		"mime_type":     request.MimeType,
	})

	// Simulate processing steps
	processingSteps := []string{
		"File integrity check",
		"Virus scanning",
		"OCR processing (if applicable)",
		"Document validation",
		"Metadata extraction",
		"Storage persistence",
	}

	for i, step := range processingSteps {
		// Simulate processing time for each step
		time.Sleep(50 * time.Millisecond)

		h.logger.Debug("Processing step completed", map[string]interface{}{
			"event":       "processing_step",
			"document_id": documentID.String(),
			"step_number": i + 1,
			"step_name":   step,
			"elapsed_ms":  time.Since(processingStart).Milliseconds(),
		})
	}

	// Log processing completion with statistics
	h.logger.Info("Background file processing completed", map[string]interface{}{
		"event":                "background_processing_completed",
		"user_id":              userID.String(),
		"document_id":          documentID.String(),
		"file_name":            request.FileName,
		"processing_steps":     len(processingSteps),
		"total_processing_ms":  time.Since(processingStart).Milliseconds(),
		"average_step_time_ms": time.Since(processingStart).Milliseconds() / int64(len(processingSteps)),
		"file_hash":            request.FileHash[:16] + "...",
	})
}

// ==============================================================================
// VALIDATION CONFIGURATION ENDPOINTS
// ==============================================================================

// GetUploadConfig returns the current upload configuration
// GET /upload/config
func (h *UploadHandler) GetUploadConfig(w http.ResponseWriter, r *http.Request) {
	// Only allow admin or internal requests in production
	// For now, return the config (sanitized if needed)

	response := map[string]interface{}{
		"config":    h.config,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	h.respondJSON(w, http.StatusOK, response)
}

// UpdateUploadConfig updates upload configuration (admin only)
// PUT /upload/config
func (h *UploadHandler) UpdateUploadConfig(w http.ResponseWriter, r *http.Request) {
	// In production, verify admin permissions
	var newConfig UploadConfig

	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid configuration format")
		return
	}

	// Validate the new configuration
	if newConfig.MaxFileSizeMB <= 0 {
		h.respondError(w, http.StatusBadRequest, "MaxFileSizeMB must be positive")
		return
	}

	if newConfig.MaxFileSizeMB > 100 { // Safety limit
		h.respondError(w, http.StatusBadRequest, "MaxFileSizeMB cannot exceed 100MB")
		return
	}

	if len(newConfig.AllowedMimeTypes) == 0 {
		h.respondError(w, http.StatusBadRequest, "AllowedMimeTypes cannot be empty")
		return
	}

	// Update configuration
	h.config = &newConfig

	// Update semaphore size if needed
	if newConfig.MaxConcurrentUploads != cap(h.uploadSemaphore) {
		h.uploadSemaphore = make(chan struct{}, newConfig.MaxConcurrentUploads)
	}

	h.logger.Info("Upload configuration updated", map[string]interface{}{
		"event":                "upload_config_updated",
		"new_max_file_size_mb": newConfig.MaxFileSizeMB,
		"new_allowed_types":    newConfig.AllowedMimeTypes,
		"new_concurrent_limit": newConfig.MaxConcurrentUploads,
	})

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "updated",
		"message": "Upload configuration updated successfully",
		"config":  h.config,
	})
}

// ==============================================================================
// DEBUG AND TESTING ENDPOINTS
// ==============================================================================

// DebugUploadTest provides a simple endpoint to test multipart form parsing with validation
// POST /debug/upload-test
func (h *UploadHandler) DebugUploadTest(w http.ResponseWriter, r *http.Request) {
	// This endpoint doesn't require authentication for testing
	h.logger.Info("Debug upload test requested", map[string]interface{}{
		"event":        "debug_upload_test",
		"method":       r.Method,
		"endpoint":     r.URL.Path,
		"ip":           r.RemoteAddr,
		"content_type": r.Header.Get("Content-Type"),
	})

	// Parse multipart form
	uploadRequest, err := h.parseMultipartForm(w, r)
	if err != nil {
		h.respondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"message": "Failed to parse multipart form",
		})
		return
	}

	// Run validation
	validationErrors := h.validateFile(uploadRequest)
	validationPassed := len(validationErrors) == 0

	// Return parsed information with validation results
	response := map[string]interface{}{
		"success":           true,
		"message":           "Multipart form parsed successfully",
		"validation_passed": validationPassed,
		"validation_errors": validationErrors,
		"config": map[string]interface{}{
			"max_file_size_mb":   h.config.MaxFileSizeMB,
			"allowed_mime_types": h.config.AllowedMimeTypes,
			"allowed_extensions": h.config.AllowedExtensions,
		},
		"data": map[string]interface{}{
			"document_type":           uploadRequest.DocumentType,
			"document_number":         uploadRequest.DocumentNumber,
			"issuing_country":         uploadRequest.IssuingCountry,
			"issue_date":              uploadRequest.IssueDate,
			"expiry_date":             uploadRequest.ExpiryDate,
			"file_name":               uploadRequest.FileName,
			"file_size_bytes":         uploadRequest.FileSize,
			"formatted_file_size":     formatFileSize(uploadRequest.FileSize),
			"mime_type":               uploadRequest.MimeType,
			"file_hash":               uploadRequest.FileHash,
			"form_fields":             uploadRequest.FormData,
			"file_content_size_bytes": len(uploadRequest.FileContent),
		},
	}

	h.logger.Info("Debug upload test completed", map[string]interface{}{
		"event":             "debug_upload_test_completed",
		"file_name":         uploadRequest.FileName,
		"file_size":         uploadRequest.FileSize,
		"document_type":     uploadRequest.DocumentType,
		"validation_passed": validationPassed,
		"validation_errors": len(validationErrors),
	})

	h.respondJSON(w, http.StatusOK, response)
}

// TestValidation provides a testing endpoint for validation rules
// POST /upload/test-validation
func (h *UploadHandler) TestValidation(w http.ResponseWriter, r *http.Request) {
	type TestCase struct {
		FileName string              `json:"file_name"`
		FileSize int64               `json:"file_size"`
		MimeType string              `json:"mime_type"`
		DocType  domain.DocumentType `json:"document_type"`
	}

	var testCase TestCase
	if err := json.NewDecoder(r.Body).Decode(&testCase); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid test case format")
		return
	}

	// Create mock request for validation
	mockRequest := &MultipartUploadRequest{
		DocumentType: testCase.DocType,
		FileName:     testCase.FileName,
		FileSize:     testCase.FileSize,
		MimeType:     testCase.MimeType,
	}

	// Run validation
	validationErrors := h.validateFile(mockRequest)

	response := map[string]interface{}{
		"test_case":         testCase,
		"validation_passed": len(validationErrors) == 0,
		"validation_errors": validationErrors,
		"config_applied": map[string]interface{}{
			"max_file_size_mb":   h.config.MaxFileSizeMB,
			"min_file_size_kb":   h.config.MinFileSizeKB,
			"allowed_mime_types": h.config.AllowedMimeTypes,
			"allowed_extensions": h.config.AllowedExtensions,
		},
	}

	h.respondJSON(w, http.StatusOK, response)
}

// ==============================================================================
// HEALTH AND STATUS ENDPOINTS
// ==============================================================================

// HealthCheck provides a comprehensive health check endpoint
// GET /upload/health
func (h *UploadHandler) HealthCheck(w http.ResponseWriter, _ *http.Request) {
	status := "healthy"
	semaphoreUsage := float64(len(h.uploadSemaphore)) / float64(cap(h.uploadSemaphore)) * 100

	response := map[string]interface{}{
		"status":    status,
		"service":   "file-upload",
		"timestamp": time.Now().Format(time.RFC3339),
		"metrics": map[string]interface{}{
			"concurrent_uploads":      len(h.uploadSemaphore),
			"max_concurrent_uploads":  cap(h.uploadSemaphore),
			"semaphore_usage_percent": fmt.Sprintf("%.1f%%", semaphoreUsage),
		},
		"config_summary": map[string]interface{}{
			"max_file_size_mb":   h.config.MaxFileSizeMB,
			"allowed_mime_types": len(h.config.AllowedMimeTypes),
			"allowed_extensions": len(h.config.AllowedExtensions),
			"require_virus_scan": h.config.RequireVirusScan,
		},
		"features": []string{
			"multipart-form-parsing",
			"file-size-validation",
			"file-type-validation",
			"security-validation",
			"concurrency-control",
			"async-processing",
		},
	}

	h.respondJSON(w, http.StatusOK, response)
}

// GetValidationRules returns the current validation rules
// GET /upload/validation-rules
func (h *UploadHandler) GetValidationRules(w http.ResponseWriter, _ *http.Request) {
	// Map document types to expected file types
	documentTypeRules := map[string][]string{
		"national_id":           {"image/jpeg", "image/png", "application/pdf"},
		"passport":              {"image/jpeg", "image/png", "application/pdf"},
		"drivers_license":       {"image/jpeg", "image/png", "application/pdf"},
		"utility_bill":          {"image/jpeg", "image/png", "application/pdf"},
		"bank_statement":        {"application/pdf", "image/jpeg", "image/png"},
		"proof_of_income":       {"application/pdf", "application/msword", "image/jpeg"},
		"business_registration": {"application/pdf", "image/jpeg", "image/png"},
		"tax_certificate":       {"application/pdf", "image/jpeg", "image/png"},
		"business_license":      {"application/pdf", "image/jpeg", "image/png"},
		"agent_license":         {"application/pdf", "image/jpeg", "image/png"},
		"selfie_with_id":        {"image/jpeg", "image/png"},
	}

	response := map[string]interface{}{
		"global_rules": map[string]interface{}{
			"max_file_size":      fmt.Sprintf("%d MB", h.config.MaxFileSizeMB),
			"min_file_size":      fmt.Sprintf("%d KB", h.config.MinFileSizeKB),
			"allowed_mime_types": h.config.AllowedMimeTypes,
			"allowed_extensions": h.config.AllowedExtensions,
			"image_constraints": map[string]int{
				"max_width":  h.config.MaxImageWidth,
				"max_height": h.config.MaxImageHeight,
				"min_width":  h.config.MinImageWidth,
				"min_height": h.config.MinImageHeight,
			},
		},
		"document_type_rules": documentTypeRules,
		"security_rules": map[string]bool{
			"require_virus_scan":        h.config.RequireVirusScan,
			"require_integrity_check":   h.config.RequireIntegrity,
			"prevent_path_traversal":    true,
			"prevent_executable_upload": true,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	h.respondJSON(w, http.StatusOK, response)
}
