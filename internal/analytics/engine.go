// Package analytics provides internal AI/ML algorithms for fraud detection,
// risk scoring, anomaly detection, and business intelligence.
// No third-party AI services - pure algorithmic implementations.
package analytics

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// AnalyticsEngine provides AI-powered analytics using internal algorithms.
type AnalyticsEngine struct {
	mu              sync.RWMutex
	transactionData []TransactionFeatures
	userProfiles    map[string]*UserProfile
	fraudModel      *FraudDetectionModel
	riskModel       *RiskScoringModel
	anomalyModel    *AnomalyDetectionModel
}

// TransactionFeatures represents extracted features from a transaction.
type TransactionFeatures struct {
	ID              string
	UserID          string
	Amount          float64
	Currency        string
	TransactionType string
	Timestamp       time.Time
	Hour            int
	DayOfWeek       int
	IsWeekend       bool
	CountryCode     string
	DeviceHash      string
	IPHash          string
	RecipientHash   string
	IsNewRecipient  bool
	TimeSinceLast   float64
	AmountDeviation float64
}

// UserProfile tracks behavioral patterns for each user.
type UserProfile struct {
	UserID               string
	AvgTransactionAmount float64
	StdDevAmount         float64
	TypicalHours         []int
	TypicalDays          []int
	FrequentRecipients   map[string]int
	TransactionCount     int
	TotalVolume          float64
	LastTransaction      time.Time
	RiskScore            float64
	AccountAge           time.Duration
}

// NewAnalyticsEngine creates a new analytics engine.
func NewAnalyticsEngine() *AnalyticsEngine {
	return &AnalyticsEngine{
		userProfiles: make(map[string]*UserProfile),
		fraudModel:   NewFraudDetectionModel(),
		riskModel:    NewRiskScoringModel(),
		anomalyModel: NewAnomalyDetectionModel(),
	}
}

// FraudModel returns the fraud detection model.
func (e *AnalyticsEngine) FraudModel() *FraudDetectionModel {
	return e.fraudModel
}

// RiskModel returns the risk scoring model.
func (e *AnalyticsEngine) RiskModel() *RiskScoringModel {
	return e.riskModel
}

// AnomalyModel returns the anomaly detection model.
func (e *AnalyticsEngine) AnomalyModel() *AnomalyDetectionModel {
	return e.anomalyModel
}

// ==============================================================================
// FRAUD DETECTION MODEL
// Uses statistical analysis and rule-based scoring
// ==============================================================================

// FraudDetectionModel implements fraud detection without external AI.
type FraudDetectionModel struct {
	weights    FraudWeights
	thresholds FraudThresholds
}

// FraudWeights for different fraud indicators.
type FraudWeights struct {
	AmountAnomaly      float64
	VelocityAnomaly    float64
	TimeAnomaly        float64
	RecipientAnomaly   float64
	LocationAnomaly    float64
	DeviceAnomaly      float64
	PatternAnomaly     float64
}

// FraudThresholds for fraud classification.
type FraudThresholds struct {
	HighRisk   float64
	MediumRisk float64
	LowRisk    float64
}

// FraudScore represents the fraud analysis result.
type FraudScore struct {
	Score           float64            `json:"score"`
	RiskLevel       string             `json:"risk_level"`
	Factors         map[string]float64 `json:"factors"`
	Recommendation  string             `json:"recommendation"`
	RequiresReview  bool               `json:"requires_review"`
	Confidence      float64            `json:"confidence"`
}

// NewFraudDetectionModel creates the fraud detection model.
func NewFraudDetectionModel() *FraudDetectionModel {
	return &FraudDetectionModel{
		weights: FraudWeights{
			AmountAnomaly:    0.25,
			VelocityAnomaly:  0.20,
			TimeAnomaly:      0.15,
			RecipientAnomaly: 0.15,
			LocationAnomaly:  0.10,
			DeviceAnomaly:    0.10,
			PatternAnomaly:   0.05,
		},
		thresholds: FraudThresholds{
			HighRisk:   0.75,
			MediumRisk: 0.50,
			LowRisk:    0.25,
		},
	}
}

