// ==============================================================================
// FILE UPLOAD SERVICE - internal/fileupload/service.go
// ==============================================================================
// File upload service with local storage implementation
// ==============================================================================

package fileupload

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/logger"

	"github.com/google/uuid"
)

// ==============================================================================
// INTERFACES
// ==============================================================================

// FileUploadService defines the interface for file upload operations
type FileUploadService interface {
	// File operations
	UploadFile(ctx context.Context, req *UploadRequest) (*UploadResponse, error)
	DownloadFile(ctx context.Context, fileID uuid.UUID) ([]byte, *FileMetadata, error)
	DeleteFile(ctx context.Context, fileID uuid.UUID) error
	GetFileMetadata(ctx context.Context, fileID uuid.UUID) (*FileMetadata, error)
	ListUserFiles(ctx context.Context, userID uuid.UUID, docType *domain.DocumentType) ([]*FileMetadata, error)

	// Storage management
	GetStorageUsage(ctx context.Context) (*StorageUsage, error)
	CleanupOldFiles(ctx context.Context, retentionDays int) (int, error)
	ValidateStoragePath(ctx context.Context) error

	// Utility methods
	GeneratePresignedURL(ctx context.Context, fileID uuid.UUID, expiresIn time.Duration) (string, error)
	UpdateFileMetadata(ctx context.Context, fileID uuid.UUID, metadata map[string]interface{}) error
	CopyFile(ctx context.Context, sourceFileID uuid.UUID, targetUserID uuid.UUID) (uuid.UUID, error)
}

// StorageProvider defines the interface for different storage backends
type StorageProvider interface {
	SaveFile(ctx context.Context, fileData []byte, fileName string, metadata *FileMetadata) (string, error)
	GetFile(ctx context.Context, storagePath string) ([]byte, error)
	DeleteFile(ctx context.Context, storagePath string) error
	GetFileInfo(ctx context.Context, storagePath string) (*FileInfo, error)
	GenerateAccessURL(ctx context.Context, storagePath string, expiresIn time.Duration) (string, error)
}

// ==============================================================================
// REQUEST/RESPONSE STRUCTURES
// ==============================================================================

// UploadRequest contains all data needed for file upload
type UploadRequest struct {
	UserID          uuid.UUID
	DocumentID      *uuid.UUID // Optional, if updating existing document
	DocumentType    domain.DocumentType
	FileName        string
	FileData        []byte
	ContentType     string
	Metadata        map[string]interface{}
	AccessLevel     domain.AccessLevel
	RetentionPolicy domain.RetentionPolicy
	ExpiryDate      *time.Time
}

// UploadResponse contains the result of a file upload
type UploadResponse struct {
	FileID         uuid.UUID
	StoragePath    string
	PublicURL      string
	FileSize       int64
	ChecksumSHA256 string
	ChecksumMD5    string
	ContentType    string
	UploadedAt     time.Time
}

