package stellar

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"kyd/internal/blockchain/banking"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AegisNetConsensus implements ASV-BFT Consensus
type AegisNetConsensus struct {
	Simulator            *AegisNetSimulator
	PQCheckpointInterval int
	StakeCapFraction     float64
	ComplianceManager    banking.ComplianceManager
	ZKVerifier           *banking.ZKProofVerifier
}

// NewAegisNetConsensus creates a new consensus instance
func NewAegisNetConsensus(sim *AegisNetSimulator) *AegisNetConsensus {
	return &AegisNetConsensus{
		Simulator:            sim,
		PQCheckpointInterval: 10,
		StakeCapFraction:     0.1,
		ComplianceManager:    banking.NewComplianceManager(),
		ZKVerifier:           banking.NewZKProofVerifier(),
	}
}

// ValidateBlockParallel checks validity using parallel processing for high throughput
func (anc *AegisNetConsensus) ValidateBlockParallel(block *MicroBlock) bool {
	// 1. Check if Proposer is valid validator
	var proposer *Validator
	for _, v := range anc.Simulator.Validators {
		if v.ValidatorID == block.ProposerID {
			proposer = v
			break
		}
	}
	if proposer == nil {
		return false
	}

	// 2. Parallel Transaction Verification
	// Simulate massive parallel signature verification and compliance checks
	txCount := len(block.Transactions)
	if txCount > 0 {
		results := make(chan bool, txCount)
		for _, tx := range block.Transactions {
			go func(t *ConfidentialTransaction) {
				// Simulate signature verification cost
				// time.Sleep(1 * time.Microsecond)

				// Compliance Check
				if anc.ComplianceManager.IsSanctioned(t.SenderZKAddress) ||
					anc.ComplianceManager.IsSanctioned(t.ReceiverZKAddress) {
					results <- false
					return
				}

				// Smart Contract Execution
				if t.SmartContract != nil {
					if err := anc.ExecuteSmartContract(t); err != nil {
						fmt.Printf("[CONSENSUS] Smart Contract Failed: %v\n", err)
						results <- false
						return
					}
				}

				results <- true
			}(tx)
		}

		// Gather results
		validTxCount := 0
		for i := 0; i < txCount; i++ {
			if <-results {
				validTxCount++
			} else {
				// If any tx is invalid (sanctioned), slash validator and reject block
				anc.SlashValidator(block.ProposerID, "Included sanctioned transaction (Parallel Check)")
				return false
			}
		}
	}

	// 3. Check Double Signing (Simulated)
	return true
}

// ValidateBlock checks validity and slashes proposer if invalid
func (anc *AegisNetConsensus) ValidateBlock(block *MicroBlock) bool {
	// 1. Check if Proposer is valid validator
	var proposer *Validator
	for _, v := range anc.Simulator.Validators {
		if v.ValidatorID == block.ProposerID {
			proposer = v
			break
		}
	}
	if proposer == nil {
		return false
	}

	// 2. Check Banking Compliance (Sanctions)
	if block.Transactions != nil {
		for _, tx := range block.Transactions {
			if anc.ComplianceManager.IsSanctioned(tx.SenderZKAddress) ||
				anc.ComplianceManager.IsSanctioned(tx.ReceiverZKAddress) {
				// Invalid block containing sanctioned entity
				anc.SlashValidator(block.ProposerID, "Included sanctioned transaction")
				return false
			}

			// Smart Contract Execution
			if tx.SmartContract != nil {
				if err := anc.ExecuteSmartContract(tx); err != nil {
					fmt.Printf("[CONSENSUS] Smart Contract Failed: %v\n", err)
					return false
				}
			}
		}
	}

	// 3. Check Double Signing (Simulated)
	// In real system, check if proposer signed another block at same height/slot

	return true
}