// Analyze performs fraud detection on a transaction.
func (m *FraudDetectionModel) Analyze(tx TransactionFeatures, profile *UserProfile) FraudScore {
	factors := make(map[string]float64)

	// Factor 1: Amount Anomaly (z-score based)
	factors["amount_anomaly"] = m.calculateAmountAnomaly(tx, profile)

	// Factor 2: Velocity Anomaly (transaction frequency)
	factors["velocity_anomaly"] = m.calculateVelocityAnomaly(tx, profile)

	// Factor 3: Time Anomaly (unusual hours/days)
	factors["time_anomaly"] = m.calculateTimeAnomaly(tx, profile)

	// Factor 4: Recipient Anomaly (new or unusual recipient)
	factors["recipient_anomaly"] = m.calculateRecipientAnomaly(tx, profile)

	// Factor 5: Location Anomaly (country/region change)
	factors["location_anomaly"] = m.calculateLocationAnomaly(tx, profile)

	// Factor 6: Device Anomaly (new device)
	factors["device_anomaly"] = m.calculateDeviceAnomaly(tx, profile)

	// Factor 7: Pattern Anomaly (unusual behavior patterns)
	factors["pattern_anomaly"] = m.calculatePatternAnomaly(tx, profile)

	// Calculate weighted score
	score := factors["amount_anomaly"]*m.weights.AmountAnomaly +
		factors["velocity_anomaly"]*m.weights.VelocityAnomaly +
		factors["time_anomaly"]*m.weights.TimeAnomaly +
		factors["recipient_anomaly"]*m.weights.RecipientAnomaly +
		factors["location_anomaly"]*m.weights.LocationAnomaly +
		factors["device_anomaly"]*m.weights.DeviceAnomaly +
		factors["pattern_anomaly"]*m.weights.PatternAnomaly

	// Normalize score to 0-1
	score = math.Min(1.0, math.Max(0.0, score))

	// Determine risk level
	riskLevel := "low"
	recommendation := "approve"
	requiresReview := false

	if score >= m.thresholds.HighRisk {
		riskLevel = "high"
		recommendation = "block"
		requiresReview = true
	} else if score >= m.thresholds.MediumRisk {
		riskLevel = "medium"
		recommendation = "review"
		requiresReview = true
	} else if score >= m.thresholds.LowRisk {
		riskLevel = "low"
		recommendation = "approve_with_monitoring"
	}

	// Calculate confidence based on data availability
	confidence := m.calculateConfidence(profile)

	return FraudScore{
		Score:          score,
		RiskLevel:      riskLevel,
		Factors:        factors,
		Recommendation: recommendation,
		RequiresReview: requiresReview,
		Confidence:     confidence,
	}
}

func (m *FraudDetectionModel) calculateAmountAnomaly(tx TransactionFeatures, profile *UserProfile) float64 {
	if profile == nil || profile.TransactionCount < 5 || profile.StdDevAmount == 0 {
		// Not enough data - use absolute thresholds
		if tx.Amount > 100000 {
			return 0.8
		} else if tx.Amount > 50000 {
			return 0.5
		} else if tx.Amount > 10000 {
			return 0.3
		}
		return 0.1
	}

	// Z-score calculation
	zScore := math.Abs(tx.Amount-profile.AvgTransactionAmount) / profile.StdDevAmount

	// Convert z-score to anomaly probability using sigmoid
	return sigmoid(zScore - 2) // Scores above 2 std devs get high anomaly
}

func (m *FraudDetectionModel) calculateVelocityAnomaly(tx TransactionFeatures, profile *UserProfile) float64 {
	if profile == nil {
		return 0.2
	}

	// Check time since last transaction
	if tx.TimeSinceLast < 60 { // Less than 1 minute
		return 0.9
	} else if tx.TimeSinceLast < 300 { // Less than 5 minutes
		return 0.6
	} else if tx.TimeSinceLast < 3600 { // Less than 1 hour
		return 0.3
	}
	return 0.1
}

