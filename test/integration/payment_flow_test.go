// ==============================================================================
// INTEGRATION TESTS - test/integration/payment_flow_test.go
// ==============================================================================
// +build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kyd/internal/auth"
	"kyd/internal/handler"
	"kyd/internal/repository/postgres"
	"kyd/pkg/logger"
	"kyd/pkg/validator"
)

func setupTestDB(t *testing.T) *sqlx.DB {
	db, err := sqlx.Connect("postgres", "postgres://kyd_user:kyd_password@localhost:5432/kyd_test?sslmode=disable")
	require.NoError(t, err)

	// Clean database
	db.MustExec("TRUNCATE users, wallets, transactions CASCADE")

	return db
}

func TestCompletePaymentFlow(t *testing.T) {
	// Setup
	db := setupTestDB(t)
	defer db.Close()

	log := logger.NewNop()
	userRepo := postgres.NewUserRepository(db)
	authService := auth.NewService(userRepo, "test-secret", 15*time.Minute)
	val := validator.New()
	authHandler := handler.NewAuthHandler(authService, val, log)

	// Step 1: Register user
	t.Run("Register User", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"email":        "test@example.com",
			"password":     "SecurePass123",
			"first_name":   "John",
			"last_name":    "Doe",
			"phone":        "+265991234567",
			"country_code": "MW",
			"user_type":    "individual",
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		authHandler.Register(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response auth.TokenResponse
		json.Unmarshal(w.Body.Bytes(), &response)

		assert.NotEmpty(t, response.AccessToken)
		assert.NotNil(t, response.User)
		assert.Equal(t, "test@example.com", response.User.Email)
	})

	// Step 2: Login
	t.Run("Login User", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"email":    "test@example.com",
			"password": "SecurePass123",
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		authHandler.Login(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	// TODO: Add more integration tests
	// - Create wallet
	// - Fund wallet
	// - Initiate payment
	// - Check transaction status
}
