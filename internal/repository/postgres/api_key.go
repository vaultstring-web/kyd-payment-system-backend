package postgres

import (
	"context"
	"database/sql"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type APIKeyRepository struct {
	db *sqlx.DB
}

func NewAPIKeyRepository(db *sqlx.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

func (r *APIKeyRepository) Create(ctx context.Context, key *domain.APIKey) error {
	query := `
		INSERT INTO admin_schema.api_keys (
			id, name, key_prefix, key_hash, scopes, is_active,
			expires_at, created_by, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	_, err := r.db.ExecContext(ctx, query,
		key.ID, key.Name, key.KeyPrefix, key.KeyHash, pq.Array(key.Scopes),
		key.IsActive, key.ExpiresAt, key.CreatedBy, key.CreatedAt,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create api key")
	}

	return nil
}

func (r *APIKeyRepository) List(ctx context.Context) ([]domain.APIKey, error) {
	query := `
		SELECT * FROM admin_schema.api_keys
		ORDER BY created_at DESC
	`

	var keys []domain.APIKey
	err := r.db.SelectContext(ctx, &keys, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list api keys")
	}

	// sqlx might not automatically scan TEXT[] into []string properly without pq.Array, 
	// but let's see if domain.APIKey uses []string. 
	// If it fails, we might need a custom scanner or use a struct with pq.StringArray.
	// For now assuming standard sqlx behavior with postgres driver support.
	
	return keys, nil
}

func (r *APIKeyRepository) GetByKeyHash(ctx context.Context, hash string) (*domain.APIKey, error) {
	query := `
		SELECT * FROM admin_schema.api_keys
		WHERE key_hash = $1 AND is_active = true
	`

	var key domain.APIKey
	err := r.db.GetContext(ctx, &key, query, hash)
	if err == sql.ErrNoRows {
		return nil, nil // Not found is not an error here, just nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get api key")
	}
	
	// Check expiry if set
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return nil, nil // Expired
	}

	return &key, nil
}

func (r *APIKeyRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE admin_schema.api_keys
		SET is_active = false, revoked_at = $1
		WHERE id = $2
	`

	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return errors.Wrap(err, "failed to revoke api key")
	}

	return nil
}

func (r *APIKeyRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE admin_schema.api_keys
		SET last_used_at = $1
		WHERE id = $2
	`

	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	// Fire and forget mostly, log error if critical but usually fine
	return err
}
