package ripple

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"kyd/internal/blockchain/banking"
)

// ProofOfStake implements the consensus mechanism
type ProofOfStake struct {
	MinStake         int64
	StakeCap         int64
	MinValidators    int
	ActiveValidators map[string]*Validator

	// Banking Compliance & Privacy
	ComplianceManager banking.ComplianceManager
	ZKVerifier        *banking.ZKProofVerifier
}

// NewProofOfStake creates a new ProofOfStake instance
func NewProofOfStake(minStake, stakeCap int64, minValidators int) *ProofOfStake {
	return &ProofOfStake{
		MinStake:          minStake,
		StakeCap:          stakeCap,
		MinValidators:     minValidators,
		ActiveValidators:  make(map[string]*Validator),
		ComplianceManager: banking.NewComplianceManager(),
		ZKVerifier:        banking.NewZKProofVerifier(),
	}
}

// ProposeBlock creates a new block from pending transactions, ensuring regulatory compliance
func (pos *ProofOfStake) ProposeBlock(
	proposerID string,
	blockNumber uint64,
	parentHash string,
	pendingTxs []*Transaction,
) *Block {
	proposer, ok := pos.ActiveValidators[proposerID]
	if !ok {
		return nil
	}

	// Filter transactions for regulatory compliance
	validTxs := make([]*Transaction, 0)
	for _, tx := range pendingTxs {
		// 0. Privacy Check (ZK-Proof)
		if tx.IsPrivate {
			if tx.ZKProof == "" || !pos.ZKVerifier.VerifyConfidentialTx(tx.ZKProof, []string{tx.TxID}) {
				pos.ComplianceManager.ReportSuspiciousActivity(tx.TxID, "Invalid ZK-Proof for private transaction")
				continue
			}
		}

		// 1. Check Compliance Proof Validity
		if tx.ComplianceProof != nil && !pos.ComplianceManager.ValidateProof(tx.ComplianceProof) {
			continue // Drop invalid compliance proofs
		}

		// 2. Check Sanctions & Freeze Status
		if tx.Sender != nil {
			senderHex := tx.Sender.ToHex()
			if pos.ComplianceManager.IsSanctioned(senderHex) {
				pos.ComplianceManager.ReportSuspiciousActivity(tx.TxID, "Sender is sanctioned")
				continue
			}
			status := pos.ComplianceManager.CheckFreezeStatus(senderHex)
			if status == banking.FreezeStatusFrozen || status == banking.FreezeStatusReceiveOnly {
				continue // Sender is frozen
			}
		}

		if tx.Receiver != nil {
			receiverHex := tx.Receiver.ToHex()
			if pos.ComplianceManager.IsSanctioned(receiverHex) {
				continue
			}
			status := pos.ComplianceManager.CheckFreezeStatus(receiverHex)
			if status == banking.FreezeStatusFrozen || status == banking.FreezeStatusSendOnly {
				continue // Receiver is frozen
			}
		}

		// 3. Smart Contract Execution (Banking Logic)
		if tx.SmartContract != nil {
			if err := pos.ExecuteSmartContract(tx); err != nil {
				// Contract failure -> exclude from block (or include as failed in real system)
				// For simulation, we exclude to keep ledger clean
				continue
			}
		}

		validTxs = append(validTxs, tx)
	}

	if len(validTxs) == 0 {
		return nil
	}

	// Create Block
	block := NewBlock(blockNumber, parentHash, proposer.PublicKey)
	block.Transactions = validTxs
	block.Timestamp = float64(time.Now().UnixNano()) / 1e9

	// Calculate Merkle Root
	var leaves [][]byte
	for _, tx := range validTxs {
		leaves = append(leaves, []byte(tx.TxID))
	}
	mt := NewMerkleTree(leaves)
	block.MerkleRoot = mt.Root()

	return block
}

// RegisterValidator registers a new validator
func (pos *ProofOfStake) RegisterValidator(v *Validator) bool {
	if v.Stake < pos.MinStake {
		return false
	}
	pos.ActiveValidators[v.ValidatorID] = v
	return true
}

// SelectProposer selects a block proposer using stake-weighted VRF
func (pos *ProofOfStake) SelectProposer(seed []byte) string {
	if len(pos.ActiveValidators) == 0 {
		return ""
	}

	type scoreData struct {
		score          uint64
		effectiveStake int64
	}
	scores := make(map[string]scoreData)

	for valID, validator := range pos.ActiveValidators {
		vrfInput := append(seed, []byte(valID)...)
		vrfOutput := sha256.Sum256(vrfInput)
		score := binary.BigEndian.Uint64(vrfOutput[:8])

		effectiveStake := validator.EffectiveStake(pos.StakeCap)
		scores[valID] = scoreData{score, effectiveStake}
	}

	var bestProposer string
	maxWeight := new(big.Int).SetInt64(-1)

	for valID, data := range scores {
		// Weight = VRF_Score * Effective_Stake
		// Use big.Int to avoid overflow and maintain integer precision
		scoreBig := new(big.Int).SetUint64(data.score)
		stakeBig := new(big.Int).SetInt64(data.effectiveStake)
		weight := new(big.Int).Mul(scoreBig, stakeBig)

		if weight.Cmp(maxWeight) > 0 {
			maxWeight = weight
			bestProposer = valID
		}
	}

	return bestProposer
}

