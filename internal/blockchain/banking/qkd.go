package banking

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// ==============================================================================
// QUANTUM KEY DISTRIBUTION (QKD) NETWORK SIMULATOR
// ==============================================================================
// This module simulates a QKD network for unbreakable encryption using
// Quantum Entanglement (E91 Protocol simulation) and One-Time Pad (OTP).
//
// Features:
// - Quantum Key Generation: Simulates qubit measurement and key sifting.
// - OTP Encryption: Theoretically unbreakable encryption for critical messages.
// - Key Rotation: Automatic key destruction after use.

type QKDNode struct {
	ID        string
	KeyStore  map[string][]byte // Map[PeerID]SharedKey
	mu        sync.RWMutex
}

type QKDNetwork struct {
	Nodes map[string]*QKDNode
}

var GlobalQKDNetwork = &QKDNetwork{
	Nodes: make(map[string]*QKDNode),
}

func NewQKDNode(id string) *QKDNode {
	node := &QKDNode{
		ID:       id,
		KeyStore: make(map[string][]byte),
	}
	GlobalQKDNetwork.Nodes[id] = node
	return node
}

// SimulateKeyExchange simulates the BB84 or E91 protocol to establish a shared secret
func (n *QKDNode) SimulateKeyExchange(peerID string) error {
	peer, exists := GlobalQKDNetwork.Nodes[peerID]
	if !exists {
		return fmt.Errorf("peer node %s not found in QKD network", peerID)
	}

	// 1. Simulate Qubit Transmission & Measurement (simplified)
	// In reality, this involves quantum channels and basis reconciliation.
	// Here we generate a high-entropy 256-bit key.
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return err
	}

	// 2. Store Key on both sides (Simulating successful sifting)
	n.mu.Lock()
	n.KeyStore[peerID] = key
	n.mu.Unlock()

	peer.mu.Lock()
	peer.KeyStore[n.ID] = key
	peer.mu.Unlock()

	return nil
}

// EncryptOTP uses One-Time Pad encryption (XOR)
// Security: Perfect Secrecy if key is random, same length as message, and never reused.
func (n *QKDNode) EncryptOTP(peerID string, plaintext []byte) ([]byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	key, exists := n.KeyStore[peerID]
	if !exists {
		return nil, fmt.Errorf("no quantum key established with %s", peerID)
	}

	if len(plaintext) > len(key) {
		return nil, fmt.Errorf("message too long for quantum key (need %d bytes, have %d)", len(plaintext), len(key))
	}

	ciphertext := make([]byte, len(plaintext))
	for i := 0; i < len(plaintext); i++ {
		ciphertext[i] = plaintext[i] ^ key[i]
	}

	// DESTROY KEY AFTER USE (Critical for OTP)
	// In a real system, we'd consume only the used bytes, but here we invalidate the whole key for simplicity
	delete(n.KeyStore, peerID)

	return ciphertext, nil
}

// DecryptOTP uses One-Time Pad decryption (XOR)
func (n *QKDNode) DecryptOTP(peerID string, ciphertext []byte) ([]byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	key, exists := n.KeyStore[peerID]
	if !exists {
		return nil, fmt.Errorf("no quantum key available (was it already used?)")
	}

	if len(ciphertext) > len(key) {
		return nil, fmt.Errorf("ciphertext too long for quantum key")
	}

	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i++ {
		plaintext[i] = ciphertext[i] ^ key[i]
	}

	// DESTROY KEY AFTER USE
	delete(n.KeyStore, peerID)

	return plaintext, nil
}

// Helper to print hex
func Hex(b []byte) string {
	return hex.EncodeToString(b)
}

// StartKeyRefreshLoop automatically refreshes keys every X seconds
func (n *QKDNode) StartKeyRefreshLoop(peerID string, interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			n.SimulateKeyExchange(peerID)
		}
	}()
}
