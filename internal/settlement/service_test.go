package settlement

import (
	"context"
	"testing"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocks

type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, settlement *domain.Settlement) error {
	args := m.Called(ctx, settlement)
	return args.Error(0)
}

func (m *MockRepository) Update(ctx context.Context, settlement *domain.Settlement) error {
	args := m.Called(ctx, settlement)
	return args.Error(0)
}

func (m *MockRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Settlement, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Settlement), args.Error(1)
}

func (m *MockRepository) FindSubmitted(ctx context.Context) ([]*domain.Settlement, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Settlement), args.Error(1)
}

type MockTransactionRepository struct {
	mock.Mock
}

func (m *MockTransactionRepository) Update(ctx context.Context, tx *domain.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *MockTransactionRepository) FindPendingSettlement(ctx context.Context, limit int) ([]*domain.Transaction, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) FindBySettlementID(ctx context.Context, settlementID uuid.UUID) ([]*domain.Transaction, error) {
	args := m.Called(ctx, settlementID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) FindStuckPending(ctx context.Context, olderThan time.Duration, limit int) ([]*domain.Transaction, error) {
	args := m.Called(ctx, olderThan, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) BatchUpdateSettlementID(ctx context.Context, txIDs []uuid.UUID, settlementID uuid.UUID) error {
	args := m.Called(ctx, txIDs, settlementID)
	return args.Error(0)
}

type MockBlockchainConnector struct {
	mock.Mock
}

func (m *MockBlockchainConnector) SubmitSettlement(ctx context.Context, settlement *domain.Settlement) (*SettlementResult, error) {
	args := m.Called(ctx, settlement)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SettlementResult), args.Error(1)
}

func (m *MockBlockchainConnector) CheckConfirmation(ctx context.Context, txHash string) (bool, error) {
	args := m.Called(ctx, txHash)
	return args.Bool(0), args.Error(1)
}

type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) Info(msg string, fields map[string]interface{}) {
	m.Called(msg, fields)
}

func (m *MockLogger) Error(msg string, fields map[string]interface{}) {
	m.Called(msg, fields)
}

func (m *MockLogger) Warn(msg string, fields map[string]interface{}) {
	m.Called(msg, fields)
}

func (m *MockLogger) Debug(msg string, fields map[string]interface{}) {
	m.Called(msg, fields)
}

func (m *MockLogger) Fatal(msg string, fields map[string]interface{}) {
	m.Called(msg, fields)
}

func (m *MockLogger) WithFields(fields map[string]interface{}) logger.Logger {
	return m
}

func (m *MockLogger) WithError(err error) logger.Logger {
	return m
}

// Tests

func TestRecoverPendingSettlements(t *testing.T) {
	mockRepo := new(MockRepository)
	mockTxRepo := new(MockTransactionRepository)
	mockStellar := new(MockBlockchainConnector)
	mockRipple := new(MockBlockchainConnector)
	mockLog := new(MockLogger)

	service := NewService(mockRepo, mockTxRepo, mockStellar, mockRipple, mockLog)
	service.monitorInterval = 1 * time.Millisecond // Speed up test

	txHash := "tx123"
	settlementID := uuid.New()
	settlement := &domain.Settlement{
		ID:              settlementID,
		TransactionHash: &txHash,
		Network:         domain.NetworkRipple,
		Status:          domain.SettlementStatusSubmitted,
	}

	// Mock expectations
	mockRepo.On("FindSubmitted", mock.Anything).Return([]*domain.Settlement{settlement}, nil)
	mockLog.On("Info", "Resumed monitoring for settlement", mock.Anything).Return()
	mockLog.On("Info", "Settlement confirmed", mock.Anything).Return()

	// For monitorSettlement (runs in background)
	// It will call FindByID and then CheckConfirmation
	mockRepo.On("FindByID", mock.Anything, settlementID).Return(settlement, nil)
	mockRipple.On("CheckConfirmation", mock.Anything, txHash).Return(true, nil)
	mockRepo.On("Update", mock.Anything, mock.MatchedBy(func(s *domain.Settlement) bool {
		return s.Status == domain.SettlementStatusConfirmed
	})).Return(nil)

	// Expectations for transaction updates
	mockTxRepo.On("FindBySettlementID", mock.Anything, settlementID).Return([]*domain.Transaction{}, nil)
	// No transactions to update since we return empty list

	err := service.RecoverPendingSettlements(context.Background())
	assert.NoError(t, err)

	// Wait a bit for goroutine to run
	time.Sleep(100 * time.Millisecond)

	mockRepo.AssertExpectations(t)
	mockRipple.AssertExpectations(t)
}