// SlashValidator penalizes a validator
func (anc *AegisNetConsensus) SlashValidator(validatorID, reason string) {
	for _, v := range anc.Simulator.Validators {
		if v.ValidatorID == validatorID {
			v.PerformanceScore *= 0.5 // Halve score
			v.Stake /= 2              // Slash 50% stake
			v.SlashingHistory = append(v.SlashingHistory, fmt.Sprintf("%s: %s", time.Now().Format(time.RFC3339), reason))
			v.IsHonest = false // Mark as dishonest
			fmt.Printf("[CONSENSUS] SLASHED Validator %s: %s\n", validatorID, reason)
			break
		}
	}
}

// SelectCommitteeWithCaps selects committee with stake caps and VRF
func (anc *AegisNetConsensus) SelectCommitteeWithCaps(epochSeed string, committeeSize int) []string {
	validators := anc.Simulator.Validators
	var totalStake int64 = 0
	for _, v := range validators {
		totalStake += v.Stake
	}
	cap := int64(anc.StakeCapFraction * float64(totalStake))

	type ticket struct {
		score       float64
		validatorID string
	}
	var tickets []ticket

	vrf := &VRF{}

	for _, validator := range validators {
		effectiveStake := validator.EffectiveStake(cap)

		// VRF for ranking
		output, _ := vrf.Prove(validator.SecretKey, epochSeed)
		// Parse first 8 bytes of hex output to uint64, then normalize
		bytes, _ := hex.DecodeString(output)
		if len(bytes) < 8 {
			continue
		}
		val := binary.BigEndian.Uint64(bytes[:8])
		score := float64(val) / float64(math.MaxUint64) // Normalize to [0,1)

		baseSeats := int((float64(effectiveStake) / float64(totalStake)) * float64(committeeSize))
		fractionalPart := ((float64(effectiveStake) / float64(totalStake)) * float64(committeeSize)) - float64(baseSeats)

		if score < fractionalPart {
			baseSeats++
		}

		for i := 0; i < baseSeats; i++ {
			ticketScore := score + (float64(i) * 0.0001)
			tickets = append(tickets, ticket{score: ticketScore, validatorID: validator.ValidatorID})
		}
	}

	sort.Slice(tickets, func(i, j int) bool {
		return tickets[i].score < tickets[j].score
	})

	var selected []string
	for i := 0; i < committeeSize && i < len(tickets); i++ {
		selected = append(selected, tickets[i].validatorID)
	}

	// Deduplicate
	seen := make(map[string]bool)
	var uniqueSelected []string
	for _, id := range selected {
		if !seen[id] {
			uniqueSelected = append(uniqueSelected, id)
			seen[id] = true
		}
	}
	selected = uniqueSelected

	// Fill if needed (simplified random fill)
	if len(selected) < committeeSize {
		remaining := []string{}
		for _, v := range validators {
			found := false
			for _, s := range selected {
				if s == v.ValidatorID {
					found = true
					break
				}
			}
			if !found {
				remaining = append(remaining, v.ValidatorID)
			}
		}

		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(remaining), func(i, j int) { remaining[i], remaining[j] = remaining[j], remaining[i] })

		needed := committeeSize - len(selected)
		if needed > len(remaining) {
			needed = len(remaining)
		}
		selected = append(selected, remaining[:needed]...)
	}

	return selected
}

// ComputeDAGWeight computes cumulative weight in DAG
func (anc *AegisNetConsensus) ComputeDAGWeight(blockID string, shardID int) float64 {
	shard := anc.Simulator.Shards[shardID]
	visited := make(map[string]bool)
	totalWeight := 0.0

	var dfs func(string)
	dfs = func(currentID string) {
		if visited[currentID] {
			return
		}
		block, exists := shard.MicroBlocks[currentID]
		if !exists {
			return
		}
		visited[currentID] = true
		totalWeight += block.Weight
		for _, parent := range block.ParentRefs {
			dfs(parent)
		}
	}

	dfs(blockID)
	return totalWeight
}

