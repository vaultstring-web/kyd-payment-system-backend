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
	"strings"
	"time"
	"unicode"

	"kyd/internal/domain"
	kyderrors "kyd/pkg/errors"
	"kyd/pkg/mailer"
	"kyd/pkg/validator"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/pquerna/otp/totp"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
)

// TokenBlacklist defines the interface for managing revoked tokens.
type TokenBlacklist interface {
	Blacklist(ctx context.Context, token string, expiration time.Duration) error
	IsBlacklisted(ctx context.Context, token string) (bool, error)
}

// Service provides user registration, login, and token issuance.
type Service struct {
	repo                Repository
	blacklist           TokenBlacklist
	jwtSecret           string
	jwtSecrets          []string
	jwtExpiry           time.Duration
	mailer              *mailer.Mailer
	verificationBaseURL string
	verificationExpiry  time.Duration
	resetBaseURL        string
	resetExpiry         time.Duration
	bypassVerification  bool
	GoogleOAuth         *GoogleOAuthService // Google OAuth service
}

// NewService constructs a Service with the given repository and JWT settings.
func NewService(repo Repository, blacklist TokenBlacklist, jwtSecret string, jwtExpiry time.Duration) *Service {
	s := &Service{
		repo:        repo,
		blacklist:   blacklist,
		jwtSecret:   jwtSecret,
		jwtExpiry:   jwtExpiry,
		resetExpiry: 1 * time.Hour, // Default 1 hour
	}
	if jwtSecret != "" {
		s.jwtSecrets = []string{jwtSecret}
	}
	return s
}

func (s *Service) WithAdditionalJWTSecrets(secrets []string) *Service {
	seen := make(map[string]struct{})
	all := make([]string, 0, len(secrets)+1)
	if s.jwtSecret != "" {
		seen[s.jwtSecret] = struct{}{}
		all = append(all, s.jwtSecret)
	}
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		if _, ok := seen[sec]; ok {
			continue
		}
		seen[sec] = struct{}{}
		all = append(all, sec)
	}
	s.jwtSecrets = all
	return s
}

// WithEmailVerification configures mailer and verification options.
func (s *Service) WithEmailVerification(m *mailer.Mailer, baseURL string, expiry time.Duration, bypass bool) *Service {
	s.mailer = m
	s.verificationBaseURL = baseURL
	s.verificationExpiry = expiry
	s.bypassVerification = bypass
	return s
}

// WithPasswordReset configures password reset options.
func (s *Service) WithPasswordReset(baseURL string, expiry time.Duration) *Service {
	s.resetBaseURL = baseURL
	s.resetExpiry = expiry
	return s
}

// WithGoogleOAuth configures Google OAuth options.
func (s *Service) WithGoogleOAuth(googleOAuth *GoogleOAuthService) *Service {
	s.GoogleOAuth = googleOAuth
	return s
}

// RegisterRequest captures the fields required to create a new user.
type RegisterRequest struct {
	Email        string          `json:"email" validate:"required,email"`
	Phone        string          `json:"phone" validate:"required,phone_by_country"`
	Password     string          `json:"password" validate:"required,min=8"`
	FirstName    string          `json:"first_name" validate:"required"`
	LastName     string          `json:"last_name" validate:"required"`
	UserType     domain.UserType `json:"user_type" validate:"required"`
	CountryCode  string          `json:"country_code" validate:"required,len=2"`
	BusinessName string          `json:"business_name"`
}

