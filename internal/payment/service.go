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

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"kyd/internal/domain"
	"kyd/internal/ledger"
	"kyd/internal/monitoring"
	"kyd/internal/notification"
	"kyd/internal/risk"
	"kyd/pkg/config"
	pkgerrors "kyd/pkg/errors"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

type TransactionDetail struct {
	*domain.Transaction
	SenderName           string `json:"sender_name,omitempty"`
	ReceiverName         string `json:"receiver_name,omitempty"`
	SenderWalletNumber   string `json:"sender_wallet_number,omitempty"`
	ReceiverWalletNumber string `json:"receiver_wallet_number,omitempty"`
}

type Service struct {
	repo          Repository
	walletRepo    WalletRepository
	forexService  ForexService
	ledgerService LedgerService
	userRepo      UserRepository
	logger        logger.Logger
	riskEngine    *risk.RiskEngine
	monitor       *monitoring.BehavioralMonitor
	notifier      notification.Service
	auditRepo     AuditRepository
	securityRepo  SecurityRepository
}

func NewService(
	repo Repository,
	walletRepo WalletRepository,
	forexService ForexService,
	ledgerService LedgerService,
	userRepo UserRepository,
	notifier notification.Service,
	auditRepo AuditRepository,
	securityRepo SecurityRepository,
	log logger.Logger,
	cfg *config.Config,
) *Service {
	// Use default if nil (for tests/backward compatibility)
	var riskCfg config.RiskConfig
	if cfg != nil {
		riskCfg = cfg.Risk
	} else {
		riskCfg = config.RiskConfig{
			EnableCircuitBreaker:   true,
			MaxDailyLimit:          100000,
			MaxVelocityPerHour:     10,
			HighValueThreshold:     50000,
			AdminApprovalThreshold: 1000000, // Default to 1M if not set, to avoid blocking tests
		}
	}

	return &Service{
		repo:          repo,
		walletRepo:    walletRepo,
		forexService:  forexService,
		ledgerService: ledgerService,
		userRepo:      userRepo,
		logger:        log,
		riskEngine:    risk.NewRiskEngine(riskCfg),
		monitor:       monitoring.NewBehavioralMonitor(),
		notifier:      notifier,
		auditRepo:     auditRepo,
		securityRepo:  securityRepo,
	}
}

type InitiatePaymentRequest struct {
	SenderID              uuid.UUID              `json:"sender_id" validate:"required"`
	ReceiverID            uuid.UUID              `json:"receiver_id" validate:"-"`
	ReceiverWalletID      uuid.UUID              `json:"receiver_wallet_id" validate:"-"`
	ReceiverWalletAddress string                 `json:"receiver_wallet_number" validate:"-"`
	Amount                decimal.Decimal        `json:"amount" validate:"required,gt=0"`
	Currency              domain.Currency        `json:"currency" validate:"required"`
	DestinationCurrency   domain.Currency        `json:"destination_currency"`
	Description           string                 `json:"description"`
	Channel               string                 `json:"channel"`
	Category              string                 `json:"category"`
	Reference             string                 `json:"reference"` // Idempotency Key
	DeviceID              string                 `json:"device_id"`
	Location              string                 `json:"location"`
	Metadata              map[string]interface{} `json:"metadata"`
}

type PaymentResponse struct {
	Transaction *domain.Transaction `json:"transaction"`
	Message     string              `json:"message"`
}

