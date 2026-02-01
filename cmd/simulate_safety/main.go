package main

import (
	"fmt"
	"time"

	"kyd/internal/monitoring"
	"kyd/internal/risk"
	"kyd/pkg/config"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func main() {
	fmt.Println("=========================================================")
	fmt.Println("KYD PAYMENT SYSTEM - SAFETY & RISK SIMULATION")
	fmt.Println("=========================================================")
	fmt.Println("Demonstrating: Circuit Breakers, Risk Scoring, Behavioral Monitoring")
	fmt.Println("")

	// Initialize Engines
	cfg := config.RiskConfig{
		EnableCircuitBreaker: true,
		MaxDailyLimit:        100000000,
	}
	riskEngine := risk.NewRiskEngine(cfg)
	monitor := monitoring.NewBehavioralMonitor()

	// ---------------------------------------------------------
	// SCENARIO 1: Global Circuit Breaker
	// ---------------------------------------------------------
	fmt.Println("--- Scenario 1: Global Circuit Breaker ---")
	if err := riskEngine.CheckGlobalCircuitBreaker(); err != nil {
		fmt.Printf("[FAIL] Circuit Breaker is unexpectedly OPEN: %v\n", err)
	} else {
		fmt.Println("[PASS] Circuit Breaker is CLOSED (System Healthy)")
	}

	// Simulate failures to trip the breaker
	fmt.Println("... Simulating consecutive system failures ...")
	for i := 0; i < 12; i++ {
		riskEngine.ReportFailure()
	}

	if err := riskEngine.CheckGlobalCircuitBreaker(); err != nil {
		fmt.Printf("[PASS] Circuit Breaker TRIPPED as expected: %v\n", err)
	} else {
		fmt.Println("[FAIL] Circuit Breaker failed to trip!")
	}

	// Reset for next scenarios
	// In reality, this resets after a timeout, but we'll re-init for the script
	riskEngine = risk.NewRiskEngine(cfg)
	fmt.Println("")

	// ---------------------------------------------------------
	// SCENARIO 2: Risk Scoring (High Value vs KYC)
	// ---------------------------------------------------------
	fmt.Println("--- Scenario 2: Risk Scoring ---")

	// Case A: Low Value, Verified User (KYC Level 3)
	amountLow := decimal.NewFromInt(100)
	scoreLow := riskEngine.EvaluateRisk(amountLow, 3, false, "US") // false = trusted device
	fmt.Printf("Tx: $100, KYC Level 3, Trusted Device -> Risk Score: %d\n", scoreLow)
	if scoreLow < risk.RiskScoreMedium {
		fmt.Println("[PASS] Low risk transaction allowed.")
	} else {
		fmt.Println("[FAIL] False positive on low risk transaction.")
	}

	// Case B: High Value, New Device (KYC Level 1)
	amountHigh := decimal.NewFromInt(500000)
	scoreHigh := riskEngine.EvaluateRisk(amountHigh, 1, true, "US") // true = new device
	fmt.Printf("Tx: $500,000, KYC Level 1, NEW Device -> Risk Score: %d\n", scoreHigh)
	if scoreHigh >= risk.RiskScoreCritical {
		fmt.Println("[PASS] High risk transaction BLOCKED (Critical Score).")
	} else {
		fmt.Printf("[FAIL] High risk transaction not blocked (Score: %d)\n", scoreHigh)
	}
	fmt.Println("")

	// ---------------------------------------------------------
	// SCENARIO 3: Behavioral Monitoring (Velocity & Anomalies)
	// ---------------------------------------------------------
	fmt.Println("--- Scenario 3: Behavioral Monitoring ---")
	userID := uuid.New()
	receiverID := "user_abc123"

	// Step 1: Establish Baseline (Normal behavior)
	fmt.Println("... Establishing baseline behavior (5 normal transactions) ...")
	for i := 0; i < 5; i++ {
		monitor.RecordTransaction(userID, decimal.NewFromInt(50), receiverID, "New York")
	}

	// Step 2: Anomaly - Sudden Spike (10x average)
	fmt.Println("... Attempting 10x Spike Transaction ...")
	spikeAmount := decimal.NewFromInt(5000) // Baseline is 50
	anomalies, _ := monitor.DetectAnomalies(userID, spikeAmount, receiverID)

	if len(anomalies) > 0 {
		for _, a := range anomalies {
			fmt.Printf("[PASS] Anomaly Detected: %s (Severity: %s)\n", a.Description, a.Severity)
		}
	} else {
		fmt.Println("[FAIL] Failed to detect sudden spike anomaly.")
	}

	// Step 3: Anomaly - High Velocity (Rapid Fire)
	fmt.Println("... Attempting Rapid Fire Transaction ...")
	// We just recorded one (the spike check doesn't record, but let's record a new one immediately)
	monitor.RecordTransaction(userID, decimal.NewFromInt(50), receiverID, "New York")
	time.Sleep(10 * time.Millisecond)

	anomalies, _ = monitor.DetectAnomalies(userID, decimal.NewFromInt(50), receiverID)
	foundVelocity := false
	for _, a := range anomalies {
		if a.Type == monitoring.AnomalyHighVelocity {
			fmt.Printf("[PASS] Anomaly Detected: %s (Severity: %s)\n", a.Description, a.Severity)
			foundVelocity = true
		}
	}
	if !foundVelocity {
		fmt.Println("[FAIL] Failed to detect high velocity anomaly.")
	}

	fmt.Println("")
	fmt.Println("=========================================================")
	fmt.Println("SIMULATION COMPLETE")
	fmt.Println("=========================================================")
}
