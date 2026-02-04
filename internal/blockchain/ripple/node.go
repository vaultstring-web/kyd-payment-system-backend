package ripple

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"time"
)

type AccountState struct {
	Balance int64
	Nonce   uint64
}

type BlockchainNode struct {
	NodeID              string
	Storage             *BlockchainStore
	Consensus           *ProofOfStake
	P2P                 *P2PNode
	Crypto              *CryptoSystem
	Blocks              map[string]*Block
	PendingTransactions []*Transaction
	Accounts            map[string]*AccountState
	Shards              map[int]*Shard
	MempoolMaxSize      int
	MempoolPriority     map[string]int64
	ChainID             string
	GenesisHash         string
	HeadBlock           *Block
	FinalizedBlocks     map[string]struct{}
	TxIndex             map[string]string // TxID -> BlockHash
}

func NewBlockchainNode(nodeID string) *BlockchainNode {
	cs := &CryptoSystem{}
	store := NewBlockchainStore("dict", "./blockchain_db")
	p2p := NewP2PNode(nodeID)

	chainIDHash := sha256.Sum256([]byte(nodeID))
	chainID := hex.EncodeToString(chainIDHash[:])[:8]

	return &BlockchainNode{
		NodeID:              nodeID,
		Storage:             store,
		Consensus:           NewProofOfStake(32, 1000, 128), // Integers
		P2P:                 p2p,
		Crypto:              cs,
		Blocks:              make(map[string]*Block),
		PendingTransactions: make([]*Transaction, 0),
		Accounts:            make(map[string]*AccountState),
		Shards:              make(map[int]*Shard),
		MempoolMaxSize:      10000,
		MempoolPriority:     make(map[string]int64),
		ChainID:             chainID,
		FinalizedBlocks:     make(map[string]struct{}),
		TxIndex:             make(map[string]string),
	}
}

func (n *BlockchainNode) CreateGenesisBlock(validators []*Validator) *Block {
	var proposer *PublicKey
	if len(validators) > 0 && validators[0].PublicKey != nil {
		proposer = validators[0].PublicKey
	} else {
		proposer = NewPublicKey(AlgoEd25519, []byte("genesis"))
	}

	genesis := &Block{
		BlockID:      "genesis",
		BlockNumber:  0,
		Timestamp:    float64(time.Now().UnixNano()) / 1e9,
		Proposer:     proposer,
		ParentHash:   "0x0",
		Transactions: []*Transaction{},
		MerkleRoot:   "",
		StateRoot:    n.computeStateRoot(),
		MinerReward:  0,
		Difficulty:   1,
		Nonce:        0,
		Metadata: map[string]interface{}{
			"chain_id":     n.ChainID,
			"proposer_id":  "",
			"block_number": 0,
		},
	}

	genesis.MerkleRoot = genesis.ComputeMerkleRoot()
	hash := genesis.ComputeHash()
	n.GenesisHash = hash
	n.Blocks[hash] = genesis
	n.HeadBlock = genesis

	for _, v := range validators {
		n.Consensus.RegisterValidator(v)
		addr := n.Crypto.DeriveAddress(v.PublicKey.KeyMaterial)
		n.Accounts[addr] = &AccountState{Balance: v.Stake, Nonce: 0}
	}

	return genesis
}

func (n *BlockchainNode) AddTransaction(tx *Transaction) bool {
	if len(n.PendingTransactions) >= n.MempoolMaxSize {
		return false
	}
	txHash := tx.ComputeHash()
	priority := tx.GasPrice * int64(tx.GasLimit)
	n.MempoolPriority[txHash] = priority
	n.PendingTransactions = append(n.PendingTransactions, tx)
	return true
}

func (n *BlockchainNode) SelectTransactionsForBlock(maxBlockSize int) []*Transaction {
	type indexedTx struct {
		index int
		tx    *Transaction
		score int64
	}

	all := make([]indexedTx, 0, len(n.PendingTransactions))
	for i, tx := range n.PendingTransactions {
		h := tx.ComputeHash()
		score := n.MempoolPriority[h]
		all = append(all, indexedTx{index: i, tx: tx, score: score})
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})

	selected := make([]*Transaction, 0)
	blockSize := 0

	for _, it := range all {
		txSize := it.tx.SerializedSize()
		if blockSize+txSize > maxBlockSize {
			continue
		}
		selected = append(selected, it.tx)
		blockSize += txSize
	}

	return selected
}

