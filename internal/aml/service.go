// internal/aml/service.go
package aml

import (
	"context"
	"fmt"
	"time"

	"kyd/pkg/config"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type AMLService interface {
	CheckUser(ctx context.Context, userID uuid.UUID, amount decimal.Decimal) (string, error)
	CheckTransaction(ctx context.Context, transactionID uuid.UUID) (string, error)
	EnqueueCheck(ctx context.Context, check *AMLCheckRequest) (string, error)
	GetCheckResult(ctx context.Context, checkID string) (*AMLResult, error)
}

type AMLCheckRequest struct {
	ID            string          `json:"id"`
	UserID        uuid.UUID       `json:"user_id"`
	TransactionID *uuid.UUID      `json:"transaction_id,omitempty"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
	CheckType     string          `json:"check_type"` // "user", "transaction"
	CreatedAt     time.Time       `json:"created_at"`
}

type AMLResult struct {
	CheckID       string      `json:"check_id"`
	UserID        uuid.UUID   `json:"user_id"`
	TransactionID *uuid.UUID  `json:"transaction_id,omitempty"`
	RiskScore     float64     `json:"risk_score"` // 0-100
	RiskLevel     string      `json:"risk_level"` // "low", "medium", "high"
	Status        string      `json:"status"`     // "passed", "failed", "pending", "manual_review"
	Flags         []string    `json:"flags"`      // ["sanction_match", "pep", "adverse_media"]
	CheckedAt     time.Time   `json:"checked_at"`
	Details       interface{} `json:"details,omitempty"`
}

// Mock implementation following async pattern
type MockAMLService struct {
	config    *config.Config
	logger    logger.Logger
	results   map[string]*AMLResult
	checkChan chan *AMLCheckRequest
}

func NewMockAMLService(cfg *config.Config, log logger.Logger) *MockAMLService {
	service := &MockAMLService{
		config:    cfg,
		logger:    log,
		results:   make(map[string]*AMLResult),
		checkChan: make(chan *AMLCheckRequest, 100),
	}

	// Start async worker (pattern from settlement service)
	go service.startAMLWorker()

	return service
}

func (s *MockAMLService) CheckUser(ctx context.Context, userID uuid.UUID, amount decimal.Decimal) (string, error) {
	checkID := uuid.New().String()
	check := &AMLCheckRequest{
		ID:        checkID,
		UserID:    userID,
		Amount:    amount,
		CheckType: "user",
		CreatedAt: time.Now(),
	}

	// For small amounts, return immediate mock result
	if amount.LessThan(s.config.AML.CheckThreshold) {
		result := s.generateMockResult(check)
		s.results[checkID] = result
		return checkID, nil
	}

	// For larger amounts, enqueue async check
	return s.EnqueueCheck(ctx, check)
}

func (s *MockAMLService) CheckTransaction(ctx context.Context, transactionID uuid.UUID) (string, error) {
	checkID := uuid.New().String()
	check := &AMLCheckRequest{
		ID:            checkID,
		TransactionID: &transactionID,
		CheckType:     "transaction",
		CreatedAt:     time.Now(),
	}

	// Always enqueue transaction checks
	return s.EnqueueCheck(ctx, check)
}

func (s *MockAMLService) EnqueueCheck(ctx context.Context, check *AMLCheckRequest) (string, error) {
	select {
	case s.checkChan <- check:
		s.logger.Info("AML check enqueued", map[string]interface{}{
			"check_id": check.ID,
			"user_id":  check.UserID,
			"type":     check.CheckType,
		})
		return check.ID, nil
	default:
		return "", fmt.Errorf("AML check queue is full")
	}
}

func (s *MockAMLService) GetCheckResult(ctx context.Context, checkID string) (*AMLResult, error) {
	if result, exists := s.results[checkID]; exists {
		return result, nil
	}
	return nil, fmt.Errorf("check result not found")
}

// Async worker pattern (similar to settlement service)
func (s *MockAMLService) startAMLWorker() {
	for check := range s.checkChan {
		// Simulate async processing delay
		time.Sleep(2 * time.Second)

		result := s.generateMockResult(check)
		s.results[check.ID] = result

		s.logger.Info("AML check completed", map[string]interface{}{
			"check_id":   check.ID,
			"risk_score": result.RiskScore,
			"risk_level": result.RiskLevel,
			"status":     result.Status,
		})

		// In production, this would trigger notifications or callbacks
		s.handleCheckResult(result)
	}
}

func (s *MockAMLService) generateMockResult(check *AMLCheckRequest) *AMLResult {
	// Mock risk scoring logic
	riskScore := 10.0 // Low risk by default

	// Add some randomness
	if check.Amount.GreaterThan(decimal.NewFromInt(10000)) {
		riskScore += 30
	}

	// Determine risk level
	riskLevel := "low"
	if riskScore > 50 {
		riskLevel = "high"
	} else if riskScore > 20 {
		riskLevel = "medium"
	}

	// Determine status
	status := "passed"
	if riskLevel == "high" && s.config.AML.AutoBlock {
		status = "failed"
	} else if riskLevel == "medium" {
		status = "manual_review"
	}

	return &AMLResult{
		CheckID:       check.ID,
		UserID:        check.UserID,
		TransactionID: check.TransactionID,
		RiskScore:     riskScore,
		RiskLevel:     riskLevel,
		Status:        status,
		Flags:         []string{}, // Mock no flags
		CheckedAt:     time.Now(),
		Details:       "Mock AML check completed",
	}
}

func (s *MockAMLService) handleCheckResult(result *AMLResult) {
	// Log the result - in production this would trigger notifications
	s.logger.Info("AML check result processed", map[string]interface{}{
		"check_id":   result.CheckID,
		"risk_score": result.RiskScore,
		"status":     result.Status,
	})
}
