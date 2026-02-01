package postgres

import (
	"context"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
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

// FindByUserID returns audit logs for a specific user.
func (r *AuditRepository) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error) {
	var logs []*domain.AuditLog
	query := `
		SELECT 
			id, user_id, action, entity_type, entity_id,
			old_values, new_values, ip_address, user_agent,
			request_id, status_code, error_message, created_at
		FROM admin_schema.audit_logs
		WHERE user_id = $1 OR (entity_type = 'user' AND entity_id = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	err := r.db.SelectContext(ctx, &logs, query, userID, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find audit logs")
	}
	return logs, nil
}

// FindAll returns all audit logs with pagination.
func (r *AuditRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.AuditLog, error) {
	var logs []*domain.AuditLog
	query := `
		SELECT 
			id, user_id, action, entity_type, entity_id,
			old_values, new_values, ip_address, user_agent,
			request_id, status_code, error_message, created_at
		FROM admin_schema.audit_logs
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	err := r.db.SelectContext(ctx, &logs, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list audit logs")
	}
	return logs, nil
}

// CountAll returns the total number of audit logs.
func (r *AuditRepository) CountAll(ctx context.Context) (int, error) {
	var total int
	query := `SELECT COUNT(*) FROM admin_schema.audit_logs`
	err := r.db.GetContext(ctx, &total, query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count audit logs")
	}
	return total, nil
}
