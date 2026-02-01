package blockchain_test

import (
	"fmt"
	"testing"

	"kyd/internal/blockchain/ripple"
	"kyd/internal/blockchain/stellar"
)

func TestRippleLike(t *testing.T) {
	fmt.Println("Testing Ripple-like (Professional) Blockchain...")

	// 1. Setup Crypto
	cs := &ripple.CryptoSystem{}
	pub, priv, _ := cs.GenerateEd25519Keypair()
	sender := ripple.NewPublicKey(ripple.AlgoEd25519, pub)

	pub2, _, _ := cs.GenerateEd25519Keypair()
	receiver := ripple.NewPublicKey(ripple.AlgoEd25519, pub2)

	// 2. Create Transaction
	tx := ripple.NewTransaction(sender, receiver, 100, 1)
	sigData := cs.SignEd25519([]byte(tx.ComputeHash()), priv)
	tx.Signature = ripple.NewSignature(ripple.AlgoEd25519, sigData, sender)

	if !cs.VerifyEd25519([]byte(tx.ComputeHash()), tx.Signature.SignatureData, sender.KeyMaterial) {
		t.Errorf("Ripple Transaction Signature Verification Failed")
	}

	// 3. Create Block
	block := ripple.NewBlock(1, "genesis_hash", sender)
	block.Transactions = append(block.Transactions, tx)
	block.MerkleRoot = block.ComputeMerkleRoot()

	fmt.Printf("Ripple Block Hash: %s\n", block.ComputeHash())

	// 4. Consensus
	pos := ripple.NewProofOfStake(32, 1000, 10)
	validator := &ripple.Validator{
		ValidatorID: "val1",
		Stake:       500,
		PublicKey:   sender,
	}
	pos.RegisterValidator(validator)
	proposer := pos.SelectProposer([]byte("seed"))
	fmt.Printf("Selected Proposer: %s\n", proposer)

	node := ripple.NewBlockchainNode("node-1")
	genesis := node.CreateGenesisBlock([]*ripple.Validator{validator})
	if genesis == nil {
		t.Fatalf("failed to create genesis block")
	}

	addrSender := node.Crypto.DeriveAddress(sender.KeyMaterial)
	addrReceiver := node.Crypto.DeriveAddress(receiver.KeyMaterial)
	node.Accounts[addrSender] = &ripple.AccountState{Balance: 1000, Nonce: 0}
	node.Accounts[addrReceiver] = &ripple.AccountState{Balance: 0, Nonce: 0}

	if ok := node.AddTransaction(tx); !ok {
		t.Fatalf("failed to add transaction to node mempool")
	}

	newBlock := node.CreateBlock(validator)
	if newBlock == nil {
		t.Fatalf("failed to create new block")
	}
	if ok := node.AddBlock(newBlock); !ok {
		t.Fatalf("failed to add new block to chain")
	}
}

func TestStellarLike(t *testing.T) {
	fmt.Println("Testing Stellar-like (AegisNet) Blockchain...")

	sim := stellar.NewAegisNetSimulator(2, 10, 5)

	tx := &stellar.ConfidentialTransaction{
		TxID:            "tx1",
		SenderZKAddress: "zk_addr_1",
		Amount:          50,
		Transparent:     true,
	}

	// 3. Create MicroBlock
	mb := &stellar.MicroBlock{
		BlockID:      "mb1",
		Transactions: []*stellar.ConfidentialTransaction{tx},
		Weight:       1.0,
	}
	sim.Shards[0].MicroBlocks["mb1"] = mb

	auction := sim.MEVSystem.RunSequencerAuction(0, 1)
	fmt.Printf("MEV Auction Winner: %s, Bid: %d\n", auction.Winner, auction.WinningBid)

	committee := sim.Consensus.SelectCommitteeWithCaps("epoch_seed_123", 5)
	fmt.Printf("Selected Committee: %v\n", committee)

	epoch := sim.StartNewEpoch()
	if epoch == nil {
		t.Fatalf("failed to start epoch")
	}

	for round := 0; round < 4; round++ {
		for shardID := 0; shardID < sim.NumShards; shardID++ {
			tx := sim.GenerateConfidentialTransaction(shardID, round%3 == 0)
			sim.Shards[shardID].PendingTransactions = append(sim.Shards[shardID].PendingTransactions, tx)
		}

		active := epoch.Committee
		limit := sim.NumShards * 2
		if len(active) > limit {
			active = active[:limit]
		}
		for i, validatorID := range active {
			shardID := i % sim.NumShards
			sim.ProposeMicroBlockWithMEV(shardID, validatorID)
		}

		if (round+1)%2 == 0 {
			sim.FinalizeEpochWithCrossShardProof()
			if round+1 < 4 {
				epoch = sim.StartNewEpoch()
			}
		}
	}

	if len(sim.Epochs) == 0 {
		t.Fatalf("expected at least one epoch")
	}

	lastEpoch := sim.Epochs[len(sim.Epochs)-1]
	if len(lastEpoch.ShardCommitments) == 0 {
		t.Fatalf("expected shard commitments after finalization")
	}

	if sim.Stats.TotalEpochs == 0 {
		t.Fatalf("expected at least one finalized epoch")
	}
}