func (m *FraudDetectionModel) calculateTimeAnomaly(tx TransactionFeatures, profile *UserProfile) float64 {
	// Unusual hours (late night/early morning)
	if tx.Hour >= 0 && tx.Hour < 6 {
		return 0.7
	}

	if profile != nil && len(profile.TypicalHours) > 0 {
		// Check if hour is in typical hours
		isTypical := false
		for _, h := range profile.TypicalHours {
			if h == tx.Hour {
				isTypical = true
				break
			}
		}
		if !isTypical {
			return 0.5
		}
	}

	return 0.1
}

func (m *FraudDetectionModel) calculateRecipientAnomaly(tx TransactionFeatures, profile *UserProfile) float64 {
	if tx.IsNewRecipient {
		if tx.Amount > 50000 {
			return 0.8
		}
		return 0.5
	}

	if profile != nil && profile.FrequentRecipients != nil {
		if _, exists := profile.FrequentRecipients[tx.RecipientHash]; exists {
			return 0.1
		}
		return 0.4
	}

	return 0.2
}

func (m *FraudDetectionModel) calculateLocationAnomaly(tx TransactionFeatures, profile *UserProfile) float64 {
	// High-risk countries get higher scores
	highRiskCountries := map[string]bool{
		"NG": true, "GH": true, "KE": true, // Examples
	}

	if highRiskCountries[tx.CountryCode] {
		return 0.6
	}

	return 0.1
}

func (m *FraudDetectionModel) calculateDeviceAnomaly(tx TransactionFeatures, profile *UserProfile) float64 {
	// New device with large transaction
	if tx.DeviceHash == "" {
		return 0.5
	}
	return 0.1
}

func (m *FraudDetectionModel) calculatePatternAnomaly(tx TransactionFeatures, profile *UserProfile) float64 {
	score := 0.0

	// Round number amounts are suspicious
	if math.Mod(tx.Amount, 1000) == 0 && tx.Amount > 5000 {
		score += 0.3
	}

	// Weekend + large amount
	if tx.IsWeekend && tx.Amount > 10000 {
		score += 0.3
	}

	return math.Min(1.0, score)
}

func (m *FraudDetectionModel) calculateConfidence(profile *UserProfile) float64 {
	if profile == nil {
		return 0.3
	}

	// More data = higher confidence
	confidence := 0.5
	if profile.TransactionCount > 10 {
		confidence += 0.1
	}
	if profile.TransactionCount > 50 {
		confidence += 0.1
	}
	if profile.TransactionCount > 100 {
		confidence += 0.1
	}
	if profile.AccountAge > 30*24*time.Hour {
		confidence += 0.1
	}
	if profile.AccountAge > 90*24*time.Hour {
		confidence += 0.1
	}

	return math.Min(1.0, confidence)
}

// ==============================================================================
// RISK SCORING MODEL
// Calculates user and transaction risk scores
// ==============================================================================

// RiskScoringModel calculates risk scores.
type RiskScoringModel struct {
	weights RiskWeights
}

// RiskWeights for risk calculation.
type RiskWeights struct {
	TransactionHistory float64
	AccountAge         float64
	VerificationLevel  float64
	BehavioralPattern  float64
	GeographicRisk     float64
}

// RiskScore represents a risk assessment.
type RiskScore struct {
	OverallScore    float64            `json:"overall_score"`
	Category        string             `json:"category"`
	Components      map[string]float64 `json:"components"`
	Limits          RiskLimits         `json:"limits"`
	Recommendations []string           `json:"recommendations"`
}

// RiskLimits based on risk score.
type RiskLimits struct {
	DailyLimit        decimal.Decimal `json:"daily_limit"`
	SingleTxLimit     decimal.Decimal `json:"single_tx_limit"`
	MonthlyLimit      decimal.Decimal `json:"monthly_limit"`
	RequiresApproval  decimal.Decimal `json:"requires_approval"`
}

// NewRiskScoringModel creates a new risk scoring model.
func NewRiskScoringModel() *RiskScoringModel {
	return &RiskScoringModel{
		weights: RiskWeights{
			TransactionHistory: 0.30,
			AccountAge:         0.20,
			VerificationLevel:  0.25,
			BehavioralPattern:  0.15,
			GeographicRisk:     0.10,
		},
	}
}

