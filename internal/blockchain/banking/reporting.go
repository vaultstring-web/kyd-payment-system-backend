package banking

import (
	"encoding/xml"
	"fmt"
	"time"
)

// ISOMessage represents a simplified ISO 20022 message structure
type ISOMessage struct {
	XMLName   xml.Name `xml:"Document"`
	MsgId     string   `xml:"CstmrCdtTrfInitn>GrpHdr>MsgId"`
	CreDtTm   string   `xml:"CstmrCdtTrfInitn>GrpHdr>CreDtTm"`
	NbOfTxs   int      `xml:"CstmrCdtTrfInitn>GrpHdr>NbOfTxs"`
	CtrlSum   float64  `xml:"CstmrCdtTrfInitn>GrpHdr>CtrlSum"`
	InitgPty  string   `xml:"CstmrCdtTrfInitn>GrpHdr>InitgPty>Nm"`
	Debtor    string   `xml:"CstmrCdtTrfInitn>PmtInf>Dbtr>Nm"`
	Creditor  string   `xml:"CstmrCdtTrfInitn>PmtInf>CdtTrfTxInf>Cdtr>Nm"`
	Amt       float64  `xml:"CstmrCdtTrfInitn>PmtInf>CdtTrfTxInf>Amt>InstdAmt"`
	Ccy       string   `xml:"CstmrCdtTrfInitn>PmtInf>CdtTrfTxInf>Amt>InstdAmt>Ccy,attr"`
}

// RegulatoryAuditor represents an automated auditor that monitors the blockchain
type RegulatoryAuditor struct {
	AuthorityName string
	Region        string
	AuditLog      []string
}

// NewRegulatoryAuditor creates a new auditor
func NewRegulatoryAuditor(name, region string) *RegulatoryAuditor {
	return &RegulatoryAuditor{
		AuthorityName: name,
		Region:        region,
		AuditLog:      make([]string, 0),
	}
}

// GenerateISO20022Report converts a transaction into an ISO 20022 XML report
// This demonstrates "Real-Time Regulatory Reporting" which is superior to T+1 batch reporting
func (ra *RegulatoryAuditor) GenerateISO20022Report(txID, sender, receiver string, amount int64, currency string) (string, error) {
	msg := ISOMessage{
		MsgId:    fmt.Sprintf("ISO-%s-%d", txID, time.Now().Unix()),
		CreDtTm:  time.Now().Format(time.RFC3339),
		NbOfTxs:  1,
		CtrlSum:  float64(amount) / 100.0, // Assuming 2 decimal places for fiat
		InitgPty: "KYD Payment System",
		Debtor:   sender,
		Creditor: receiver,
		Amt:      float64(amount) / 100.0,
		Ccy:      currency,
	}

	xmlData, err := xml.MarshalIndent(msg, "", "  ")
	if err != nil {
		return "", err
	}

	report := fmt.Sprintf("<!-- Real-Time Regulatory Report for %s -->\n%s", ra.AuthorityName, string(xmlData))
	ra.AuditLog = append(ra.AuditLog, report)
	return report, nil
}

// VerifyCompliance checks if a transaction meets the region's specific rules
// e.g., "Travel Rule" enforcement
func (ra *RegulatoryAuditor) VerifyCompliance(amount int64, senderKYC, receiverKYC ComplianceLevel) bool {
	// Travel Rule: For amounts > 1000, both sender and receiver must have Full KYC
	if amount > 100000 && (senderKYC < ComplianceLevelFull || receiverKYC < ComplianceLevelFull) {
		return false
	}
	return true
}
