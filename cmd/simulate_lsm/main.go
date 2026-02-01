package main

import (
	"fmt"
	"kyd/internal/blockchain/banking"
)

func main() {
	fmt.Println("=========================================================")
	fmt.Println("KYD PAYMENT SYSTEM - LSM GRIDLOCK RESOLUTION SIMULATION")
	fmt.Println("=========================================================")
	fmt.Println("Demonstrating: Multilateral Netting to solve liquidity gridlock")
	fmt.Println("Scenario: 3 Banks, Circular Debt, Insufficient Initial Liquidity")
	fmt.Println("---------------------------------------------------------")

	lsm := banking.NewGridlockResolver()

	// 1. Setup Participants with Low Liquidity
	// Each bank has only $2M liquidity
	lsm.AddParticipant("Bank_A", 2000000)
	lsm.AddParticipant("Bank_B", 2000000)
	lsm.AddParticipant("Bank_C", 2000000)

	fmt.Println("Initial State:")
	fmt.Println("  Bank_A: $2,000,000")
	fmt.Println("  Bank_B: $2,000,000")
	fmt.Println("  Bank_C: $2,000,000")
	fmt.Println("")

	// 2. Queue Circular Obligations (High Value)
	// A owes B $10M
	// B owes C $10M
	// C owes A $10M
	fmt.Println("Queueing Transactions:")
	fmt.Println("  1. Bank_A -> Bank_B: $10,000,000 (Priority: 1)")
	lsm.AddObligation("tx1", "Bank_A", "Bank_B", 10000000, 1)

	fmt.Println("  2. Bank_B -> Bank_C: $10,000,000 (Priority: 1)")
	lsm.AddObligation("tx2", "Bank_B", "Bank_C", 10000000, 1)

	fmt.Println("  3. Bank_C -> Bank_A: $10,000,000 (Priority: 1)")
	lsm.AddObligation("tx3", "Bank_C", "Bank_A", 10000000, 1)
	fmt.Println("")

	fmt.Println("Note: Individually, NONE of these can settle because $10M > $2M.")
	fmt.Println("Running Gridlock Resolution Algorithm...")
	fmt.Println("---------------------------------------------------------")

	// 3. Resolve
	cleared, err := lsm.Resolve()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Resolution Complete. Cleared Transactions: %d\n", len(cleared))
	for _, txID := range cleared {
		fmt.Printf("  - Cleared: %s\n", txID)
	}

	if len(cleared) == 3 {
		fmt.Println("\n[SUCCESS] All transactions cleared via Multilateral Netting!")
	} else {
		fmt.Println("\n[FAIL] Gridlock not resolved.")
	}
}
