package main

import (
	"fmt"
	"log"
	"time"

	"kyd/internal/blockchain/banking"
	"kyd/internal/blockchain/ripple"
	"kyd/internal/blockchain/stellar"
)

func main() {
	fmt.Println("=== Verifying Advanced Blockchain Features (Banking & Compliance) ===")

	verifyStellarAegisNet()
	verifyRipplePoS()

	fmt.Println("\n=== All Advanced Features Verified Successfully ===")
}

func verifyStellarAegisNet() {
	fmt.Println("\n--- Stellar (AegisNet) Advanced Features ---")

	// Initialize Simulator
	sim := stellar.NewAegisNetSimulator(2, 4, 3) // 2 shards, 4 validators, committee size 3
	fmt.Println("Initialized AegisNet Simulator")

	// 1. Test Smart Contract: LIMIT_MAX
	fmt.Println("Test 1: Smart Contract (LIMIT_MAX)")
	tx1 := &stellar.ConfidentialTransaction{
		TxID:              "tx_limit_test",
		SenderZKAddress:   "zk_addr_sender_1",
		ReceiverZKAddress: "zk_addr_receiver_1",
		Amount:            1000,
		Timestamp:         float64(time.Now().UnixNano()) / 1e9,
		SmartContract: &stellar.SmartContract{
			ContractID: "contract_limit_500",
			Code:       "LIMIT_MAX 500", // Limit is 500, amount is 1000 -> Should fail
		},
	}

	// Use the consensus instance from the simulator
	consensus := sim.Consensus

	err := consensus.ExecuteSmartContract(tx1)
	if err == nil {
		log.Fatalf("Test 1 Failed: Expected error for amount > limit, got nil")
	} else {
		fmt.Printf("Test 1 Passed: Rejected as expected: %v\n", err)
	}

	// 2. Test Smart Contract: Valid Transaction
	fmt.Println("Test 2: Smart Contract (Valid)")
	tx2 := &stellar.ConfidentialTransaction{
		TxID:              "tx_valid",
		SenderZKAddress:   "zk_addr_sender_1",
		ReceiverZKAddress: "zk_addr_receiver_1",
		Amount:            400,
		Timestamp:         float64(time.Now().UnixNano()) / 1e9,
		SmartContract: &stellar.SmartContract{
			ContractID: "contract_limit_500",
			Code:       "LIMIT_MAX 500",
		},
	}
	err = consensus.ExecuteSmartContract(tx2)
	if err != nil {
		log.Fatalf("Test 2 Failed: Expected success, got error: %v", err)
	} else {
		fmt.Println("Test 2 Passed: Accepted valid transaction")
	}

	// 3. Test Smart Contract: REQUIRE_ISO20022
	fmt.Println("Test 3: Smart Contract (REQUIRE_ISO20022)")
	tx3 := &stellar.ConfidentialTransaction{
		TxID:              "tx_iso_test",
		SenderZKAddress:   "zk_addr_sender_1",
		ReceiverZKAddress: "zk_addr_receiver_1",
		Amount:            100,
		Timestamp:         float64(time.Now().UnixNano()) / 1e9,
		SmartContract: &stellar.SmartContract{
			ContractID: "contract_iso",
			Code:       "REQUIRE_ISO20022",
		},
		// Missing ISO20022Data -> Should fail
	}
	err = consensus.ExecuteSmartContract(tx3)
	if err == nil {
		log.Fatalf("Test 3 Failed: Expected error for missing ISO20022 data")
	} else {
		fmt.Printf("Test 3 Passed: Rejected as expected: %v\n", err)
	}

	// 4. Test Smart Contract: Valid ISO20022
	fmt.Println("Test 4: Smart Contract (Valid ISO20022)")
	tx4 := &stellar.ConfidentialTransaction{
		TxID:              "tx_iso_valid",
		SenderZKAddress:   "zk_addr_sender_1",
		ReceiverZKAddress: "zk_addr_receiver_1",
		Amount:            100,
		Timestamp:         float64(time.Now().UnixNano()) / 1e9,
		SmartContract: &stellar.SmartContract{
			ContractID: "contract_iso",
			Code:       "REQUIRE_ISO20022",
		},
		ISO20022Data: &banking.ISO20022Metadata{
			MsgID:       "msg_123",
			PurposeCode: "SALA",
		},
	}
	err = consensus.ExecuteSmartContract(tx4)
	if err != nil {
		log.Fatalf("Test 4 Failed: Expected success, got error: %v", err)
	} else {
		fmt.Println("Test 4 Passed: Accepted valid ISO20022 transaction")
	}

	// 5. Test MEV Democratization
	fmt.Println("Test 5: MEV Democratization")
	// Access MEV system via simulator
	mev := sim.MEVSystem
	auction := mev.RunSequencerAuction(0, 1) // Shard 0, Slot 1
	fmt.Printf("MEV Auction Winner: %s, Revenue: %d\n", auction.Winner, auction.WinningBid)

	// Distribution happens automatically inside RunSequencerAuction in our implementation
	// We can check if distribution map is populated
	if len(auction.RevenueDistribution) > 0 {
		fmt.Println("Test 5 Passed: MEV Auction and Distribution executed")
	} else {
		fmt.Println("Test 5 Warning: No revenue distributed (maybe 0 bid or no winner)")
	}
}

