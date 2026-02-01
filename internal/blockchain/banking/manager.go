package banking

import (
	"fmt"
	"sync"
)

// StandardComplianceManager is a concrete implementation of ComplianceManager
type StandardComplianceManager struct {
	mu            sync.RWMutex
	sanctionList  map[string]bool
	freezeStatuses map[string]FreezeStatus
	suspiciousActivityLog []string
}

func NewComplianceManager() *StandardComplianceManager {
	return &StandardComplianceManager{
		sanctionList:   make(map[string]bool),
		freezeStatuses: make(map[string]FreezeStatus),
		suspiciousActivityLog: make([]string, 0),
	}
}

// ValidateProof checks if the proof is valid, not expired, and signed by a trusted authority
func (m *StandardComplianceManager) ValidateProof(proof *ComplianceProof) bool {
	if proof == nil {
		return false
	}
	// In a real system, verify ApproverSignature against trusted CA
	// Here we just check expiry
	return proof.IsValid()
}

// CheckFreezeStatus returns the freeze status of an entity
func (m *StandardComplianceManager) CheckFreezeStatus(subjectID string) FreezeStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if status, exists := m.freezeStatuses[subjectID]; exists {
		return status
	}
	return FreezeStatusActive
}

// ReportSuspiciousActivity logs suspicious activity
func (m *StandardComplianceManager) ReportSuspiciousActivity(txID string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	logEntry := fmt.Sprintf("TxID: %s | Reason: %s", txID, reason)
	m.suspiciousActivityLog = append(m.suspiciousActivityLog, logEntry)
	// In a real system, this would trigger an alert to the compliance team
}

// AddSanction adds an entity to the sanctions list
func (m *StandardComplianceManager) AddSanction(entityID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sanctionList[entityID] = true
}

// IsSanctioned checks if an entity is sanctioned
func (m *StandardComplianceManager) IsSanctioned(entityID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sanctionList[entityID]
}

// SetFreezeStatus sets the freeze status for an entity
func (m *StandardComplianceManager) SetFreezeStatus(entityID string, status FreezeStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.freezeStatuses[entityID] = status
}
