// ==============================================================================
// COMPLETE KYD PAYMENT SYSTEM - GO BACKEND
// ==============================================================================

// ==============================================================================
// PAYMENT SERVICE - internal/payment/service.go
// ==============================================================================
package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"kyd/internal/domain"
	"kyd/internal/ledger"
	pkgerrors "kyd/pkg/errors"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	repo          Repository
	walletRepo    WalletRepository
	forexService  ForexService
	ledgerService LedgerService
	logger        logger.Logger
}

func NewService(
	repo Repository,
	walletRepo WalletRepository,
	forexService ForexService,
	ledgerService LedgerService,
	log logger.Logger,
) *Service {
	return &Service{
		repo:          repo,
		walletRepo:    walletRepo,
		forexService:  forexService,
		ledgerService: ledgerService,
		logger:        log,
	}
}

type InitiatePaymentRequest struct {
	SenderID    uuid.UUID       `json:"sender_id" validate:"required"`
	ReceiverID  uuid.UUID       `json:"receiver_id" validate:"required"`
	Amount      decimal.Decimal `json:"amount" validate:"required"`
	Currency    domain.Currency `json:"currency" validate:"required"`
	Description string          `json:"description"`
	Channel     string          `json:"channel"`
	Category    string          `json:"category"`
}

type PaymentResponse struct {
	Transaction *domain.Transaction `json:"transaction"`
	Message     string              `json:"message"`
}

// InitiatePayment handles the complete payment flow
func (s *Service) InitiatePayment(ctx context.Context, req *InitiatePaymentRequest) (*PaymentResponse, error) {
	s.logger.Info("Initiating payment", map[string]interface{}{
		"sender_id":   req.SenderID,
		"receiver_id": req.ReceiverID,
		"amount":      req.Amount,
		"currency":    req.Currency,
	})

	// 1. Get sender and receiver wallets
	senderWallet, err := s.walletRepo.FindByUserAndCurrency(ctx, req.SenderID, req.Currency)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "sender wallet not found")
	}

	// Get receiver's wallet (auto-determine currency)
	receiverWallet, err := s.getReceiverWallet(ctx, req.ReceiverID, req.Currency)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "receiver wallet not found")
	}

	// 2. Check if currency conversion needed
	exchangeRate := decimal.NewFromInt(1)
	convertedAmount := req.Amount
	convertedCurrency := req.Currency

	if senderWallet.Currency != receiverWallet.Currency {
		// Get exchange rate
		rate, err := s.forexService.GetRate(ctx, senderWallet.Currency, receiverWallet.Currency)
		if err != nil {
			return nil, pkgerrors.Wrap(err, "failed to get exchange rate")
		}
		exchangeRate = rate.Rate
		convertedAmount = req.Amount.Mul(exchangeRate)
		convertedCurrency = receiverWallet.Currency
	}

	// 3. Calculate fees (1.5% standard fee)
	feeAmount := req.Amount.Mul(decimal.NewFromFloat(0.015))
	totalDebit := req.Amount.Add(feeAmount)

	// 4. Check sender balance
	if senderWallet.AvailableBalance.LessThan(totalDebit) {
		return nil, pkgerrors.ErrInsufficientBalance
	}

	// 5. Create transaction record
	tx := &domain.Transaction{
		ID:                uuid.New(),
		Reference:         s.generateReference(),
		SenderID:          req.SenderID,
		ReceiverID:        req.ReceiverID,
		SenderWalletID:    senderWallet.ID,
		ReceiverWalletID:  receiverWallet.ID,
		Amount:            req.Amount,
		Currency:          req.Currency,
		ExchangeRate:      exchangeRate,
		ConvertedAmount:   convertedAmount,
		ConvertedCurrency: convertedCurrency,
		FeeAmount:         feeAmount,
		FeeCurrency:       req.Currency,
		NetAmount:         convertedAmount,
		Status:            domain.TransactionStatusPending,
		TransactionType:   domain.TransactionTypePayment,
		Channel:           &req.Channel,
		Category:          &req.Category,
		Description:       &req.Description,
		Metadata:          make(domain.Metadata),
		InitiatedAt:       time.Now(),
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// 6. Process payment atomically
	if err := s.processPayment(ctx, tx, senderWallet, receiverWallet, totalDebit); err != nil {
		tx.Status = domain.TransactionStatusFailed
		reason := err.Error()
		tx.StatusReason = &reason
		_ = s.repo.Create(ctx, tx)
		return nil, err
	}

	tx.Status = domain.TransactionStatusCompleted
	now := time.Now()
	tx.CompletedAt = &now

	if err := s.repo.Update(ctx, tx); err != nil {
		return nil, err
	}

	s.logger.Info("Payment completed", map[string]interface{}{
		"transaction_id": tx.ID,
		"reference":      tx.Reference,
	})

	return &PaymentResponse{
		Transaction: tx,
		Message:     "Payment processed successfully",
	}, nil
}

