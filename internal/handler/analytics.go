// Package handler provides HTTP handlers for the KYD services.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"kyd/internal/analytics"
	"kyd/pkg/logger"
)

// AnalyticsHandler manages analytics endpoints.
type AnalyticsHandler struct {
	engine *analytics.AnalyticsEngine
	logger logger.Logger
}

// NewAnalyticsHandler creates an AnalyticsHandler.
func NewAnalyticsHandler(engine *analytics.AnalyticsEngine, log logger.Logger) *AnalyticsHandler {
	return &AnalyticsHandler{
		engine: engine,
		logger: log,
	}
}

// AnalyzeFraudRequest is the request body for fraud analysis.
type AnalyzeFraudRequest struct {
	TransactionID   string  `json:"transaction_id"`
	UserID          string  `json:"user_id"`
	Amount          float64 `json:"amount"`
	Currency        string  `json:"currency"`
	TransactionType string  `json:"transaction_type"`
	RecipientID     string  `json:"recipient_id"`
	IsNewRecipient  bool    `json:"is_new_recipient"`
	DeviceHash      string  `json:"device_hash"`
	IPAddress       string  `json:"ip_address"`
	CountryCode     string  `json:"country_code"`
}

// AnalyzeFraud performs fraud detection on a transaction.
func (h *AnalyticsHandler) AnalyzeFraud(w http.ResponseWriter, r *http.Request) {
	var req AnalyzeFraudRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	now := time.Now()

	features := analytics.TransactionFeatures{
		ID:              req.TransactionID,
		UserID:          req.UserID,
		Amount:          req.Amount,
		Currency:        req.Currency,
		TransactionType: req.TransactionType,
		Timestamp:       now,
		Hour:            now.Hour(),
		DayOfWeek:       int(now.Weekday()),
		IsWeekend:       now.Weekday() == time.Saturday || now.Weekday() == time.Sunday,
		CountryCode:     req.CountryCode,
		DeviceHash:      req.DeviceHash,
		RecipientHash:   req.RecipientID,
		IsNewRecipient:  req.IsNewRecipient,
	}

	// Get or create user profile (in production, fetch from DB)
	var profile *analytics.UserProfile

	// Perform fraud analysis
	result := h.engine.FraudModel().Analyze(features, profile)

	h.respondJSON(w, http.StatusOK, result)
}

// CalculateRiskRequest is the request for risk scoring.
type CalculateRiskRequest struct {
	UserID            string `json:"user_id"`
	VerificationLevel int    `json:"verification_level"`
	CountryCode       string `json:"country_code"`
}

// CalculateRisk calculates a user's risk score.
func (h *AnalyticsHandler) CalculateRisk(w http.ResponseWriter, r *http.Request) {
	var req CalculateRiskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get user profile (in production, fetch from DB)
	var profile *analytics.UserProfile

	result := h.engine.RiskModel().CalculateRisk(profile, req.VerificationLevel, req.CountryCode)

	h.respondJSON(w, http.StatusOK, result)
}

// DetectAnomalyRequest is the request for anomaly detection.
type DetectAnomalyRequest struct {
	MetricName string  `json:"metric_name"`
	Value      float64 `json:"value"`
}

// DetectAnomaly checks if a value is anomalous.
func (h *AnalyticsHandler) DetectAnomaly(w http.ResponseWriter, r *http.Request) {
	var req DetectAnomalyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	result := h.engine.AnomalyModel().DetectAnomaly(req.MetricName, req.Value)

	h.respondJSON(w, http.StatusOK, result)
}

// GetKPIs returns key performance indicators.
func (h *AnalyticsHandler) GetKPIs(w http.ResponseWriter, r *http.Request) {
	// For now, return all data as period was previously ignored
	kpis := h.engine.CalculateKPIs(r.Context(), "", "")

	h.respondJSON(w, http.StatusOK, kpis)
}

// GetDashboard returns dashboard data.
func (h *AnalyticsHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	dashboard := map[string]interface{}{
		"kpis":      h.engine.CalculateKPIs(r.Context(), "", ""),
		"timestamp": time.Now(),
		"status":    "operational",
	}

	h.respondJSON(w, http.StatusOK, dashboard)
}

// EarningsReport represents the earnings report data.
type EarningsReport struct {
	Period                        string  `json:"period"`
	TotalEarnings                 float64 `json:"total_earnings"`
	TransactionCount              int     `json:"transaction_count"`
	AverageEarningsPerTransaction float64 `json:"average_earnings_per_transaction"`
	Currency                      string  `json:"currency"`
}

// GetEarningsReport returns the earnings report.
func (h *AnalyticsHandler) GetEarningsReport(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	kpis := h.engine.CalculateKPIs(r.Context(), startDate, endDate)
	revenue, _ := kpis.RevenueEstimate.Float64()

	avgEarnings := 0.0
	if kpis.TransactionCount > 0 {
		avgEarnings = revenue / float64(kpis.TransactionCount)
	}

	report := EarningsReport{
		Period:                        "current", // simplified
		TotalEarnings:                 revenue,
		TransactionCount:              kpis.TransactionCount,
		AverageEarningsPerTransaction: avgEarnings,
		Currency:                      "MWK", // Default currency
	}

	// Return as a list to match frontend expectation of { reports: [] }
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"reports": []EarningsReport{report},
	})
}

func (h *AnalyticsHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *AnalyticsHandler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