// LoginRequest captures credentials for login.
type LoginRequest struct {
	Email       string `json:"email" validate:"required,email"`
	Password    string `json:"password" validate:"required"`
	TOTPCode    string `json:"totp_code"`
	DeviceID    string `json:"device_id"`   // Client-generated UUID or fingerprint
	DeviceName  string `json:"device_name"` // e.g. "iPhone 13" or "Chrome Windows"
	IPAddress   string `json:"ip_address"`
	CountryCode string `json:"country_code"`
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

	// Validate password complexity
	if err := validatePassword(req.Password); err != nil {
		return nil, fmt.Errorf("invalid password: %w", err)
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user
	user := &domain.User{
		ID:            uuid.New(),
		Email:         req.Email,
		Phone:         req.Phone,
		PasswordHash:  string(passwordHash),
		FirstName:     req.FirstName,
		LastName:      req.LastName,
		UserType:      req.UserType,
		KYCLevel:      0,
		KYCStatus:     domain.KYCStatusPending,
		CountryCode:   req.CountryCode,
		RiskScore:     decimal.Zero,
		IsActive:      true,
		EmailVerified: s.bypassVerification,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if req.BusinessName != "" {
		user.BusinessName = &req.BusinessName
	}

	if err := s.repo.Create(ctx, user); err != nil {
		// Handle unique constraint violations (email/phone)
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, kyderrors.ErrUserAlreadyExists
		}
		return nil, err
	}

	// Send email verification if configured
	if s.mailer != nil && s.verificationBaseURL != "" && !s.bypassVerification {
		_ = s.sendVerificationEmail(user)
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

	if !user.IsActive {
		return nil, kyderrors.ErrInvalidCredentials
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, kyderrors.ErrInvalidCredentials
	}

	// Verify TOTP if enabled
	if user.IsTOTPEnabled {
		if req.TOTPCode == "" {
			return nil, kyderrors.ErrTOTPRequired
		}
		if user.TOTPSecret == nil || !totp.Validate(req.TOTPCode, *user.TOTPSecret) {
			return nil, kyderrors.ErrInvalidTOTP
		}
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if err := s.repo.Update(ctx, user); err != nil {
		return nil, err
	}

	// Record Device
	if req.DeviceID != "" {
		device := &domain.UserDevice{
			UserID:      user.ID,
			DeviceHash:  req.DeviceID,
			DeviceName:  &req.DeviceName,
			IPAddress:   &req.IPAddress,
			CountryCode: &req.CountryCode,
			IsTrusted:   true, // Trust on successful login
			LastSeenAt:  now,
			CreatedAt:   now,
		}
		// Best effort device tracking
		_ = s.repo.AddDevice(ctx, device)
	}

	return s.generateTokens(user)
}

// Logout invalidates the user's token by adding it to the blacklist.
func (s *Service) Logout(ctx context.Context, tokenString string) error {
	// Parse token to get expiration
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if s.jwtSecret != "" {
			return []byte(s.jwtSecret), nil
		}
		if len(s.jwtSecrets) > 0 {
			return []byte(s.jwtSecrets[0]), nil
		}
		return nil, jwt.ErrSignatureInvalid
	})

	if err != nil {
		// If token is invalid, we can't get expiration, but we should still try to blacklist it using default expiry?
		// Actually, if it's invalid, it's already useless. But let's handle the case where we can read claims.
		// If we can't parse it, maybe we just return nil as it's not usable anyway.
		return nil
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil
	}

	expiration := time.Until(time.Unix(int64(exp), 0))
	if expiration < 0 {
		return nil // Already expired
	}

	return s.blacklist.Blacklist(ctx, tokenString, expiration)
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
	signingSecret := s.jwtSecret
	if signingSecret == "" && len(s.jwtSecrets) > 0 {
		signingSecret = s.jwtSecrets[0]
	}
	accessToken, err := token.SignedString([]byte(signingSecret))
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

// UpdateUser updates user details.
func (s *Service) UpdateUser(ctx context.Context, user *domain.User) error {
	return s.repo.Update(ctx, user)
}

// GetUserByID fetches a user by ID.
func (s *Service) GetUserByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.repo.FindByID(ctx, id)
}

// ListUsers returns a paginated list of users and total count.
func (s *Service) ListUsers(ctx context.Context, limit, offset int, userType string) ([]*domain.User, int, error) {
	users, err := s.repo.FindAll(ctx, limit, offset, userType)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountAll(ctx, userType)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// ListUsersAdmin returns a paginated list of users with admin filters.
func (s *Service) ListUsersAdmin(ctx context.Context, limit, offset int, userType string, kycStatus string) ([]*domain.User, int, error) {
	if userType == "" && kycStatus == "" {
		return s.ListUsers(ctx, limit, offset, "")
	}
	users, err := s.repo.FindAllWithFilters(ctx, limit, offset, userType, kycStatus)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountAllWithFilters(ctx, userType, kycStatus)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// ChangePassword updates a user's password hash after validating complexity.
func (s *Service) ChangePassword(ctx context.Context, user *domain.User, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return fmt.Errorf("invalid password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	user.PasswordHash = string(hash)
	now := time.Now()
	user.UpdatedAt = now
	return s.repo.Update(ctx, user)
}

func (s *Service) DeleteUser(ctx context.Context, id uuid.UUID) error {
	user, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	user.IsActive = false
	user.UserStatus = domain.UserStatusDeleted
	user.UpdatedAt = time.Now()
	return s.repo.Update(ctx, user)
}

// SanitizeUserInput trims and escapes user-modifiable fields to prevent XSS.
func SanitizeUserInput(u *domain.User) {
	u.FirstName = validator.Sanitize(u.FirstName)
	u.LastName = validator.Sanitize(u.LastName)
	u.Email = strings.TrimSpace(u.Email)
	u.Phone = strings.TrimSpace(u.Phone)
	if u.BusinessName != nil {
		b := validator.Sanitize(*u.BusinessName)
		u.BusinessName = &b
	}
	if u.BusinessRegistration != nil {
		br := validator.Sanitize(*u.BusinessRegistration)
		u.BusinessRegistration = &br
	}
	u.CountryCode = strings.ToUpper(strings.TrimSpace(u.CountryCode))
}

func (s *Service) sendVerificationEmail(user *domain.User) error {
	claims := jwt.MapClaims{
		"user_id": user.ID.String(),
		"email":   user.Email,
		"purpose": "email_verification",
		"exp":     time.Now().Add(s.verificationExpiry).Unix(),
		"iat":     time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signingSecret := s.jwtSecret
	if signingSecret == "" && len(s.jwtSecrets) > 0 {
		signingSecret = s.jwtSecrets[0]
	}
	signed, err := token.SignedString([]byte(signingSecret))
	if err != nil {
		return err
	}
	link := fmt.Sprintf("%s?token=%s", s.verificationBaseURL, signed)

	if s.bypassVerification {
		fmt.Printf("\n[DEV] EMAIL VERIFICATION LINK for %s: %s\n\n", user.Email, link)
		return nil
	}

	body := fmt.Sprintf(`<p>Hello %s,</p>
<p>Verify your email by clicking the link below:</p>
<p><a href="%s">%s</a></p>
<p>If you did not request this, please ignore.</p>`, user.FirstName, link, link)
	return s.mailer.Send(user.Email, "Verify your email", body)
}

func (s *Service) SendVerificationByEmail(ctx context.Context, email string) error {
	if s.mailer == nil || s.verificationBaseURL == "" || s.bypassVerification {
		return nil
	}
	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return err
	}
	return s.sendVerificationEmail(user)
}

// VerifyEmail decodes the verification token and marks the user's email as verified.
func (s *Service) VerifyEmail(ctx context.Context, tokenString string) error {
	var token *jwt.Token
	var err error

	// Try all configured secrets
	for _, secret := range s.jwtSecrets {
		token, err = jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err == nil && token.Valid {
			break
		}
	}

	if err != nil || token == nil || !token.Valid {
		return kyderrors.ErrInvalidCredentials
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return kyderrors.ErrInvalidCredentials
	}
	if purpose, _ := claims["purpose"].(string); purpose != "email_verification" {
		return kyderrors.ErrInvalidCredentials
	}
	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return kyderrors.ErrInvalidCredentials
	}
	id, err := uuid.Parse(userIDStr)
	if err != nil {
		return kyderrors.ErrInvalidCredentials
	}
	return s.repo.SetEmailVerified(ctx, id)
}

// DebugFindByEmail finds a user by email for debugging purposes.
func (s *Service) DebugFindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return s.repo.FindByEmail(ctx, email)
}

// RequestPasswordReset generates a reset token and sends a reset email.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	if s.mailer == nil || s.resetBaseURL == "" {
		return errors.New("password reset is not configured")
	}

	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		// Do not return error to prevent user enumeration
		return nil
	}

	// Generate a reset token
	claims := jwt.MapClaims{
		"user_id": user.ID.String(),
		"purpose": "password_reset",
		"exp":     time.Now().Add(s.resetExpiry).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return err
	}

	link := fmt.Sprintf("%s?token=%s", s.resetBaseURL, signed)

	if s.bypassVerification {
		fmt.Printf("\n[DEV] PASSWORD RESET LINK for %s: %s\n\n", user.Email, link)
		return nil
	}

	body := fmt.Sprintf(`<p>Hello %s,</p>
<p>You requested a password reset. Click the link below to set a new password:</p>
<p><a href="%s">%s</a></p>
<p>This link will expire in 1 hour. If you did not request this, please ignore this email.</p>`,
		user.FirstName, link, link)

	return s.mailer.Send(user.Email, "Reset your password", body)
}

