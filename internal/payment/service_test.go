package payment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"kyd/internal/domain"
	"kyd/internal/ledger"
	"kyd/internal/notification"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, tx *domain.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *MockRepository) Update(ctx context.Context, tx *domain.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *MockRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transaction), args.Error(1)
}

func (m *MockRepository) FindByReference(ctx context.Context, ref string) (*domain.Transaction, error) {
	args := m.Called(ctx, ref)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transaction), args.Error(1)
}

func (m *MockRepository) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.Transaction, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

func (m *MockRepository) CountByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) GetDailyTotal(ctx context.Context, userID uuid.UUID, currency domain.Currency) (decimal.Decimal, error) {
	args := m.Called(ctx, userID, currency)
	return args.Get(0).(decimal.Decimal), args.Error(1)
}

func (m *MockRepository) GetHourlyHighValueCount(ctx context.Context, userID uuid.UUID, threshold decimal.Decimal) (int, error) {
	args := m.Called(ctx, userID, threshold)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) GetHourlyCount(ctx context.Context, userID uuid.UUID) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) FindByStatus(ctx context.Context, status domain.TransactionStatus, limit, offset int) ([]*domain.Transaction, error) {
	args := m.Called(ctx, status, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

func (m *MockRepository) CountByStatus(ctx context.Context, status domain.TransactionStatus) (int, error) {
	args := m.Called(ctx, status)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) SumVolume(ctx context.Context) (decimal.Decimal, error) {
	args := m.Called(ctx)
	return args.Get(0).(decimal.Decimal), args.Error(1)
}

func (m *MockRepository) SumEarnings(ctx context.Context) (decimal.Decimal, error) {
	args := m.Called(ctx)
	return args.Get(0).(decimal.Decimal), args.Error(1)
}

func (m *MockRepository) CountAll(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) FindFlagged(ctx context.Context, limit, offset int) ([]*domain.Transaction, error) {
	args := m.Called(ctx, limit, offset)
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

func (m *MockWalletRepository) FindByAddress(ctx context.Context, address string) (*domain.Wallet, error) {
	args := m.Called(ctx, address)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
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

func (m *MockLedgerService) PostTransaction(ctx context.Context, posting *ledger.LedgerPosting) error {
	args := m.Called(ctx, posting)
	return args.Error(0)
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

func (m *MockUserRepository) IsCountryTrusted(ctx context.Context, userID uuid.UUID, countryCode string) (bool, error) {
	args := m.Called(ctx, userID, countryCode)
	return args.Bool(0), args.Error(1)
}

func (m *MockUserRepository) IsDeviceTrusted(ctx context.Context, userID uuid.UUID, deviceID string) (bool, error) {
	args := m.Called(ctx, userID, deviceID)
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

type MockNotificationService struct {
	mock.Mock
}

func (m *MockNotificationService) Notify(ctx context.Context, userID uuid.UUID, eventType string, data map[string]interface{}) error {
	args := m.Called(ctx, userID, eventType, data)
	return args.Error(0)
}

func (m *MockNotificationService) SendRaw(ctx context.Context, n *notification.Notification) error {
	args := m.Called(ctx, n)
	return args.Error(0)
}

type MockAuditRepository struct {
	mock.Mock
}

func (m *MockAuditRepository) Create(ctx context.Context, log *domain.AuditLog) error {
	args := m.Called(ctx, log)
	return args.Error(0)
}

func (m *MockAuditRepository) FindByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AuditLog), args.Error(1)
}

func (m *MockAuditRepository) FindAll(ctx context.Context, limit, offset int) ([]*domain.AuditLog, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AuditLog), args.Error(1)
}

func (m *MockAuditRepository) CountAll(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

type MockSecurityRepository struct {
	mock.Mock
}

func (m *MockSecurityRepository) IsBlacklisted(ctx context.Context, identifier string) (bool, error) {
	args := m.Called(ctx, identifier)
	return args.Bool(0), args.Error(1)
}

func (m *MockSecurityRepository) LogSecurityEvent(ctx context.Context, event *domain.SecurityEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

// --- Tests ---

func TestInitiatePayment_FeeCalculation(t *testing.T) {
	mockRepo := new(MockRepository)
	mockWalletRepo := new(MockWalletRepository)
	mockForex := new(MockForexService)
	mockLedger := new(MockLedgerService)
	mockUserRepo := new(MockUserRepository)
	mockLog := new(MockLogger)
	mockNotifier := new(MockNotificationService)
	mockAuditRepo := new(MockAuditRepository)
	mockSecurityRepo := new(MockSecurityRepository)

	service := NewService(mockRepo, mockWalletRepo, mockForex, mockLedger, mockUserRepo, mockNotifier, mockAuditRepo, mockSecurityRepo, mockLog, nil)

	ctx := context.Background()
	senderID := uuid.New()
	receiverID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()

	amount := decimal.NewFromInt(1000)
	currency := domain.MWK

	req := &InitiatePaymentRequest{
		SenderID:              senderID,
		ReceiverID:            receiverID,
		ReceiverWalletAddress: "1234567890123456",
		Amount:                amount,
		Currency:              currency,
		DestinationCurrency:   currency,
		Description:           "Test Payment",
	}

	// Mock Sender User (KYC Verified)
	senderUser := &domain.User{
		ID:        senderID,
		KYCStatus: domain.KYCStatusVerified,
		KYCLevel:  3,
	}
	mockUserRepo.On("FindByID", ctx, senderID).Return(senderUser, nil)

	// Mock Sender Wallet
	senderWallet := &domain.Wallet{
		ID:               senderWalletID,
		UserID:           senderID,
		Currency:         currency,
		AvailableBalance: decimal.NewFromInt(2000), // Enough for amount + fee
		Status:           domain.WalletStatusActive,
	}
	mockWalletRepo.On("FindByUserAndCurrency", ctx, senderID, currency).Return(senderWallet, nil)

	// Mock Receiver Wallet
	receiverWallet := &domain.Wallet{
		ID:       receiverWalletID,
		UserID:   receiverID,
		Currency: currency,
		Status:   domain.WalletStatusActive,
	}
	mockWalletRepo.On("FindByAddress", ctx, "1234567890123456").Return(receiverWallet, nil)

	// Mock Exchange Rate (Same currency)
	rate := &domain.ExchangeRate{
		Rate:     decimal.NewFromInt(1),
		SellRate: decimal.NewFromInt(1),
	}
	mockForex.On("GetRate", ctx, currency, currency).Return(rate, nil)

	// Mock Daily Limit Check
	mockRepo.On("GetDailyTotal", ctx, senderID, currency).Return(decimal.Zero, nil)
	// Mock Hourly Limit Check
	mockRepo.On("GetHourlyCount", ctx, senderID).Return(0, nil)

	// Mock Transaction Creation
	// We verify here that FeeAmount is correctly set
	mockRepo.On("Create", ctx, mock.MatchedBy(func(tx *domain.Transaction) bool {
		expectedFee := amount.Mul(decimal.NewFromFloat(0.015))
		if !tx.FeeAmount.Equal(expectedFee) {
			fmt.Printf("Fee mismatch: expected %s, got %s\n", expectedFee, tx.FeeAmount)
			return false
		}
		if !tx.Amount.Equal(amount) {
			fmt.Printf("Amount mismatch: expected %s, got %s\n", amount, tx.Amount)
			return false
		}
		return true
	})).Return(nil)

	// Mock Ledger Posting
	mockLedger.On("PostTransaction", ctx, mock.Anything).Return(nil)

	// Mock Transaction Update (Completion)
	mockRepo.On("Update", ctx, mock.Anything).Return(nil)

	// Mock Logger
	mockLog.On("Info", mock.Anything, mock.Anything).Return()
	mockLog.On("Warn", mock.Anything, mock.Anything).Return()

	// Mock Notifications
	mockNotifier.On("Notify", mock.Anything, senderID, "PAYMENT_SENT", mock.Anything).Return(nil)
	mockNotifier.On("Notify", mock.Anything, receiverID, "PAYMENT_RECEIVED", mock.Anything).Return(nil)

	// Mock Security
	mockSecurityRepo.On("IsBlacklisted", mock.Anything, mock.Anything).Return(false, nil)
	mockSecurityRepo.On("LogSecurityEvent", mock.Anything, mock.Anything).Return(nil)

	// Execute
	resp, err := service.InitiatePayment(ctx, req)

	// Assertions
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	// Verify total debited from sender wallet logic (indirectly via ledger or logic check)
	// The service logic calculates totalDebit = Amount + Fee.
	// We can check if `PostTransaction` was called with correct debit amount if we want,
	// but `mockRepo.Create` verification above confirms the FeeAmount field on the transaction object.
}

func TestGetReceipt_Success(t *testing.T) {
	mockRepo := new(MockRepository)
	mockWalletRepo := new(MockWalletRepository)
	mockForex := new(MockForexService)
	mockLedger := new(MockLedgerService)
	mockUserRepo := new(MockUserRepository)
	mockLog := new(MockLogger)
	mockNotifier := new(MockNotificationService)
	mockAuditRepo := new(MockAuditRepository)

	service := NewService(mockRepo, mockWalletRepo, mockForex, mockLedger, mockUserRepo, mockNotifier, mockAuditRepo, mockLog, nil)

	ctx := context.Background()
	txID := uuid.New()
	senderID := uuid.New()
	receiverID := uuid.New()

	desc := "Payment Ref 123"
	tx := &domain.Transaction{
		ID:          txID,
		SenderID:    senderID,
		ReceiverID:  receiverID,
		Amount:      decimal.NewFromInt(1000),
		FeeAmount:   decimal.NewFromFloat(15),
		Currency:    domain.MWK,
		Status:      domain.TransactionStatusCompleted,
		CreatedAt:   time.Now(),
		Reference:   "REF123",
		Description: &desc,
	}

	sender := &domain.User{FirstName: "John", LastName: "Doe"}
	receiver := &domain.User{FirstName: "Jane", LastName: "Smith"}

	mockRepo.On("FindByID", ctx, txID).Return(tx, nil)
	mockUserRepo.On("FindByID", ctx, senderID).Return(sender, nil)
	mockUserRepo.On("FindByID", ctx, receiverID).Return(receiver, nil)

	receipt, err := service.GetReceipt(ctx, txID, senderID)

	assert.NoError(t, err)
	assert.Equal(t, "John Doe", receipt.SenderName)
	assert.Equal(t, "Jane Smith", receipt.ReceiverName)
	assert.Equal(t, "15", receipt.Fee.String())
	assert.Equal(t, "1015", receipt.TotalDebited.String())
}
