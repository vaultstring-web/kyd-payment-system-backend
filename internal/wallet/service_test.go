package wallet

import (
	"context"
	"testing"
	"time"

	"kyd/internal/domain"
	"kyd/pkg/logger"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	args := m.Called(ctx, wallet)
	return args.Error(0)
}

func (m *MockRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	args := m.Called(ctx, wallet)
	return args.Error(0)
}

func (m *MockRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockRepository) FindByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Wallet), args.Error(1)
}

func (m *MockRepository) FindByUserAndCurrency(ctx context.Context, userID uuid.UUID, currency domain.Currency) (*domain.Wallet, error) {
	args := m.Called(ctx, userID, currency)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockRepository) FindByAddress(ctx context.Context, address string) (*domain.Wallet, error) {
	args := m.Called(ctx, address)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockRepository) DebitWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	args := m.Called(ctx, walletID, amount)
	return args.Error(0)
}

func (m *MockRepository) CreditWallet(ctx context.Context, walletID uuid.UUID, amount decimal.Decimal) error {
	args := m.Called(ctx, walletID, amount)
	return args.Error(0)
}

func (m *MockRepository) Count(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.Wallet, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Wallet), args.Error(1)
}

type MockTransactionRepository struct {
	mock.Mock
}

func (m *MockTransactionRepository) FindByWalletID(ctx context.Context, walletID uuid.UUID, limit, offset int) ([]*domain.Transaction, error) {
	args := m.Called(ctx, walletID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

func (m *MockTransactionRepository) CountByWalletID(ctx context.Context, walletID uuid.UUID) (int, error) {
	args := m.Called(ctx, walletID)
	return args.Int(0), args.Error(1)
}

type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockUserRepository) IsDeviceTrusted(ctx context.Context, userID uuid.UUID, deviceID string) (bool, error) {
	args := m.Called(ctx, userID, deviceID)
	return args.Bool(0), args.Error(1)
}

// --- Tests ---

func TestGetTransactionHistory(t *testing.T) {
	mockRepo := new(MockRepository)
	mockTxRepo := new(MockTransactionRepository)
	mockUserRepo := new(MockUserRepository)
	mockLogger := logger.NewNop()

	service := NewService(mockRepo, mockTxRepo, mockUserRepo, mockLogger)
	ctx := context.Background()

	walletID := uuid.New()
	senderID := uuid.New()
	receiverID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()

	senderAddr := "1234567890123456"
	receiverAddr := "6543210987654321"

	senderWallet := &domain.Wallet{ID: senderWalletID, UserID: senderID, WalletAddress: &senderAddr}
	receiverWallet := &domain.Wallet{ID: receiverWalletID, UserID: receiverID, WalletAddress: &receiverAddr}
	senderUser := &domain.User{ID: senderID, FirstName: "John", LastName: "Doe"}
	receiverUser := &domain.User{ID: receiverID, FirstName: "Jane", LastName: "Doe"}

	// Mock Wallet Existence Check (initial check)
	userID := uuid.New()
	mockRepo.On("FindByID", ctx, walletID).Return(&domain.Wallet{ID: walletID, UserID: userID}, nil)

	// Mock Transaction Fetch
	expectedTxs := []*domain.Transaction{
		{
			ID:               uuid.New(),
			SenderID:         senderID,
			ReceiverID:       receiverID,
			SenderWalletID:   senderWalletID,
			ReceiverWalletID: receiverWalletID,
			Amount:           decimal.NewFromInt(100),
			Currency:         domain.MWK,
			CreatedAt:        time.Now(),
		},
	}
	mockTxRepo.On("FindByWalletID", ctx, walletID, 10, 0).Return(expectedTxs, nil)

	// Mock Count
	mockTxRepo.On("CountByWalletID", ctx, walletID).Return(1, nil)

	// Mock Enrichment Calls
	mockUserRepo.On("FindByID", ctx, senderID).Return(senderUser, nil)
	mockUserRepo.On("FindByID", ctx, receiverID).Return(receiverUser, nil)
	mockRepo.On("FindByID", ctx, senderWalletID).Return(senderWallet, nil)
	mockRepo.On("FindByID", ctx, receiverWalletID).Return(receiverWallet, nil)

	// Execute
	txs, total, err := service.GetTransactionHistory(ctx, walletID, userID, 10, 0)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, txs, 1)
	assert.Equal(t, expectedTxs[0].ID, txs[0].Transaction.ID)
	assert.Equal(t, "John Doe", txs[0].SenderName)
	assert.Equal(t, "Jane Doe", txs[0].ReceiverName)
	assert.Equal(t, senderAddr, txs[0].SenderWalletNumber)
	assert.Equal(t, receiverAddr, txs[0].ReceiverWalletNumber)

	mockRepo.AssertExpectations(t)
	mockTxRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
}

func TestGetUserWallets(t *testing.T) {
	mockRepo := new(MockRepository)
	mockTxRepo := new(MockTransactionRepository)
	mockUserRepo := new(MockUserRepository)
	mockLogger := logger.NewNop()

	service := NewService(mockRepo, mockTxRepo, mockUserRepo, mockLogger)
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "1234567812345678"

	wallets := []*domain.Wallet{
		{
			ID:               uuid.New(),
			UserID:           userID,
			WalletAddress:    &walletAddr,
			Currency:         domain.USD,
			AvailableBalance: decimal.NewFromFloat(100.00),
		},
	}

	mockRepo.On("FindByUserID", ctx, userID).Return(wallets, nil)
	mockUserRepo.On("FindByID", ctx, userID).Return(&domain.User{
		ID:        userID,
		FirstName: "John",
		LastName:  "Doe",
	}, nil)

	responses, err := service.GetUserWallets(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, responses, 1)
	assert.Equal(t, "1234 5678 1234 5678", responses[0].FormattedWalletAddress)
	assert.Equal(t, domain.USD, responses[0].Currency)
	assert.Equal(t, "John Doe", responses[0].CardholderName)
	assert.NotEmpty(t, responses[0].ExpiryDate)
	assert.Equal(t, "Mastercard", responses[0].CardType)
}

func TestGetBalance(t *testing.T) {
	mockRepo := new(MockRepository)
	mockTxRepo := new(MockTransactionRepository)
	mockUserRepo := new(MockUserRepository)
	mockLogger := logger.NewNop()

	service := NewService(mockRepo, mockTxRepo, mockUserRepo, mockLogger)
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()
	walletAddr := "8765432187654321"

	wallet := &domain.Wallet{
		ID:               walletID,
		UserID:           userID,
		WalletAddress:    &walletAddr,
		Currency:         domain.USD,
		AvailableBalance: decimal.NewFromFloat(500.50),
		CreatedAt:        time.Now(),
	}

	mockRepo.On("FindByID", ctx, walletID).Return(wallet, nil)
	mockUserRepo.On("FindByID", ctx, userID).Return(&domain.User{
		ID:        userID,
		FirstName: "Jane",
		LastName:  "Smith",
	}, nil)

	response, err := service.GetBalance(ctx, walletID)

	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "8765 4321 8765 4321", response.FormattedWalletAddress)
	assert.Equal(t, domain.USD, response.Currency)
	assert.Equal(t, "Jane Smith", response.CardholderName)
	assert.NotEmpty(t, response.ExpiryDate)
	assert.Equal(t, "Mastercard", response.CardType)
}
