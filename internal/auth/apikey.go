package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
)

// APIKeyRepository defines storage operations for API keys
type APIKeyRepository interface {
	Create(ctx context.Context, key *domain.APIKey) error
	List(ctx context.Context) ([]domain.APIKey, error)
	GetByKeyHash(ctx context.Context, hash string) (*domain.APIKey, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error
}

type APIKeyService struct {
	repo APIKeyRepository
}

func NewAPIKeyService(repo APIKeyRepository) *APIKeyService {
	return &APIKeyService{repo: repo}
}

// CreateKey generates a new API key with the given name and scopes
func (s *APIKeyService) CreateKey(ctx context.Context, name string, scopes []string, createdBy uuid.UUID) (*domain.APIKey, string, error) {
	// Generate random key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, "", errors.Wrap(err, "failed to generate random bytes")
	}

	// Format: kyd_live_... or kyd_test_... (assuming live for now)
	rawKey := "kyd_live_" + hex.EncodeToString(keyBytes)

	// Hash the key
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	keyPrefix := rawKey[:10] // kyd_live_ + 1 char

	apiKey := &domain.APIKey{
		ID:        uuid.New(),
		Name:      name,
		KeyPrefix: keyPrefix,
		KeyHash:   keyHash,
		Scopes:    scopes,
		IsActive:  true,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}

	if err := s.repo.Create(ctx, apiKey); err != nil {
		return nil, "", err
	}

	return apiKey, rawKey, nil
}

func (s *APIKeyService) ListKeys(ctx context.Context) ([]domain.APIKey, error) {
	return s.repo.List(ctx)
}

func (s *APIKeyService) RevokeKey(ctx context.Context, id uuid.UUID) error {
	return s.repo.Revoke(ctx, id)
}

func (s *APIKeyService) ValidateKey(ctx context.Context, rawKey string) (*domain.APIKey, error) {
	// Hash the incoming key
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	key, err := s.repo.GetByKeyHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errors.New("invalid api key")
	}

	// Async update last used
	go func() {
		_ = s.repo.UpdateLastUsed(context.Background(), key.ID)
	}()

	return key, nil
}