func (anc *AegisNetConsensus) ResolveDAGConflicts(shardID int) []string {
	shard := anc.Simulator.Shards[shardID]
	blockWeights := make(map[string]float64)
	ids := make([]string, 0, len(shard.MicroBlocks))
	for id := range shard.MicroBlocks {
		blockWeights[id] = anc.ComputeDAGWeight(id, shardID)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return blockWeights[ids[i]] > blockWeights[ids[j]]
	})
	return ids
}

// MEVDemocratization handles MEV auctions
type MEVDemocratization struct {
	Simulator     *AegisNetSimulator
	TreasuryCut   float64
	StakerCut     float64
	UserRebateCut float64
	ProposerCut   float64
}

// NewMEVDemocratization creates new MEV system
func NewMEVDemocratization(sim *AegisNetSimulator) *MEVDemocratization {
	return &MEVDemocratization{
		Simulator:     sim,
		TreasuryCut:   0.1,
		StakerCut:     0.4,
		UserRebateCut: 0.3,
		ProposerCut:   0.2,
	}
}

// RunSequencerAuction runs a sealed-bid sequencer auction
func (mev *MEVDemocratization) RunSequencerAuction(shardID, slot int) *SequencerAuction {
	auction := &SequencerAuction{
		AuctionID:  fmt.Sprintf("auction_%d_%d", shardID, slot),
		ShardID:    shardID,
		SlotNumber: slot,
		IsOpen:     true,
		Bids:       make(map[string]int64),
		SealedBids: make(map[string]string),
	}

	// Simulate bid submission (simplified)
	committee := mev.Simulator.Consensus.SelectCommitteeWithCaps("seed", 5)
	for _, valID := range committee {
		// In a real system, these would be external inputs
		bidAmount := rand.Int63n(5000) // Random bid 0-5000 atomic units
		nonce := fmt.Sprintf("%d", rand.Int63())

		// 1. Submit sealed bid
		data := fmt.Sprintf("%d%s", bidAmount, nonce)
		hash := sha256.Sum256([]byte(data))
		bidHash := hex.EncodeToString(hash[:])
		auction.SubmitSealedBid(valID, bidHash)

		// 2. Reveal bid
		auction.RevealBid(valID, bidAmount, nonce)
	}

	// Distribute revenue
	if auction.Winner != "" {
		mev.DistributeMEVRevenue(auction)
	}

	return auction
}

// DistributeMEVRevenue distributes MEV revenue
func (mev *MEVDemocratization) DistributeMEVRevenue(auction *SequencerAuction) {
	totalRevenue := auction.WinningBid

	// Treasury Cut
	treasuryShare := float64(totalRevenue) * mev.TreasuryCut
	mev.Simulator.ProtocolTreasury += treasuryShare

	// Staker Cut
	stakerShare := float64(totalRevenue) * mev.StakerCut
	mev.DistributeToStakers(int64(stakerShare))

	// User Rebates
	userShare := float64(totalRevenue) * mev.UserRebateCut
	mev.DistributeUserRebates(int64(userShare), auction.ShardID)

	// Proposer Cut
	proposerShare := float64(totalRevenue) * mev.ProposerCut
	if auction.Winner != "" {
		mev.Simulator.ValidatorRewards[auction.Winner] += proposerShare
	}

	auction.RevenueDistribution = map[string]int64{
		"treasury": int64(treasuryShare),
		"stakers":  int64(stakerShare),
		"users":    int64(userShare),
		"proposer": int64(proposerShare),
	}
}

// DistributeToStakers distributes rewards to stakers
func (mev *MEVDemocratization) DistributeToStakers(amount int64) {
	var totalStake int64 = 0
	for _, v := range mev.Simulator.Validators {
		totalStake += v.Stake
	}
	if totalStake == 0 {
		return
	}
	for _, v := range mev.Simulator.Validators {
		share := (float64(v.Stake) / float64(totalStake)) * float64(amount)
		mev.Simulator.ValidatorRewards[v.ValidatorID] += share
	}
}