// CalculateRisk computes a risk score for a user.
func (m *RiskScoringModel) CalculateRisk(profile *UserProfile, verificationLevel int, countryCode string) RiskScore {
	components := make(map[string]float64)

	// Component 1: Transaction History Score (higher is better)
	components["transaction_history"] = m.scoreTransactionHistory(profile)

	// Component 2: Account Age Score
	components["account_age"] = m.scoreAccountAge(profile)

	// Component 3: Verification Level Score
	components["verification_level"] = m.scoreVerificationLevel(verificationLevel)

	// Component 4: Behavioral Pattern Score
	components["behavioral_pattern"] = m.scoreBehavioralPattern(profile)

	// Component 5: Geographic Risk Score
	components["geographic_risk"] = m.scoreGeographicRisk(countryCode)

	// Calculate weighted score (lower = better)
	riskScore := 1.0 - (
		components["transaction_history"]*m.weights.TransactionHistory+
			components["account_age"]*m.weights.AccountAge+
			components["verification_level"]*m.weights.VerificationLevel+
			components["behavioral_pattern"]*m.weights.BehavioralPattern+
			components["geographic_risk"]*m.weights.GeographicRisk)

	riskScore = math.Max(0.0, math.Min(1.0, riskScore))

	// Determine category
	category := "high_risk"
	if riskScore < 0.3 {
		category = "low_risk"
	} else if riskScore < 0.6 {
		category = "medium_risk"
	}

	// Calculate limits based on risk
	limits := m.calculateLimits(riskScore)

	// Generate recommendations
	recommendations := m.generateRecommendations(components, riskScore)

	return RiskScore{
		OverallScore:    riskScore,
		Category:        category,
		Components:      components,
		Limits:          limits,
		Recommendations: recommendations,
	}
}

func (m *RiskScoringModel) scoreTransactionHistory(profile *UserProfile) float64 {
	if profile == nil {
		return 0.2
	}

	score := 0.2

	if profile.TransactionCount > 10 {
		score += 0.2
	}
	if profile.TransactionCount > 50 {
		score += 0.2
	}
	if profile.TransactionCount > 100 {
		score += 0.2
	}

	// Check for suspicious patterns
	if profile.RiskScore > 0.7 {
		score -= 0.3
	}

	return math.Max(0.0, math.Min(1.0, score))
}

func (m *RiskScoringModel) scoreAccountAge(profile *UserProfile) float64 {
	if profile == nil {
		return 0.1
	}

	days := profile.AccountAge.Hours() / 24

	if days > 365 {
		return 1.0
	} else if days > 180 {
		return 0.8
	} else if days > 90 {
		return 0.6
	} else if days > 30 {
		return 0.4
	} else if days > 7 {
		return 0.2
	}

	return 0.1
}

func (m *RiskScoringModel) scoreVerificationLevel(level int) float64 {
	switch level {
	case 3: // Fully verified
		return 1.0
	case 2: // ID verified
		return 0.7
	case 1: // Email/phone verified
		return 0.4
	default: // Unverified
		return 0.1
	}
}

func (m *RiskScoringModel) scoreBehavioralPattern(profile *UserProfile) float64 {
	if profile == nil {
		return 0.3
	}

	// Consistent behavior = higher score
	score := 0.5

	// Regular transaction patterns
	if profile.TransactionCount > 20 && profile.StdDevAmount < profile.AvgTransactionAmount*0.5 {
		score += 0.2
	}

	// Consistent recipients
	if len(profile.FrequentRecipients) > 3 {
		score += 0.2
	}

	return math.Min(1.0, score)
}

func (m *RiskScoringModel) scoreGeographicRisk(countryCode string) float64 {
	// Low-risk countries
	lowRisk := map[string]bool{
		"US": true, "GB": true, "DE": true, "FR": true, "JP": true, "SG": true, "AU": true,
	}

	// Medium-risk countries
	mediumRisk := map[string]bool{
		"MW": true, "ZA": true, "KE": true, "TZ": true, "ZM": true, "BW": true, "CN": true,
	}

	if lowRisk[countryCode] {
		return 0.9
	} else if mediumRisk[countryCode] {
		return 0.6
	}

	return 0.3
}

