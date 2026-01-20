// internal/fileupload/service.go
package fileupload

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"time"

	"kyd/pkg/config"
	"kyd/pkg/logger"

	"github.com/google/uuid"
)

// Interface following ForexService pattern
type FileUploadService interface {
	UploadFile(ctx context.Context, fileHeader *multipart.FileHeader, userID uuid.UUID, docType string) (*UploadResult, error)
	GetFileURL(ctx context.Context, filePath string) (string, error)
	DeleteFile(ctx context.Context, filePath string) error
	GeneratePresignedURL(ctx context.Context, fileName string, contentType string) (*PresignedURL, error)
}

type UploadResult struct {
	FileID      uuid.UUID `json:"file_id"`
	FileName    string    `json:"file_name"`
	FilePath    string    `json:"file_path"`
	FileSize    int64     `json:"file_size"`
	ContentType string    `json:"content_type"`
	UploadedAt  time.Time `json:"uploaded_at"`
	URL         string    `json:"url"`
	Checksum    string    `json:"checksum"`
}

type PresignedURL struct {
	URL       string    `json:"url"`
	Method    string    `json:"method"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Local implementation following ExchangeRateAPIProvider pattern
type LocalFileUploadService struct {
	config *config.Config
	logger logger.Logger
}

func NewLocalFileUploadService(cfg *config.Config, log logger.Logger) *LocalFileUploadService {
	return &LocalFileUploadService{
		config: cfg,
		logger: log,
	}
}

func (s *LocalFileUploadService) UploadFile(ctx context.Context, fileHeader *multipart.FileHeader, userID uuid.UUID, docType string) (*UploadResult, error) {
	// Validate file size
	if fileHeader.Size > s.config.KYC.MaxFileSize {
		return nil, fmt.Errorf("file size exceeds limit: %d bytes", s.config.KYC.MaxFileSize)
	}

	// Validate file type
	contentType := fileHeader.Header.Get("Content-Type")
	if !s.isAllowedFileType(contentType) {
		return nil, fmt.Errorf("file type not allowed: %s", contentType)
	}

	// Open file
	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create upload directory if it doesn't exist
	uploadDir := filepath.Join(s.config.FileUpload.LocalUploadDir, userID.String())
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// Generate unique filename
	fileID := uuid.New()
	fileName := fmt.Sprintf("%s_%s_%s%s",
		docType,
		time.Now().Format("20060102"),
		fileID.String()[:8],
		filepath.Ext(fileHeader.Filename),
	)
	filePath := filepath.Join(uploadDir, fileName)

	// Save file
	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	// Calculate checksum
	checksum, err := s.calculateChecksum(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}

	result := &UploadResult{
		FileID:      fileID,
		FileName:    fileName,
		FilePath:    filePath,
		FileSize:    fileHeader.Size,
		ContentType: contentType,
		UploadedAt:  time.Now(),
		URL:         s.buildFileURL(userID.String(), fileName),
		Checksum:    checksum,
	}

	s.logger.Info("File uploaded successfully", map[string]interface{}{
		"file_id":   fileID,
		"user_id":   userID,
		"file_size": fileHeader.Size,
		"file_type": contentType,
		"doc_type":  docType,
	})

	return result, nil
}

func (s *LocalFileUploadService) GetFileURL(ctx context.Context, filePath string) (string, error) {
	// For local storage, return relative path or CDN URL
	if s.config.FileUpload.CDNBaseURL != "" {
		// Extract relative path from absolute
		relPath, err := filepath.Rel(s.config.FileUpload.LocalUploadDir, filePath)
		if err != nil {
			return "", err
		}
		return s.config.FileUpload.CDNBaseURL + "/" + relPath, nil
	}
	return filePath, nil
}

func (s *LocalFileUploadService) DeleteFile(ctx context.Context, filePath string) error {
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (s *LocalFileUploadService) GeneratePresignedURL(ctx context.Context, fileName string, contentType string) (*PresignedURL, error) {
	// For local storage, presigned URLs aren't needed
	// This is a stub for S3/MinIO implementation
	return nil, fmt.Errorf("presigned URLs not supported for local storage")
}

// Helper methods
func (s *LocalFileUploadService) isAllowedFileType(contentType string) bool {
	for _, allowed := range s.config.KYC.AllowedFileTypes {
		if contentType == allowed {
			return true
		}
	}
	return false
}

func (s *LocalFileUploadService) calculateChecksum(filePath string) (string, error) {
	// Simple MD5 checksum implementation
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Using crypto/md5 - in production use sha256
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (s *LocalFileUploadService) buildFileURL(userID, fileName string) string {
	return fmt.Sprintf("%s/%s/%s", s.config.FileUpload.CDNBaseURL, userID, fileName)
}
