package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// EscrowRequest defines the parameters for creating an escrow transaction
type EscrowRequest struct {
	SenderID    uuid.UUID       `json:"sender_id" validate:"required"`
	ReceiverID  uuid.UUID       `json:"receiver_id" validate:"required"`
	Amount      decimal.Decimal `json:"amount" validate:"required,gt=0"`
	Currency    domain.Currency `json:"currency" validate:"required"`
	Condition   string          `json:"condition" validate:"required"`
	Expiry      time.Time       `json:"expiry" validate:"required"`
	Description string          `json:"description"`
}

// CreateEscrow initiates a transaction but holds funds in a reserved state
func (s *Service) CreateEscrow(ctx context.Context, req *EscrowRequest) (*PaymentResponse, error) {
	// Reuse validation and risk checks from InitiatePayment logic where possible
	// For brevity, we implement core logic here

	// 1. Basic Validation
	if req.Expiry.Before(time.Now()) {
		return nil, errors.New("escrow expiry must be in the future")
	}

	// 2. Fetch Wallets
	senderWallet, err := s.walletRepo.FindByUserAndCurrency(ctx, req.SenderID, req.Currency)
	if err != nil {
		return nil, errors.New("sender wallet not found for this currency")
	}

	receiverWallet, err := s.getReceiverWallet(ctx, req.ReceiverID, req.Currency, "")
	if err != nil {
		return nil, errors.New("receiver wallet not found")
	}

	// 3. Create Transaction Record (Status: RESERVED)
	tx := &domain.Transaction{
		ID:                uuid.New(),
		Reference:         s.generateReference(),
		SenderID:          req.SenderID,
		ReceiverID:        req.ReceiverID,
		SenderWalletID:    &senderWallet.ID,
		ReceiverWalletID:  &receiverWallet.ID,
		Amount:            req.Amount,
		Currency:          req.Currency,
		ConvertedAmount:   req.Amount, // Assuming same currency for simplicity in escrow
		ConvertedCurrency: req.Currency,
		ExchangeRate:      decimal.NewFromInt(1),
		Status:            domain.TransactionStatusReserved, // CRITICAL: Reserved, not Completed
		TransactionType:   domain.TransactionTypePayment,
		Channel:           "api",
		Description:       req.Description,
		InitiatedAt:       time.Now(),
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
		Metadata: domain.Metadata{
			"escrow_condition": req.Condition,
			"escrow_expiry":    req.Expiry.Format(time.RFC3339),
			"type":             "ESCROW",
		},
	}

	if err := s.repo.Create(ctx, tx); err != nil {
		return nil, err
	}

	// 4. Reserve Funds (Move from Available to Reserved)
	if err := s.walletRepo.ReserveFunds(ctx, senderWallet.ID, req.Amount); err != nil {
		tx.Status = domain.TransactionStatusFailed
		s.repo.Update(ctx, tx)
		return nil, fmt.Errorf("failed to reserve funds: %v", err)
	}

	s.logger.Info("Escrow created", map[string]interface{}{
		"tx_id":  tx.ID,
		"sender": req.SenderID,
		"amount": req.Amount,
		"expiry": req.Expiry,
	})

	return &PaymentResponse{
		Transaction: tx,
		Message:     "Escrow created. Funds reserved.",
	}, nil
}

// ReleaseEscrow releases funds to the receiver
func (s *Service) ReleaseEscrow(ctx context.Context, txID uuid.UUID, userID uuid.UUID) error {
	tx, err := s.repo.FindByID(ctx, txID)
	if err != nil {
		return err
	}

	if tx.Status != domain.TransactionStatusReserved {
		return errors.New("transaction is not in escrow/reserved state")
	}

	// Only Sender or Admin (or Oracle) should release.
	// For now, let's allow Sender to release (satisfaction) or Receiver (if condition met - handled by oracle usually).
	// We'll assume the caller (userID) has authority.
	if tx.SenderID != userID && tx.ReceiverID != userID { // Simplified auth
		return errors.New("unauthorized to release escrow")
	}

	// Credit Receiver
	if tx.ReceiverWalletID == nil {
		return errors.New("receiver wallet missing")
	}
	if err := s.walletRepo.CreditWallet(ctx, *tx.ReceiverWalletID, tx.ConvertedAmount); err != nil {
		return fmt.Errorf("failed to credit receiver: %v", err)
	}

	// Update Status
	tx.Status = domain.TransactionStatusCompleted
	now := time.Now()
	tx.CompletedAt = &now
	tx.UpdatedAt = now

	return s.repo.Update(ctx, tx)
}

// RefundEscrow returns funds to the sender (e.g. expiry or cancellation)
func (s *Service) RefundEscrow(ctx context.Context, txID uuid.UUID, userID uuid.UUID) error {
	tx, err := s.repo.FindByID(ctx, txID)
	if err != nil {
		return err
	}

	if tx.Status != domain.TransactionStatusReserved {
		return errors.New("transaction is not in escrow/reserved state")
	}

	// Validate Expiry or Permission
	expiryStr, ok := tx.Metadata["escrow_expiry"].(string)
	if ok {
		expiry, _ := time.Parse(time.RFC3339, expiryStr)
		if time.Now().Before(expiry) && tx.SenderID != userID {
			return errors.New("escrow has not expired yet")
		}
	}

	// Refund Sender
	if tx.SenderWalletID == nil {
		return errors.New("sender wallet missing")
	}
	if err := s.walletRepo.CreditWallet(ctx, *tx.SenderWalletID, tx.Amount); err != nil {
		return fmt.Errorf("failed to refund sender: %v", err)
	}

	// Update Status
	tx.Status = domain.TransactionStatusCancelled
	reason := "Escrow Refunded"
	tx.StatusReason = reason
	now := time.Now()
	tx.UpdatedAt = now

	return s.repo.Update(ctx, tx)
}
