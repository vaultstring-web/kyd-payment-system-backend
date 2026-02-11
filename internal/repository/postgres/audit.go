package postgres

import (
	"context"

	"kyd/internal/domain"
	"kyd/internal/security"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// AuditRepository implements audit log persistence.
type AuditRepository struct {
	db     *sqlx.DB
	crypto *security.CryptoService
}

// NewAuditRepository creates a new AuditRepository.
func NewAuditRepository(db *sqlx.DB, crypto *security.CryptoService) *AuditRepository {
	return &AuditRepository{db: db, crypto: crypto}
}

// Create inserts a new audit log entry.
func (r *AuditRepository) Create(ctx context.Context, log *domain.AuditLog) error {
	var entityID interface{} = nil
	if log.EntityID != "" {
		entityID = log.EntityID
	}

	// Use map to handle nullable fields explicitly
	var oldValues interface{} = nil
	if len(log.OldValues) > 0 {
		oldValues = string(log.OldValues)
	}
	var newValues interface{} = nil
	if len(log.NewValues) > 0 {
		newValues = string(log.NewValues)
	}

	values := map[string]interface{}{
		"id":            log.ID,
		"user_id":       log.UserID,
		"action":        log.Action,
		"entity_type":   log.EntityType,
		"entity_id":     entityID,
		"old_values":    oldValues,
		"new_values":    newValues,
		"ip_address":    log.IPAddress,
		"user_agent":    log.UserAgent,
		"request_id":    log.RequestID,
		"status_code":   log.StatusCode,
		"error_message": log.ErrorMessage,
		"created_at":    log.CreatedAt,
	}

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

	_, err := r.db.NamedExecContext(ctx, query, values)
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
			a.id, a.user_id, a.action, COALESCE(a.entity_type, '') AS entity_type,
            CASE WHEN a.entity_id IS NULL THEN '00000000-0000-0000-0000-000000000000' ELSE a.entity_id END AS entity_id,
			COALESCE(a.old_values, '{}'::jsonb) AS old_values, COALESCE(a.new_values, '{}'::jsonb) AS new_values, 
            COALESCE(a.ip_address, '0.0.0.0') AS ip_address, COALESCE(a.user_agent, '') AS user_agent,
			COALESCE(a.request_id, '') AS request_id, a.status_code, COALESCE(a.error_message, '') AS error_message, a.created_at,
            COALESCE(u.email, '') AS user_email
		FROM admin_schema.audit_logs a
        LEFT JOIN customer_schema.users u ON a.user_id = u.id
		WHERE a.user_id = $1 OR (a.entity_type = 'user' AND a.entity_id = $2)
		ORDER BY a.created_at DESC
		LIMIT $3 OFFSET $4
	`
	// Convert UUID to string for entity_id check
	err := r.db.SelectContext(ctx, &logs, query, userID, userID.String(), limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find audit logs")
	}

	for _, log := range logs {
		if log.UserEmail != "" {
			decrypted, err := r.crypto.Decrypt(log.UserEmail)
			if err == nil {
				log.UserEmail = decrypted
			}
		}
	}

	return logs, nil
}

// FindAll returns all audit logs with pagination.
func (r *AuditRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.AuditLog, error) {
	var logs []*domain.AuditLog
	query := `
		SELECT 
			a.id, a.user_id, a.action, COALESCE(a.entity_type, '') AS entity_type,
            CASE WHEN a.entity_id IS NULL THEN '00000000-0000-0000-0000-000000000000' ELSE a.entity_id END AS entity_id,
			COALESCE(a.old_values, '{}'::jsonb) AS old_values, COALESCE(a.new_values, '{}'::jsonb) AS new_values, 
            COALESCE(a.ip_address, '0.0.0.0') AS ip_address, COALESCE(a.user_agent, '') AS user_agent,
			COALESCE(a.request_id, '') AS request_id, a.status_code, COALESCE(a.error_message, '') AS error_message, a.created_at,
            COALESCE(u.email, '') AS user_email
		FROM admin_schema.audit_logs a
        LEFT JOIN customer_schema.users u ON a.user_id = u.id
		ORDER BY a.created_at DESC
		LIMIT $1 OFFSET $2
	`
	err := r.db.SelectContext(ctx, &logs, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list audit logs")
	}

	for _, log := range logs {
		if log.UserEmail != "" {
			decrypted, err := r.crypto.Decrypt(log.UserEmail)
			if err == nil {
				log.UserEmail = decrypted
			}
		} else if log.Action == "LOGIN_FAILED" && log.EntityID != "" {
			// For login failed, entity_id is the email
			log.UserEmail = log.EntityID
		}
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
