package banking

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// LiquidityBridge represents a cross-chain bridge for instant settlement
type LiquidityBridge struct {
	ChainA      string // e.g., "Stellar-KYD"
	ChainB      string // e.g., "Ripple-KYD"
	LockedFunds map[string]int64
	Mutex       sync.Mutex
}

// NewLiquidityBridge creates a new bridge
func NewLiquidityBridge(chainA, chainB string) *LiquidityBridge {
	return &LiquidityBridge{
		ChainA:      chainA,
		ChainB:      chainB,
		LockedFunds: make(map[string]int64),
	}
}

// AtomicSwap represents a hash time-locked contract (HTLC) for cross-chain value transfer
type AtomicSwap struct {
	SwapID       string
	Sender       string
	Receiver     string
	Amount       int64
	HashLock     string
	TimeLock     int64
	State        string // "PENDING", "COMPLETED", "REFUNDED"
}

// InitiateSwap locks funds on the source chain
// This simulates the "Lock" phase of an atomic swap
func (lb *LiquidityBridge) InitiateSwap(sender, receiver string, amount int64, hashLock string) *AtomicSwap {
	lb.Mutex.Lock()
	defer lb.Mutex.Unlock()

	swapID := fmt.Sprintf("SWAP-%d-%s", time.Now().UnixNano(), sender[:4])
	lb.LockedFunds[swapID] = amount

	return &AtomicSwap{
		SwapID:   swapID,
		Sender:   sender,
		Receiver: receiver,
		Amount:   amount,
		HashLock: hashLock,
		TimeLock: time.Now().Add(1 * time.Hour).Unix(),
		State:    "PENDING",
	}
}

// CompleteSwap releases funds on the destination chain using the preimage
// This simulates the "Claim" phase
func (lb *LiquidityBridge) CompleteSwap(swap *AtomicSwap, preimage string) error {
	lb.Mutex.Lock()
	defer lb.Mutex.Unlock()

	if swap.State != "PENDING" {
		return errors.New("swap not pending")
	}

	// In a real system, we would hash the preimage and compare with HashLock
	// For simulation, we assume if provided, it's valid
	if preimage == "" {
		return errors.New("invalid preimage")
	}

	delete(lb.LockedFunds, swap.SwapID)
	swap.State = "COMPLETED"
	return nil
}

// ForeignExchangeRate simulates an oracle for FX rates
func (lb *LiquidityBridge) GetExchangeRate(fromCcy, toCcy string) float64 {
	// Hardcoded simulation rates
	rates := map[string]float64{
		"ZMW-MWK": 65.0,
		"MWK-ZMW": 0.015,
		"CNY-MWK": 240.0,
		"MWK-CNY": 0.0042,
	}
	
	key := fmt.Sprintf("%s-%s", fromCcy, toCcy)
	if rate, ok := rates[key]; ok {
		return rate
	}
	return 1.0
}