// ResetPassword validates the reset token and updates the user's password.
func (s *Service) ResetPassword(ctx context.Context, tokenString, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	var token *jwt.Token
	var err error

	// Try all configured secrets
	for _, secret := range s.jwtSecrets {
		token, err = jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err == nil && token.Valid {
			break
		}
	}

	if err != nil || token == nil || !token.Valid {
		return kyderrors.ErrInvalidCredentials
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return kyderrors.ErrInvalidCredentials
	}

	if purpose, _ := claims["purpose"].(string); purpose != "password_reset" {
		return kyderrors.ErrInvalidCredentials
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return kyderrors.ErrInvalidCredentials
	}

	id, err := uuid.Parse(userIDStr)
	if err != nil {
		return kyderrors.ErrInvalidCredentials
	}

	user, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	// Update password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(passwordHash)
	user.UpdatedAt = time.Now()

	return s.repo.Update(ctx, user)
}

func generateRandomToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func validatePassword(password string) error {
	var (
		hasUpper   bool
		hasLower   bool
		hasNumber  bool
		hasSpecial bool
	)

	if len(password) < 8 {
		return errors.New("must be at least 8 characters long")
	}

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if !hasUpper {
		return errors.New("must contain at least one uppercase letter")
	}
	if !hasLower {
		return errors.New("must contain at least one lowercase letter")
	}
	if !hasNumber {
		return errors.New("must contain at least one number")
	}
	if !hasSpecial {
		return errors.New("must contain at least one special character")
	}

	return nil
}

// Repository interface
type Repository interface {
	Create(ctx context.Context, user *domain.User) error
	FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	Update(ctx context.Context, user *domain.User) error
	SetEmailVerified(ctx context.Context, id uuid.UUID) error
	AddDevice(ctx context.Context, device *domain.UserDevice) error
	IsCountryTrusted(ctx context.Context, userID uuid.UUID, countryCode string) (bool, error)
	FindAll(ctx context.Context, limit, offset int, userType string) ([]*domain.User, error)
	CountAll(ctx context.Context, userType string) (int, error)
	FindAllWithFilters(ctx context.Context, limit, offset int, userType string, kycStatus string) ([]*domain.User, error)
	CountAllWithFilters(ctx context.Context, userType string, kycStatus string) (int, error)
}
