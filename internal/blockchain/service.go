package blockchain

import (
	"context"
	"time"

	"kyd/internal/domain"
	"kyd/internal/repository/postgres"

	"github.com/google/uuid"
)

type Service struct {
	repo *postgres.BlockchainNetworkRepository
}

func NewService(repo *postgres.BlockchainNetworkRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateNetwork(ctx context.Context, network *domain.BlockchainNetworkInfo) error {
	if network.ID == "" {
		network.ID = uuid.New().String()
	}
	network.CreatedAt = time.Now()
	network.UpdatedAt = time.Now()
	return s.repo.Create(ctx, network)
}

func (s *Service) UpdateNetwork(ctx context.Context, network *domain.BlockchainNetworkInfo) error {
	network.UpdatedAt = time.Now()
	return s.repo.Update(ctx, network)
}

func (s *Service) GetNetwork(ctx context.Context, id string) (*domain.BlockchainNetworkInfo, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) ListNetworks(ctx context.Context) ([]*domain.BlockchainNetworkInfo, error) {
	return s.repo.FindAll(ctx)
}

func (s *Service) DeleteNetwork(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
