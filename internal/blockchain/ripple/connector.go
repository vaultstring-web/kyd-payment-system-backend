// Package ripple provides support for integrating with the Ripple network.
package ripple

import (
	"context"
	"crypto/sha256"
	"fmt"

	"kyd/internal/blockchain/banking"
	"kyd/internal/domain"
	"kyd/internal/settlement"
	"kyd/pkg/iso20022"

	"github.com/shopspring/decimal"
)

// Connector provides integration with the Ripple-like Professional Blockchain.
type Connector struct {
	Node      *BlockchainNode
	SecretKey string
}

// NewConnector initializes a new local blockchain node for settlement.
func NewConnector(_, secretKey string) (*Connector, error) {
	node := NewBlockchainNode("local-ripple-node")

	// Setup initial validator and funding
	cs := &CryptoSystem{}
	// Use deterministic key based on secretKey if provided
	var pub *PublicKey
	if secretKey != "" {
		// In a real system, we'd derive Ed25519 key from secret properly
		// Here we just use it as seed if length is sufficient, else pad/hash
		seed := sha256.Sum256([]byte(secretKey))
		p, _, _ := cs.GenerateKeypairFromSeed(seed[:])
		pub = NewPublicKey(AlgoEd25519, p)
	} else {
		p, _, _ := cs.GenerateEd25519Keypair()
		pub = NewPublicKey(AlgoEd25519, p)
	}

	pk := pub // already PublicKey type

	val := &Validator{
		ValidatorID: "genesis_validator",
		PublicKey:   pk,
		Stake:       1000000,
		IsActive:    true,
	}

	// Create genesis block
	if genesis := node.CreateGenesisBlock([]*Validator{val}); genesis == nil {
		return nil, fmt.Errorf("failed to create genesis block")
	}

	// Fund a default account for settlements
	node.Accounts["settlement_wallet"] = &AccountState{Balance: 1000000000000, Nonce: 0} // 1M units

	go node.P2P.Start(context.Background())

	return &Connector{Node: node, SecretKey: secretKey}, nil
}

// SubmitSettlement submits a settlement transaction to the blockchain.
func (c *Connector) SubmitSettlement(_ context.Context, s *domain.Settlement) (*settlement.SettlementResult, error) {
	// Convert decimal amount to integer drops (e.g., x 1,000,000)
	amount := s.TotalAmount.Mul(decimal.NewFromInt(1000000)).IntPart()

	// Create transaction
	// Source: "settlement_wallet"

	cs := &CryptoSystem{}
	var sender *PublicKey
	var priv []byte

	if c.SecretKey != "" {
		seed := sha256.Sum256([]byte(c.SecretKey))
		pubBytes, privBytes, _ := cs.GenerateKeypairFromSeed(seed[:])
		sender = NewPublicKey(AlgoEd25519, pubBytes)
		priv = privBytes
	} else {
		// Fallback for dev/test without config
		// Use Post-Quantum Dilithium keys for advanced security
		pub, pr, _ := cs.GenerateDilithiumKeypair()
		sender = NewPublicKey(AlgoDilithium, pub)
		priv = pr
	}

	// Destination: s.DestinationAccount or dummy
	// Use Post-Quantum Dilithium keys
	pub2, _, _ := cs.GenerateDilithiumKeypair()
	receiver := NewPublicKey(AlgoDilithium, pub2)

	tx := NewTransaction(sender, receiver, amount, 1) // Nonce should be managed

	// Enrich with ISO 20022 Metadata
	// In a real scenario, this data comes from the settlement request or external source
	xmlMsg, _ := iso20022.GeneratePacs008(tx.TxID, "sender_bic", "receiver_bic", float64(amount)/1000000.0, "MWK")

	tx.ISO20022Data = &banking.ISO20022Metadata{
		MsgID:          fmt.Sprintf("msg_%s", tx.TxID),
		CreditorAgent:  "ABCDUS33", // Simulated BIC
		DebtorAgent:    "WXYZGB22", // Simulated BIC
		RemittanceInfo: fmt.Sprintf("Settlement %s", s.ID),
		PurposeCode:    "TRAD",
		EndToEndID:     fmt.Sprintf("e2e_%s", tx.TxID),
		InstructionID:  fmt.Sprintf("instr_%s", tx.TxID),
		FullXML:        xmlMsg,
	}

	// Sign transaction
	var sigData []byte
	if sender.Algorithm == AlgoDilithium {
		sigData = cs.SignDilithium([]byte(tx.ComputeHash()), priv)
	} else {
		sigData = cs.SignEd25519([]byte(tx.ComputeHash()), priv)
	}
	tx.Signature = NewSignature(sender.Algorithm, sigData, sender)

	// Add to node
	if ok := c.Node.AddTransaction(tx); !ok {
		return nil, fmt.Errorf("failed to add transaction to node")
	}

	// Force block creation to confirm it (immediate settlement for this secure system)
	// In a distributed system, we'd wait.
	// For now, we return the hash and let the node background process handle it (or manual trigger)
	// Trigger block creation for demo/speed
	// Note: We need a validator to propose. Using genesis validator (derived from same secret)
	val := &Validator{
		ValidatorID: "genesis_validator",
		PublicKey:   sender, // Assuming sender is the validator/admin
	}
	if block := c.Node.CreateBlock(val); block != nil {
		if !c.Node.AddBlock(block) {
			return nil, fmt.Errorf("failed to add block to chain (validation failed)")
		}
	} else {
		return nil, fmt.Errorf("failed to create block")
	}

	return &settlement.SettlementResult{
		TxHash:    tx.TxID,
		Confirmed: false, // Will be confirmed by poller
	}, nil
}

// CheckConfirmation checks if a transaction is confirmed.
func (c *Connector) CheckConfirmation(_ context.Context, txHash string) (bool, error) {
	if _, ok := c.Node.TxIndex[txHash]; ok {
		return true, nil
	}
	return false, nil
}
