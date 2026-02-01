package main

import (
	"context"
	"fmt"
	"log"

	"kyd/internal/blockchain/bridge"
	"kyd/internal/blockchain/ripple"
	"kyd/internal/blockchain/stellar"
)

func main() {
	fmt.Println("=========================================================")
	fmt.Println("   KYD BLOCKCHAIN INTEROPERABILITY BRIDGE SIMULATION")
	fmt.Println("   Connecting Stellar (ASV-BFT) <-> Ripple (PoS-Sim)")
	fmt.Println("=========================================================")

	// 1. Initialize Blockchains
	fmt.Println("\n[Init] Booting up Blockchain Nodes...")

	// Stellar (AegisNet)
	stellarNode := stellar.NewAegisNetSimulator(2, 20, 5) // 2 Shards, 20 Validators, 5 Committee
	// go stellarNode.StartConsensus() // Method not available, simulated by manual epoch progression if needed

	// Ripple (Connector)
	rippleConn, _ := ripple.NewConnector("", "s1") // URL ignored in local mode

	// 2. Initialize Bridge
	liquidityBridge := bridge.NewLiquidityBridge(stellarNode, rippleConn)
	fmt.Println("[Init] Bridge Service Online. Ready for Cross-Chain Swaps.")

	ctx := context.Background()

	// 3. Scenario A: MWK Remittance (Malawi -> USA)
	// User sends MWK on Stellar, Receiver gets USD-Wrapped on Ripple
	fmt.Println("\n---------------------------------------------------------")
	fmt.Println("SCENARIO A: Cross-Border Remittance (MWK -> USD)")
	fmt.Println("---------------------------------------------------------")

	amountMWK := int64(500000) // 500,000 MWK
	sender := "G_MALAWI_BANK_USER_1"
	receiver := "r_USA_BANK_USER_1"

	swapID1, err := liquidityBridge.ExecuteAtomicSwap(ctx, bridge.StellarToRipple, amountMWK, sender, receiver)
	if err != nil {
		log.Fatalf("Swap A Failed: %v", err)
	}
	fmt.Printf("[Success] Remittance Complete! Swap ID: %s\n", swapID1)

	// 4. Scenario B: Trade Settlement (USA -> Malawi)
	// User sends USD on Ripple, Receiver gets MWK-Wrapped on Stellar
	fmt.Println("\n---------------------------------------------------------")
	fmt.Println("SCENARIO B: Trade Settlement (USD -> MWK)")
	fmt.Println("---------------------------------------------------------")

	amountUSD := int64(1000) // 1,000 USD
	senderUS := "r_USA_IMPORTER"
	receiverMW := "G_MALAWI_EXPORTER"

	swapID2, err := liquidityBridge.ExecuteAtomicSwap(ctx, bridge.RippleToStellar, amountUSD, senderUS, receiverMW)
	if err != nil {
		log.Fatalf("Swap B Failed: %v", err)
	}
	fmt.Printf("[Success] Settlement Complete! Swap ID: %s\n", swapID2)

	fmt.Println("\n=========================================================")
	fmt.Println("   BRIDGE SIMULATION COMPLETED SUCCESSFULLY")
	fmt.Println("=========================================================")
}