func (n *BlockchainNode) CreateBlock(proposer *Validator) *Block {
	if n.HeadBlock == nil {
		return nil
	}

	txs := n.SelectTransactionsForBlock(1_000_000)
	number := n.HeadBlock.BlockNumber + 1

	block := &Block{
		BlockID:      "block_" + strconv.FormatUint(number, 10),
		BlockNumber:  number,
		Timestamp:    float64(time.Now().UnixNano()) / 1e9,
		Proposer:     proposer.PublicKey,
		ParentHash:   n.HeadBlock.ComputeHash(),
		Transactions: txs,
		MerkleRoot:   "",
		StateRoot:    n.computeStateRoot(),
		MinerReward:  1000000,
		Difficulty:   1,
		Nonce:        0,
		Metadata: map[string]interface{}{
			"chain_id":    n.ChainID,
			"proposer_id": proposer.ValidatorID,
		},
	}

	block.MerkleRoot = block.ComputeMerkleRoot()

	return block
}

func (n *BlockchainNode) AddBlock(block *Block) bool {
	if block == nil {
		return false
	}
	hash := block.ComputeHash()
	if _, ok := n.Blocks[hash]; ok {
		return false
	}
	if !n.verifyBlock(block) {
		return false
	}
	n.Blocks[hash] = block
	n.HeadBlock = block

	included := make(map[string]struct{})
	for _, tx := range block.Transactions {
		included[tx.TxID] = struct{}{}
		n.TxIndex[tx.TxID] = hash
	}

	filtered := n.PendingTransactions[:0]
	for _, tx := range n.PendingTransactions {
		if _, ok := included[tx.TxID]; !ok {
			filtered = append(filtered, tx)
		}
	}
	n.PendingTransactions = filtered

	proposerID, ok := block.Metadata["proposer_id"].(string)
	if ok && proposerID != "" {
		n.Consensus.DistributeRewards(proposerID, nil, block.MinerReward)
	}

	return true
}

func (n *BlockchainNode) verifyBlock(block *Block) bool {
	if block.ParentHash != "0x0" {
		if _, ok := n.Blocks[block.ParentHash]; !ok && n.GenesisHash != block.ParentHash {
			return false
		}
	}

	expectedMerkle := block.ComputeMerkleRoot()
	if block.MerkleRoot != expectedMerkle {
		return false
	}

	for _, tx := range block.Transactions {
		if !n.verifyTransaction(tx) {
			return false
		}
	}

	return true
}

func (n *BlockchainNode) verifyTransaction(tx *Transaction) bool {
	if tx == nil {
		return false
	}

	// Banking Compliance & Smart Contract Verification
	if n.Consensus != nil {
		// 1. Execute Smart Contract
		if err := n.Consensus.ExecuteSmartContract(tx); err != nil {
			// In a real node, we might log this reason
			return false
		}

		// 2. Check Sanctions (if sender is present)
		if tx.Sender != nil {
			if n.Consensus.ComplianceManager.IsSanctioned(tx.Sender.ToHex()) {
				return false
			}
		}
	}

	return true
}

func (n *BlockchainNode) computeStateRoot() string {
	if len(n.Accounts) == 0 {
		hash := sha256.Sum256([]byte("genesis_state"))
		return hex.EncodeToString(hash[:])
	}

	keys := make([]string, 0, len(n.Accounts))
	for addr := range n.Accounts {
		keys = append(keys, addr)
	}
	sort.Strings(keys)

	builder := make([]byte, 0)
	for _, addr := range keys {
		state := n.Accounts[addr]
		part := addr + ":" + strconv.FormatInt(state.Balance, 10) + ";"
		builder = append(builder, []byte(part)...)
	}

	hash := sha256.Sum256(builder)
	return hex.EncodeToString(hash[:])
}

func (n *BlockchainNode) GetBalance(address string) int64 {
	if st, ok := n.Accounts[address]; ok {
		return st.Balance
	}
	return 0
}

func (n *BlockchainNode) Transfer(sender, receiver string, amount int64) bool {
	if n.GetBalance(sender) < amount {
		return false
	}
	if _, ok := n.Accounts[receiver]; !ok {
		n.Accounts[receiver] = &AccountState{}
	}
	n.Accounts[sender].Balance -= amount
	n.Accounts[receiver].Balance += amount
	return true
}

func (n *BlockchainNode) Run(ctx context.Context) {
	if n == nil {
		return
	}
	n.P2P.Start(ctx)
	<-ctx.Done()
	n.P2P.Stop()
}
