package postgres

import (
	"context"
	"database/sql"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type SecurityRepository struct {
	db *sqlx.DB
}

func NewSecurityRepository(db *sqlx.DB) *SecurityRepository {
	return &SecurityRepository{db: db}
}

func (r *SecurityRepository) GetSecurityEvents(ctx context.Context, limit, offset int) ([]domain.SecurityEvent, int, error) {
	var events []domain.SecurityEvent
	var total int

	query := `
		SELECT id, type, severity, description, status, user_id, ip_address, created_at, resolved_at, resolved_by 
		FROM admin_schema.security_events 
		ORDER BY created_at DESC 
		LIMIT $1 OFFSET $2`

	err := r.db.SelectContext(ctx, &events, query, limit, offset)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get security events")
	}

	err = r.db.GetContext(ctx, &total, "SELECT COUNT(*) FROM admin_schema.security_events")
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get total security events count")
	}

	return events, total, nil
}

func (r *SecurityRepository) LogSecurityEvent(ctx context.Context, event *domain.SecurityEvent) error {
	query := `
		INSERT INTO admin_schema.security_events (type, severity, description, status, user_id, ip_address, created_at)
		VALUES (:type, :severity, :description, :status, :user_id, :ip_address, :created_at)
		RETURNING id`

	rows, err := r.db.NamedQueryContext(ctx, query, event)
	if err != nil {
		return errors.Wrap(err, "failed to log security event")
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&event.ID); err != nil {
			return errors.Wrap(err, "failed to scan security event id")
		}
	}

	return nil
}

func (r *SecurityRepository) GetBlocklist(ctx context.Context) ([]domain.BlocklistEntry, error) {
	var entries []domain.BlocklistEntry
	query := `
		SELECT id, type, value, reason, expires_at, created_at, created_by AS added_by
		FROM admin_schema.blocklist 
		WHERE is_active = true 
		ORDER BY created_at DESC`

	err := r.db.SelectContext(ctx, &entries, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get blocklist")
	}

	return entries, nil
}

func (r *SecurityRepository) AddToBlocklist(ctx context.Context, entry *domain.BlocklistEntry) error {
	query := `
		INSERT INTO admin_schema.blocklist (type, value, reason, is_active, expires_at, created_by, created_at)
		VALUES (:type, :value, :reason, true, :expires_at, :added_by, :created_at)
		RETURNING id`

	rows, err := r.db.NamedQueryContext(ctx, query, entry)
	if err != nil {
		return errors.Wrap(err, "failed to add to blocklist")
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&entry.ID); err != nil {
			return errors.Wrap(err, "failed to scan blocklist id")
		}
	}
	return nil
}

func (r *SecurityRepository) RemoveFromBlocklist(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE admin_schema.blocklist SET is_active = false WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "failed to remove from blocklist")
	}
	return nil
}

func (r *SecurityRepository) IsBlacklisted(ctx context.Context, value string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM admin_schema.blocklist WHERE value = $1 AND is_active = true`
	err := r.db.GetContext(ctx, &count, query, value)
	if err != nil {
		return false, errors.Wrap(err, "failed to check blocklist")
	}
	return count > 0, nil
}

func (r *SecurityRepository) GetSystemHealth(ctx context.Context) ([]domain.SystemHealthMetric, error) {
	var metrics []domain.SystemHealthMetric
	// Use the view we created
	query := `
		SELECT metric AS metric_name, value, timestamp AS recorded_at
		FROM admin_schema.system_health_metrics`

	err := r.db.SelectContext(ctx, &metrics, query)
	if err != nil {
		if err == sql.ErrNoRows {
			return []domain.SystemHealthMetric{}, nil
		}
		return nil, errors.Wrap(err, "failed to get system health metrics")
	}
	return metrics, nil
}

func (r *SecurityRepository) RecordHealthSnapshot(ctx context.Context, metric *domain.SystemHealthMetric) error {
	query := `
		INSERT INTO admin_schema.system_health_snapshots (metric_name, value, recorded_at)
		VALUES (:metric_name, :value, :recorded_at)`

	_, err := r.db.NamedExecContext(ctx, query, metric)
	if err != nil {
		return errors.Wrap(err, "failed to record health snapshot")
	}
	return nil
}

// UpdateSecurityEventStatus updates the status of a security event
func (r *SecurityRepository) UpdateSecurityEventStatus(ctx context.Context, id uuid.UUID, status string, resolvedBy *uuid.UUID) error {
	var query string
	var args []interface{}

	if status == "resolved" || status == "false_positive" {
		query = `UPDATE admin_schema.security_events SET status = $1, resolved_at = NOW(), resolved_by = $2 WHERE id = $3`
		args = []interface{}{status, resolvedBy, id}
	} else {
		query = `UPDATE admin_schema.security_events SET status = $1 WHERE id = $2`
		args = []interface{}{status, id}
	}

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return errors.Wrap(err, "failed to update security event status")
	}
	return nil
}