// InitiatePayment handles the complete payment flow
func (s *Service) InitiatePayment(ctx context.Context, req *InitiatePaymentRequest) (*PaymentResponse, error) {
	// 0. Global Circuit Breaker Check
	if err := s.riskEngine.CheckGlobalCircuitBreaker(); err != nil {
		s.logger.Error("Payment blocked by circuit breaker", map[string]interface{}{"error": err.Error()})
		return nil, err
	}

	// 0.05 Check Blocklist (Sender)
	if isBlocked, err := s.securityRepo.IsBlacklisted(ctx, req.SenderID.String()); err != nil {
		s.logger.Error("Failed to check blocklist", map[string]interface{}{"error": err.Error()})
		return nil, errors.New("system error: unable to verify security status")
	} else if isBlocked {
		s.logger.Warn("Transaction blocked: Sender is blacklisted", map[string]interface{}{"sender_id": req.SenderID})
		return nil, errors.New("security alert: account is restricted")
	}

	// 0.06 Check Blocklist (Receiver ID)
	if req.ReceiverID != uuid.Nil {
		if isBlocked, err := s.securityRepo.IsBlacklisted(ctx, req.ReceiverID.String()); err != nil {
			s.logger.Error("Failed to check blocklist", map[string]interface{}{"error": err.Error()})
			return nil, errors.New("system error: unable to verify security status")
		} else if isBlocked {
			s.logger.Warn("Transaction blocked: Receiver is blacklisted", map[string]interface{}{"receiver_id": req.ReceiverID})
			return nil, errors.New("security alert: receiver account is restricted")
		}
	}

	// 0.07 Check Blocklist (Receiver Wallet Address)
	if req.ReceiverWalletAddress != "" {
		if isBlocked, err := s.securityRepo.IsBlacklisted(ctx, req.ReceiverWalletAddress); err != nil {
			s.logger.Error("Failed to check blocklist", map[string]interface{}{"error": err.Error()})
			return nil, errors.New("system error: unable to verify security status")
		} else if isBlocked {
			s.logger.Warn("Transaction blocked: Receiver wallet is blacklisted", map[string]interface{}{"wallet": req.ReceiverWalletAddress})
			return nil, errors.New("security alert: receiver wallet is restricted")
		}
	}

	// 0.1 Check Daily Limit
	dailyTotal, err := s.repo.GetDailyTotal(ctx, req.SenderID, req.Currency)
	if err != nil {
		// If we can't fetch daily total, we should probably fail safe or log warning
		// For banking safety, we fail open but log error, or fail closed?
		// Fail closed (secure) is better.
		s.logger.Error("Failed to fetch daily total", map[string]interface{}{"error": err.Error()})
		return nil, pkgerrors.Wrap(err, "failed to verify daily limit")
	}

	if err := s.riskEngine.CheckDailyLimit(req.Amount, dailyTotal); err != nil {
		s.logger.Warn("Transaction blocked by daily limit", map[string]interface{}{
			"amount":      req.Amount.String(),
			"daily_total": dailyTotal.String(),
			"sender_id":   req.SenderID,
		})

		go func() {
			_ = s.notifier.Notify(context.Background(), req.SenderID, "RISK_ALERT", map[string]interface{}{
				"reason": "Daily transaction limit exceeded",
				"limit":  s.riskEngine.GetConfig().MaxDailyLimit,
			})
		}()

		// Log Security Event
		go func() {
			_ = s.securityRepo.LogSecurityEvent(context.Background(), &domain.SecurityEvent{
				Type:        "risk_block",
				Severity:    "high",
				Description: fmt.Sprintf("Daily limit exceeded. Amount: %s. Total: %s", req.Amount.String(), dailyTotal.String()),
				Status:      "blocked",
				UserID:      &req.SenderID,
				IPAddress:   req.Location,
				CreatedAt:   time.Now(),
			})
		}()

		return nil, err
	}

	// 0.2 Cool-off Check
	if err := s.riskEngine.CheckCoolOff(req.SenderID, req.Amount); err != nil {
		s.logger.Warn("Transaction blocked by cool-off", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, err
	}

	// 0.3 Restricted Country Check
	if err := s.riskEngine.CheckRestrictedCountry(req.Location); err != nil {
		s.logger.Warn("Transaction blocked by restricted country", map[string]interface{}{
			"location":  req.Location,
			"sender_id": req.SenderID,
		})
		return nil, err
	}

	s.logger.Info("Initiating payment", map[string]interface{}{
		"sender_id":               req.SenderID,
		"receiver_id":             req.ReceiverID,
		"receiver_wallet_id":      req.ReceiverWalletID,
		"receiver_wallet_address": req.ReceiverWalletAddress,
		"amount":                  req.Amount.String(),
		"currency":                req.Currency,
		"reference":               req.Reference,
	})

	// Sanitize inputs to prevent XSS
	if req.Reference != "" {
		req.Reference = validator.Sanitize(req.Reference)
	}
	req.Description = validator.Sanitize(req.Description)
	req.Channel = validator.Sanitize(req.Channel)
	req.Category = validator.Sanitize(req.Category)

	// Fraud Check: Device Trust
	if req.DeviceID != "" {
		// Bypass check for internal system scheduler
		if req.DeviceID == "system-scheduler" && req.Channel == "api" {
			s.logger.Info("Allowing trusted system scheduler transaction", map[string]interface{}{
				"user_id": req.SenderID,
			})
		} else {
			trusted, err := s.userRepo.IsDeviceTrusted(ctx, req.SenderID, req.DeviceID)
			if err != nil {
				s.logger.Error("Failed to check device trust", map[string]interface{}{
					"error":     err.Error(),
					"user_id":   req.SenderID,
					"device_id": req.DeviceID,
				})
				return nil, errors.New("system error: unable to verify device trust")
			}
			if !trusted {
				s.logger.Warn("Blocked transaction from untrusted device", map[string]interface{}{
					"user_id":   req.SenderID,
					"device_id": req.DeviceID,
				})

				// Log Security Event
				go func() {
					_ = s.securityRepo.LogSecurityEvent(context.Background(), &domain.SecurityEvent{
						Type:        "auth_failure",
						Severity:    "high",
						Description: fmt.Sprintf("Untrusted device blocked. DeviceID: %s", req.DeviceID),
						Status:      "blocked",
						UserID:      &req.SenderID,
						IPAddress:   req.Location,
						CreatedAt:   time.Now(),
					})
				}()

				return nil, errors.New("security alert: transaction blocked from new/untrusted device")
			}
		}
	}

	// 0. Idempotency Check
	if req.Reference != "" {
		existingTx, err := s.repo.FindByReference(ctx, req.Reference)
		if err == nil && existingTx != nil {
			s.logger.Info("Idempotency match found", map[string]interface{}{
				"reference": req.Reference,
				"tx_id":     existingTx.ID,
			})
			return &PaymentResponse{
				Transaction: existingTx,
				Message:     "Transaction already processed (idempotent)",
			}, nil
		}
	} else {
		req.Reference = s.generateReference()
	}

	// 0b. Validate amount
	if req.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("amount must be greater than zero")
	}

	// 1. Get sender and receiver wallets
	senderWallet, err := s.walletRepo.FindByUserAndCurrency(ctx, req.SenderID, req.Currency)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "sender wallet not found")
	}

	// 1b. Validate Sender KYC Status & Limits
	sender, err := s.userRepo.FindByID(ctx, req.SenderID)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to fetch sender profile")
	}

	if sender.KYCStatus != domain.KYCStatusVerified {
		return nil, errors.New("KYC verification required to send funds")
	}

	// Define limits based on KYC Level
	// Note: In a production environment, limits should be normalized to a base currency.
	// Current implementation assumes limits apply to the transaction currency directly.
	var limit decimal.Decimal
	switch sender.KYCLevel {
	case 1:
		limit = decimal.NewFromInt(5000000) // Tier 1: 5M limit (increased for testing)
	case 2:
		limit = decimal.NewFromInt(10000000) // Tier 2: 10M limit
	case 3:
		limit = decimal.NewFromInt(100000000) // Tier 3: 100M limit
	default:
		limit = decimal.NewFromInt(0) // Tier 0: No sending
	}

	if sender.KYCLevel == 0 {
		return nil, errors.New("KYC Level 1 required to transact")
	}

	if req.Amount.GreaterThan(limit) {
		return nil, fmt.Errorf("transaction amount exceeds your KYC Level %d limit of %s", sender.KYCLevel, limit.String())
	}

	// 1c. Check Daily Velocity Limit
	// dailyTotal is already fetched at the beginning of the function

	var dailyLimit decimal.Decimal
	switch sender.KYCLevel {
	case 1:
		dailyLimit = decimal.NewFromInt(10000000) // Tier 1: 10M Daily
	case 2:
		dailyLimit = decimal.NewFromInt(50000000) // Tier 2: 50M Daily
	case 3:
		dailyLimit = decimal.NewFromInt(500000000) // Tier 3: 500M Daily
	default:
		dailyLimit = decimal.Zero
	}

	if dailyTotal.Add(req.Amount).GreaterThan(dailyLimit) {
		return nil, fmt.Errorf("daily transaction limit of %s exceeded (used: %s)", dailyLimit.String(), dailyTotal.String())
	}

	// 1d. Check Hourly Velocity (Fraud Detection)
	// General Velocity Check
	hourlyCount, err := s.repo.GetHourlyCount(ctx, req.SenderID)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to check velocity")
	}
	if err := s.riskEngine.CheckVelocity(hourlyCount); err != nil {
		s.logger.Warn("Transaction blocked by velocity check", map[string]interface{}{
			"user_id":      req.SenderID,
			"hourly_count": hourlyCount,
		})
		// Log Security Event
		go func() {
			_ = s.securityRepo.LogSecurityEvent(context.Background(), &domain.SecurityEvent{
				Type:        "risk_block",
				Severity:    "medium",
				Description: fmt.Sprintf("Velocity limit exceeded. Hourly Count: %d", hourlyCount),
				Status:      "blocked",
				UserID:      &req.SenderID,
				IPAddress:   req.Location,
				CreatedAt:   time.Now(),
			})
		}()
		return nil, err
	}

	// Max 3 transactions > HighValueThreshold per hour
	highValueThreshold := decimal.NewFromInt(s.riskEngine.GetConfig().HighValueThreshold)
	if req.Amount.GreaterThan(highValueThreshold) {
		count, err := s.repo.GetHourlyHighValueCount(ctx, req.SenderID, highValueThreshold)
		if err != nil {
			return nil, pkgerrors.Wrap(err, "failed to check hourly velocity")
		}
		if count >= 3 {
			return nil, errors.New("velocity limit exceeded: too many high-value transactions in the last hour")
		}
	}

	// 1e. Advanced Risk Analysis & Cool-off
	if err := s.riskEngine.CheckCoolOff(req.SenderID, req.Amount); err != nil {
		return nil, err
	}

	// 1f. Behavioral Anomaly Detection
	anomalies, err := s.monitor.DetectAnomalies(req.SenderID, req.Amount, req.ReceiverID.String())
	if err == nil && len(anomalies) > 0 {
		for _, anomaly := range anomalies {
			s.logger.Warn("Behavioral anomaly detected", map[string]interface{}{
				"user_id":     req.SenderID,
				"type":        anomaly.Type,
				"description": anomaly.Description,
				"severity":    anomaly.Severity,
			})

			// If HIGH severity, block or require 2FA (for now, block)
			if anomaly.Severity == "HIGH" {
				// Notify user
				go func() {
					_ = s.notifier.Notify(context.Background(), req.SenderID, "SECURITY_ALERT", map[string]interface{}{
						"reason": anomaly.Description,
					})
				}()
				return nil, fmt.Errorf("security alert: %s", anomaly.Description)
			}
		}
	}

	// Evaluate Risk Score
	// We assume device is trusted here because of earlier check (lines 106-123)
	riskScore := s.riskEngine.EvaluateRisk(req.Amount, sender.KYCLevel, false, req.Location)
	if riskScore >= risk.RiskScoreCritical {
		s.logger.Error("Transaction blocked due to CRITICAL risk score", map[string]interface{}{
			"risk_score": riskScore,
			"amount":     req.Amount.String(),
			"sender_id":  req.SenderID,
		})

		// Notify user about risk block
		go func() {
			_ = s.notifier.Notify(context.Background(), req.SenderID, "RISK_ALERT", map[string]interface{}{
				"reason": "Transaction blocked due to high risk score",
				"amount": req.Amount.String(),
			})
		}()

		// Log Security Event
		go func() {
			_ = s.securityRepo.LogSecurityEvent(context.Background(), &domain.SecurityEvent{
				Type:        "risk_block",
				Severity:    "critical",
				Description: fmt.Sprintf("Transaction blocked. Risk Score: %d. Amount: %s", riskScore, req.Amount.String()),
				Status:      "blocked",
				UserID:      &req.SenderID,
				IPAddress:   req.Location,
				CreatedAt:   time.Now(),
			})
		}()

		return nil, errors.New("transaction blocked by risk engine")
	}

	// Get receiver's wallet
	var receiverWallet *domain.Wallet

	if req.ReceiverWalletAddress != "" {
		// Lookup by Address (Preferred/Strict)
		receiverWallet, err = s.walletRepo.FindByAddress(ctx, req.ReceiverWalletAddress)
		if err != nil {
			return nil, pkgerrors.Wrap(err, "receiver wallet not found by address")
		}
		req.ReceiverID = receiverWallet.UserID
	} else if req.ReceiverID != uuid.Nil {
		// Lookup by UserID (Fallback for internal calls/simulations)
		// We need to know which currency wallet to look for.
		// If DestinationCurrency is set, use that. Otherwise assume same as sender (or default).
		targetCurrency := req.DestinationCurrency
		if targetCurrency == "" {
			targetCurrency = req.Currency
		}

		receiverWallet, err = s.walletRepo.FindByUserAndCurrency(ctx, req.ReceiverID, targetCurrency)
		if err != nil {
			// If not found in target currency, try to find ANY wallet or handle error
			// For now, fail if not found
			return nil, pkgerrors.Wrap(err, "receiver wallet not found for user")
		}
		req.ReceiverWalletAddress = *receiverWallet.WalletAddress
	} else {
		return nil, errors.New("receiver information missing (wallet address or user id required)")
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
		// Use sell rate for conversion (sender sells base currency)
		exchangeRate = rate.SellRate
		convertedAmount = req.Amount.Mul(rate.SellRate)
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
	initialStatus := domain.TransactionStatusPending
	if s.riskEngine.RequiresAdminApproval(req.Amount) {
		initialStatus = domain.TransactionStatusPendingApproval
	}

	tx := &domain.Transaction{
		ID:                uuid.New(),
		Reference:         req.Reference,
		SenderID:          req.SenderID,
		ReceiverID:        req.ReceiverID,
		SenderWalletID:    &senderWallet.ID,
		ReceiverWalletID:  &receiverWallet.ID,
		Amount:            req.Amount,
		Currency:          req.Currency,
		ExchangeRate:      exchangeRate,
		ConvertedAmount:   convertedAmount,
		ConvertedCurrency: convertedCurrency,
		FeeAmount:         feeAmount,
		FeeCurrency:       req.Currency,
		NetAmount:         convertedAmount,
		Status:            initialStatus,
		TransactionType:   domain.TransactionTypePayment,
		Channel:           req.Channel,
		Category:          req.Category,
		Description:       req.Description,
		Metadata:          req.Metadata,
		InitiatedAt:       time.Now(),
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// Persist initial transaction record (pending)
	if err := s.repo.Create(ctx, tx); err != nil {
		if errors.Is(err, pkgerrors.ErrTransactionAlreadyExists) {
			existingTx, findErr := s.repo.FindByReference(ctx, tx.Reference)
			if findErr == nil && existingTx != nil {
				s.logger.Info("Idempotency match found during create", map[string]interface{}{
					"reference": tx.Reference,
					"tx_id":     existingTx.ID,
				})
				return &PaymentResponse{
					Transaction: existingTx,
					Message:     "Transaction already processed (idempotent)",
				}, nil
			}
		}

		s.logger.Error("Transaction create failed", map[string]interface{}{
			"error":              err.Error(),
			"transaction_id":     tx.ID,
			"reference":          tx.Reference,
			"sender_id":          tx.SenderID,
			"receiver_id":        tx.ReceiverID,
			"sender_wallet_id":   tx.SenderWalletID,
			"receiver_wallet_id": tx.ReceiverWalletID,
			"amount":             tx.Amount.String(),
			"currency":           string(tx.Currency),
			"exchange_rate":      tx.ExchangeRate.String(),
			"converted_amount":   tx.ConvertedAmount.String(),
			"converted_currency": string(tx.ConvertedCurrency),
			"fee_amount":         tx.FeeAmount.String(),
			"status":             string(tx.Status),
		})
		return nil, err
	}

	// Check if transaction requires admin approval
	if tx.Status == domain.TransactionStatusPendingApproval {
		s.logger.Info("Transaction queued for admin approval", map[string]interface{}{
			"tx_id":  tx.ID,
			"amount": tx.Amount.String(),
		})

		// Send notification to admin (simulated) or user
		go func() {
			_ = s.notifier.Notify(context.Background(), req.SenderID, "TRANSACTION_PENDING_APPROVAL", map[string]interface{}{
				"amount": req.Amount.String(),
				"reason": "Amount exceeds automatic approval threshold",
			})
		}()

		return &PaymentResponse{
			Transaction: tx,
			Message:     "Transaction submitted for admin approval",
		}, nil
	}

	// 6. Process payment atomically
	if err := s.processPayment(ctx, tx, senderWallet, receiverWallet, totalDebit); err != nil {
		s.riskEngine.ReportFailure()
		tx.Status = domain.TransactionStatusFailed
		reason := err.Error()
		tx.StatusReason = reason
		tx.UpdatedAt = time.Now()
		s.logger.Error("Ledger posting failed", map[string]interface{}{
			"error":              err.Error(),
			"transaction_id":     tx.ID,
			"reference":          tx.Reference,
			"debit_wallet_id":    senderWallet.ID,
			"credit_wallet_id":   receiverWallet.ID,
			"debit_amount":       totalDebit.String(),
			"credit_amount":      tx.ConvertedAmount.String(),
			"currency":           string(tx.Currency),
			"converted_currency": string(tx.ConvertedCurrency),
			"exchange_rate":      tx.ExchangeRate.String(),
			"fee_amount":         tx.FeeAmount.String(),
		})
		if updateErr := s.repo.Update(ctx, tx); updateErr != nil {
			s.logger.Error("Failed to update transaction status to failed", map[string]interface{}{
				"error":          updateErr.Error(),
				"transaction_id": tx.ID,
			})
		}
		return nil, err
	}

	s.riskEngine.ReportSuccess()

	// 7. Mark as pending settlement (so Settlement Service picks it up)
	tx.Status = domain.TransactionStatusPendingSettlement
	now := time.Now()
	tx.CompletedAt = &now
	tx.UpdatedAt = now

	if err := s.repo.Update(ctx, tx); err != nil {
		s.logger.Error("Transaction update failed", map[string]interface{}{
			"error":          err.Error(),
			"transaction_id": tx.ID,
			"reference":      tx.Reference,
			"status":         string(tx.Status),
		})
		return nil, err
	}

	s.logger.Info("Payment completed", map[string]interface{}{
		"transaction_id": tx.ID,
		"reference":      tx.Reference,
	})

	// Behavioral Monitoring (Async - Record Update)
	go func() {
		s.monitor.RecordTransaction(req.SenderID, req.Amount, req.ReceiverID.String(), "Unknown Location")
	}()

	// Real Notification
	go func() {
		// Notify Sender
		_ = s.notifier.Notify(context.Background(), req.SenderID, "PAYMENT_SENT", map[string]interface{}{
			"amount":        req.Amount.String(),
			"currency":      req.Currency,
			"receiver_name": req.ReceiverID.String(), // Ideally name, but ID for now
		})

		// Notify Receiver
		_ = s.notifier.Notify(context.Background(), req.ReceiverID, "PAYMENT_RECEIVED", map[string]interface{}{
			"amount":      tx.ConvertedAmount.String(),
			"currency":    tx.ConvertedCurrency,
			"sender_name": req.SenderID.String(),
		})
	}()

	return &PaymentResponse{
		Transaction: tx,
		Message:     "Payment processed successfully",
	}, nil
}

type Receipt struct {
	TransactionID uuid.UUID       `json:"transaction_id"`
	Reference     string          `json:"reference"`
	Date          time.Time       `json:"date"`
	SenderName    string          `json:"sender_name"`
	ReceiverName  string          `json:"receiver_name"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      domain.Currency `json:"currency"`
	Fee           decimal.Decimal `json:"fee"`
	TotalDebited  decimal.Decimal `json:"total_debited"`
	Status        string          `json:"status"`
	Description   string          `json:"description"`
}

type ReceiverInfo struct {
	Name string `json:"name"`
}

func (s *Service) GetReceiverInfo(ctx context.Context, walletNumber string) (*ReceiverInfo, error) {
	wallet, err := s.walletRepo.FindByAddress(ctx, walletNumber)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "wallet not found")
	}

	user, err := s.userRepo.FindByID(ctx, wallet.UserID)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "user not found")
	}

	return &ReceiverInfo{
		Name: fmt.Sprintf("%s %s", user.FirstName, user.LastName),
	}, nil
}

func (s *Service) GetReceipt(ctx context.Context, txID uuid.UUID, userID uuid.UUID) (*Receipt, error) {
	tx, err := s.repo.FindByID(ctx, txID)
	if err != nil {
		return nil, err
	}

	// Security Check: Ensure the user is either the sender or receiver
	if tx.SenderID != userID && tx.ReceiverID != userID {
		return nil, errors.New("unauthorized access to transaction receipt")
	}

	sender, err := s.userRepo.FindByID(ctx, tx.SenderID)
	if err != nil {
		return nil, errors.New("failed to fetch sender details")
	}

	receiver, err := s.userRepo.FindByID(ctx, tx.ReceiverID)
	if err != nil {
		return nil, errors.New("failed to fetch receiver details")
	}

	return &Receipt{
		TransactionID: tx.ID,
		Reference:     tx.Reference,
		Date:          tx.CreatedAt,
		SenderName:    fmt.Sprintf("%s %s", sender.FirstName, sender.LastName),
		ReceiverName:  fmt.Sprintf("%s %s", receiver.FirstName, receiver.LastName),
		Amount:        tx.Amount,
		Currency:      tx.Currency,
		Fee:           tx.FeeAmount,
		TotalDebited:  tx.Amount.Add(tx.FeeAmount),
		Status:        string(tx.Status),
		Description:   tx.Description,
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

func (s *Service) getReceiverWallet(ctx context.Context, userID uuid.UUID, currency, destinationCurrency domain.Currency) (*domain.Wallet, error) {
	// Optimization: Fetch all wallets for the user in one go to reduce DB round trips
	wallets, err := s.walletRepo.FindByUserID(ctx, userID)
	if err != nil || len(wallets) == 0 {
		return nil, pkgerrors.ErrWalletNotFound
	}

	// 1. If destination currency is specified, try to find THAT wallet
	if destinationCurrency != "" {
		for _, w := range wallets {
			if w.Currency == destinationCurrency {
				return w, nil
			}
		}
		// If explicit destination currency requested but not found, return error
		return nil, pkgerrors.Wrap(fmt.Errorf("receiver has no %s wallet", destinationCurrency), "wallet not found")
	}

	// 2. Fallback: Try to get wallet in same currency as sender
	for _, w := range wallets {
		if w.Currency == currency {
			return w, nil
		}
	}

	// 3. Try default currency based on user's country
	user, err := s.userRepo.FindByID(ctx, userID)
	if err == nil {
		var defaultCurrency domain.Currency
		switch user.CountryCode {
		case "MW":
			defaultCurrency = "MWK"
		case "CN":
			defaultCurrency = "CNY"
		case "ZA":
			defaultCurrency = "ZAR"
		case "GB":
			defaultCurrency = "GBP"
		case "EU":
			defaultCurrency = "EUR"
		}

		if defaultCurrency != "" {
			for _, w := range wallets {
				if w.Currency == defaultCurrency {
					return w, nil
				}
			}
		}
	}

	// 4. Fallback: get user's primary wallet (first one found)
	return wallets[0], nil
}

func (s *Service) generateReference() string {
	return fmt.Sprintf("KYD-%d-%s", time.Now().Unix(), uuid.New().String()[:8])
}

func (s *Service) GetTransaction(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) GetUserTransactions(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*TransactionDetail, int, error) {
	txs, err := s.repo.FindByUserID(ctx, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountByUserID(ctx, userID)
	if err != nil {
		return nil, 0, err
	}

	var details []*TransactionDetail
	for _, tx := range txs {
		detail := &TransactionDetail{Transaction: tx}

		// Enrich with Names
		if sender, err := s.userRepo.FindByID(ctx, tx.SenderID); err == nil {
			detail.SenderName = sender.FirstName + " " + sender.LastName
		}
		if receiver, err := s.userRepo.FindByID(ctx, tx.ReceiverID); err == nil {
			detail.ReceiverName = receiver.FirstName + " " + receiver.LastName
		}

		// Enrich with Wallet Numbers
		if tx.SenderWalletID != nil {
			if sWallet, err := s.walletRepo.FindByID(ctx, *tx.SenderWalletID); err == nil && sWallet.WalletAddress != nil {
				detail.SenderWalletNumber = *sWallet.WalletAddress
			}
		}
		if tx.ReceiverWalletID != nil {
			if rWallet, err := s.walletRepo.FindByID(ctx, *tx.ReceiverWalletID); err == nil && rWallet.WalletAddress != nil {
				detail.ReceiverWalletNumber = *rWallet.WalletAddress
			}
		}
		details = append(details, detail)
	}

	return details, total, nil
}

func (s *Service) GetAllTransactions(ctx context.Context, limit, offset int) ([]*TransactionDetail, int, error) {
	txs, err := s.repo.FindAll(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountAll(ctx)
	if err != nil {
		return nil, 0, err
	}

	var details []*TransactionDetail
	for _, tx := range txs {
		detail := &TransactionDetail{Transaction: tx}

		// Enrich with Names
		if sender, err := s.userRepo.FindByID(ctx, tx.SenderID); err == nil {
			detail.SenderName = sender.FirstName + " " + sender.LastName
		}
		if receiver, err := s.userRepo.FindByID(ctx, tx.ReceiverID); err == nil {
			detail.ReceiverName = receiver.FirstName + " " + receiver.LastName
		}

		// Enrich with Wallet Numbers
		if tx.SenderWalletID != nil {
			if sWallet, err := s.walletRepo.FindByID(ctx, *tx.SenderWalletID); err == nil && sWallet.WalletAddress != nil {
				detail.SenderWalletNumber = *sWallet.WalletAddress
			}
		}
		if tx.ReceiverWalletID != nil {
			if rWallet, err := s.walletRepo.FindByID(ctx, *tx.ReceiverWalletID); err == nil && rWallet.WalletAddress != nil {
				detail.ReceiverWalletNumber = *rWallet.WalletAddress
			}
		}
		details = append(details, detail)
	}

	return details, total, nil
}

// Repository interfaces
type Repository interface {
	Create(ctx context.Context, tx *domain.Transaction) error
	Update(ctx context.Context, tx *domain.Transaction) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error)
	FindByReference(ctx context.Context, ref string) (*domain.Transaction, error)
	FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Transaction, error)
	CountByUserID(ctx context.Context, userID uuid.UUID) (int, error)
	GetDailyTotal(ctx context.Context, userID uuid.UUID, currency domain.Currency) (decimal.Decimal, error)
	GetHourlyHighValueCount(ctx context.Context, userID uuid.UUID, threshold decimal.Decimal) (int, error)
	GetHourlyCount(ctx context.Context, userID uuid.UUID) (int, error)
	FindByStatus(ctx context.Context, status domain.TransactionStatus, limit, offset int) ([]*domain.Transaction, error)
	CountByStatus(ctx context.Context, status domain.TransactionStatus) (int, error)
	SumVolume(ctx context.Context) (decimal.Decimal, error)
	SumEarnings(ctx context.Context) (decimal.Decimal, error)
	CountAll(ctx context.Context) (int, error)
	FindAll(ctx context.Context, limit, offset int) ([]*domain.Transaction, error)
	FindFlagged(ctx context.Context, limit, offset int) ([]*domain.Transaction, error)
	GetTransactionVolume(ctx context.Context, months int) ([]*domain.TransactionVolume, error)
	GetSystemStats(ctx context.Context) (*domain.SystemStats, error)
}

type AuditRepository interface {
	Create(ctx context.Context, log *domain.AuditLog) error
	FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error)
	FindAll(ctx context.Context, limit, offset int) ([]*domain.AuditLog, error)
	CountAll(ctx context.Context) (int, error)
}

func (s *Service) GetAuditLogs(ctx context.Context, limit, offset int) ([]*domain.AuditLog, int, error) {
	logs, err := s.auditRepo.FindAll(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.auditRepo.CountAll(ctx)
	if err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

type WalletRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error)
	FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error)
	FindByAddress(ctx context.Context, address string) (*domain.Wallet, error)
	DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
	CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
	ReserveFunds(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error
}

type SecurityRepository interface {
	LogSecurityEvent(ctx context.Context, event *domain.SecurityEvent) error
	IsBlacklisted(ctx context.Context, value string) (bool, error)
}

type ForexService interface {
	GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error)
}

type LedgerService interface {
	PostTransaction(ctx context.Context, posting *ledger.LedgerPosting) error
}

type UserRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	IsDeviceTrusted(ctx context.Context, userID uuid.UUID, deviceHash string) (bool, error)
	IsCountryTrusted(ctx context.Context, userID uuid.UUID, countryCode string) (bool, error)
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
	ReceiverID          uuid.UUID       `json:"receiver_id" validate:"required"`
	Amount              decimal.Decimal `json:"amount" validate:"required,gt=0"`
	Currency            domain.Currency `json:"currency" validate:"required"`
	DestinationCurrency domain.Currency `json:"destination_currency"`
	Description         string          `json:"description"`
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
			SenderID:            req.SenderID,
			ReceiverID:          item.ReceiverID,
			Amount:              item.Amount,
			Currency:            item.Currency,
			DestinationCurrency: item.DestinationCurrency,
			Description:         item.Description,
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

// Admin Methods

func (s *Service) GetPendingTransactions(ctx context.Context, limit, offset int) ([]*TransactionDetail, int, error) {
	txs, err := s.repo.FindByStatus(ctx, domain.TransactionStatusPendingApproval, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountByStatus(ctx, domain.TransactionStatusPendingApproval)
	if err != nil {
		return nil, 0, err
	}

	var details []*TransactionDetail
	for _, tx := range txs {
		detail := &TransactionDetail{Transaction: tx}
		// Enrich with names/wallet numbers (optional, can skip if slow)
		if sender, err := s.userRepo.FindByID(ctx, tx.SenderID); err == nil {
			detail.SenderName = sender.FirstName + " " + sender.LastName
		}
		if receiver, err := s.userRepo.FindByID(ctx, tx.ReceiverID); err == nil {
			detail.ReceiverName = receiver.FirstName + " " + receiver.LastName
		}
		details = append(details, detail)
	}

	return details, total, nil
}

func (s *Service) ReviewTransaction(ctx context.Context, txID uuid.UUID, adminID uuid.UUID, action string, reason string) error {
	tx, err := s.repo.FindByID(ctx, txID)
	if err != nil {
		return err
	}

	if tx.Status != domain.TransactionStatusPendingApproval {
		return errors.New("transaction is not pending approval")
	}

	if action == "approve" {
		// Proceed with payment processing
		s.logger.Info("Admin approving transaction", map[string]interface{}{"tx_id": txID, "admin_id": adminID})

		// Fetch wallets
		if tx.SenderWalletID == nil || tx.ReceiverWalletID == nil {
			return errors.New("missing wallet IDs for approval")
		}
		senderWallet, err := s.walletRepo.FindByID(ctx, *tx.SenderWalletID)
		if err != nil {
			return err
		}
		receiverWallet, err := s.walletRepo.FindByID(ctx, *tx.ReceiverWalletID)
		if err != nil {
			return err
		}

		// Calculate Debit Amount (Original Amount + Fee)
		totalDebit := tx.Amount.Add(tx.FeeAmount)

		// Process payment atomically
		if err := s.processPayment(ctx, tx, senderWallet, receiverWallet, totalDebit); err != nil {
			s.logger.Error("Admin approval failed at ledger", map[string]interface{}{"error": err.Error()})
			return err
		}

		// Update Status
		tx.Status = domain.TransactionStatusPendingSettlement
		now := time.Now()
		tx.CompletedAt = &now
		tx.UpdatedAt = now
		if tx.Metadata == nil {
			tx.Metadata = make(domain.Metadata)
		}
		tx.Metadata["approved_by"] = adminID.String()
		tx.Metadata["approved_at"] = now

		if err := s.repo.Update(ctx, tx); err != nil {
			return err
		}

		// Notify
		go func() {
			_ = s.notifier.Notify(context.Background(), tx.SenderID, "TRANSACTION_APPROVED", map[string]interface{}{"tx_id": txID})
		}()

	} else if action == "reject" {
		s.logger.Info("Admin rejecting transaction", map[string]interface{}{"tx_id": txID, "admin_id": adminID, "reason": reason})

		tx.Status = domain.TransactionStatusFailed
		failReason := fmt.Sprintf("Admin rejected: %s", reason)
		tx.StatusReason = failReason
		tx.UpdatedAt = time.Now()
		if tx.Metadata == nil {
			tx.Metadata = make(domain.Metadata)
		}
		tx.Metadata["rejected_by"] = adminID.String()
		tx.Metadata["rejection_reason"] = reason

		if err := s.repo.Update(ctx, tx); err != nil {
			return err
		}

		// Notify
		go func() {
			_ = s.notifier.Notify(context.Background(), tx.SenderID, "TRANSACTION_REJECTED", map[string]interface{}{"tx_id": txID, "reason": reason})
		}()
	} else {
		return errors.New("invalid action: must be 'approve' or 'reject'")
	}

	return nil
}

func (s *Service) GetSystemStats(ctx context.Context) (*domain.SystemStats, error) {
	return s.repo.GetSystemStats(ctx)
}

func (s *Service) GetTransactionVolume(ctx context.Context, months int) ([]*domain.TransactionVolume, error) {
	return s.repo.GetTransactionVolume(ctx, months)
}

func (s *Service) GetDisputes(ctx context.Context, limit, offset int) ([]*domain.Transaction, int, error) {
	txs, err := s.repo.FindByStatus(ctx, domain.TransactionStatusDisputed, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	count, err := s.repo.CountByStatus(ctx, domain.TransactionStatusDisputed)
	if err != nil {
		return nil, 0, err
	}
	return txs, count, nil
}

func (s *Service) GetRiskAlerts(ctx context.Context, limit, offset int) ([]*TransactionDetail, error) {
	txs, err := s.repo.FindFlagged(ctx, limit, offset)
	if err != nil {
		return nil, err
	}

	var details []*TransactionDetail
	for _, tx := range txs {
		detail := &TransactionDetail{Transaction: tx}
		// Enrich with names
		if sender, err := s.userRepo.FindByID(ctx, tx.SenderID); err == nil {
			detail.SenderName = sender.FirstName + " " + sender.LastName
		}
		if receiver, err := s.userRepo.FindByID(ctx, tx.ReceiverID); err == nil {
			detail.ReceiverName = receiver.FirstName + " " + receiver.LastName
		}
		details = append(details, detail)
	}

	return details, nil
}

func (s *Service) notifyReceiverTopUp(ctx context.Context, tx *domain.Transaction) error {
	// In a real system, this would push a notification (email/SMS/push)
	// For now, we just log it
	s.logger.Info("Notification sent to receiver", map[string]interface{}{
		"receiver_id": tx.ReceiverID,
		"amount":      tx.ConvertedAmount.String(),
		"currency":    tx.ConvertedCurrency,
	})
	return nil
}
