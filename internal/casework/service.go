package casework

import (
	"context"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
)

type Filter struct {
	Status     *string
	Priority   *string
	EntityType *string
	EntityID   *string
	Query      *string
}

type Repository interface {
	CreateCase(ctx context.Context, c *domain.Case) error
	UpdateCase(ctx context.Context, c *domain.Case) error
	GetCaseByID(ctx context.Context, id uuid.UUID) (*domain.Case, error)
	ListCases(ctx context.Context, filter *Filter, limit, offset int) ([]domain.Case, int, error)
	CreateCaseEvent(ctx context.Context, e *domain.CaseEvent) error
	ListCaseEvents(ctx context.Context, caseID uuid.UUID, limit, offset int) ([]domain.CaseEvent, int, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateCase(ctx context.Context, c *domain.Case, initialNote *string) (*domain.Case, error) {
	now := time.Now()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = domain.CaseStatusOpen
	}
	if c.Priority == "" {
		c.Priority = domain.CasePriorityMedium
	}

	if err := s.repo.CreateCase(ctx, c); err != nil {
		return nil, err
	}

	if initialNote != nil && *initialNote != "" {
		ev := &domain.CaseEvent{
			ID:        uuid.New(),
			CaseID:    c.ID,
			EventType: domain.CaseEventNote,
			Message:   initialNote,
			Metadata:  domain.Metadata{},
			CreatedBy: c.CreatedBy,
			CreatedAt: now,
		}
		_ = s.repo.CreateCaseEvent(ctx, ev)
	}

	return c, nil
}

func (s *Service) UpdateCase(ctx context.Context, updated *domain.Case, actorID *uuid.UUID, note *string) (*domain.Case, error) {
	existing, err := s.repo.GetCaseByID(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	changedStatus := existing.Status != updated.Status
	changedAssignee := (existing.AssignedTo == nil) != (updated.AssignedTo == nil)
	if !changedAssignee && existing.AssignedTo != nil && updated.AssignedTo != nil && *existing.AssignedTo != *updated.AssignedTo {
		changedAssignee = true
	}

	updated.CreatedAt = existing.CreatedAt
	updated.EntityType = existing.EntityType
	updated.EntityID = existing.EntityID
	updated.CreatedBy = existing.CreatedBy
	updated.UpdatedAt = now

	if updated.Status == domain.CaseStatusResolved || updated.Status == domain.CaseStatusFalsePositive {
		if updated.ResolvedAt == nil {
			updated.ResolvedAt = &now
		}
	} else {
		updated.ResolvedAt = nil
	}

	if err := s.repo.UpdateCase(ctx, updated); err != nil {
		return nil, err
	}

	if changedStatus {
		msg := "status changed"
		ev := &domain.CaseEvent{
			ID:        uuid.New(),
			CaseID:    updated.ID,
			EventType: domain.CaseEventStatus,
			Message:   &msg,
			Metadata:  domain.Metadata{"from": existing.Status, "to": updated.Status},
			CreatedBy: actorID,
			CreatedAt: now,
		}
		_ = s.repo.CreateCaseEvent(ctx, ev)
	}
	if changedAssignee {
		msg := "assignment changed"
		ev := &domain.CaseEvent{
			ID:        uuid.New(),
			CaseID:    updated.ID,
			EventType: domain.CaseEventAssignment,
			Message:   &msg,
			Metadata:  domain.Metadata{"from": existing.AssignedTo, "to": updated.AssignedTo},
			CreatedBy: actorID,
			CreatedAt: now,
		}
		_ = s.repo.CreateCaseEvent(ctx, ev)
	}
	if note != nil && *note != "" {
		ev := &domain.CaseEvent{
			ID:        uuid.New(),
			CaseID:    updated.ID,
			EventType: domain.CaseEventNote,
			Message:   note,
			Metadata:  domain.Metadata{},
			CreatedBy: actorID,
			CreatedAt: now,
		}
		_ = s.repo.CreateCaseEvent(ctx, ev)
	}

	return updated, nil
}

func (s *Service) GetCase(ctx context.Context, id uuid.UUID) (*domain.Case, error) {
	return s.repo.GetCaseByID(ctx, id)
}

func (s *Service) ListCases(ctx context.Context, filter *Filter, limit, offset int) ([]domain.Case, int, error) {
	return s.repo.ListCases(ctx, filter, limit, offset)
}

func (s *Service) ListCaseEvents(ctx context.Context, caseID uuid.UUID, limit, offset int) ([]domain.CaseEvent, int, error) {
	return s.repo.ListCaseEvents(ctx, caseID, limit, offset)
}

func (s *Service) AddCaseNote(ctx context.Context, caseID uuid.UUID, actorID *uuid.UUID, note string) error {
	now := time.Now()
	msg := note
	ev := &domain.CaseEvent{
		ID:        uuid.New(),
		CaseID:    caseID,
		EventType: domain.CaseEventNote,
		Message:   &msg,
		Metadata:  domain.Metadata{},
		CreatedBy: actorID,
		CreatedAt: now,
	}
	return s.repo.CreateCaseEvent(ctx, ev)
}

