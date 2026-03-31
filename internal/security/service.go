package security

import (
	"context"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
)

type EventFilter struct {
	Type      *string
	Severity  *string
	Status    *string
	UserID    *uuid.UUID
	IPAddress *string
	Query     *string
}

type Repository interface {
	GetSecurityEvents(ctx context.Context, filter *EventFilter, limit, offset int) ([]domain.SecurityEvent, int, error)
	LogSecurityEvent(ctx context.Context, event *domain.SecurityEvent) error
	GetBlocklist(ctx context.Context) ([]domain.BlocklistEntry, error)
	AddToBlocklist(ctx context.Context, entry *domain.BlocklistEntry) error
	RemoveFromBlocklist(ctx context.Context, id uuid.UUID) error
	IsBlacklisted(ctx context.Context, value string) (bool, error)
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

func (s *Service) GetSecurityEvents(ctx context.Context, filter *EventFilter, limit, offset int) ([]domain.SecurityEvent, int, error) {
	return s.repo.GetSecurityEvents(ctx, filter, limit, offset)
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

func (s *Service) IsBlacklisted(ctx context.Context, value string) (bool, error) {
	return s.repo.IsBlacklisted(ctx, value)
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

func (s *Service) NewBlockchainMismatchEvent(network string, shardID int, height uint64, vector string, anomalies int, metadata domain.Metadata) *domain.SecurityEvent {
	return &domain.SecurityEvent{
		Type:        domain.SecurityEventTypeBlockchainMismatch,
		Severity:    domain.SecuritySeverityHigh,
		Description: "blockchain_mismatch",
		Status:      domain.SecurityEventStatusOpen,
		Metadata: domain.Metadata{
			"network":              network,
			"shard_id":             shardID,
			"height":               height,
			"anomaly_count":        anomalies,
			"deterministic_vector": vector,
			"extra":                metadata,
		},
		CreatedAt: time.Now(),
	}
}
