// Package auth implements authentication services (register/login and token issuance).
//
// Note: Implement getIntEnv and getDurationEnv similarly
//
// ==============================================================================
// AUTH SERVICE - internal/auth/service.go
// ==============================================================================
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"kyd/internal/domain"
	kyderrors "kyd/pkg/errors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
)

// Service provides user registration, login, and token issuance.
type Service struct {
	repo      Repository
	jwtSecret string
	jwtExpiry time.Duration
}

// NewService constructs a Service with the given repository and JWT settings.
func NewService(repo Repository, jwtSecret string, jwtExpiry time.Duration) *Service {
	return &Service{
		repo:      repo,
		jwtSecret: jwtSecret,
		jwtExpiry: jwtExpiry,
	}
}

// RegisterRequest captures the fields required to create a new user.
type RegisterRequest struct {
	Email       string          `json:"email" validate:"required,email"`
	Phone       string          `json:"phone" validate:"required,phone_by_country"`
	Password    string          `json:"password" validate:"required,min=8"`
	FirstName   string          `json:"first_name" validate:"required"`
	LastName    string          `json:"last_name" validate:"required"`
	UserType    domain.UserType `json:"user_type" validate:"required"`
	CountryCode string          `json:"country_code" validate:"required,len=2"`
}

// LoginRequest captures credentials for login.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// TokenResponse is returned on successful register/login with issued tokens.
type TokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresAt    time.Time    `json:"expires_at"`
	User         *domain.User `json:"user"`
}

// Register creates a new user and returns tokens.
func (s *Service) Register(ctx context.Context, req *RegisterRequest) (*TokenResponse, error) {
	// Check if user exists
	exists, err := s.repo.ExistsByEmail(ctx, req.Email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, kyderrors.ErrUserAlreadyExists
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user
	user := &domain.User{
		ID:           uuid.New(),
		Email:        req.Email,
		Phone:        req.Phone,
		PasswordHash: string(passwordHash),
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		UserType:     req.UserType,
		KYCLevel:     0,
		KYCStatus:    domain.KYCStatusPending,
		CountryCode:  req.CountryCode,
		RiskScore:    decimal.Zero,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		// Handle unique constraint violations (email/phone)
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, kyderrors.ErrUserAlreadyExists
		}
		return nil, err
	}

	// Generate tokens
	return s.generateTokens(user)
}

// Login authenticates a user and returns tokens.
func (s *Service) Login(ctx context.Context, req *LoginRequest) (*TokenResponse, error) {
	user, err := s.repo.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, kyderrors.ErrInvalidCredentials
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, kyderrors.ErrInvalidCredentials
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if err := s.repo.Update(ctx, user); err != nil {
		return nil, err
	}

	return s.generateTokens(user)
}

func (s *Service) generateTokens(user *domain.User) (*TokenResponse, error) {
	expiresAt := time.Now().Add(s.jwtExpiry)

	// Create access token
	claims := jwt.MapClaims{
		"user_id":   user.ID.String(),
		"email":     user.Email,
		"user_type": user.UserType,
		"exp":       expiresAt.Unix(),
		"iat":       time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	// Generate refresh token
	refreshToken, err := generateRandomToken(32)
	if err != nil {
		return nil, err
	}

	return &TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		User:         user,
	}, nil
}

func generateRandomToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// Repository interface
type Repository interface {
	Create(ctx context.Context, user *domain.User) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	Update(ctx context.Context, user *domain.User) error
}
