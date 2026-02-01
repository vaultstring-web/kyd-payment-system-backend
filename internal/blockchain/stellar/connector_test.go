package stellar

import (
	"context"
	"testing"
	"time"

	"kyd/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestStellarConnector(t *testing.T) {
	// Initialize Connector
	connector, err := NewConnector("https://horizon-testnet.stellar.org", "S...", true)
	assert.NoError(t, err)
	assert.NotNil(t, connector)
	assert.NotNil(t, connector.Simulator)

	// Create a dummy settlement
	settlementID := uuid.New()
	settlement := &domain.Settlement{
		ID:          settlementID,
		TotalAmount: decimal.NewFromFloat(100.50),
		Currency:    domain.USD,
		Status:      domain.SettlementStatusPending,
		CreatedAt:   time.Now(),
	}

	ctx := context.Background()

	// Test SubmitSettlement
	result, err := connector.SubmitSettlement(ctx, settlement)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Confirmed)
	assert.NotEmpty(t, result.TxHash)

	// Test CheckConfirmation
	confirmed, err := connector.CheckConfirmation(ctx, result.TxHash)
	assert.NoError(t, err)
	assert.True(t, confirmed)
}
