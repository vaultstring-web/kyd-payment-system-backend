package monitoring

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// UserProfile tracks behavioral statistics
type UserProfile struct {
	AverageTxAmount   decimal.Decimal
	TxCount           int64
	LastTxTime        time.Time
	FrequentReceivers map[string]int
	LastLocation      string
}

// AnomalyType defines the category of suspicious behavior
type AnomalyType string

const (
	AnomalyHighVelocity    AnomalyType = "HIGH_VELOCITY"
	AnomalySuddenSpike     AnomalyType = "SUDDEN_SPIKE"
	AnomalyUnusualLocation AnomalyType = "UNUSUAL_LOCATION"
	AnomalyNewBeneficiary  AnomalyType = "NEW_BENEFICIARY"
)

type Anomaly struct {
	Type        AnomalyType
	Description string
	Severity    string // LOW, MEDIUM, HIGH
	Timestamp   time.Time
}

type BehavioralMonitor struct {
	profiles map[string]*UserProfile
	mu       sync.RWMutex
}

func NewBehavioralMonitor() *BehavioralMonitor {
	return &BehavioralMonitor{
		profiles: make(map[string]*UserProfile),
	}
}

// RecordTransaction updates the user's behavioral profile
func (bm *BehavioralMonitor) RecordTransaction(userID uuid.UUID, amount decimal.Decimal, receiverID string, location string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	uid := userID.String()
	profile, exists := bm.profiles[uid]
	if !exists {
		profile = &UserProfile{
			AverageTxAmount:   decimal.Zero,
			TxCount:           0,
			FrequentReceivers: make(map[string]int),
		}
		bm.profiles[uid] = profile
	}

	// Update Average (Moving Average)
	// NewAvg = OldAvg + (NewVal - OldAvg) / NewCount
	profile.TxCount++
	countDec := decimal.NewFromInt(profile.TxCount)
	profile.AverageTxAmount = profile.AverageTxAmount.Add(
		amount.Sub(profile.AverageTxAmount).Div(countDec),
	)

	profile.LastTxTime = time.Now()
	profile.FrequentReceivers[receiverID]++
	profile.LastLocation = location
}

// DetectAnomalies checks current transaction against historical profile
func (bm *BehavioralMonitor) DetectAnomalies(userID uuid.UUID, amount decimal.Decimal, receiverID string) ([]Anomaly, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	var anomalies []Anomaly
	uid := userID.String()
	profile, exists := bm.profiles[uid]

	if !exists || profile.TxCount < 5 {
		// Not enough history to detect anomalies
		return anomalies, nil
	}

	// Check 1: Sudden Spike (Amount > 5x Average)
	if amount.GreaterThan(profile.AverageTxAmount.Mul(decimal.NewFromInt(5))) {
		anomalies = append(anomalies, Anomaly{
			Type:        AnomalySuddenSpike,
			Description: fmt.Sprintf("Transaction amount %s is > 5x average %s", amount.String(), profile.AverageTxAmount.String()),
			Severity:    "HIGH",
			Timestamp:   time.Now(),
		})
	}

	// Check 2: New Beneficiary for high value
	if _, known := profile.FrequentReceivers[receiverID]; !known && amount.GreaterThan(decimal.NewFromInt(1000)) {
		anomalies = append(anomalies, Anomaly{
			Type:        AnomalyNewBeneficiary,
			Description: "High value transfer to new beneficiary",
			Severity:    "MEDIUM",
			Timestamp:   time.Now(),
		})
	}

	// Check 3: Velocity (Time since last tx < 1 minute)
	if time.Since(profile.LastTxTime) < 1*time.Minute {
		anomalies = append(anomalies, Anomaly{
			Type:        AnomalyHighVelocity,
			Description: "Rapid transaction frequency detected",
			Severity:    "MEDIUM",
			Timestamp:   time.Now(),
		})
	}

	return anomalies, nil
}
