package postgres

import (
	"context"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/jmoiron/sqlx"
)

// AuditRepository implements audit log persistence.
type AuditRepository struct {
	db *sqlx.DB
}

// NewAuditRepository creates a new AuditRepository.
func NewAuditRepository(db *sqlx.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Create inserts a new audit log entry.
func (r *AuditRepository) Create(ctx context.Context, log *domain.AuditLog) error {
	query := `
		INSERT INTO admin_schema.audit_logs (
			id, user_id, action, entity_type, entity_id,
			old_values, new_values, ip_address, user_agent,
			request_id, status_code, error_message, created_at
		) VALUES (
			:id, :user_id, :action, :entity_type, :entity_id,
			:old_values, :new_values, :ip_address, :user_agent,
			:request_id, :status_code, :error_message, :created_at
		)
	`

	_, err := r.db.NamedExecContext(ctx, query, log)
	if err != nil {
		return errors.Wrap(err, "failed to create audit log")
	}

	return nil
}