func (m *RiskScoringModel) calculateLimits(riskScore float64) RiskLimits {
	if riskScore < 0.3 {
		return RiskLimits{
			DailyLimit:       decimal.NewFromInt(100000),
			SingleTxLimit:    decimal.NewFromInt(50000),
			MonthlyLimit:     decimal.NewFromInt(500000),
			RequiresApproval: decimal.NewFromInt(25000),
		}
	} else if riskScore < 0.6 {
		return RiskLimits{
			DailyLimit:       decimal.NewFromInt(50000),
			SingleTxLimit:    decimal.NewFromInt(20000),
			MonthlyLimit:     decimal.NewFromInt(200000),
			RequiresApproval: decimal.NewFromInt(10000),
		}
	}

	return RiskLimits{
		DailyLimit:       decimal.NewFromInt(10000),
		SingleTxLimit:    decimal.NewFromInt(5000),
		MonthlyLimit:     decimal.NewFromInt(50000),
		RequiresApproval: decimal.NewFromInt(2000),
	}
}

func (m *RiskScoringModel) generateRecommendations(components map[string]float64, riskScore float64) []string {
	var recs []string

	if components["verification_level"] < 0.5 {
		recs = append(recs, "Complete identity verification to increase limits")
	}
	if components["account_age"] < 0.5 {
		recs = append(recs, "Account is relatively new - limits will increase over time")
	}
	if components["transaction_history"] < 0.5 {
		recs = append(recs, "Build transaction history to unlock higher limits")
	}

	return recs
}

// ==============================================================================
// ANOMALY DETECTION MODEL
// Detects unusual patterns in transactions
// ==============================================================================

// AnomalyDetectionModel detects anomalies using statistical methods.
type AnomalyDetectionModel struct {
	movingAverages map[string]*MovingAverage
	mu             sync.RWMutex
}

// MovingAverage for time-series analysis.
type MovingAverage struct {
	Values  []float64
	Sum     float64
	Count   int
	Window  int
	Mean    float64
	StdDev  float64
}

// AnomalyResult represents anomaly detection output.
type AnomalyResult struct {
	IsAnomaly       bool               `json:"is_anomaly"`
	AnomalyScore    float64            `json:"anomaly_score"`
	AnomalyType     string             `json:"anomaly_type"`
	Deviations      float64            `json:"deviations"`
	ExpectedRange   [2]float64         `json:"expected_range"`
	ActualValue     float64            `json:"actual_value"`
	Details         map[string]float64 `json:"details"`
}

// NewAnomalyDetectionModel creates a new anomaly detection model.
func NewAnomalyDetectionModel() *AnomalyDetectionModel {
	return &AnomalyDetectionModel{
		movingAverages: make(map[string]*MovingAverage),
	}
}

// DetectAnomaly checks if a value is anomalous for a given metric.
func (m *AnomalyDetectionModel) DetectAnomaly(metricName string, value float64) AnomalyResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	ma, exists := m.movingAverages[metricName]
	if !exists {
		ma = &MovingAverage{
			Window: 100,
			Values: make([]float64, 0, 100),
		}
		m.movingAverages[metricName] = ma
	}

	result := AnomalyResult{
		ActualValue: value,
		Details:     make(map[string]float64),
	}

	// Need at least 10 samples for meaningful analysis
	if ma.Count < 10 {
		ma.addValue(value)
		result.AnomalyScore = 0
		result.IsAnomaly = false
		result.AnomalyType = "insufficient_data"
		return result
	}

	// Calculate z-score
	zScore := 0.0
	if ma.StdDev > 0 {
		zScore = math.Abs(value-ma.Mean) / ma.StdDev
	}

	result.Deviations = zScore
	result.ExpectedRange = [2]float64{
		ma.Mean - 2*ma.StdDev,
		ma.Mean + 2*ma.StdDev,
	}
	result.Details["mean"] = ma.Mean
	result.Details["std_dev"] = ma.StdDev
	result.Details["z_score"] = zScore

	// Determine if anomalous
	if zScore > 3 {
		result.IsAnomaly = true
		result.AnomalyScore = 0.9
		result.AnomalyType = "extreme_outlier"
	} else if zScore > 2 {
		result.IsAnomaly = true
		result.AnomalyScore = 0.6
		result.AnomalyType = "outlier"
	} else if zScore > 1.5 {
		result.IsAnomaly = false
		result.AnomalyScore = 0.3
		result.AnomalyType = "minor_deviation"
	} else {
		result.IsAnomaly = false
		result.AnomalyScore = 0.1
		result.AnomalyType = "normal"
	}

	// Add value to moving average
	ma.addValue(value)

	return result
}

