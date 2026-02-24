package ripple

import (
	"testing"
)

func TestNewShard_InitializesCollections(t *testing.T) {
	shardID := 1
	shard := NewShard(shardID)

	if shard.ShardID != shardID {
		t.Fatalf("expected ShardID %d, got %d", shardID, shard.ShardID)
	}
	if shard.Blocks == nil {
		t.Fatalf("expected Blocks map to be initialized")
	}
	if shard.State == nil {
		t.Fatalf("expected State map to be initialized")
	}
	if shard.Validators == nil {
		t.Fatalf("expected Validators slice to be initialized")
	}
}

func TestShard_AddBlock_StoresByHashAndPreventsDuplicates(t *testing.T) {
	shard := NewShard(1)

	proposer := NewPublicKey(AlgoEd25519, []byte("validator_key_material_32_bytes"))
	block := NewBlock(1, "parent", proposer)

	// First add should succeed
	first := shard.AddBlock(block)
	if !first {
		t.Fatalf("expected first AddBlock call to return true")
	}
	if len(shard.Blocks) != 1 {
		t.Fatalf("expected 1 block in shard, got %d", len(shard.Blocks))
	}

	// Second add of the same block (same pointer, same hash) should be rejected
	second := shard.AddBlock(block)
	if second {
		t.Fatalf("expected second AddBlock call with same block to return false")
	}
}