// FileMetadata contains metadata for stored files
type FileMetadata struct {
	FileID           uuid.UUID              `json:"file_id"`
	UserID           uuid.UUID              `json:"user_id"`
	DocumentID       *uuid.UUID             `json:"document_id,omitempty"`
	DocumentType     domain.DocumentType    `json:"document_type"`
	FileName         string                 `json:"file_name"`
	StoragePath      string                 `json:"storage_path"`
	StorageProvider  domain.StorageProvider `json:"storage_provider"`
	FileSize         int64                  `json:"file_size"`
	ContentType      string                 `json:"content_type"`
	ChecksumSHA256   string                 `json:"checksum_sha256"`
	ChecksumMD5      string                 `json:"checksum_md5"`
	AccessLevel      domain.AccessLevel     `json:"access_level"`
	RetentionPolicy  domain.RetentionPolicy `json:"retention_policy"`
	ExpiryDate       *time.Time             `json:"expiry_date,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	Downloads        int                    `json:"downloads"`
	LastDownloadedAt *time.Time             `json:"last_downloaded_at,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// FileInfo contains basic file information from storage
type FileInfo struct {
	Size         int64
	ContentType  string
	LastModified time.Time
	ETag         string
}

// StorageUsage contains storage usage statistics
type StorageUsage struct {
	TotalFiles     int64   `json:"total_files"`
	TotalSize      int64   `json:"total_size_bytes"`
	UsedSpace      int64   `json:"used_space_bytes"`
	AvailableSpace int64   `json:"available_space_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
}

// ==============================================================================
// LOCAL STORAGE IMPLEMENTATION
// ==============================================================================

// LocalStorageConfig contains configuration for local storage
type LocalStorageConfig struct {
	BasePath          string      `json:"base_path"`
	AllowedExtensions []string    `json:"allowed_extensions"`
	MaxFileSize       int64       `json:"max_file_size"`
	CreateDirectories bool        `json:"create_directories"`
	FilePermissions   os.FileMode `json:"file_permissions"`
	DirPermissions    os.FileMode `json:"dir_permissions"`
}

// LocalStorageProvider implements StorageProvider for local filesystem
type LocalStorageProvider struct {
	config *LocalStorageConfig
	logger logger.Logger
}

// NewLocalStorageProvider creates a new local storage provider
func NewLocalStorageProvider(config *LocalStorageConfig, logger logger.Logger) *LocalStorageProvider {
	if config == nil {
		config = &LocalStorageConfig{
			BasePath:          "./uploads",
			AllowedExtensions: []string{".jpg", ".jpeg", ".png", ".pdf", ".doc", ".docx"},
			MaxFileSize:       10 * 1024 * 1024, // 10MB
			CreateDirectories: true,
			FilePermissions:   0644,
			DirPermissions:    0755,
		}
	}

	return &LocalStorageProvider{
		config: config,
		logger: logger,
	}
}

// SaveFile saves a file to local storage
func (p *LocalStorageProvider) SaveFile(ctx context.Context, fileData []byte, fileName string, metadata *FileMetadata) (string, error) {
	startTime := time.Now()

	// Validate file size
	if int64(len(fileData)) > p.config.MaxFileSize {
		return "", fmt.Errorf("file size exceeds maximum allowed size: %d bytes", p.config.MaxFileSize)
	}

	// Sanitize file name
	sanitizedName := sanitizeFileName(fileName)
	fileExt := strings.ToLower(filepath.Ext(sanitizedName))

	// Validate file extension
	extensionValid := false
	for _, allowedExt := range p.config.AllowedExtensions {
		if strings.EqualFold(fileExt, allowedExt) {
			extensionValid = true
			break
		}
	}

	if !extensionValid {
		return "", fmt.Errorf("file extension not allowed: %s (allowed: %s)",
			fileExt, strings.Join(p.config.AllowedExtensions, ", "))
	}

	// Generate storage path based on user ID and document type
	storageDir := filepath.Join(
		p.config.BasePath,
		"users",
		metadata.UserID.String(),
		string(metadata.DocumentType),
		time.Now().Format("2006/01/02"),
	)

	// Ensure directory exists
	if p.config.CreateDirectories {
		if err := os.MkdirAll(storageDir, p.config.DirPermissions); err != nil {
			return "", fmt.Errorf("failed to create storage directory: %w", err)
		}
	}

	// Generate unique file name
	uniqueFileName := fmt.Sprintf("%s_%s%s",
		uuid.New().String()[:8],
		time.Now().Format("150405"), // HHMMSS
		fileExt,
	)

	storagePath := filepath.Join(storageDir, uniqueFileName)

	// Write file to disk
	if err := os.WriteFile(storagePath, fileData, p.config.FilePermissions); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	// Log the operation
	p.logger.Info("File saved to local storage", map[string]interface{}{
		"event":         "file_saved_local",
		"user_id":       metadata.UserID.String(),
		"document_type": metadata.DocumentType,
		"file_name":     sanitizedName,
		"storage_path":  storagePath,
		"file_size":     len(fileData),
		"duration_ms":   time.Since(startTime).Milliseconds(),
	})

	return storagePath, nil
}

// GetFile retrieves a file from local storage
func (p *LocalStorageProvider) GetFile(ctx context.Context, storagePath string) ([]byte, error) {
	// Verify the path is within the base directory for security
	if !strings.HasPrefix(storagePath, p.config.BasePath) {
		return nil, fmt.Errorf("access denied: path outside base directory")
	}

	// Check if file exists
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", storagePath)
	}

	// Read file
	fileData, err := os.ReadFile(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return fileData, nil
}

// DeleteFile deletes a file from local storage
func (p *LocalStorageProvider) DeleteFile(ctx context.Context, storagePath string) error {
	// Verify the path is within the base directory
	if !strings.HasPrefix(storagePath, p.config.BasePath) {
		return fmt.Errorf("access denied: path outside base directory")
	}

	// Check if file exists
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		return nil // File already deleted, nothing to do
	}

	// Delete the file
	if err := os.Remove(storagePath); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Try to clean up empty parent directories
	p.cleanupEmptyDirectories(filepath.Dir(storagePath))

	return nil
}

// GetFileInfo gets information about a file
func (p *LocalStorageProvider) GetFileInfo(ctx context.Context, storagePath string) (*FileInfo, error) {
	// Verify the path is within the base directory
	if !strings.HasPrefix(storagePath, p.config.BasePath) {
		return nil, fmt.Errorf("access denied: path outside base directory")
	}

	// Get file stats
	fileInfo, err := os.Stat(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Try to detect content type
	contentType := "application/octet-stream"
	if fileData, err := os.ReadFile(storagePath); err == nil && len(fileData) > 0 {
		contentType = http.DetectContentType(fileData[:512])
	}

	return &FileInfo{
		Size:         fileInfo.Size(),
		ContentType:  contentType,
		LastModified: fileInfo.ModTime(),
		ETag:         generateETag(fileInfo),
	}, nil
}

// GenerateAccessURL generates a URL for accessing the file
func (p *LocalStorageProvider) GenerateAccessURL(ctx context.Context, storagePath string, expiresIn time.Duration) (string, error) {
	// For local storage, we return a file:// URL or a relative path
	// In production, you might want to serve files through a dedicated endpoint
	absPath, err := filepath.Abs(storagePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return "file://" + absPath, nil
}

// ==============================================================================
// FILE UPLOAD SERVICE IMPLEMENTATION
// ==============================================================================

// FileUploadServiceImpl implements FileUploadService with local storage
type FileUploadServiceImpl struct {
	storage      StorageProvider
	metadataRepo FileMetadataRepository
	logger       logger.Logger
	config       *LocalStorageConfig
}

// FileMetadataRepository defines repository interface for file metadata
type FileMetadataRepository interface {
	SaveMetadata(ctx context.Context, metadata *FileMetadata) error
	GetMetadata(ctx context.Context, fileID uuid.UUID) (*FileMetadata, error)
	DeleteMetadata(ctx context.Context, fileID uuid.UUID) error
	ListByUserID(ctx context.Context, userID uuid.UUID, docType *domain.DocumentType) ([]*FileMetadata, error)
	UpdateMetadata(ctx context.Context, fileID uuid.UUID, updates map[string]interface{}) error
	IncrementDownloadCount(ctx context.Context, fileID uuid.UUID, downloadedAt time.Time) error
}

// NewFileUploadService creates a new file upload service
func NewFileUploadService(
	storage StorageProvider,
	metadataRepo FileMetadataRepository,
	logger logger.Logger,
	config *LocalStorageConfig,
) *FileUploadServiceImpl {
	return &FileUploadServiceImpl{
		storage:      storage,
		metadataRepo: metadataRepo,
		logger:       logger,
		config:       config,
	}
}

// UploadFile handles the complete file upload process
func (s *FileUploadServiceImpl) UploadFile(ctx context.Context, req *UploadRequest) (*UploadResponse, error) {
	startTime := time.Now()

	// Generate file ID
	fileID := uuid.New()

	// Calculate checksums
	sha256Hash := sha256.Sum256(req.FileData)
	md5Hash := md5.Sum(req.FileData)

	// Determine content type
	contentType := req.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(req.FileName))
		if contentType == "" {
			contentType = http.DetectContentType(req.FileData[:512])
		}
	}

	// Create file metadata
	metadata := &FileMetadata{
		FileID:          fileID,
		UserID:          req.UserID,
		DocumentID:      req.DocumentID,
		DocumentType:    req.DocumentType,
		FileName:        req.FileName,
		StorageProvider: domain.StorageProviderLocal,
		FileSize:        int64(len(req.FileData)),
		ContentType:     contentType,
		ChecksumSHA256:  hex.EncodeToString(sha256Hash[:]),
		ChecksumMD5:     hex.EncodeToString(md5Hash[:]),
		AccessLevel:     req.AccessLevel,
		RetentionPolicy: req.RetentionPolicy,
		ExpiryDate:      req.ExpiryDate,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		Metadata:        req.Metadata,
	}

	// Save file to storage
	storagePath, err := s.storage.SaveFile(ctx, req.FileData, req.FileName, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to save file to storage: %w", err)
	}
	metadata.StoragePath = storagePath

	// Save metadata to repository
	if err := s.metadataRepo.SaveMetadata(ctx, metadata); err != nil {
		// Clean up the stored file if metadata save fails
		s.storage.DeleteFile(ctx, storagePath)
		return nil, fmt.Errorf("failed to save file metadata: %w", err)
	}

	// Generate access URL (for local storage, this is a file:// URL)
	accessURL, _ := s.storage.GenerateAccessURL(ctx, storagePath, 24*time.Hour)

	// Log successful upload
	s.logger.Info("File upload completed", map[string]interface{}{
		"event":         "file_upload_completed",
		"user_id":       req.UserID.String(),
		"file_id":       fileID.String(),
		"document_type": req.DocumentType,
		"file_name":     req.FileName,
		"file_size":     len(req.FileData),
		"content_type":  contentType,
		"storage_path":  storagePath,
		"duration_ms":   time.Since(startTime).Milliseconds(),
	})

	return &UploadResponse{
		FileID:         fileID,
		StoragePath:    storagePath,
		PublicURL:      accessURL,
		FileSize:       int64(len(req.FileData)),
		ChecksumSHA256: hex.EncodeToString(sha256Hash[:]),
		ChecksumMD5:    hex.EncodeToString(md5Hash[:]),
		ContentType:    contentType,
		UploadedAt:     time.Now(),
	}, nil
}

// DownloadFile retrieves a file and its metadata
func (s *FileUploadServiceImpl) DownloadFile(ctx context.Context, fileID uuid.UUID) ([]byte, *FileMetadata, error) {
	// Get file metadata
	metadata, err := s.metadataRepo.GetMetadata(ctx, fileID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Verify user has access (in production, add proper authorization)
	// For now, we'll just check if the file exists

	// Get file from storage
	fileData, err := s.storage.GetFile(ctx, metadata.StoragePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve file: %w", err)
	}

	// Update download statistics
	go func() {
		ctx := context.Background()
		if err := s.metadataRepo.IncrementDownloadCount(ctx, fileID, time.Now()); err != nil {
			s.logger.Warn("Failed to update download count", map[string]interface{}{
				"file_id": fileID.String(),
				"error":   err.Error(),
			})
		}
	}()

	s.logger.Info("File downloaded", map[string]interface{}{
		"event":   "file_downloaded",
		"file_id": fileID.String(),
		"user_id": metadata.UserID.String(),
	})

	return fileData, metadata, nil
}

// DeleteFile removes a file and its metadata
func (s *FileUploadServiceImpl) DeleteFile(ctx context.Context, fileID uuid.UUID) error {
	// Get file metadata to find storage path
	metadata, err := s.metadataRepo.GetMetadata(ctx, fileID)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Delete from storage
	if err := s.storage.DeleteFile(ctx, metadata.StoragePath); err != nil {
		return fmt.Errorf("failed to delete file from storage: %w", err)
	}

	// Delete metadata
	if err := s.metadataRepo.DeleteMetadata(ctx, fileID); err != nil {
		return fmt.Errorf("failed to delete file metadata: %w", err)
	}

	s.logger.Info("File deleted", map[string]interface{}{
		"event":   "file_deleted",
		"file_id": fileID.String(),
		"user_id": metadata.UserID.String(),
	})

	return nil
}

// GetFileMetadata retrieves metadata for a file
func (s *FileUploadServiceImpl) GetFileMetadata(ctx context.Context, fileID uuid.UUID) (*FileMetadata, error) {
	return s.metadataRepo.GetMetadata(ctx, fileID)
}

// ListUserFiles lists all files for a user
func (s *FileUploadServiceImpl) ListUserFiles(ctx context.Context, userID uuid.UUID, docType *domain.DocumentType) ([]*FileMetadata, error) {
	return s.metadataRepo.ListByUserID(ctx, userID, docType)
}

// ==============================================================================
// STORAGE MANAGEMENT METHODS
// ==============================================================================

// GetStorageUsage gets storage usage statistics
func (s *FileUploadServiceImpl) GetStorageUsage(ctx context.Context) (*StorageUsage, error) {
	var totalSize int64
	var totalFiles int64

	// Walk through storage directory
	err := filepath.Walk(s.config.BasePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			totalFiles++
			totalSize += info.Size()
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to calculate storage usage: %w", err)
	}

	// Get disk usage
	var usagePercent float64
	if stat, err := getDiskUsage(s.config.BasePath); err == nil {
		usagePercent = float64(stat.Used) / float64(stat.Total) * 100
	}

	return &StorageUsage{
		TotalFiles:   totalFiles,
		TotalSize:    totalSize,
		UsedSpace:    totalSize,
		UsagePercent: usagePercent,
	}, nil
}

// CleanupOldFiles removes files older than retentionDays
func (s *FileUploadServiceImpl) CleanupOldFiles(ctx context.Context, retentionDays int) (int, error) {
	deletedCount := 0
	retentionDate := time.Now().AddDate(0, 0, -retentionDays)

	// Get all expired files from metadata
	// This would require a repository method to find expired files
	// For now, implement a simple directory walk

	err := filepath.Walk(s.config.BasePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.ModTime().Before(retentionDate) {
			// Delete the file
			if err := os.Remove(path); err == nil {
				deletedCount++
				s.logger.Info("Cleaned up old file", map[string]interface{}{
					"event":      "file_cleanup",
					"file_path":  path,
					"file_age":   time.Since(info.ModTime()).Hours() / 24,
					"deleted_at": time.Now(),
				})
			}
		}

		return nil
	})

	return deletedCount, err
}

// ValidateStoragePath validates the storage path configuration
func (s *FileUploadServiceImpl) ValidateStoragePath(ctx context.Context) error {
	// Check if base path exists and is writable
	if _, err := os.Stat(s.config.BasePath); os.IsNotExist(err) {
		// Try to create it
		if err := os.MkdirAll(s.config.BasePath, s.config.DirPermissions); err != nil {
			return fmt.Errorf("failed to create storage directory: %w", err)
		}
	}

	// Test write permissions
	testFile := filepath.Join(s.config.BasePath, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("storage directory is not writable: %w", err)
	}
	os.Remove(testFile)

	return nil
}

// ==============================================================================
// UTILITY METHODS
// ==============================================================================

// GeneratePresignedURL generates a time-limited access URL
func (s *FileUploadServiceImpl) GeneratePresignedURL(ctx context.Context, fileID uuid.UUID, expiresIn time.Duration) (string, error) {
	metadata, err := s.metadataRepo.GetMetadata(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("failed to get file metadata: %w", err)
	}

	// For local storage, generate a simple access URL
	// In production with cloud storage, this would generate a signed URL
	return s.storage.GenerateAccessURL(ctx, metadata.StoragePath, expiresIn)
}

// UpdateFileMetadata updates file metadata
func (s *FileUploadServiceImpl) UpdateFileMetadata(ctx context.Context, fileID uuid.UUID, metadata map[string]interface{}) error {
	return s.metadataRepo.UpdateMetadata(ctx, fileID, metadata)
}

// CopyFile creates a copy of a file for another user
func (s *FileUploadServiceImpl) CopyFile(ctx context.Context, sourceFileID uuid.UUID, targetUserID uuid.UUID) (uuid.UUID, error) {
	// Get source file data and metadata
	fileData, sourceMetadata, err := s.DownloadFile(ctx, sourceFileID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to get source file: %w", err)
	}

	// Create upload request for the copy
	uploadReq := &UploadRequest{
		UserID:       targetUserID,
		DocumentType: sourceMetadata.DocumentType,
		FileName:     sourceMetadata.FileName,
		FileData:     fileData,
		ContentType:  sourceMetadata.ContentType,
		Metadata: map[string]interface{}{
			"copied_from":    sourceFileID.String(),
			"original_owner": sourceMetadata.UserID.String(),
			"copied_at":      time.Now().Format(time.RFC3339),
		},
		AccessLevel:     sourceMetadata.AccessLevel,
		RetentionPolicy: sourceMetadata.RetentionPolicy,
	}

	// Upload the copy
	response, err := s.UploadFile(ctx, uploadReq)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to upload file copy: %w", err)
	}

	return response.FileID, nil
}

// ==============================================================================
// HELPER FUNCTIONS
// ==============================================================================

// sanitizeFileName removes dangerous characters from file names
func sanitizeFileName(fileName string) string {
	// Remove path components
	fileName = filepath.Base(fileName)

	// Replace problematic characters
	replacements := map[string]string{
		"..": "",
		"/":  "_",
		"\\": "_",
		" ":  "_",
		"\"": "",
		"'":  "",
		"`":  "",
		"|":  "_",
		"&":  "_",
		";":  "_",
		"$":  "_",
		"(":  "_",
		")":  "_",
		"[":  "_",
		"]":  "_",
		"{":  "_",
		"}":  "_",
		"<":  "_",
		">":  "_",
		"*":  "_",
		"?":  "_",
		"!":  "_",
		"@":  "_",
		"#":  "_",
		"%":  "_",
		"+":  "_",
		"=":  "_",
		"^":  "_",
		"~":  "_",
	}

	for old, new := range replacements {
		fileName = strings.ReplaceAll(fileName, old, new)
	}

	// Limit length
	if len(fileName) > 255 {
		ext := filepath.Ext(fileName)
		name := fileName[:255-len(ext)]
		fileName = name + ext
	}

	return fileName
}

// generateETag generates an ETag for a file
func generateETag(fileInfo os.FileInfo) string {
	// Use modification time and size for ETag
	return fmt.Sprintf("%x-%x", fileInfo.Size(), fileInfo.ModTime().Unix())
}

// getDiskUsage gets disk usage statistics
func getDiskUsage(_ string) (*struct {
	Total uint64
	Used  uint64
	Free  uint64
}, error) {
	// This is platform-specific
	// For now, return a stub
	return &struct {
		Total uint64
		Used  uint64
		Free  uint64
	}{
		Total: 100 * 1024 * 1024 * 1024, // 100GB
		Used:  10 * 1024 * 1024 * 1024,  // 10GB
		Free:  90 * 1024 * 1024 * 1024,  // 90GB
	}, nil
}

// cleanupEmptyDirectories removes empty directories
func (p *LocalStorageProvider) cleanupEmptyDirectories(dir string) {
	for {
		// Check if directory is empty
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}

		// Remove empty directory
		if err := os.Remove(dir); err != nil {
			break
		}

		// Move up to parent directory
		parent := filepath.Dir(dir)
		if parent == dir || !strings.HasPrefix(parent, p.config.BasePath) {
			break
		}
		dir = parent
	}
}
