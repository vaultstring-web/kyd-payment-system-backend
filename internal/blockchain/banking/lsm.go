package banking

import (
	"fmt"
	"math/big"
	"sort"
	"sync"
)

// ==============================================================================
// LIQUIDITY SAVINGS MECHANISM (LSM) - GRIDLOCK RESOLUTION
// ==============================================================================
// This module implements advanced algorithms to resolve gridlocks in Real-Time
// Gross Settlement (RTGS) systems. It detects circular dependencies (A->B->C->A)
// and multilateral netting opportunities to clear payments that would otherwise
// fail due to insufficient liquidity.
//
// Concepts:
// - Multilateral Netting: Calculate net positions for all participants.
// - Gridlock Resolution: Find a subset of payments that can be cleared simultaneously.
// - EAF (Early Arrival Facility): Prioritize payments to unblock queues.

type Obligation struct {
	ID       string
	Sender   string
	Receiver string
	Amount   *big.Int
	Priority int // 0 = Normal, 1 = Urgent, 2 = Critical
}

type ParticipantState struct {
	ID            string
	Balance       *big.Int
	ReservedFunds *big.Int
}

type GridlockResolver struct {
	Participants map[string]*ParticipantState
	Queue        []*Obligation
	mu           sync.RWMutex
}

func NewGridlockResolver() *GridlockResolver {
	return &GridlockResolver{
		Participants: make(map[string]*ParticipantState),
		Queue:        make([]*Obligation, 0),
	}
}

func (gr *GridlockResolver) AddParticipant(id string, initialBalance int64) {
	gr.mu.Lock()
	defer gr.mu.Unlock()
	gr.Participants[id] = &ParticipantState{
		ID:            id,
		Balance:       big.NewInt(initialBalance),
		ReservedFunds: big.NewInt(0),
	}
}

func (gr *GridlockResolver) AddObligation(id, sender, receiver string, amount int64, priority int) {
	gr.mu.Lock()
	defer gr.mu.Unlock()
	gr.Queue = append(gr.Queue, &Obligation{
		ID:       id,
		Sender:   sender,
		Receiver: receiver,
		Amount:   big.NewInt(amount),
		Priority: priority,
	})
}

// Resolve attempts to clear payments using Multilateral Netting
func (gr *GridlockResolver) Resolve() ([]string, error) {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	// 1. Calculate Net Positions assuming ALL queued payments are cleared
	netPositions := make(map[string]*big.Int)
	for id, p := range gr.Participants {
		netPositions[id] = new(big.Int).Set(p.Balance)
	}

	clearedIDs := make([]string, 0)
	
	// Sort queue by priority (high to low)
	sort.Slice(gr.Queue, func(i, j int) bool {
		return gr.Queue[i].Priority > gr.Queue[j].Priority
	})

	// 2. Simulation Step: Try to clear maximal subset
	// Simple greedy approach for demonstration:
	// Identify participants with negative net positions and remove their outgoing payments
	// until everyone is positive.
	
	// In a real LSM, this is a graph theory problem (finding cycles).
	// Here we use a standard banking algorithm: "Simulated Netting with Removal"
	
	// Copy queue for simulation
	activeObligations := make([]*Obligation, len(gr.Queue))
	copy(activeObligations, gr.Queue)

	for {
		// Calculate projected balances
		projectedBalances := make(map[string]*big.Int)
		for id, p := range gr.Participants {
			projectedBalances[id] = new(big.Int).Set(p.Balance)
		}

		for _, ob := range activeObligations {
			projectedBalances[ob.Sender].Sub(projectedBalances[ob.Sender], ob.Amount)
			projectedBalances[ob.Receiver].Add(projectedBalances[ob.Receiver], ob.Amount)
		}

		// Check if any participant is insolvent in this projection
		insolventID := ""
		maxShortfall := big.NewInt(0)

		for id, bal := range projectedBalances {
			if bal.Sign() < 0 {
				shortfall := new(big.Int).Abs(bal)
				if shortfall.Cmp(maxShortfall) > 0 {
					maxShortfall = shortfall
					insolventID = id
				}
			}
		}

		if insolventID == "" {
			// All valid! Commit settlement
			break
		}

		// Remove one outgoing payment from the most insolvent participant (last in queue/lowest priority)
		// and try again.
		removed := false
		for i := len(activeObligations) - 1; i >= 0; i-- {
			if activeObligations[i].Sender == insolventID {
				// Remove this obligation from the active set
				activeObligations = append(activeObligations[:i], activeObligations[i+1:]...)
				removed = true
				break
			}
		}

		if !removed {
			// Should not happen unless balance logic is broken or initial balance is negative
			return nil, fmt.Errorf("gridlock resolution failed: participant %s insolvent but no outgoing payments", insolventID)
		}
	}

	// 3. Apply settlements
	for _, ob := range activeObligations {
		gr.Participants[ob.Sender].Balance.Sub(gr.Participants[ob.Sender].Balance, ob.Amount)
		gr.Participants[ob.Receiver].Balance.Add(gr.Participants[ob.Receiver].Balance, ob.Amount)
		clearedIDs = append(clearedIDs, ob.ID)
	}

	// Update queue (remove cleared)
	newQueue := make([]*Obligation, 0)
	clearedSet := make(map[string]bool)
	for _, id := range clearedIDs {
		clearedSet[id] = true
	}
	
	for _, ob := range gr.Queue {
		if !clearedSet[ob.ID] {
			newQueue = append(newQueue, ob)
		}
	}
	gr.Queue = newQueue

	return clearedIDs, nil
}
