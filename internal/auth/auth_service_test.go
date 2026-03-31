package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"kyd/internal/domain"
	kyderrors "kyd/pkg/errors"
	"kyd/pkg/mailer"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRepository is a mock implementation of the Repository interface.
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	args := m.Called(ctx, email)
	return args.Bool(0), args.Error(1)
}

func (m *MockRepository) Update(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockRepository) SetEmailVerified(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) AddDevice(ctx context.Context, device *domain.UserDevice) error {
	args := m.Called(ctx, device)
	return args.Error(0)
}

func (m *MockRepository) IsCountryTrusted(ctx context.Context, userID uuid.UUID, countryCode string) (bool, error) {
	args := m.Called(ctx, userID, countryCode)
	return args.Bool(0), args.Error(1)
}

func (m *MockRepository) FindAll(ctx context.Context, limit, offset int, userType string) ([]*domain.User, error) {
	args := m.Called(ctx, limit, offset, userType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockRepository) CountAll(ctx context.Context, userType string) (int, error) {
	args := m.Called(ctx, userType)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) FindAllWithFilters(ctx context.Context, limit, offset int, userType string, kycStatus string) ([]*domain.User, error) {
	args := m.Called(ctx, limit, offset, userType, kycStatus)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockRepository) CountAllWithFilters(ctx context.Context, userType string, kycStatus string) (int, error) {
	args := m.Called(ctx, userType, kycStatus)
	return args.Int(0), args.Error(1)
}

// MockSender is a mock implementation of the mailer.Sender interface.
type MockSender struct {
	mock.Mock
}

func (m *MockSender) Send(to, subject, body string) error {
	args := m.Called(to, subject, body)
	return args.Error(0)
}

func TestRequestPasswordReset(t *testing.T) {
	repo := new(MockRepository)
	service := NewService(repo, nil, "secret", time.Hour)

	// Create a mailer with our mock sender
	mockSender := new(MockSender)
	m := &mailer.Mailer{}
	m.WithSender(mockSender)

	service.WithEmailVerification(m, "http://verify", time.Hour, false)
	service.WithPasswordReset("http://reset", time.Hour)

	user := &domain.User{
		ID:        uuid.New(),
		Email:     "test@example.com",
		FirstName: "Test",
	}

	t.Run("Success", func(t *testing.T) {
		repo.On("FindByEmail", mock.Anything, "test@example.com").Return(user, nil).Once()
		mockSender.On("Send", "test@example.com", "Reset your password", mock.Anything).Return(nil).Once()

		err := service.RequestPasswordReset(context.Background(), "test@example.com")
		assert.NoError(t, err)
	})

	t.Run("UserNotFound", func(t *testing.T) {
		repo.On("FindByEmail", mock.Anything, "nonexistent@example.com").Return(nil, errors.New("not found")).Once()

		err := service.RequestPasswordReset(context.Background(), "nonexistent@example.com")
		assert.NoError(t, err) // Should return nil to prevent enumeration
	})
}

func TestResetPassword(t *testing.T) {
	repo := new(MockRepository)
	service := NewService(repo, nil, "secret", time.Hour)
	service.WithPasswordReset("http://reset", time.Hour)

	user := &domain.User{
		ID:    uuid.New(),
		Email: "test@example.com",
	}

	t.Run("Success", func(t *testing.T) {
		// Generate a valid token
		claims := jwt.MapClaims{
			"user_id": user.ID.String(),
			"purpose": "password_reset",
			"exp":     time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, _ := token.SignedString([]byte("secret"))

		repo.On("FindByID", mock.Anything, user.ID).Return(user, nil).Once()
		repo.On("Update", mock.Anything, mock.Anything).Return(nil).Once()

		err := service.ResetPassword(context.Background(), tokenString, "NewPassword123!")
		assert.NoError(t, err)
	})

	t.Run("InvalidToken", func(t *testing.T) {
		err := service.ResetPassword(context.Background(), "invalid-token", "NewPassword123!")
		assert.Error(t, err)
		assert.Equal(t, kyderrors.ErrInvalidCredentials, err)
	})

	t.Run("WeakPassword", func(t *testing.T) {
		err := service.ResetPassword(context.Background(), "some-token", "weak")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be at least 8 characters")
	})
}

func TestEmailVerificationBypass(t *testing.T) {
	repo := new(MockRepository)
	service := NewService(repo, nil, "secret", time.Hour)

	// Create a mailer with our mock sender
	mockSender := new(MockSender)
	m := &mailer.Mailer{}
	m.WithSender(mockSender)

	// Configure with bypass = true
	service.WithEmailVerification(m, "http://verify", time.Hour, true)
	service.WithPasswordReset("http://reset", time.Hour)

	t.Run("RegisterBypass", func(t *testing.T) {
		req := &RegisterRequest{
			Email:       "bypass@example.com",
			Phone:       "+265888123456",
			Password:    "Password123!",
			FirstName:   "Bypass",
			LastName:    "User",
			UserType:    domain.UserTypeIndividual,
			CountryCode: "MW",
		}

		repo.On("ExistsByEmail", mock.Anything, "bypass@example.com").Return(false, nil).Once()
		repo.On("Create", mock.Anything, mock.MatchedBy(func(u *domain.User) bool {
			return u.Email == "bypass@example.com" && u.EmailVerified == true
		})).Return(nil).Once()

		// mockSender.On("Send", ...) should NOT be called

		resp, err := service.Register(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.User.EmailVerified)
		mockSender.AssertNotCalled(t, "Send", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("RequestPasswordResetBypass", func(t *testing.T) {
		user := &domain.User{
			ID:        uuid.New(),
			Email:     "reset-bypass@example.com",
			FirstName: "Bypass",
		}
		repo.On("FindByEmail", mock.Anything, "reset-bypass@example.com").Return(user, nil).Once()

		// mockSender.On("Send", ...) should NOT be called

		err := service.RequestPasswordReset(context.Background(), "reset-bypass@example.com")
		assert.NoError(t, err)
		mockSender.AssertNotCalled(t, "Send", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestHandleGoogleSignIn(t *testing.T) {
	repo := new(MockRepository)
	service := NewService(repo, nil, "secret", time.Hour)
	googleService, _ := NewGoogleOAuthService(&GoogleOAuthConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}, service)

	userInfo := &GoogleUserInfo{
		ID:            "google-id",
		Email:         "google@example.com",
		VerifiedEmail: true,
		Name:          "Google User",
	}

	t.Run("ExistingUser", func(t *testing.T) {
		user := &domain.User{
			ID:    uuid.New(),
			Email: "google@example.com",
		}
		repo.On("FindByEmail", mock.Anything, "google@example.com").Return(user, nil).Once()

		resp, err := googleService.HandleGoogleSignIn(context.Background(), userInfo, nil)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, user.ID, resp.User.ID)
	})

	t.Run("NewUser", func(t *testing.T) {
		repo.On("FindByEmail", mock.Anything, "google@example.com").Return(nil, errors.New("not found")).Once()
		repo.On("Create", mock.Anything, mock.Anything).Return(nil).Once()

		resp, err := googleService.HandleGoogleSignIn(context.Background(), userInfo, nil)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "google@example.com", resp.User.Email)
		assert.Equal(t, "google", resp.User.AuthProvider)
	})
}
