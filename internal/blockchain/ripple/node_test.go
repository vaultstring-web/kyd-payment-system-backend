package ripple

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlockchainNode_AddBlock_TxIndex(t *testing.T) {
	node := NewBlockchainNode("node1")
	// Initialize genesis block
	node.CreateGenesisBlock(nil)

	sender := NewPublicKey(AlgoEd25519, []byte("sender_key_material_32_bytes_long"))
	receiver := NewPublicKey(AlgoEd25519, []byte("receiver_key_material_32_bytes_long"))
	tx := NewTransaction(sender, receiver, 100, 1)

	// Add transaction to mempool
	added := node.AddTransaction(tx)
	assert.True(t, added, "Transaction should be added to mempool")

	// Create validator for block creation
	validator := &Validator{
		ValidatorID: "val1",
		PublicKey:   NewPublicKey(AlgoEd25519, []byte("validator_key_material_32_bytes")),
		Stake:       1000,
	}

	// Create a new block
	// Note: NewBlockchainNode initializes Genesis block as HeadBlock
	block := node.CreateBlock(validator)
	assert.NotNil(t, block, "Block should be created")

	// Verify tx is in the block
	assert.Equal(t, 1, len(block.Transactions), "Block should contain 1 transaction")
	assert.Equal(t, tx.TxID, block.Transactions[0].TxID)

	// Add block to node
	success := node.AddBlock(block)
	assert.True(t, success, "Block should be added successfully")

	// Verify TxIndex
	blockHash := block.ComputeHash()
	indexedHash, ok := node.TxIndex[tx.TxID]
	assert.True(t, ok, "Transaction should be indexed")
	assert.Equal(t, blockHash, indexedHash, "Indexed block hash should match")
}
