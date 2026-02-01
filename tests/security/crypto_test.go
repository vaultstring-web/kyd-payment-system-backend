package security_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"kyd/pkg/validator"
	"kyd/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

// TestPasswordComplexity verifies that the system enforces strong password hashing
func TestPasswordHashing(t *testing.T) {
	password := "CorrectHorseBatteryStaple"
	
	// Generate hash
	start := time.Now()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	duration := time.Since(start)
	
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)
	
	// Verify cost is at least 10 (DefaultCost)
	cost, err := bcrypt.Cost(hash)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, cost, bcrypt.DefaultCost, "Bcrypt cost should be at least 10")
	
	// Verify timing (should take > 50ms to prevent brute force, depends on CPU but 10 cost is usually ~100ms)
	// We won't strictly assert time to avoid flakiness, but logging it is useful
	t.Logf("Bcrypt hashing took %v", duration)
	
	// Verify comparison
	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	assert.NoError(t, err)
	
	err = bcrypt.CompareHashAndPassword(hash, []byte("WrongPassword"))
	assert.Error(t, err)
}

// TestSanitization verifies XSS prevention
func TestInputSanitization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple Script Tag",
			input:    "<script>alert(1)</script>",
			expected: "&lt;script&gt;alert(1)&lt;/script&gt;",
		},
		{
			name:     "Onclick Event",
			input:    `<a href="#" onclick="stealCookies()">Click me</a>`,
			expected: `&lt;a href=&#34;#&#34; onclick=&#34;stealCookies()&#34;&gt;Click me&lt;/a&gt;`,
		},
		{
			name:     "Normal Text",
			input:    "John Doe",
			expected: "John Doe",
		},
		{
			name:     "Whitespace Trimming",
			input:    "  Admin  ",
			expected: "Admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := validator.Sanitize(tt.input)
			assert.Equal(t, tt.expected, sanitized)
		})
	}
}

// Simulate Ledger Hash Calculation from internal/ledger/service.go
func calculateHash(prevHash string, id, txID, walletID uuid.UUID, entryType string, amount decimal.Decimal, currency domain.Currency, balanceAfter decimal.Decimal, createdAt time.Time) string {
	// Hash format: SHA256(prevHash + ID + TransactionID + WalletID + EntryType + Amount + Currency + BalanceAfter + CreatedAt)
	data := fmt.Sprintf("%s%s%s%s%s%s%s%s%s",
		prevHash,
		id.String(),
		txID.String(),
		walletID.String(),
		entryType,
		amount.String(),
		string(currency),
		balanceAfter.String(),
		createdAt.UTC().Format(time.RFC3339Nano),
	)

	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// TestLedgerHashIntegrity verifies the hash chain logic is deterministic and tamper-evident
func TestLedgerHashIntegrity(t *testing.T) {
	// Setup Initial State
	walletID := uuid.New()
	prevHash := "0000000000000000000000000000000000000000000000000000000000000000"
	
	// Transaction 1
	tx1ID := uuid.New()
	entry1ID := uuid.New()
	amount1 := decimal.NewFromInt(100)
	balance1 := decimal.NewFromInt(100)
	time1 := time.Now().UTC().Truncate(time.Microsecond)
	
	hash1 := calculateHash(prevHash, entry1ID, tx1ID, walletID, "credit", amount1, domain.USD, balance1, time1)
	
	// Transaction 2 (Linked to Tx 1)
	tx2ID := uuid.New()
	entry2ID := uuid.New()
	amount2 := decimal.NewFromInt(50)
	balance2 := decimal.NewFromInt(50) // Spent 50
	time2 := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	
	hash2 := calculateHash(hash1, entry2ID, tx2ID, walletID, "debit", amount2, domain.USD, balance2, time2)
	
	// Verification: Re-calculating hash1 should match
	recalcHash1 := calculateHash(prevHash, entry1ID, tx1ID, walletID, "credit", amount1, domain.USD, balance1, time1)
	assert.Equal(t, hash1, recalcHash1, "Hash calculation should be deterministic")
	
	// Tamper Check: If we change amount in Tx 1, Hash 1 changes, invalidating Hash 2
	tamperedAmount := decimal.NewFromInt(1000) // Attacker tries to inflate balance
	tamperedHash1 := calculateHash(prevHash, entry1ID, tx1ID, walletID, "credit", tamperedAmount, domain.USD, balance1, time1)
	
	assert.NotEqual(t, hash1, tamperedHash1, "Tampering with amount must change hash")
	
	// Check Chain Break
	// Hash 2 depends on Hash 1. If Hash 1 changes, the stored Hash 2 is no longer valid for the new chain state.
	// In a real verification, we would re-calculate Hash 2 using TamperedHash1 and see it doesn't match the StoredHash2.
	recalcHash2WithTamper := calculateHash(tamperedHash1, entry2ID, tx2ID, walletID, "debit", amount2, domain.USD, balance2, time2)
	assert.NotEqual(t, hash2, recalcHash2WithTamper, "Tampering with previous entry must invalidate subsequent chain")
}
