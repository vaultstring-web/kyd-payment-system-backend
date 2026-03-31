package postgres

import (
	"context"

	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// NotificationRepository provides access to customer_schema.notifications.
type NotificationRepository struct {
	db *sqlx.DB
}

// NewNotificationRepository creates a new NotificationRepository.
func NewNotificationRepository(db *sqlx.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// Create inserts a new notification record.
func (r *NotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	values := map[string]interface{}{
		"id":          n.ID,
		"user_id":     n.UserID,
		"type":        n.Type,
		"title":       n.Title,
		"message":     n.Message,
		"data":        n.Data,
		"is_read":     n.IsRead,
		"is_archived": n.IsArchived,
		"created_at":  n.CreatedAt,
	}

	query := `
		INSERT INTO customer_schema.notifications (
			id, user_id, type, title, message, data,
			is_read, is_archived, created_at
		) VALUES (
			:id, :user_id, :type, :title, :message, :data,
			:is_read, :is_archived, :created_at
		)
	`

	_, err := r.db.NamedExecContext(ctx, query, values)
	if err != nil {
		return errors.Wrap(err, "failed to create notification")
	}

	return nil
}

// ListByUser returns notifications for a specific user with pagination.
func (r *NotificationRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Notification, int, error) {
	var rows []*domain.Notification

	query := `
		SELECT
			id, user_id, type, title, message, data,
			is_read, is_archived, created_at
		FROM customer_schema.notifications
		WHERE user_id = $1 AND is_archived = FALSE
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	if err := r.db.SelectContext(ctx, &rows, query, userID, limit, offset); err != nil {
		return nil, 0, errors.Wrap(err, "failed to list notifications")
	}

	var total int
	countQuery := `
		SELECT COUNT(*) 
		FROM customer_schema.notifications
		WHERE user_id = $1 AND is_archived = FALSE
	`
	if err := r.db.GetContext(ctx, &total, countQuery, userID); err != nil {
		return nil, 0, errors.Wrap(err, "failed to count notifications")
	}

	return rows, total, nil
}

// ListAll returns all notifications for admin views with pagination.
func (r *NotificationRepository) ListAll(ctx context.Context, limit, offset int) ([]*domain.Notification, int, error) {
	var rows []*domain.Notification

	query := `
		SELECT
			id, user_id, type, title, message, data,
			is_read, is_archived, created_at
		FROM customer_schema.notifications
		WHERE is_archived = FALSE
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	if err := r.db.SelectContext(ctx, &rows, query, limit, offset); err != nil {
		return nil, 0, errors.Wrap(err, "failed to list all notifications")
	}

	var total int
	countQuery := `
		SELECT COUNT(*) 
		FROM customer_schema.notifications
		WHERE is_archived = FALSE
	`
	if err := r.db.GetContext(ctx, &total, countQuery); err != nil {
		return nil, 0, errors.Wrap(err, "failed to count all notifications")
	}

	return rows, total, nil
}

func (r *NotificationRepository) MarkRead(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE customer_schema.notifications
		SET is_read = TRUE
		WHERE id = $1
	`
	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return errors.Wrap(err, "failed to mark notification read")
	}
	return nil
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context) error {
	query := `
		UPDATE customer_schema.notifications
		SET is_read = TRUE
		WHERE is_read = FALSE AND is_archived = FALSE
	`
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		return errors.Wrap(err, "failed to mark all notifications read")
	}
	return nil
}

func (r *NotificationRepository) Archive(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE customer_schema.notifications
		SET is_archived = TRUE
		WHERE id = $1
	`
	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return errors.Wrap(err, "failed to archive notification")
	}
	return nil
}