// DistributeUserRebates distributes rebates to users
func (mev *MEVDemocratization) DistributeUserRebates(amount int64, shardID int) {
	shard := mev.Simulator.Shards[shardID]
	if len(shard.PendingTransactions) == 0 {
		return
	}
	rebate := float64(amount) / float64(len(shard.PendingTransactions))

	// Sample first 10 for simulation
	count := 0
	for _, tx := range shard.PendingTransactions {
		shard.MEVPool[tx.SenderZKAddress] += int64(rebate)
		count++
		if count >= 10 {
			break
		}
	}
}

// ExecuteSmartContract executes the banking-grade smart contract logic
func (anc *AegisNetConsensus) ExecuteSmartContract(tx *ConfidentialTransaction) error {
	if tx.SmartContract == nil {
		return nil
	}

	instructions := strings.Split(tx.SmartContract.Code, ";")
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
			// In simulation, we assume Metadata carries KYC info or we check compliance manager
			// For now, simple simulation: assume valid if level <= 3 (since we don't have user object here)
			level, _ := strconv.Atoi(parts[1])
			if level > 3 {
				return fmt.Errorf("KYC level insufficient (simulated)")
			}
		case "LIMIT_MAX":
			if len(parts) < 2 {
				return fmt.Errorf("invalid LIMIT_MAX arguments")
			}
			limit, _ := strconv.ParseInt(parts[1], 10, 64)
			if tx.Amount > limit {
				return fmt.Errorf("transaction amount %d exceeds contract limit %d", tx.Amount, limit)
			}
		case "CHECK_SANCTIONS":
			if anc.ComplianceManager.IsSanctioned(tx.SenderZKAddress) {
				return fmt.Errorf("sender is sanctioned (contract check)")
			}
		case "REQUIRE_ISO20022":
			if tx.ISO20022Data == nil {
				return fmt.Errorf("transaction missing required ISO20022 metadata")
			}
			// In a real system, we would validate specific fields like PurposeCode
			if tx.ISO20022Data.PurposeCode == "" {
				return fmt.Errorf("ISO20022 PurposeCode required")
			}
		default:
			// Allow extension
		}
	}
	return nil
}

// AegisNetSimulator is the main simulator struct
type AegisNetSimulator struct {
	NumShards        int
	NumValidators    int
	CommitteeSize    int
	Validators       []*Validator
	Shards           map[int]*ShardState
	Consensus        *AegisNetConsensus
	MEVSystem        *MEVDemocratization
	ProtocolTreasury float64
	ValidatorRewards map[string]float64
}

// NewAegisNetSimulator creates a new simulator
func NewAegisNetSimulator(numShards, numValidators, committeeSize int) *AegisNetSimulator {
	sim := &AegisNetSimulator{
		NumShards:        numShards,
		NumValidators:    numValidators,
		CommitteeSize:    committeeSize,
		Shards:           make(map[int]*ShardState),
		ValidatorRewards: make(map[string]float64),
	}

	// Initialize Validators
	hc := &HybridCrypto{}
	for i := 0; i < numValidators; i++ {
		pub, priv, _ := hc.GenerateKeypair()
		sim.Validators = append(sim.Validators, &Validator{
			ValidatorID:      fmt.Sprintf("val_%d", i),
			Stake:            int64(rand.Intn(10000) + 1000), // Random stake
			PublicKey:        pub,
			SecretKey:        priv,
			PerformanceScore: 1.0,
			IsHonest:         true,
		})
	}

	// Initialize Shards
	for i := 0; i < numShards; i++ {
		sim.Shards[i] = NewShardState(i)
	}

	sim.Consensus = NewAegisNetConsensus(sim)
	sim.MEVSystem = NewMEVDemocratization(sim)

	return sim
}
