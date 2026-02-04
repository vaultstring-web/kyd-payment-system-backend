// ==============================================================================
// SETTLEMENT SERVICE - internal/settlement/service.go
// ==============================================================================
package settlement

import (
	"context"
	"fmt"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	repo             Repository
	txRepo           TransactionRepository
	stellarConnector BlockchainConnector
	rippleConnector  BlockchainConnector
	logger           logger.Logger
	monitorInterval  time.Duration
}

func NewService(
	repo Repository,
	txRepo TransactionRepository,
	stellar, ripple BlockchainConnector,
	log logger.Logger,
) *Service {
	s := &Service{
		repo:             repo,
		txRepo:           txRepo,
		stellarConnector: stellar,
		rippleConnector:  ripple,
		logger:           log,
		monitorInterval:  2 * time.Second,
	}

	// Start settlement worker
	go s.startSettlementWorker()

	return s
}

// startSettlementWorker runs periodic settlement batching
func (s *Service) startSettlementWorker() {
	// Initial recovery of settlements that were interrupted
	ctx := context.Background()
	if err := s.RecoverPendingSettlements(ctx); err != nil {
		s.logger.Error("Failed to recover pending settlements", map[string]interface{}{
			"error": err.Error(),
		})
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if err := s.ProcessPendingSettlements(ctx); err != nil {
			s.logger.Error("Settlement worker error", map[string]interface{}{
				"error": err.Error(),
			})
		}

		if err := s.RecoverPendingSettlements(ctx); err != nil {
			s.logger.Error("Recovery worker error", map[string]interface{}{
				"error": err.Error(),
			})
		}

		if err := s.CleanupStuckTransactions(ctx); err != nil {
			s.logger.Error("Cleanup worker error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}
}

// RecoverPendingSettlements finds settlements in Submitted state and resumes monitoring
func (s *Service) RecoverPendingSettlements(ctx context.Context) error {
	submitted, err := s.repo.FindSubmitted(ctx)
	if err != nil {
		return err
	}

	for _, settlement := range submitted {
		if settlement.TransactionHash != "" {
			go s.monitorSettlement(settlement.ID, settlement.TransactionHash)
			s.logger.Info("Resumed monitoring for settlement", map[string]interface{}{
				"settlement_id": settlement.ID,
				"tx_hash":       settlement.TransactionHash,
			})
		}
	}
	return nil
}

// CleanupStuckTransactions finds and fails transactions stuck in pending state
func (s *Service) CleanupStuckTransactions(ctx context.Context) error {
	// Find transactions pending for more than 1 hour
	stuckTxs, err := s.txRepo.FindStuckPending(ctx, 1*time.Hour, 100)
	if err != nil {
		return err
	}

	if len(stuckTxs) > 0 {
		s.logger.Info("Found stuck transactions", map[string]interface{}{
			"count": len(stuckTxs),
		})
	}

	for _, tx := range stuckTxs {
		tx.Status = domain.TransactionStatusFailed
		reason := "Timeout: Transaction stuck in pending state"
		tx.StatusReason = reason
		now := time.Now()
		tx.CompletedAt = &now
		tx.UpdatedAt = now

		if err := s.txRepo.Update(ctx, tx); err != nil {
			s.logger.Error("Failed to mark stuck transaction as failed", map[string]interface{}{
				"tx_id": tx.ID,
				"error": err.Error(),
			})
		} else {
			s.logger.Info("Marked stuck transaction as failed", map[string]interface{}{
				"tx_id": tx.ID,
			})
		}
	}
	return nil
}

// ProcessPendingSettlements batches and settles pending transactions
func (s *Service) ProcessPendingSettlements(ctx context.Context) error {
	s.logger.Info("Processing pending settlements", nil)

	// Get pending transactions
	pendingTxs, err := s.txRepo.FindPendingSettlement(ctx, 100)
	if err != nil {
		return err
	}

	if len(pendingTxs) == 0 {
		s.logger.Info("No pending transactions", nil)
		return nil
	}

	// Group by currency pair
	batches := s.groupByCurrency(pendingTxs)

	for pair, txs := range batches {
		if err := s.settleBatch(ctx, pair, txs); err != nil {
			s.logger.Error("Batch settlement failed", map[string]interface{}{
				"pair":  pair,
				"count": len(txs),
				"error": err.Error(),
			})
			continue
		}
	}

	return nil
}

func (s *Service) settleBatch(ctx context.Context, pair string, txs []*domain.Transaction) error {
	// Calculate total amount
	totalAmount := decimal.Zero
	for _, tx := range txs {
		totalAmount = totalAmount.Add(tx.ConvertedAmount)
	}

	// Create settlement record
	settlement := &domain.Settlement{
		ID:             uuid.New(),
		BatchReference: s.generateBatchReference(),
		TotalAmount:    totalAmount,
		Currency:       txs[0].ConvertedCurrency,
		FeeAmount:      decimal.Zero,
		FeeCurrency:    txs[0].ConvertedCurrency,
		Status:         domain.SettlementStatusPending,
		Metadata:       make(domain.Metadata),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Determine network based on transaction volume
	if totalAmount.GreaterThan(decimal.NewFromInt(100000)) {
		// Large B2B transactions -> Ripple
		settlement.Network = domain.NetworkRipple
	} else {
		// Retail transactions -> Stellar
		settlement.Network = domain.NetworkStellar
	}

	// Store settlement
	if err := s.repo.Create(ctx, settlement); err != nil {
		return err
	}

	// Associate transactions with settlement
	txIDs := make([]uuid.UUID, len(txs))
	for i, tx := range txs {
		txIDs[i] = tx.ID
		// Update in-memory objects for consistency if needed later
		tx.SettlementID = &settlement.ID
		tx.Status = domain.TransactionStatusSettling
	}

	if err := s.txRepo.BatchUpdateSettlementID(ctx, txIDs, settlement.ID); err != nil {
		s.logger.Error("Failed to batch update transactions", map[string]interface{}{
			"settlement_id": settlement.ID,
			"error":         err.Error(),
		})
		// If we can't update the transactions, we should probably fail the settlement creation
		// or at least not proceed to blockchain submission.
		// Since settlement is already created, we might want to mark it as failed or delete it?
		// For now, let's return error so we don't submit to blockchain.
		settlement.Status = domain.SettlementStatusFailed
		_ = s.repo.Update(ctx, settlement)
		return err
	}

	// Execute blockchain settlement
	var connector BlockchainConnector
	if settlement.Network == domain.NetworkStellar {
		connector = s.stellarConnector
	} else {
		connector = s.rippleConnector
	}

	result, err := connector.SubmitSettlement(ctx, settlement)
	if err != nil {
		settlement.Status = domain.SettlementStatusFailed
		_ = s.repo.Update(ctx, settlement)
		return err
	}

	// Update settlement with blockchain info
	settlement.TransactionHash = result.TxHash
	settlement.Status = domain.SettlementStatusSubmitted
	settlement.SubmissionCount++
	now := time.Now()
	settlement.LastSubmittedAt = &now

	if err := s.repo.Update(ctx, settlement); err != nil {
		return err
	}

	// Monitor confirmation
	go s.monitorSettlement(settlement.ID, result.TxHash)

	s.logger.Info("Settlement submitted", map[string]interface{}{
		"settlement_id": settlement.ID,
		"tx_hash":       result.TxHash,
		"network":       settlement.Network,
		"amount":        totalAmount.String(),
	})

	return nil
}

func (s *Service) monitorSettlement(settlementID uuid.UUID, txHash string) {
	ctx := context.Background()
	maxAttempts := 30
	interval := s.monitorInterval

	for i := 0; i < maxAttempts; i++ {
		time.Sleep(interval)

		settlement, err := s.repo.FindByID(ctx, settlementID)
		if err != nil {
			continue
		}

		var connector BlockchainConnector
		if settlement.Network == domain.NetworkStellar {
			connector = s.stellarConnector
		} else {
			connector = s.rippleConnector
		}

		confirmed, err := connector.CheckConfirmation(ctx, txHash)
		if err != nil {
			s.logger.Warn("Confirmation check failed", map[string]interface{}{
				"tx_hash": txHash,
				"attempt": i + 1,
			})
			continue
		}

		if confirmed {
			now := time.Now()
			settlement.Status = domain.SettlementStatusConfirmed
			settlement.ConfirmedAt = &now
			settlement.CompletedAt = &now

			if err := s.repo.Update(ctx, settlement); err != nil {
				s.logger.Error("Failed to update settlement", map[string]interface{}{
					"settlement_id": settlementID,
					"error":         err.Error(),
				})
			}

			// Update all associated transactions
			txs, _ := s.txRepo.FindBySettlementID(ctx, settlementID)
			for _, tx := range txs {
				tx.Status = domain.TransactionStatusCompleted
				tx.CompletedAt = &now
				_ = s.txRepo.Update(ctx, tx)
			}

			s.logger.Info("Settlement confirmed", map[string]interface{}{
				"settlement_id": settlementID,
				"tx_hash":       txHash,
			})

			return
		}
	}

	s.logger.Error("Settlement confirmation timeout", map[string]interface{}{
		"settlement_id": settlementID,
		"tx_hash":       txHash,
	})
}

func (s *Service) groupByCurrency(txs []*domain.Transaction) map[string][]*domain.Transaction {
	groups := make(map[string][]*domain.Transaction)

	for _, tx := range txs {
		key := fmt.Sprintf("%s-%s", tx.Currency, tx.ConvertedCurrency)
		groups[key] = append(groups[key], tx)
	}

	return groups
}

func (s *Service) generateBatchReference() string {
	return fmt.Sprintf("BATCH-%d-%s", time.Now().Unix(), uuid.New().String()[:8])
}

// Interfaces
type Repository interface {
	Create(ctx context.Context, settlement *domain.Settlement) error
	Update(ctx context.Context, settlement *domain.Settlement) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Settlement, error)
	FindSubmitted(ctx context.Context) ([]*domain.Settlement, error)
}

type TransactionRepository interface {
	Update(ctx context.Context, tx *domain.Transaction) error
	FindPendingSettlement(ctx context.Context, limit int) ([]*domain.Transaction, error)
	FindBySettlementID(ctx context.Context, settlementID uuid.UUID) ([]*domain.Transaction, error)
	FindStuckPending(ctx context.Context, olderThan time.Duration, limit int) ([]*domain.Transaction, error)
	BatchUpdateSettlementID(ctx context.Context, txIDs []uuid.UUID, settlementID uuid.UUID) error
}

type BlockchainConnector interface {
	SubmitSettlement(ctx context.Context, settlement *domain.Settlement) (*SettlementResult, error)
	CheckConfirmation(ctx context.Context, txHash string) (bool, error)
}

type SettlementResult struct {
	TxHash      string
	Confirmed   bool
	BlockNumber int64
}
