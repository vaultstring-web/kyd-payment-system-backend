// ==============================================================================
// WALLET SERVICE - internal/wallet/service.go
// ==============================================================================
package wallet

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
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

type DepositRequest struct {
	WalletID uuid.UUID       `json:"wallet_id"`
	Amount   decimal.Decimal `json:"amount" validate:"required,gt=0"`
	SourceID string          `json:"source_id" validate:"required"`
	Currency domain.Currency `json:"currency" validate:"required"`
}

// Deposit adds funds to a wallet
func (s *Service) Deposit(ctx context.Context, req *DepositRequest) (*domain.Wallet, error) {
	wallet, err := s.repo.FindByID(ctx, req.WalletID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find wallet")
	}

	// Verify User KYC Status
	user, err := s.userRepo.FindByID(ctx, wallet.UserID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch user for deposit verification")
	}
	if user.KYCStatus != domain.KYCStatusVerified {
		return nil, errors.New("deposit rejected: user is not KYC verified")
	}

	if wallet.Currency != req.Currency {
		return nil, errors.New("currency mismatch")
	}

	// Update balance
	wallet.LedgerBalance = wallet.LedgerBalance.Add(req.Amount)
	wallet.AvailableBalance = wallet.AvailableBalance.Add(req.Amount)
	wallet.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, wallet); err != nil {
		return nil, errors.Wrap(err, "failed to update wallet balance")
	}

	// Create transaction record
	tx := &domain.Transaction{
		ID:               uuid.New(),
		TransactionType:  domain.TransactionTypeDeposit,
		Status:           domain.TransactionStatusCompleted,
		Amount:           req.Amount,
		Currency:         req.Currency,
		SenderWalletID:   nil, // External source
		ReceiverWalletID: &wallet.ID,
		Reference:        fmt.Sprintf("DEP-%s", uuid.New().String()[:8]),
		Description:      fmt.Sprintf("Deposit from %s", req.SourceID),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.txRepo.Create(ctx, tx); err != nil {
		// Log error but don't fail the request as balance is already updated
		// In a real system, this should be transactional
		s.logger.Error("Failed to create transaction record for deposit", map[string]interface{}{
			"error":     err.Error(),
			"wallet_id": wallet.ID,
			"amount":    req.Amount,
		})
	}

	s.logger.Info("Deposit successful", map[string]interface{}{
		"wallet_id": wallet.ID,
		"amount":    req.Amount,
		"currency":  req.Currency,
	})

	return wallet, nil
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

	if user.KYCStatus != domain.KYCStatusVerified {
		return nil, errors.New("wallet creation rejected: user is not KYC verified")
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
	case "ZM":
		if req.Currency != domain.ZMW {
			return nil, errors.ErrCurrencyNotAllowed
		}
	default:
		// Default fallback
		// Allow ZMW or MWK as international options
		if req.Currency != domain.ZMW && req.Currency != domain.MWK {
			return nil, errors.ErrCurrencyNotAllowed
		}
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
	return s.GetWalletsWithFilter(ctx, limit, offset, nil)
}

func (s *Service) GetWalletsWithFilter(ctx context.Context, limit, offset int, userID *uuid.UUID) ([]*BalanceResponse, int, error) {
	var wallets []*domain.Wallet
	var total int
	var err error

	if userID != nil {
		wallets, err = s.repo.FindAllWithFilter(ctx, limit, offset, userID)
		total, _ = s.repo.CountWithFilter(ctx, userID)
	} else {
		wallets, err = s.repo.FindAll(ctx, limit, offset)
		total, _ = s.repo.Count(ctx)
	}

	if err != nil {
		return nil, 0, err
	}

	var responses []*BalanceResponse
	for _, wallet := range wallets {
		user, err := s.userRepo.FindByID(ctx, wallet.UserID)
		if err != nil {
			s.logger.Error("User not found for wallet", map[string]interface{}{"wallet_id": wallet.ID, "user_id": wallet.UserID})
			continue
		}

		var formattedAddress string
		displayAddress := resolvedDisplayWalletAddress(wallet)
		if len(displayAddress) == 16 {
			formattedAddress = fmt.Sprintf("%s %s %s %s", displayAddress[0:4], displayAddress[4:8], displayAddress[8:12], displayAddress[12:16])
		} else {
			formattedAddress = displayAddress
		}

		expiry := wallet.CreatedAt.AddDate(3, 0, 0).Format("01/06")

		responses = append(responses, &BalanceResponse{
			WalletID:               wallet.ID,
			WalletAddress:          stringPtr(displayAddress),
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

func (s *Service) FixWalletAddresses(ctx context.Context) (int, error) {
	wallets, err := s.repo.FindAll(ctx, 10000, 0)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, w := range wallets {
		addr := ""
		if w.WalletAddress != nil {
			addr = *w.WalletAddress
		}

		// Check if address is valid 16-digit number
		isValid := false
		if len(addr) == 16 {
			_, err := strconv.ParseInt(addr, 10, 64)
			if err == nil {
				isValid = true
			}
		}

		if !isValid {
			newAddr, err := s.generateWalletNumber()
			if err != nil {
				continue
			}

			w.WalletAddress = &newAddr
			w.UpdatedAt = time.Now()
			if err := s.repo.Update(ctx, w); err == nil {
				count++
			}
		}
	}
	return count, nil
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
		displayAddress := resolvedDisplayWalletAddress(wallet)
		if len(displayAddress) == 16 {
			formattedAddress = fmt.Sprintf("%s %s %s %s", displayAddress[0:4], displayAddress[4:8], displayAddress[8:12], displayAddress[12:16])
		} else {
			formattedAddress = displayAddress
		}

		// Simulate Expiry Date: CreatedAt + 3 years
		expiry := wallet.CreatedAt.AddDate(3, 0, 0).Format("01/06")

		responses = append(responses, &BalanceResponse{
			WalletID:               wallet.ID,
			WalletAddress:          stringPtr(displayAddress),
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
	displayAddress := resolvedDisplayWalletAddress(wallet)
	if len(displayAddress) == 16 {
		// Format as 1234 5678 1234 5678
		formattedAddress = fmt.Sprintf("%s %s %s %s", displayAddress[0:4], displayAddress[4:8], displayAddress[8:12], displayAddress[12:16])
	} else {
		formattedAddress = displayAddress
	}

	// Simulate Expiry Date: CreatedAt + 3 years
	expiry := wallet.CreatedAt.AddDate(3, 0, 0).Format("01/06")

	return &BalanceResponse{
		WalletID:               wallet.ID,
		WalletAddress:          stringPtr(displayAddress),
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
	address = normalizeWalletAddress(address)
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
	query = normalizeWalletAddress(query)
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
		addr := resolvedDisplayWalletAddress(wallet)
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
	wallet, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, 0, err
	}
	if wallet.UserID != userID {
		return nil, 0, fmt.Errorf("unauthorized access to wallet")
	}

	return s.getTransactionHistoryInternal(ctx, walletID, limit, offset)
}

func (s *Service) GetTransactionHistoryAdmin(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*TransactionDetail, int, error) {
	_, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, 0, err
	}
	return s.getTransactionHistoryInternal(ctx, walletID, limit, offset)
}

func (s *Service) getTransactionHistoryInternal(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*TransactionDetail, int, error) {
	txs, err := s.txRepo.FindByWalletID(ctx, walletID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.txRepo.CountByWalletID(ctx, walletID)
	if err != nil {
		s.logger.Error("Failed to count wallet transactions", map[string]interface{}{
			"wallet_id": walletID,
			"error":     err.Error(),
		})
	}

	var details []*TransactionDetail
	for _, tx := range txs {
		detail := &TransactionDetail{Transaction: tx}

		if sender, err := s.userRepo.FindByID(ctx, tx.SenderID); err == nil {
			detail.SenderName = sender.FirstName + " " + sender.LastName
		}

		if receiver, err := s.userRepo.FindByID(ctx, tx.ReceiverID); err == nil {
			detail.ReceiverName = receiver.FirstName + " " + receiver.LastName
		}

		if tx.SenderWalletID != nil {
			if sWallet, err := s.repo.FindByID(ctx, *tx.SenderWalletID); err == nil && sWallet.WalletAddress != nil {
				detail.SenderWalletNumber = *sWallet.WalletAddress
			}
		}

		if tx.ReceiverWalletID != nil {
			if rWallet, err := s.repo.FindByID(ctx, *tx.ReceiverWalletID); err == nil && rWallet.WalletAddress != nil {
				detail.ReceiverWalletNumber = *rWallet.WalletAddress
			}
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
	FindAllWithFilter(ctx context.Context, limit, offset int, userID *uuid.UUID) ([]*domain.Wallet, error)
	Count(ctx context.Context) (int, error)
	CountWithFilter(ctx context.Context, userID *uuid.UUID) (int, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error)
	FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error)
	FindByAddress(ctx context.Context, address string) (*domain.Wallet, error)
	SearchByAddress(ctx context.Context, partialAddress string, limit int) ([]*domain.Wallet, error)
	DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
	CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
}

type TransactionRepository interface {
	Create(ctx context.Context, tx *domain.Transaction) error
	FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*domain.Transaction, error)
	CountByWalletID(ctx context.Context, walletID uuid.UUID) (int, error)
}

type UserRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

func (s *Service) generateWalletNumber() (string, error) {
	// Generate a unique 16-digit number; retry on collision.
	for i := 0; i < 10; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10000000000000000))
		if err != nil {
			return "", err
		}
		candidate := fmt.Sprintf("%016d", n)

		_, err = s.repo.FindByAddress(context.Background(), candidate)
		if err != nil {
			// ErrWalletNotFound means candidate is free to use.
			if err == errors.ErrWalletNotFound {
				return candidate, nil
			}
			return "", err
		}
	}
	return "", errors.New("failed to generate unique wallet number")
}

var nonDigitWalletChars = regexp.MustCompile(`\D`)

func normalizeWalletAddress(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return v
	}
	// For digital card-style numbers, remove spaces/separators.
	// Leave legacy alphanumeric addresses untouched except trimming.
	digits := nonDigitWalletChars.ReplaceAllString(v, "")
	if len(digits) >= 8 {
		return digits
	}
	return strings.ReplaceAll(v, " ", "")
}

func resolvedDisplayWalletAddress(wallet *domain.Wallet) string {
	if wallet != nil && wallet.WalletAddress != nil {
		addr := normalizeWalletAddress(*wallet.WalletAddress)
		if len(addr) == 16 && nonDigitWalletChars.ReplaceAllString(addr, "") == addr {
			return addr
		}
	}
	// Fallback to deterministic 16-digit digital number tied to wallet ID.
	return digitalWalletNumberFromWalletID(wallet.ID)
}

func digitalWalletNumberFromWalletID(id uuid.UUID) string {
	raw := strings.ToLower(id.String())
	mapped := strings.NewReplacer(
		"a", "1", "b", "2", "c", "3", "d", "4", "e", "5", "f", "6", "-", "",
	).Replace(raw)
	digits := nonDigitWalletChars.ReplaceAllString(mapped, "")
	return (digits + "4539102834756192")[:16]
}

func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