func (ma *MovingAverage) addValue(value float64) {
	ma.Values = append(ma.Values, value)
	ma.Sum += value
	ma.Count++

	// Maintain window size
	if len(ma.Values) > ma.Window {
		removed := ma.Values[0]
		ma.Values = ma.Values[1:]
		ma.Sum -= removed
	}

	// Update statistics
	n := float64(len(ma.Values))
	ma.Mean = ma.Sum / n

	// Calculate standard deviation
	var variance float64
	for _, v := range ma.Values {
		variance += (v - ma.Mean) * (v - ma.Mean)
	}
	ma.StdDev = math.Sqrt(variance / n)
}

// ==============================================================================
// ANALYTICS AGGREGATION
// Business intelligence and KPI calculations
// ==============================================================================

// KPIMetrics represents key performance indicators.
type KPIMetrics struct {
	TotalVolume        decimal.Decimal `json:"total_volume"`
	TransactionCount   int             `json:"transaction_count"`
	ActiveUsers        int             `json:"active_users"`
	AverageTransaction decimal.Decimal `json:"average_transaction"`
	SuccessRate        float64         `json:"success_rate"`
	FraudRate          float64         `json:"fraud_rate"`
	GrowthRate         float64         `json:"growth_rate"`
	RevenueEstimate    decimal.Decimal `json:"revenue_estimate"`
}

// CalculateKPIs computes key performance indicators.
func (e *AnalyticsEngine) CalculateKPIs(ctx context.Context, period string) KPIMetrics {
	// This would typically query the database
	// For now, return calculated metrics based on in-memory data
	
	e.mu.RLock()
	defer e.mu.RUnlock()

	metrics := KPIMetrics{}

	if len(e.transactionData) == 0 {
		return metrics
	}

	// Calculate metrics from transaction data
	var totalVolume float64
	successCount := 0
	fraudCount := 0
	uniqueUsers := make(map[string]bool)

	for _, tx := range e.transactionData {
		totalVolume += tx.Amount
		uniqueUsers[tx.UserID] = true
	}

	metrics.TotalVolume = decimal.NewFromFloat(totalVolume)
	metrics.TransactionCount = len(e.transactionData)
	metrics.ActiveUsers = len(uniqueUsers)

	if metrics.TransactionCount > 0 {
		metrics.AverageTransaction = metrics.TotalVolume.Div(decimal.NewFromInt(int64(metrics.TransactionCount)))
		metrics.SuccessRate = float64(successCount) / float64(metrics.TransactionCount)
		metrics.FraudRate = float64(fraudCount) / float64(metrics.TransactionCount)
	}

	// Estimate revenue (1.5% fee)
	metrics.RevenueEstimate = metrics.TotalVolume.Mul(decimal.NewFromFloat(0.015))

	return metrics
}

// ==============================================================================
// HELPER FUNCTIONS
// ==============================================================================

// sigmoid function for probability conversion.
func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}

// Percentile calculates the nth percentile of a sorted slice.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}

	rank := p / 100.0 * float64(len(sorted)-1)
	lower := int(rank)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	weight := rank - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

// SortFloat64s sorts a slice of float64 in ascending order.
func SortFloat64s(data []float64) {
	sort.Float64s(data)
}
