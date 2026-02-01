package banking

import (
	"time"
)

// ComplianceLevel represents the KYC/AML level of an entity
type ComplianceLevel int

const (
	ComplianceLevelNone  ComplianceLevel = 0
	ComplianceLevelBasic ComplianceLevel = 1
	ComplianceLevelFull  ComplianceLevel = 2
	ComplianceLevelInst  ComplianceLevel = 3 // Institutional
)

// ComplianceProof represents a proof of regulatory compliance
type ComplianceProof struct {
	ProofID           string
	SubjectID         string // The wallet/entity ID
	ComplianceLevel   ComplianceLevel
	ApproverID        string // The Regulatory Authority ID
	ApproverSignature string
	Expiry            float64
	Restrictions      []string // e.g., "US-Only", "No-Gambling"
}

// IsValid checks if the proof is valid and not expired
func (cp *ComplianceProof) IsValid() bool {
	return float64(time.Now().Unix()) < cp.Expiry
}

// FreezeStatus represents the asset control status
type FreezeStatus int

const (
	FreezeStatusActive      FreezeStatus = 0
	FreezeStatusFrozen      FreezeStatus = 1 // Cannot send or receive
	FreezeStatusSendOnly    FreezeStatus = 2 // Can only send (e.g. liquidation)
	FreezeStatusReceiveOnly FreezeStatus = 3 // Can only receive
)

// ISO20022Metadata represents banking standard data fields
type ISO20022Metadata struct {
	MsgID          string // Message Identification
	CreditorAgent  string // BIC
	DebtorAgent    string // BIC
	RemittanceInfo string // Unstructured remittance info
	PurposeCode    string // e.g., SALA (Salary), TAXS (Tax)
	EndToEndID     string
	InstructionID  string
	FullXML        string // Complete XML payload
}

// ComplianceManager defines the interface for regulatory checks
type ComplianceManager interface {
	ValidateProof(proof *ComplianceProof) bool
	CheckFreezeStatus(subjectID string) FreezeStatus
	IsSanctioned(entityID string) bool
	ReportSuspiciousActivity(txID string, reason string)
}