// SlashValidator slashes a validator for misbehavior
func (pos *ProofOfStake) SlashValidator(validatorID string, amount int64) bool {
	if val, exists := pos.ActiveValidators[validatorID]; exists {
		val.SlashedAmount += amount
		val.Stake -= amount
		if val.Stake < pos.MinStake {
			val.IsActive = false
		}
		return true
	}
	return false
}

func (pos *ProofOfStake) DistributeRewards(proposerID string, attesters []string, totalReward int64) {
	if totalReward <= 0 {
		return
	}
	if v, ok := pos.ActiveValidators[proposerID]; ok {
		proposerReward := (totalReward * 40) / 100
		v.Stake += proposerReward
		v.BlocksProposed++
	}
	if len(attesters) == 0 {
		return
	}
	attesterPool := (totalReward * 60) / 100
	attesterReward := attesterPool / int64(len(attesters))
	for _, id := range attesters {
		if v, ok := pos.ActiveValidators[id]; ok {
			v.Stake += attesterReward
			v.BlocksAttested++
		}
	}
}

// MerkleTree represents an efficient Merkle tree implementation
type MerkleTree struct {
	Leaves [][]byte
	Tree   [][]string
}

// NewMerkleTree creates a new MerkleTree
func NewMerkleTree(leaves [][]byte) *MerkleTree {
	mt := &MerkleTree{
		Leaves: leaves,
		Tree:   make([][]string, 0),
	}
	mt.Build()
	return mt
}

// Build builds the Merkle tree
func (mt *MerkleTree) Build() {
	var currentLevel []string
	for _, leaf := range mt.Leaves {
		hash := sha256.Sum256(leaf)
		currentLevel = append(currentLevel, hex.EncodeToString(hash[:]))
	}
	mt.Tree = append(mt.Tree, currentLevel)

	for len(currentLevel) > 1 {
		var nextLevel []string
		if len(currentLevel)%2 != 0 {
			currentLevel = append(currentLevel, currentLevel[len(currentLevel)-1])
		}

		for i := 0; i < len(currentLevel); i += 2 {
			combined := currentLevel[i] + currentLevel[i+1]
			hash := sha256.Sum256([]byte(combined))
			nextLevel = append(nextLevel, hex.EncodeToString(hash[:]))
		}
		mt.Tree = append(mt.Tree, nextLevel)
		currentLevel = nextLevel
	}
}

// Root returns the Merkle root
func (mt *MerkleTree) Root() string {
	if len(mt.Tree) == 0 || len(mt.Tree[len(mt.Tree)-1]) == 0 {
		return ""
	}
	return mt.Tree[len(mt.Tree)-1][0]
}

// ExecuteSmartContract executes the smart contract logic for a transaction
func (pos *ProofOfStake) ExecuteSmartContract(tx *Transaction) error {
	if tx.SmartContract == nil {
		return nil
	}

	// Simple script interpreter for banking rules
	// Supported commands:
	// - REQUIRE_KYC <level>
	// - LIMIT_MAX <amount>
	// - CHECK_SANCTIONS

	instructions := strings.Split(tx.SmartContract.Script, ";")
	for _, instr := range instructions {
		parts := strings.Fields(strings.TrimSpace(instr))
		if len(parts) == 0 {
			continue
		}

		cmd := parts[0]
		switch cmd {
		case "REQUIRE_KYC":
			if len(parts) < 2 {
				return fmt.Errorf("invalid REQUIRE_KYC arguments")
			}
			level, err := strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid KYC level")
			}
			// In a real system, we would check the sender's KYC level from Identity Provider
			// For simulation, we assume level 3 is required for high value
			// If req > 3, fail (simulating insufficient KYC)
			if level > 3 {
				return fmt.Errorf("KYC level insufficient (simulated)")
			}

		case "LIMIT_MAX":
			if len(parts) < 2 {
				return fmt.Errorf("invalid LIMIT_MAX arguments")
			}
			limit, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid limit amount")
			}
			if tx.Amount > limit {
				return fmt.Errorf("transaction amount %d exceeds contract limit %d", tx.Amount, limit)
			}

		case "CHECK_SANCTIONS":
			if tx.Sender != nil {
				if pos.ComplianceManager.IsSanctioned(tx.Sender.ToHex()) {
					return fmt.Errorf("sender is sanctioned (contract check)")
				}
			}

		case "REQUIRE_ISO20022":
			if tx.ISO20022Data == nil {
				return fmt.Errorf("transaction missing required ISO20022 metadata")
			}
			if tx.ISO20022Data.PurposeCode == "" {
				return fmt.Errorf("ISO20022 PurposeCode required")
			}

		default:
			// Unknown command, ignore to allow future extensibility
		}
	}

	return nil
}
