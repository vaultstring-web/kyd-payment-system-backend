package main

import (
	"fmt"
	"log"

	"kyd/internal/blockchain/banking"
)

func main() {
	fmt.Println("=========================================================")
	fmt.Println("KYD PAYMENT SYSTEM - BANKING SETTLEMENT SIMULATION")
	fmt.Println("Demonstrating: ISO 20022 Pacs.008 Generation & Validation")
	fmt.Println("=========================================================")

	validator := banking.NewISO20022Validator()

	// Simulation Data
	msgID := "MSG-20231027-001"
	amount := "1500000.00"
	currency := "MWK"
	
	// Debtor (Sender)
	debtorName := "John Doe"
	debtorIBAN := "MWK29384729384723"
	debtorBIC := "NBMWMWZA" // National Bank of Malawi

	// Creditor (Receiver)
	creditorName := "Jane Smith"
	creditorIBAN := "MWK99887766554433"
	creditorBIC := "STANMWZA" // Standard Bank Malawi

	remitInfo := "Payment for Services Rendered - Invoice #12345"

	fmt.Println("\n--- Step 1: Generating ISO 20022 Pacs.008 XML ---")
	xmlPayload, err := validator.GeneratePacs008(
		msgID, amount, currency,
		debtorName, debtorIBAN, debtorBIC,
		creditorName, creditorIBAN, creditorBIC,
		remitInfo,
	)
	if err != nil {
		log.Fatalf("Failed to generate Pacs.008: %v", err)
	}

	fmt.Println("Generated XML Payload:")
	fmt.Println(xmlPayload)

	fmt.Println("\n--- Step 2: Validating Compliance Metadata ---")
	// Verify that the BICs used are valid according to the validator's rules
	meta := &banking.ISO20022Metadata{
		MsgID:         msgID,
		DebtorAgent:   debtorBIC,
		CreditorAgent: creditorBIC,
		EndToEndID:    msgID,
		PurposeCode:   "SUPP", // Supplier Payment
	}

	if err := validator.ValidateMetadata(meta); err != nil {
		fmt.Printf("[FAIL] Metadata Validation Failed: %v\n", err)
	} else {
		fmt.Println("[PASS] ISO 20022 Metadata Validated Successfully")
	}

	fmt.Println("\n=========================================================")
	fmt.Println("SETTLEMENT SIMULATION COMPLETE")
	fmt.Println("=========================================================")
}
