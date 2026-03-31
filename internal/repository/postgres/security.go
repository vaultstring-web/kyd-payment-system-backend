// Package postgres contains Postgres-backed repositories.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"kyd/internal/domain"
	"kyd/internal/security"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// SecurityRepository persists security events, blocklist entries, and health snapshots.
type SecurityRepository struct {
	db *sqlx.DB
}

// NewSecurityRepository creates a new SecurityRepository.
func NewSecurityRepository(db *sqlx.DB) *SecurityRepository {
	return &SecurityRepository{db: db}
}

// GetSecurityEvents returns the most recent security events plus the total count.
func (r *SecurityRepository) GetSecurityEvents(ctx context.Context, filter *security.EventFilter, limit, offset int) ([]domain.SecurityEvent, int, error) {
	var events []domain.SecurityEvent
	var total int

	baseQuery := `
		SELECT
			id,
			event_type AS type,
			severity,
			''::text AS description,
			status,
			user_id,
			ip_address,
			created_at,
			resolved_at,
			resolved_by,
			details AS metadata
		FROM admin_schema.security_events`

	where, args := buildSecurityEventsWhere(filter)
	query := fmt.Sprintf("%s %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d", baseQuery, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	err := r.db.SelectContext(ctx, &events, query, args...)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get security events")
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM admin_schema.security_events %s", where)
	err = r.db.GetContext(ctx, &total, countQuery, args[:len(args)-2]...)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get total security events count")
	}

	return events, total, nil
}

func buildSecurityEventsWhere(filter *security.EventFilter) (string, []interface{}) {
	if filter == nil {
		return "", nil
	}

	var (
		clauses []string
		args    []interface{}
	)

	add := func(clause string, value interface{}) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}

	if filter.Type != nil && strings.TrimSpace(*filter.Type) != "" {
		add("event_type = $%d", strings.TrimSpace(*filter.Type))
	}
	if filter.Severity != nil && strings.TrimSpace(*filter.Severity) != "" {
		add("severity = $%d", strings.TrimSpace(*filter.Severity))
	}
	if filter.Status != nil && strings.TrimSpace(*filter.Status) != "" {
		add("status = $%d", strings.TrimSpace(*filter.Status))
	}
	if filter.UserID != nil && *filter.UserID != uuid.Nil {
		add("user_id = $%d", *filter.UserID)
	}
	if filter.IPAddress != nil && strings.TrimSpace(*filter.IPAddress) != "" {
		add("ip_address = $%d", strings.TrimSpace(*filter.IPAddress))
	}
	if filter.Query != nil && strings.TrimSpace(*filter.Query) != "" {
		q := "%" + strings.TrimSpace(*filter.Query) + "%"
		args = append(args, q)
		i := len(args)
		args = append(args, q)
		j := len(args)
		clauses = append(clauses, fmt.Sprintf("(event_type ILIKE $%d OR CAST(details AS TEXT) ILIKE $%d)", i, j))
	}

	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

// LogSecurityEvent inserts a security event.
func (r *SecurityRepository) LogSecurityEvent(ctx context.Context, event *domain.SecurityEvent) error {
	query := `
		INSERT INTO admin_schema.security_events (event_type, severity, user_id, ip_address, details, status, created_at)
		VALUES (:type, :severity, :user_id, :ip_address, :metadata, :status, :created_at)
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

// GetBlocklist returns the currently-active blocklist.
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

// AddToBlocklist creates an active blocklist entry.
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

// RemoveFromBlocklist deactivates a blocklist entry.
func (r *SecurityRepository) RemoveFromBlocklist(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE admin_schema.blocklist SET is_active = false WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "failed to remove from blocklist")
	}
	return nil
}

// IsBlacklisted checks whether a value is currently blacklisted.
func (r *SecurityRepository) IsBlacklisted(ctx context.Context, value string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM admin_schema.blocklist WHERE value = $1 AND is_active = true`
	err := r.db.GetContext(ctx, &count, query, value)
	if err != nil {
		return false, errors.Wrap(err, "failed to check blocklist")
	}
	return count > 0, nil
}

// GetSystemHealth returns the latest system health snapshots per metric.
func (r *SecurityRepository) GetSystemHealth(ctx context.Context) ([]domain.SystemHealthMetric, error) {
	var metrics []domain.SystemHealthMetric
	// Use the view we created
	query := `
		SELECT metric, value, status, change, timestamp AS recorded_at
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

// RecordHealthSnapshot writes a new health snapshot.
func (r *SecurityRepository) RecordHealthSnapshot(ctx context.Context, metric *domain.SystemHealthMetric) error {
	query := `
		INSERT INTO admin_schema.system_health_snapshots (metric, value, status, change, recorded_at)
		VALUES (:metric, :value, :status, :change, :recorded_at)`

	_, err := r.db.NamedExecContext(ctx, query, metric)
	if err != nil {
		return errors.Wrap(err, "failed to record health snapshot")
	}
	return nil
}

// UpdateSecurityEventStatus updates the status of a security event.
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
