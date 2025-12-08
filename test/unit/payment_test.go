// ==============================================================================
// UNIT TESTS - test/unit/payment_test.go
// ==============================================================================
package unit_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"kyd/internal/domain"
	"kyd/internal/payment"
	"kyd/pkg/logger"
)

// Mock repositories
type MockTransactionRepository struct {
	mock.Mock
}

func (m *MockTransactionRepository) Create(ctx context.Context, tx *domain.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *MockTransactionRepository) Update(ctx context.Context, tx *domain.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *MockTransactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) FindByReference(ctx context.Context, ref string) (*domain.Transaction, error) {
	args := m.Called(ctx, ref)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Transaction, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
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

type MockWalletRepository struct {
	mock.Mock
}

func (m *MockWalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepository) FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepository) FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error) {
	args := m.Called(ctx, userID, currency)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	args := m.Called(ctx, wallet)
	return args.Error(0)
}

func (m *MockWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	args := m.Called(ctx, wallet)
	return args.Error(0)
}

func (m *MockWalletRepository) DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	args := m.Called(ctx, walletID, amount)
	return args.Error(0)
}

func (m *MockWalletRepository) CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	args := m.Called(ctx, walletID, amount)
	return args.Error(0)
}

type MockForexService struct {
	mock.Mock
}

func (m *MockForexService) GetRate(ctx context.Context, from, to domain.Currency) (*domain.ExchangeRate, error) {
	args := m.Called(ctx, from, to)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.ExchangeRate), args.Error(1)
}

type MockLedgerService struct {
	mock.Mock
}

func (m *MockLedgerService) PostTransaction(ctx context.Context, posting *payment.LedgerPosting) error {
	args := m.Called(ctx, posting)
	return args.Error(0)
}

// Test cases
func TestInitiatePayment_Success(t *testing.T) {
	// Setup
	mockTxRepo := new(MockTransactionRepository)
	mockWalletRepo := new(MockWalletRepository)
	mockForex := new(MockForexService)
	mockLedger := new(MockLedgerService)

	service := payment.NewService(
		mockTxRepo,
		mockWalletRepo,
		mockForex,
		mockLedger,
		logger.NewNop(),
	)

	// Test data
	senderID := uuid.New()
	receiverID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()

	req := &payment.InitiatePaymentRequest{
		SenderID:   senderID,
		ReceiverID: receiverID,
		Amount:     decimal.NewFromInt(1000),
		Currency:   domain.MWK,
	}

	senderWallet := &domain.Wallet{
		ID:               senderWalletID,
		UserID:           senderID,
		Currency:         domain.MWK,
		AvailableBalance: decimal.NewFromInt(5000),
		LedgerBalance:    decimal.NewFromInt(5000),
	}

	receiverWallet := &domain.Wallet{
		ID:               receiverWalletID,
		UserID:           receiverID,
		Currency:         domain.CNY,
		AvailableBalance: decimal.Zero,
		LedgerBalance:    decimal.Zero,
	}

	exchangeRate := &domain.ExchangeRate{
		BaseCurrency:   domain.MWK,
		TargetCurrency: domain.CNY,
		Rate:           decimal.NewFromFloat(0.0085),
		SellRate:       decimal.NewFromFloat(0.008373),
	}

	// Expectations
	mockWalletRepo.On("FindByUserAndCurrency", mock.Anything, senderID, domain.MWK).
		Return(senderWallet, nil)
	mockWalletRepo.On("FindByUserAndCurrency", mock.Anything, receiverID, domain.MWK).
		Return(nil, errors.ErrWalletNotFound)
	mockWalletRepo.On("FindByUserID", mock.Anything, receiverID).
		Return([]*domain.Wallet{receiverWallet}, nil)
	mockForex.On("GetRate", mock.Anything, domain.MWK, domain.CNY).
		Return(exchangeRate, nil)
	mockLedger.On("PostTransaction", mock.Anything, mock.AnythingOfType("*payment.LedgerPosting")).
		Return(nil)
	mockTxRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.Transaction")).
		Return(nil)
	mockTxRepo.On("Update", mock.Anything, mock.AnythingOfType("*domain.Transaction")).
		Return(nil)

	// Execute
	result, err := service.InitiatePayment(context.Background(), req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Transaction)
	assert.Equal(t, domain.TransactionStatusCompleted, result.Transaction.Status)
	assert.Equal(t, req.Amount, result.Transaction.Amount)
	assert.True(t, result.Transaction.ConvertedAmount.GreaterThan(decimal.Zero))

	// Verify all expectations
	mockWalletRepo.AssertExpectations(t)
	mockForex.AssertExpectations(t)
	mockLedger.AssertExpectations(t)
	mockTxRepo.AssertExpectations(t)
}

func TestInitiatePayment_InsufficientBalance(t *testing.T) {
	// Setup
	mockTxRepo := new(MockTransactionRepository)
	mockWalletRepo := new(MockWalletRepository)
	mockForex := new(MockForexService)
	mockLedger := new(MockLedgerService)

	service := payment.NewService(
		mockTxRepo,
		mockWalletRepo,
		mockForex,
		mockLedger,
		logger.NewNop(),
	)

	// Test data
	senderID := uuid.New()
	receiverID := uuid.New()

	req := &payment.InitiatePaymentRequest{
		SenderID:   senderID,
		ReceiverID: receiverID,
		Amount:     decimal.NewFromInt(10000),
		Currency:   domain.MWK,
	}

	senderWallet := &domain.Wallet{
		ID:               uuid.New(),
		AvailableBalance: decimal.NewFromInt(100), // Insufficient
	}

	mockWalletRepo.On("FindByUserAndCurrency", mock.Anything, senderID, domain.MWK).
		Return(senderWallet, nil)

	// Execute
	result, err := service.InitiatePayment(context.Background(), req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "insufficient balance")
}
