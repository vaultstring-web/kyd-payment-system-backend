package security

import (
	"context"

	"kyd/internal/domain"
	"github.com/google/uuid"
)

type Repository interface {
	GetSecurityEvents(ctx context.Context, limit, offset int) ([]domain.SecurityEvent, int, error)
	LogSecurityEvent(ctx context.Context, event *domain.SecurityEvent) error
	GetBlocklist(ctx context.Context) ([]domain.BlocklistEntry, error)
	AddToBlocklist(ctx context.Context, entry *domain.BlocklistEntry) error
	RemoveFromBlocklist(ctx context.Context, id uuid.UUID) error
	GetSystemHealth(ctx context.Context) ([]domain.SystemHealthMetric, error)
	RecordHealthSnapshot(ctx context.Context, metric *domain.SystemHealthMetric) error
	UpdateSecurityEventStatus(ctx context.Context, id uuid.UUID, status string, resolvedBy *uuid.UUID) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetSecurityEvents(ctx context.Context, limit, offset int) ([]domain.SecurityEvent, int, error) {
	return s.repo.GetSecurityEvents(ctx, limit, offset)
}

func (s *Service) LogSecurityEvent(ctx context.Context, event *domain.SecurityEvent) error {
	return s.repo.LogSecurityEvent(ctx, event)
}

func (s *Service) GetBlocklist(ctx context.Context) ([]domain.BlocklistEntry, error) {
	return s.repo.GetBlocklist(ctx)
}

func (s *Service) AddToBlocklist(ctx context.Context, entry *domain.BlocklistEntry) error {
	return s.repo.AddToBlocklist(ctx, entry)
}

func (s *Service) RemoveFromBlocklist(ctx context.Context, id uuid.UUID) error {
	return s.repo.RemoveFromBlocklist(ctx, id)
}

func (s *Service) GetSystemHealth(ctx context.Context) ([]domain.SystemHealthMetric, error) {
	return s.repo.GetSystemHealth(ctx)
}

func (s *Service) RecordHealthSnapshot(ctx context.Context, metric *domain.SystemHealthMetric) error {
	return s.repo.RecordHealthSnapshot(ctx, metric)
}

func (s *Service) UpdateSecurityEventStatus(ctx context.Context, id uuid.UUID, status string, resolvedBy *uuid.UUID) error {
	return s.repo.UpdateSecurityEventStatus(ctx, id, status, resolvedBy)
}