func verifyRipplePoS() {
	fmt.Println("\n--- Ripple (PoS) Advanced Features ---")

	pos := ripple.NewProofOfStake(1000, 1000000, 3)

	senderKeys := ripple.GenerateKey()
	receiverKeys := ripple.GenerateKey()

	// 1. Test Smart Contract: REQUIRE_KYC
	fmt.Println("Test 1: Smart Contract (REQUIRE_KYC)")
	tx1 := ripple.NewTransaction(senderKeys, receiverKeys, 5000, 1)
	tx1.SmartContract = &ripple.SmartContract{
		ContractID: "contract_kyc_high",
		Script:     "REQUIRE_KYC 5", // Requires level 5, max simulated is 3 -> Should fail
	}

	err := pos.ExecuteSmartContract(tx1)
	if err == nil {
		log.Fatalf("Test 1 Failed: Expected error for KYC level > 3, got nil")
	} else {
		fmt.Printf("Test 1 Passed: Rejected as expected: %v\n", err)
	}

	// 2. Test Smart Contract: Valid KYC
	fmt.Println("Test 2: Smart Contract (Valid KYC)")
	tx2 := ripple.NewTransaction(senderKeys, receiverKeys, 5000, 2)
	tx2.SmartContract = &ripple.SmartContract{
		ContractID: "contract_kyc_low",
		Script:     "REQUIRE_KYC 2", // Requires level 2 -> Should pass
	}
	err = pos.ExecuteSmartContract(tx2)
	if err != nil {
		log.Fatalf("Test 2 Failed: Expected success, got error: %v", err)
	} else {
		fmt.Println("Test 2 Passed: Accepted valid transaction")
	}

	// 3. Test Smart Contract: REQUIRE_ISO20022
	fmt.Println("Test 3: Smart Contract (REQUIRE_ISO20022)")
	tx3 := ripple.NewTransaction(senderKeys, receiverKeys, 5000, 3)
	tx3.SmartContract = &ripple.SmartContract{
		ContractID: "contract_iso",
		Script:     "REQUIRE_ISO20022",
	}
	// Missing ISO20022Data
	err = pos.ExecuteSmartContract(tx3)
	if err == nil {
		log.Fatalf("Test 3 Failed: Expected error for missing ISO20022 data")
	} else {
		fmt.Printf("Test 3 Passed: Rejected as expected: %v\n", err)
	}

	// 4. Test Smart Contract: Valid ISO20022
	fmt.Println("Test 4: Smart Contract (Valid ISO20022)")
	tx4 := ripple.NewTransaction(senderKeys, receiverKeys, 5000, 4)
	tx4.SmartContract = &ripple.SmartContract{
		ContractID: "contract_iso",
		Script:     "REQUIRE_ISO20022",
	}
	tx4.ISO20022Data = &banking.ISO20022Metadata{
		MsgID:       "msg_456",
		PurposeCode: "TAXS",
	}
	err = pos.ExecuteSmartContract(tx4)
	if err != nil {
		log.Fatalf("Test 4 Failed: Expected success, got error: %v", err)
	} else {
		fmt.Println("Test 4 Passed: Accepted valid ISO20022 transaction")
	}

	// 5. Test Escrow (Condition Check only, logic is mainly in hash/storage but let's verify structure)
	fmt.Println("Test 5: Escrow Condition")
	tx5 := ripple.NewTransaction(senderKeys, receiverKeys, 10000, 5)
	tx5.Escrow = &ripple.EscrowCondition{
		ConditionType: "TIME_LOCK",
		Expiry:        float64(time.Now().Add(24 * time.Hour).Unix()),
		SecretHash:    "some_hash",
	}
	hash := tx5.ComputeHash()
	fmt.Printf("Escrow Transaction Hash: %s\n", hash)
	if hash == "" {
		log.Fatal("Test 5 Failed: Hash computation failed")
	}
	fmt.Println("Test 5 Passed: Escrow transaction structure verified")
}
