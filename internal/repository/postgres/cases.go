package postgres

import (
	"context"
	"fmt"
	"strings"

	"kyd/internal/casework"
	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type CaseRepository struct {
	db *sqlx.DB
}

func NewCaseRepository(db *sqlx.DB) *CaseRepository {
	return &CaseRepository{db: db}
}

func buildCasesWhere(filter *casework.Filter) (string, []interface{}) {
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

	if filter.Status != nil && strings.TrimSpace(*filter.Status) != "" {
		add("status = $%d", strings.TrimSpace(*filter.Status))
	}
	if filter.Priority != nil && strings.TrimSpace(*filter.Priority) != "" {
		add("priority = $%d", strings.TrimSpace(*filter.Priority))
	}
	if filter.EntityType != nil && strings.TrimSpace(*filter.EntityType) != "" {
		add("entity_type = $%d", strings.TrimSpace(*filter.EntityType))
	}
	if filter.EntityID != nil && strings.TrimSpace(*filter.EntityID) != "" {
		add("entity_id = $%d", strings.TrimSpace(*filter.EntityID))
	}
	if filter.Query != nil && strings.TrimSpace(*filter.Query) != "" {
		q := "%" + strings.TrimSpace(*filter.Query) + "%"
		args = append(args, q)
		i := len(args)
		args = append(args, q)
		j := len(args)
		clauses = append(clauses, fmt.Sprintf("(title ILIKE $%d OR COALESCE(description,'') ILIKE $%d)", i, j))
	}

	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *CaseRepository) CreateCase(ctx context.Context, c *domain.Case) error {
	query := `
		INSERT INTO admin_schema.cases (
			id, title, description, status, priority, entity_type, entity_id,
			created_by, assigned_to, resolved_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12
		)
	`
	_, err := r.db.ExecContext(ctx, query,
		c.ID, c.Title, c.Description, c.Status, c.Priority, c.EntityType, c.EntityID,
		c.CreatedBy, c.AssignedTo, c.ResolvedAt, c.CreatedAt, c.UpdatedAt,
	)
	return errors.Wrap(err, "failed to create case")
}

func (r *CaseRepository) UpdateCase(ctx context.Context, c *domain.Case) error {
	query := `
		UPDATE admin_schema.cases SET
			title = $1,
			description = $2,
			status = $3,
			priority = $4,
			assigned_to = $5,
			resolved_at = $6,
			updated_at = $7
		WHERE id = $8
	`
	_, err := r.db.ExecContext(ctx, query,
		c.Title, c.Description, c.Status, c.Priority, c.AssignedTo, c.ResolvedAt, c.UpdatedAt, c.ID,
	)
	return errors.Wrap(err, "failed to update case")
}

func (r *CaseRepository) GetCaseByID(ctx context.Context, id uuid.UUID) (*domain.Case, error) {
	var c domain.Case
	err := r.db.GetContext(ctx, &c, `SELECT * FROM admin_schema.cases WHERE id = $1`, id)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get case")
	}
	return &c, nil
}

func (r *CaseRepository) ListCases(ctx context.Context, filter *casework.Filter, limit, offset int) ([]domain.Case, int, error) {
	where, args := buildCasesWhere(filter)

	query := `
		SELECT *
		FROM admin_schema.cases
		` + where + `
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`
	args = append(args, limit, offset)
	query = fmt.Sprintf(query, len(args)-1, len(args))

	var items []domain.Case
	if err := r.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, 0, errors.Wrap(err, "failed to list cases")
	}

	countQuery := `SELECT COUNT(*) FROM admin_schema.cases ` + where
	var total int
	if err := r.db.GetContext(ctx, &total, countQuery, args[:len(args)-2]...); err != nil {
		return nil, 0, errors.Wrap(err, "failed to count cases")
	}

	return items, total, nil
}

func (r *CaseRepository) CreateCaseEvent(ctx context.Context, e *domain.CaseEvent) error {
	query := `
		INSERT INTO admin_schema.case_events (
			id, case_id, event_type, message, metadata, created_by, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
	`
	_, err := r.db.ExecContext(ctx, query, e.ID, e.CaseID, e.EventType, e.Message, e.Metadata, e.CreatedBy, e.CreatedAt)
	return errors.Wrap(err, "failed to create case event")
}

func (r *CaseRepository) ListCaseEvents(ctx context.Context, caseID uuid.UUID, limit, offset int) ([]domain.CaseEvent, int, error) {
	var items []domain.CaseEvent
	query := `
		SELECT *
		FROM admin_schema.case_events
		WHERE case_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	if err := r.db.SelectContext(ctx, &items, query, caseID, limit, offset); err != nil {
		return nil, 0, errors.Wrap(err, "failed to list case events")
	}

	var total int
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM admin_schema.case_events WHERE case_id = $1`, caseID); err != nil {
		return nil, 0, errors.Wrap(err, "failed to count case events")
	}
	return items, total, nil
}

