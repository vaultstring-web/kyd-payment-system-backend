// ==============================================================================
// WALLET SERVICE - internal/wallet/service.go
// ==============================================================================
package wallet

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"kyd/internal/domain"
	"kyd/pkg/errors"
	"kyd/pkg/logger"
)

type Service struct {
	repo   Repository
	logger logger.Logger
}

func NewService(repo Repository, log logger.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: log,
	}
}

type CreateWalletRequest struct {
	UserID   uuid.UUID       `json:"user_id" validate:"required"`
	Currency domain.Currency `json:"currency" validate:"required"`
}

// CreateWallet creates a new wallet for a user
func (s *Service) CreateWallet(ctx context.Context, req *CreateWalletRequest) (*domain.Wallet, error) {
	// Check if wallet already exists
	existing, err := s.repo.FindByUserAndCurrency(ctx, req.UserID, req.Currency)
	if err == nil && existing != nil {
		return nil, errors.ErrWalletAlreadyExists
	}

	wallet := &domain.Wallet{
		ID:               uuid.New(),
		UserID:           req.UserID,
		Currency:         req.Currency,
		AvailableBalance: decimal.Zero,
		LedgerBalance:    decimal.Zero,
		ReservedBalance:  decimal.Zero,
		Status:           domain.WalletStatusActive,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.repo.Create(ctx, wallet); err != nil {
		return nil, err
	}

	s.logger.Info("Wallet created", map[string]interface{}{
		"wallet_id": wallet.ID,
		"user_id":   req.UserID,
		"currency":  req.Currency,
	})

	return wallet, nil
}

func (s *Service) GetWallet(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) GetUserWallets(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
	return s.repo.FindByUserID(ctx, userID)
}

func (s *Service) GetBalance(ctx context.Context, walletID uuid.UUID) (*BalanceResponse, error) {
	wallet, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	return &BalanceResponse{
		WalletID:         wallet.ID,
		Currency:         wallet.Currency,
		AvailableBalance: wallet.AvailableBalance,
		LedgerBalance:    wallet.LedgerBalance,
		ReservedBalance:  wallet.ReservedBalance,
	}, nil
}

type BalanceResponse struct {
	WalletID         uuid.UUID       `json:"wallet_id"`
	Currency         domain.Currency `json:"currency"`
	AvailableBalance decimal.Decimal `json:"available_balance"`
	LedgerBalance    decimal.Decimal `json:"ledger_balance"`
	ReservedBalance  decimal.Decimal `json:"reserved_balance"`
}

type Repository interface {
	Create(ctx context.Context, wallet *domain.Wallet) error
	Update(ctx context.Context, wallet *domain.Wallet) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error)
	FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error)
	DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
	CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
}