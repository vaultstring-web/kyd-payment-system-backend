// ==============================================================================
// FILE METADATA REPOSITORY - internal/fileupload/repository.go
// ==============================================================================
// In-memory repository for file metadata (for demonstration)
// In production, use a database like PostgreSQL
// ==============================================================================

package fileupload

import (
	"context"
	"fmt"
	"sync"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
)

// InMemoryMetadataRepository implements FileMetadataRepository in memory
type InMemoryMetadataRepository struct {
	mu        sync.RWMutex
	files     map[uuid.UUID]*FileMetadata
	userFiles map[uuid.UUID][]uuid.UUID // userID -> []fileID
}

// NewInMemoryMetadataRepository creates a new in-memory repository
func NewInMemoryMetadataRepository() *InMemoryMetadataRepository {
	return &InMemoryMetadataRepository{
		files:     make(map[uuid.UUID]*FileMetadata),
		userFiles: make(map[uuid.UUID][]uuid.UUID),
	}
}

// SaveMetadata saves file metadata
func (r *InMemoryMetadataRepository) SaveMetadata(ctx context.Context, metadata *FileMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Store metadata
	r.files[metadata.FileID] = metadata

	// Add to user's file list
	if _, exists := r.userFiles[metadata.UserID]; !exists {
		r.userFiles[metadata.UserID] = []uuid.UUID{}
	}
	r.userFiles[metadata.UserID] = append(r.userFiles[metadata.UserID], metadata.FileID)

	return nil
}

// GetMetadata retrieves file metadata
func (r *InMemoryMetadataRepository) GetMetadata(ctx context.Context, fileID uuid.UUID) (*FileMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metadata, exists := r.files[fileID]
	if !exists {
		return nil, fmt.Errorf("file metadata not found: %s", fileID)
	}

	// Return a copy to prevent external modification
	metadataCopy := *metadata
	return &metadataCopy, nil
}

// DeleteMetadata deletes file metadata
func (r *InMemoryMetadataRepository) DeleteMetadata(ctx context.Context, fileID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata, exists := r.files[fileID]
	if !exists {
		return nil // Already deleted
	}

	// Remove from user's file list
	if userFiles, userExists := r.userFiles[metadata.UserID]; userExists {
		for i, id := range userFiles {
			if id == fileID {
				r.userFiles[metadata.UserID] = append(userFiles[:i], userFiles[i+1:]...)
				break
			}
		}
	}

	// Remove from files map
	delete(r.files, fileID)

	return nil
}

// ListByUserID lists files for a user
func (r *InMemoryMetadataRepository) ListByUserID(ctx context.Context, userID uuid.UUID, docType *domain.DocumentType) ([]*FileMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	userFileIDs, exists := r.userFiles[userID]
	if !exists {
		return []*FileMetadata{}, nil
	}

	var result []*FileMetadata
	for _, fileID := range userFileIDs {
		metadata, exists := r.files[fileID]
		if !exists {
			continue
		}

		// Filter by document type if specified
		if docType != nil && metadata.DocumentType != *docType {
			continue
		}

		// Return a copy
		metadataCopy := *metadata
		result = append(result, &metadataCopy)
	}

	return result, nil
}

// UpdateMetadata updates file metadata
func (r *InMemoryMetadataRepository) UpdateMetadata(ctx context.Context, fileID uuid.UUID, updates map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata, exists := r.files[fileID]
	if !exists {
		return fmt.Errorf("file metadata not found: %s", fileID)
	}

	// Apply updates
	for key, value := range updates {
		switch key {
		case "access_level":
			if val, ok := value.(domain.AccessLevel); ok {
				metadata.AccessLevel = val
			}
		case "retention_policy":
			if val, ok := value.(domain.RetentionPolicy); ok {
				metadata.RetentionPolicy = val
			}
		case "expiry_date":
			if val, ok := value.(*time.Time); ok {
				metadata.ExpiryDate = val
			}
		case "metadata":
			if val, ok := value.(map[string]interface{}); ok {
				if metadata.Metadata == nil {
					metadata.Metadata = make(map[string]interface{})
				}
				for k, v := range val {
					metadata.Metadata[k] = v
				}
			}
		}
	}

	metadata.UpdatedAt = time.Now()
	r.files[fileID] = metadata

	return nil
}

// IncrementDownloadCount increments the download counter
func (r *InMemoryMetadataRepository) IncrementDownloadCount(ctx context.Context, fileID uuid.UUID, downloadedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata, exists := r.files[fileID]
	if !exists {
		return fmt.Errorf("file metadata not found: %s", fileID)
	}

	metadata.Downloads++
	metadata.LastDownloadedAt = &downloadedAt
	metadata.UpdatedAt = time.Now()

	return nil
}
