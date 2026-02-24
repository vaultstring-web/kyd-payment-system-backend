package ripple

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlockchainNode_AddBlock_TxIndex(t *testing.T) {
	node := NewBlockchainNode("node1")
	// Initialize genesis block
	node.CreateGenesisBlock(nil)

	cs := &CryptoSystem{}
	pubSender, privSender, err := cs.GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("failed to generate sender keypair: %v", err)
	}
	pubReceiver, _, err := cs.GenerateEd25519Keypair()
	if err != nil {
		t.Fatalf("failed to generate receiver keypair: %v", err)
	}

	sender := NewPublicKey(AlgoEd25519, pubSender)
	receiver := NewPublicKey(AlgoEd25519, pubReceiver)
	tx := NewTransaction(sender, receiver, 100, 1)

	sig := cs.SignEd25519([]byte(tx.ComputeHash()), privSender)
	tx.Signature = NewSignature(AlgoEd25519, sig, sender)

	// Add transaction to mempool
	added := node.AddTransaction(tx)
	assert.True(t, added, "Transaction should be added to mempool")

	// Create validator for block creation
	validator := &Validator{
		ValidatorID: "val1",
		PublicKey:   NewPublicKey(AlgoEd25519, []byte("validator_key_material_32_bytes")),
		Stake:       1000,
	}

	node.Consensus.RegisterValidator(validator)

	senderAddr := node.Crypto.DeriveAddress(sender.KeyMaterial)
	receiverAddr := node.Crypto.DeriveAddress(receiver.KeyMaterial)
	node.Accounts[senderAddr] = &AccountState{Balance: 1000, Nonce: 0}
	node.Accounts[receiverAddr] = &AccountState{Balance: 0, Nonce: 0}

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