func (s *Service) processPayment(
	ctx context.Context,
	tx *domain.Transaction,
	senderWallet, receiverWallet *domain.Wallet,
	totalDebit decimal.Decimal,
) error {
	// This must be atomic - use database transaction
	return s.ledgerService.PostTransaction(ctx, &ledger.LedgerPosting{
		TransactionID:     tx.ID,
		DebitWalletID:     senderWallet.ID,
		CreditWalletID:    receiverWallet.ID,
		DebitAmount:       totalDebit,
		CreditAmount:      tx.ConvertedAmount,
		Currency:          tx.Currency,
		ConvertedCurrency: tx.ConvertedCurrency,
		ExchangeRate:      tx.ExchangeRate,
		FeeAmount:         tx.FeeAmount,
	})
}

func (s *Service) getReceiverWallet(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error) {
	// Try to get wallet in same currency
	wallet, err := s.walletRepo.FindByUserAndCurrency(ctx, userID, currency)
	if err == nil {
		return wallet, nil
	}

	// If not found, get user's primary wallet
	wallets, err := s.walletRepo.FindByUserID(ctx, userID)
	if err != nil || len(wallets) == 0 {
		return nil, pkgerrors.ErrWalletNotFound
	}

	return wallets[0], nil
}

func (s *Service) generateReference() string {
	return fmt.Sprintf("KYD-%d-%s", time.Now().Unix(), uuid.New().String()[:8])
}

func (s *Service) GetTransaction(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) GetUserTransactions(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Transaction, error) {
	return s.repo.FindByUserID(ctx, userID, limit, offset)
}

// Repository interfaces
type Repository interface {
	Create(ctx context.Context, tx *domain.Transaction) error
	Update(ctx context.Context, tx *domain.Transaction) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error)
	FindByReference(ctx context.Context, ref string) (*domain.Transaction, error)
	FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Transaction, error)
}

type WalletRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error)
	FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error)
	DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
	CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
}

type ForexService interface {
	GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error)
}

type LedgerService interface {
	PostTransaction(ctx context.Context, posting *ledger.LedgerPosting) error
}

func (s *Service) CancelTransaction(ctx context.Context, txID, userID uuid.UUID) error {
	tx, err := s.repo.FindByID(ctx, txID)
	if err != nil {
		return err
	}

	// Verify ownership
	if tx.SenderID != userID {
		return errors.New("unauthorized to cancel this transaction")
	}

	// Only pending transactions can be cancelled
	if tx.Status != domain.TransactionStatusPending {
		return errors.New("only pending transactions can be cancelled")
	}

	tx.Status = domain.TransactionStatusCancelled
	now := time.Now()
	tx.CompletedAt = &now

	return s.repo.Update(ctx, tx)
}

type BulkPaymentRequest struct {
	SenderID uuid.UUID     `json:"sender_id"`
	Payments []PaymentItem `json:"payments" validate:"required,min=1,max=100"`
}

type PaymentItem struct {
	ReceiverID  uuid.UUID       `json:"receiver_id" validate:"required"`
	Amount      decimal.Decimal `json:"amount" validate:"required,gt=0"`
	Currency    domain.Currency `json:"currency" validate:"required"`
	Description string          `json:"description"`
}

type BulkPaymentResult struct {
	Successful []uuid.UUID        `json:"successful"`
	Failed     []BulkPaymentError `json:"failed"`
	TotalCount int                `json:"total_count"`
}

type BulkPaymentError struct {
	ReceiverID uuid.UUID `json:"receiver_id"`
	Error      string    `json:"error"`
}

func (s *Service) BulkPayment(ctx context.Context, req *BulkPaymentRequest) (*BulkPaymentResult, error) {
	result := &BulkPaymentResult{
		Successful: []uuid.UUID{},
		Failed:     []BulkPaymentError{},
		TotalCount: len(req.Payments),
	}

	for _, item := range req.Payments {
		paymentReq := &InitiatePaymentRequest{
			SenderID:    req.SenderID,
			ReceiverID:  item.ReceiverID,
			Amount:      item.Amount,
			Currency:    item.Currency,
			Description: item.Description,
		}

		response, err := s.InitiatePayment(ctx, paymentReq)
		if err != nil {
			result.Failed = append(result.Failed, BulkPaymentError{
				ReceiverID: item.ReceiverID,
				Error:      err.Error(),
			})
			continue
		}

		result.Successful = append(result.Successful, response.Transaction.ID)
	}

	return result, nil
}
