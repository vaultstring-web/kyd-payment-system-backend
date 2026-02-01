package ripple

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"kyd/internal/blockchain/banking"
)

// Transaction represents a blockchain transaction with optional privacy
type Transaction struct {
	TxID      string
	Sender    *PublicKey
	Receiver  *PublicKey
	Amount    int64
	Currency  string // Added for bridge simulation
	Nonce     uint64
	GasLimit  uint64
	GasPrice  int64
	Data      []byte
	Signature *Signature
	Timestamp float64
	IsPrivate bool
	ZKProof   string // Zero-knowledge proof for privacy

	// Banking Compliance
	ComplianceProof *banking.ComplianceProof
	ISO20022Data    *banking.ISO20022Metadata

	// Advanced Banking Features
	Escrow        *EscrowCondition
	SmartContract *SmartContract
}

// EscrowCondition defines conditional payment release
type EscrowCondition struct {
	ConditionType string  `json:"condition_type"` // "TIME_LOCK", "HASH_LOCK"
	Expiry        float64 `json:"expiry"`
	SecretHash    string  `json:"secret_hash"`
}

// SmartContract defines programmable money logic
type SmartContract struct {
	ContractID string                 `json:"contract_id"`
	Script     string                 `json:"script"`
	State      map[string]interface{} `json:"state"`
}

// NewTransaction creates a new transaction
func NewTransaction(sender, receiver *PublicKey, amount int64, nonce uint64) *Transaction {
	tx := &Transaction{
		Sender:    sender,
		Receiver:  receiver,
		Amount:    amount,
		Nonce:     nonce,
		GasLimit:  21000,
		GasPrice:  1, // 1 drop
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		IsPrivate: false,
	}
	tx.TxID = tx.ComputeHash()
	return tx
}

// ComputeHash computes the transaction hash
func (tx *Transaction) ComputeHash() string {
	data := fmt.Sprintf("%s%s%d%d%f",
		tx.Sender.ToHex(),
		tx.Receiver.ToHex(),
		tx.Amount,
		tx.Nonce,
		tx.Timestamp,
	)

	if tx.ComplianceProof != nil {
		data += tx.ComplianceProof.ProofID
	}

	if tx.Escrow != nil {
		data += fmt.Sprintf("%s%f%s", tx.Escrow.ConditionType, tx.Escrow.Expiry, tx.Escrow.SecretHash)
	}

	if tx.SmartContract != nil {
		data += tx.SmartContract.ContractID + tx.SmartContract.Script
	}

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func (tx *Transaction) SerializedSize() int {
	size := 64 + 66 + 66 + 16 + 8 + 8 + 8 + 256 + 100
	if len(tx.Data) > 0 {
		size += len(tx.Data)
	}
	if tx.ZKProof != "" {
		size += len(tx.ZKProof)
	}
	return size
}

// Block represents a blockchain block with advanced features
type Block struct {
	BlockID      string
	BlockNumber  uint64
	Timestamp    float64
	Proposer     *PublicKey
	ParentHash   string
	Transactions []*Transaction
	MerkleRoot   string
	StateRoot    string
	MinerReward  int64
	Difficulty   uint64
	Nonce        uint64
	Signature    *Signature
	Metadata     map[string]interface{}
}

// NewBlock creates a new block
func NewBlock(number uint64, parentHash string, proposer *PublicKey) *Block {
	return &Block{
		BlockNumber: number,
		Timestamp:   float64(time.Now().UnixNano()) / 1e9,
		Proposer:    proposer,
		ParentHash:  parentHash,
		Metadata:    make(map[string]interface{}),
		MinerReward: 0,
		Difficulty:  1,
	}
}

// ComputeHash computes the block hash
func (b *Block) ComputeHash() string {
	txHashes := ""
	for _, tx := range b.Transactions {
		txHashes += tx.ComputeHash()
	}
	data := fmt.Sprintf("%d%f%s%s%s%s",
		b.BlockNumber,
		b.Timestamp,
		b.ParentHash,
		txHashes,
		b.MerkleRoot,
		b.StateRoot,
	)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// ComputeMerkleRoot computes the Merkle root of transactions
func (b *Block) ComputeMerkleRoot() string {
	if len(b.Transactions) == 0 {
		hash := sha256.Sum256([]byte("empty"))
		return hex.EncodeToString(hash[:])
	}

	var txHashes []string
	for _, tx := range b.Transactions {
		txHashes = append(txHashes, tx.ComputeHash())
	}

	// Build Merkle tree bottom-up
	for len(txHashes) > 1 {
		if len(txHashes)%2 != 0 {
			txHashes = append(txHashes, txHashes[len(txHashes)-1])
		}

		var newLevel []string
		for i := 0; i < len(txHashes); i += 2 {
			combined := txHashes[i] + txHashes[i+1]
			hash := sha256.Sum256([]byte(combined))
			newLevel = append(newLevel, hex.EncodeToString(hash[:]))
		}
		txHashes = newLevel
	}

	return txHashes[0]
}

// Validator represents a network validator/node operator
type Validator struct {
	ValidatorID     string
	PublicKey       *PublicKey
	Stake           int64
	Delegations     map[string]int64
	SlashedAmount   int64
	IsActive        bool
	CommissionRate  float64
	BlocksProposed  uint64
	BlocksAttested  uint64
	LastActiveEpoch uint64

	// Banking Compliance
	RegulatoryID string
	FreezeStatus banking.FreezeStatus
}

// EffectiveStake calculates effective stake with cap
func (v *Validator) EffectiveStake(maxStakeCap int64) int64 {
	if v.Stake > maxStakeCap {
		return maxStakeCap
	}
	return v.Stake
}

// Shard represents a blockchain shard for scalability
type Shard struct {
	ShardID            int
	Blocks             map[string]*Block
	State              map[string]interface{}
	Validators         []*Validator
	CrossShardMessages []string // Simplified from deque
}

// NewShard creates a new shard
func NewShard(id int) *Shard {
	return &Shard{
		ShardID:    id,
		Blocks:     make(map[string]*Block),
		State:      make(map[string]interface{}),
		Validators: make([]*Validator, 0),
	}
}

// AddBlock adds a block to the shard
func (s *Shard) AddBlock(block *Block) bool {
	hash := block.ComputeHash()
	if _, exists := s.Blocks[hash]; !exists {
		s.Blocks[hash] = block
		return true
	}
	return false
}
