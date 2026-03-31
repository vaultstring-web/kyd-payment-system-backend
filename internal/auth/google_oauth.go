// Package auth implements authentication services including Google OAuth integration.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

// GoogleOAuthConfig holds configuration for Google OAuth integration
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	TokenIssuer  string
	MockMode     bool
}

// GoogleUserInfo represents the user information returned by Google OAuth
type GoogleUserInfo struct {
	ID            string `json:"sub"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"email_verified"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	Locale        string `json:"locale"`
}

// GoogleOAuthService handles Google OAuth authentication
type GoogleOAuthService struct {
	config      *GoogleOAuthConfig
	oauthConfig *oauth2.Config
	authService *Service
}

// NewGoogleOAuthService creates a new Google OAuth service
func NewGoogleOAuthService(config *GoogleOAuthConfig, authService *Service) (*GoogleOAuthService, error) {
	if !config.MockMode && (config.ClientID == "" || config.ClientSecret == "") {
		return nil, fmt.Errorf("Google OAuth client ID and secret are required")
	}

	var oauthConfig *oauth2.Config
	if !config.MockMode {
		oauthConfig = &oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  config.RedirectURI,
			Scopes: []string{
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
			},
			Endpoint: google.Endpoint,
		}
	}

	return &GoogleOAuthService{
		config:      config,
		oauthConfig: oauthConfig,
		authService: authService,
	}, nil
}

// GetAuthURL returns the URL to redirect users to for Google OAuth authentication
func (s *GoogleOAuthService) GetAuthURL(state string) string {
	if s.config.MockMode {
		// In mock mode, we redirect to a local mock login page/endpoint
		// Use Gateway URL if available, otherwise assume localhost:9000
		baseURL := os.Getenv("GATEWAY_URL")
		if baseURL == "" {
			baseURL = "http://localhost:9000"
		}
		return fmt.Sprintf("%s/api/v1/auth/google/mock-login?state=%s", baseURL, state)
	}
	return s.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// ExchangeCode exchanges an authorization code for tokens and returns user info
func (s *GoogleOAuthService) ExchangeCode(ctx context.Context, code string) (*GoogleUserInfo, *oauth2.Token, error) {
	if s.config.MockMode && code == "mock-code" {
		mockToken := &oauth2.Token{
			AccessToken:  "mock-access-token",
			RefreshToken: "mock-refresh-token",
			Expiry:       time.Now().Add(1 * time.Hour),
		}
		userInfo := &GoogleUserInfo{
			ID:            "mock-google-id",
			Email:         "mock.user@example.com",
			VerifiedEmail: true,
			Name:          "Mock Google User",
			GivenName:     "Mock",
			FamilyName:    "User",
			Picture:       "https://ui-avatars.com/api/?name=Mock+User",
			Locale:        "en",
		}
		return userInfo, mockToken, nil
	}
	token, err := s.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	userInfo, err := s.GetUserInfo(ctx, token)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user info: %w", err)
	}

	return userInfo, token, nil
}

// GetUserInfo retrieves user information using the access token
func (s *GoogleOAuthService) GetUserInfo(ctx context.Context, token *oauth2.Token) (*GoogleUserInfo, error) {
	client := s.oauthConfig.Client(ctx, token)

	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google API returned status: %s", resp.Status)
	}

	var userInfo GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	return &userInfo, nil
}

// ValidateIDToken validates a Google ID token and returns user information
func (s *GoogleOAuthService) ValidateIDToken(ctx context.Context, idToken string) (*GoogleUserInfo, error) {
	if s.config.MockMode && idToken == "mock-id-token" {
		return &GoogleUserInfo{
			ID:            "mock-google-id",
			Email:         "mock.user@example.com",
			VerifiedEmail: true,
			Name:          "Mock Google User",
			GivenName:     "Mock",
			FamilyName:    "User",
			Picture:       "https://ui-avatars.com/api/?name=Mock+User",
			Locale:        "en",
		}, nil
	}
	payload, err := idtoken.Validate(ctx, idToken, s.config.ClientID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate ID token: %w", err)
	}

	if payload.Issuer != s.config.TokenIssuer && payload.Issuer != "accounts.google.com" {
		return nil, fmt.Errorf("invalid token issuer: %s", payload.Issuer)
	}

	// Extract user information from claims
	userInfo := &GoogleUserInfo{
		ID:            payload.Subject,
		Email:         payload.Claims["email"].(string),
		VerifiedEmail: payload.Claims["email_verified"].(bool),
		Name:          payload.Claims["name"].(string),
		GivenName:     payload.Claims["given_name"].(string),
		FamilyName:    payload.Claims["family_name"].(string),
		Picture:       payload.Claims["picture"].(string),
		Locale:        payload.Claims["locale"].(string),
	}

	return userInfo, nil
}

// HandleGoogleSignIn handles Google OAuth sign-in/sign-up
func (s *GoogleOAuthService) HandleGoogleSignIn(ctx context.Context, userInfo *GoogleUserInfo, googleToken *oauth2.Token) (*TokenResponse, error) {
	// Check if user already exists by email
	existingUser, err := s.authService.repo.FindByEmail(ctx, userInfo.Email)
	if err == nil && existingUser != nil {
		// User exists, log them in
		return s.authService.generateTokens(existingUser)
	}

	// User doesn't exist, create new account
	names := strings.SplitN(userInfo.Name, " ", 2)
	var firstName, lastName string
	if len(names) > 0 {
		firstName = names[0]
	}
	if len(names) > 1 {
		lastName = names[1]
	}

	var accessToken, refreshToken string
	if googleToken != nil {
		accessToken = googleToken.AccessToken
		refreshToken = googleToken.RefreshToken
	}

	// Generate a random password for Google-authenticated users
	randomPassword, err := generateRandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random password: %w", err)
	}

	// Create user with Google authentication
	user := &domain.User{
		ID:                   uuid.New(),
		Email:                userInfo.Email,
		PasswordHash:         "", // No password for Google-authenticated users
		FirstName:            firstName,
		LastName:             lastName,
		UserType:             domain.UserTypeIndividual,
		KYCLevel:             0,
		KYCStatus:            domain.KYCStatusPending,
		CountryCode:          "US", // Default, can be updated later
		RiskScore:            decimal.Zero,
		IsActive:             true,
		EmailVerified:        userInfo.VerifiedEmail,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
		AuthProvider:         "google",
		ProviderID:           &userInfo.ID,
		ProfilePictureURL:    userInfo.Picture,
		ProviderAccessToken:  accessToken,
		ProviderRefreshToken: refreshToken,
	}

	// Hash the random password
	if randomPassword != "" {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}
		user.PasswordHash = string(passwordHash)
	}

	if err := s.authService.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return s.authService.generateTokens(user)
}

// IsGoogleUser checks if a user is authenticated via Google
func (s *GoogleOAuthService) IsGoogleUser(user *domain.User) bool {
	return user.AuthProvider == "google" && user.ProviderID != nil
}

// UpdateGoogleUserInfo updates user information from Google OAuth
func (s *GoogleOAuthService) UpdateGoogleUserInfo(ctx context.Context, user *domain.User, userInfo *GoogleUserInfo) error {
	user.Email = userInfo.Email
	user.EmailVerified = userInfo.VerifiedEmail
	user.ProfilePictureURL = userInfo.Picture

	names := strings.SplitN(userInfo.Name, " ", 2)
	if len(names) > 0 {
		user.FirstName = names[0]
	}
	if len(names) > 1 {
		user.LastName = names[1]
	}

	user.UpdatedAt = time.Now()

	return s.authService.repo.Update(ctx, user)
}
