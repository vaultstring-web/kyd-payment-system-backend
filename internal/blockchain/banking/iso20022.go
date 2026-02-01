package banking

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ISO20022Validator provides validation for banking message standards
type ISO20022Validator struct {
	bicRegex *regexp.Regexp
}

func NewISO20022Validator() *ISO20022Validator {
	// Simple regex for BIC (Business Identifier Code): 4 letters, 2 country code, 2 location, optional 3 branch
	return &ISO20022Validator{
		bicRegex: regexp.MustCompile(`^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$`),
	}
}

// ValidateMetadata checks if the ISO20022 metadata conforms to standards
func (v *ISO20022Validator) ValidateMetadata(meta *ISO20022Metadata) error {
	if meta == nil {
		return nil // Optional
	}

	if meta.MsgID == "" {
		return errors.New("MsgID is required")
	}

	if meta.CreditorAgent != "" && !v.bicRegex.MatchString(meta.CreditorAgent) {
		return errors.New("invalid CreditorAgent BIC")
	}

	if meta.DebtorAgent != "" && !v.bicRegex.MatchString(meta.DebtorAgent) {
		return errors.New("invalid DebtorAgent BIC")
	}

	if len(meta.EndToEndID) > 35 {
		return errors.New("EndToEndID too long (max 35 chars)")
	}

	// Validate Purpose Codes (Subset)
	validCodes := map[string]bool{
		"SALA": true, // Salary
		"TAXS": true, // Tax
		"CASH": true, // Cash Management
		"DIVI": true, // Dividend
		"INTC": true, // Intra-Company
		"SUPP": true, // Supplier Payment
		"TRAD": true, // Trade
	}

	if meta.PurposeCode != "" {
		if !validCodes[strings.ToUpper(meta.PurposeCode)] {
			return errors.New("invalid or unsupported PurposeCode")
		}
	}

	return nil
}

// GeneratePacs008 creates a simulated XML payload for settlement (Financial Institution To Financial Institution Customer Credit Transfer)
func (v *ISO20022Validator) GeneratePacs008(msgID, amount, currency, debtorName, debtorIBAN, debtorBIC, creditorName, creditorIBAN, creditorBIC, remitInfo string) (string, error) {
	// Basic validation
	if !v.bicRegex.MatchString(debtorBIC) || !v.bicRegex.MatchString(creditorBIC) {
		return "", errors.New("invalid BIC")
	}

	// In a real system, we would marshal this to XML using a struct
	// For this simulation, we'll return a structured XML-like string to demonstrate format compliance
	xml := fmt.Sprintf(`
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.08">
    <FIToFICstmrCdtTrf>
        <GrpHdr>
            <MsgId>%s</MsgId>
            <CreDtTm>%s</CreDtTm>
            <NbOfTxs>1</NbOfTxs>
        </GrpHdr>
        <CdtTrfTxInf>
            <PmtId>
                <EndToEndId>%s</EndToEndId>
            </PmtId>
            <IntrBkSttlmAmt Ccy="%s">%s</IntrBkSttlmAmt>
            <Dbtr>
                <Nm>%s</Nm>
                <Acct><Id><IBAN>%s</IBAN></Id></Acct>
                <Agt><FinInstnId><BICFI>%s</BICFI></FinInstnId></Agt>
            </Dbtr>
            <Cdtr>
                <Nm>%s</Nm>
                <Acct><Id><IBAN>%s</IBAN></Id></Acct>
                <Agt><FinInstnId><BICFI>%s</BICFI></FinInstnId></Agt>
            </Cdtr>
            <RmtInf>
                <Ustrd>%s</Ustrd>
            </RmtInf>
        </CdtTrfTxInf>
    </FIToFICstmrCdtTrf>
</Document>`,
		msgID,
		time.Now().Format(time.RFC3339),
		msgID, // Using MsgId as EndToEndId for simulation simplicity
		currency,
		amount,
		debtorName, debtorIBAN, debtorBIC,
		creditorName, creditorIBAN, creditorBIC,
		remitInfo,
	)

	return strings.TrimSpace(xml), nil
}
