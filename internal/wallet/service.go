// ==============================================================================
// WALLET SERVICE - internal/wallet/service.go
// ==============================================================================
package wallet

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/errors"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	repo     Repository
	txRepo   TransactionRepository
	userRepo UserRepository
	logger   logger.Logger
}

func NewService(repo Repository, txRepo TransactionRepository, userRepo UserRepository, log logger.Logger) *Service {
	return &Service{
		repo:     repo,
		txRepo:   txRepo,
		userRepo: userRepo,
		logger:   log,
	}
}

type CreateWalletRequest struct {
	UserID   uuid.UUID       `json:"user_id" validate:"required"`
	Currency domain.Currency `json:"currency" validate:"required"`
}

// CreateWallet creates a new wallet for a user
func (s *Service) CreateWallet(ctx context.Context, req *CreateWalletRequest) (*domain.Wallet, error) {
	user, err := s.userRepo.FindByID(ctx, req.UserID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch user for wallet creation")
	}

	switch user.CountryCode {
	case "CN":
		if req.Currency != domain.CNY {
			return nil, errors.ErrCurrencyNotAllowed
		}
	case "MW":
		if req.Currency != domain.MWK {
			return nil, errors.ErrCurrencyNotAllowed
		}
	default:
		return nil, errors.ErrCurrencyNotAllowed
	}

	// Check if wallet already exists
	existing, err := s.repo.FindByUserAndCurrency(ctx, req.UserID, req.Currency)
	if err == nil && existing != nil {
		return nil, errors.ErrWalletAlreadyExists
	}

	walletNumber, err := s.generateWalletNumber()
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate wallet number")
	}

	wallet := &domain.Wallet{
		ID:               uuid.New(),
		UserID:           req.UserID,
		WalletAddress:    &walletNumber,
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

func (s *Service) GetAllWallets(ctx context.Context, limit, offset int) ([]*BalanceResponse, int, error) {
	wallets, err := s.repo.FindAll(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.repo.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	var responses []*BalanceResponse
	for _, wallet := range wallets {
		user, err := s.userRepo.FindByID(ctx, wallet.UserID)
		if err != nil {
			// Skip wallet if user not found (integrity issue, but shouldn't block list)
			s.logger.Error("User not found for wallet", map[string]interface{}{"wallet_id": wallet.ID, "user_id": wallet.UserID})
			continue
		}

		var formattedAddress string
		if wallet.WalletAddress != nil && len(*wallet.WalletAddress) == 16 {
			addr := *wallet.WalletAddress
			formattedAddress = fmt.Sprintf("%s %s %s %s", addr[0:4], addr[4:8], addr[8:12], addr[12:16])
		} else if wallet.WalletAddress != nil {
			formattedAddress = *wallet.WalletAddress
		}

		expiry := wallet.CreatedAt.AddDate(3, 0, 0).Format("01/06")

		responses = append(responses, &BalanceResponse{
			WalletID:               wallet.ID,
			WalletAddress:          wallet.WalletAddress,
			FormattedWalletAddress: formattedAddress,
			Currency:               wallet.Currency,
			AvailableBalance:       wallet.AvailableBalance,
			LedgerBalance:          wallet.LedgerBalance,
			ReservedBalance:        wallet.ReservedBalance,
			CardholderName:         fmt.Sprintf("%s %s", user.FirstName, user.LastName),
			ExpiryDate:             expiry,
			CardType:               "Mastercard",
			Status:                 wallet.Status,
			CreatedAt:              wallet.CreatedAt,
		})
	}

	return responses, total, nil
}

func (s *Service) GetWallet(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) GetUserWallets(ctx context.Context, userID uuid.UUID) ([]*BalanceResponse, error) {
	wallets, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch user details")
	}

	var responses []*BalanceResponse
	for _, wallet := range wallets {
		var formattedAddress string
		if wallet.WalletAddress != nil && len(*wallet.WalletAddress) == 16 {
			addr := *wallet.WalletAddress
			formattedAddress = fmt.Sprintf("%s %s %s %s", addr[0:4], addr[4:8], addr[8:12], addr[12:16])
		} else if wallet.WalletAddress != nil {
			formattedAddress = *wallet.WalletAddress
		}

		// Simulate Expiry Date: CreatedAt + 3 years
		expiry := wallet.CreatedAt.AddDate(3, 0, 0).Format("01/06")

		responses = append(responses, &BalanceResponse{
			WalletID:               wallet.ID,
			WalletAddress:          wallet.WalletAddress,
			FormattedWalletAddress: formattedAddress,
			Currency:               wallet.Currency,
			AvailableBalance:       wallet.AvailableBalance,
			LedgerBalance:          wallet.LedgerBalance,
			ReservedBalance:        wallet.ReservedBalance,
			CardholderName:         fmt.Sprintf("%s %s", user.FirstName, user.LastName),
			ExpiryDate:             expiry,
			CardType:               "Mastercard",
			Status:                 wallet.Status,
			CreatedAt:              wallet.CreatedAt,
		})
	}

	return responses, nil
}

func (s *Service) GetBalance(ctx context.Context, walletID uuid.UUID) (*BalanceResponse, error) {
	wallet, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.FindByID(ctx, wallet.UserID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch user details")
	}

	var formattedAddress string
	if wallet.WalletAddress != nil && len(*wallet.WalletAddress) == 16 {
		// Format as 1234 5678 1234 5678
		addr := *wallet.WalletAddress
		formattedAddress = fmt.Sprintf("%s %s %s %s", addr[0:4], addr[4:8], addr[8:12], addr[12:16])
	} else if wallet.WalletAddress != nil {
		formattedAddress = *wallet.WalletAddress
	}

	// Simulate Expiry Date: CreatedAt + 3 years
	expiry := wallet.CreatedAt.AddDate(3, 0, 0).Format("01/06")

	return &BalanceResponse{
		WalletID:               wallet.ID,
		WalletAddress:          wallet.WalletAddress,
		FormattedWalletAddress: formattedAddress,
		Currency:               wallet.Currency,
		AvailableBalance:       wallet.AvailableBalance,
		LedgerBalance:          wallet.LedgerBalance,
		ReservedBalance:        wallet.ReservedBalance,
		CardholderName:         fmt.Sprintf("%s %s", user.FirstName, user.LastName),
		ExpiryDate:             expiry,
		CardType:               "Mastercard",
		Status:                 wallet.Status,
		CreatedAt:              wallet.CreatedAt,
	}, nil
}

type BalanceResponse struct {
	WalletID               uuid.UUID           `json:"wallet_id"`
	WalletAddress          *string             `json:"wallet_address,omitempty"`
	FormattedWalletAddress string              `json:"formatted_wallet_address,omitempty"`
	Currency               domain.Currency     `json:"currency"`
	AvailableBalance       decimal.Decimal     `json:"available_balance"`
	LedgerBalance          decimal.Decimal     `json:"ledger_balance"`
	ReservedBalance        decimal.Decimal     `json:"reserved_balance"`
	CardholderName         string              `json:"cardholder_name"`
	ExpiryDate             string              `json:"expiry_date"`
	CardType               string              `json:"card_type"`
	Status                 domain.WalletStatus `json:"status"`
	CreatedAt              time.Time           `json:"created_at"`
}

type LookupResponse struct {
	Name     string          `json:"name"`
	Currency domain.Currency `json:"currency"`
	Address  string          `json:"address"`
}

func (s *Service) LookupWallet(ctx context.Context, address string) (*LookupResponse, error) {
	wallet, err := s.repo.FindByAddress(ctx, address)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.FindByID(ctx, wallet.UserID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch user details")
	}

	return &LookupResponse{
		Name:     fmt.Sprintf("%s %s", user.FirstName, user.LastName),
		Currency: wallet.Currency,
		Address:  address,
	}, nil
}

func (s *Service) SearchWallets(ctx context.Context, query string) ([]*LookupResponse, error) {
	if len(query) < 3 {
		return []*LookupResponse{}, nil
	}

	wallets, err := s.repo.SearchByAddress(ctx, query, 5)
	if err != nil {
		return nil, err
	}

	var responses []*LookupResponse
	for _, wallet := range wallets {
		user, err := s.userRepo.FindByID(ctx, wallet.UserID)
		if err != nil {
			continue
		}
		addr := ""
		if wallet.WalletAddress != nil {
			addr = *wallet.WalletAddress
		}
		responses = append(responses, &LookupResponse{
			Name:     fmt.Sprintf("%s %s", user.FirstName, user.LastName),
			Currency: wallet.Currency,
			Address:  addr,
		})
	}

	return responses, nil
}

type TransactionDetail struct {
	*domain.Transaction
	SenderName           string `json:"sender_name,omitempty"`
	ReceiverName         string `json:"receiver_name,omitempty"`
	SenderWalletNumber   string `json:"sender_wallet_number,omitempty"`
	ReceiverWalletNumber string `json:"receiver_wallet_number,omitempty"`
}

func (s *Service) GetTransactionHistory(ctx context.Context, walletID, userID uuid.UUID, limit, offset int) ([]*TransactionDetail, int, error) {
	// Verify wallet exists and belongs to user
	wallet, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, 0, err
	}
	if wallet.UserID != userID {
		return nil, 0, fmt.Errorf("unauthorized access to wallet")
	}

	txs, err := s.txRepo.FindByWalletID(ctx, walletID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.txRepo.CountByWalletID(ctx, walletID)
	if err != nil {
		// Log error but continue with 0 total if count fails
		s.logger.Error("Failed to count wallet transactions", map[string]interface{}{
			"wallet_id": walletID,
			"error":     err.Error(),
		})
	}

	// Enrich transactions
	var details []*TransactionDetail
	for _, tx := range txs {
		detail := &TransactionDetail{Transaction: tx}

		// Fetch Sender Name
		if sender, err := s.userRepo.FindByID(ctx, tx.SenderID); err == nil {
			detail.SenderName = sender.FirstName + " " + sender.LastName
		}

		// Fetch Receiver Name
		if receiver, err := s.userRepo.FindByID(ctx, tx.ReceiverID); err == nil {
			detail.ReceiverName = receiver.FirstName + " " + receiver.LastName
		}

		// Fetch Sender Wallet Number
		if sWallet, err := s.repo.FindByID(ctx, tx.SenderWalletID); err == nil && sWallet.WalletAddress != nil {
			detail.SenderWalletNumber = *sWallet.WalletAddress
		}

		// Fetch Receiver Wallet Number
		if rWallet, err := s.repo.FindByID(ctx, tx.ReceiverWalletID); err == nil && rWallet.WalletAddress != nil {
			detail.ReceiverWalletNumber = *rWallet.WalletAddress
		}

		details = append(details, detail)
	}

	return details, total, nil
}

type Repository interface {
	Create(ctx context.Context, wallet *domain.Wallet) error
	Update(ctx context.Context, wallet *domain.Wallet) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)
	FindAll(ctx context.Context, limit, offset int) ([]*domain.Wallet, error)
	Count(ctx context.Context) (int, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error)
	FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error)
	FindByAddress(ctx context.Context, address string) (*domain.Wallet, error)
	SearchByAddress(ctx context.Context, partialAddress string, limit int) ([]*domain.Wallet, error)
	DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
	CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
}

type TransactionRepository interface {
	FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*domain.Transaction, error)
	CountByWalletID(ctx context.Context, walletID uuid.UUID) (int, error)
}

type UserRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

func (s *Service) generateWalletNumber() (string, error) {
	// Generate a 16-digit random number
	n, err := rand.Int(rand.Reader, big.NewInt(10000000000000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%016d", n), nil
}
