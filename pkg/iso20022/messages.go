package iso20022

import (
	"encoding/xml"
	"fmt"
	"time"
)

// Simplified ISO 20022 structures for demonstration

type Document struct {
	XMLName xml.Name `xml:"urn:iso:std:iso:20022:tech:xsd:pacs.008.001.08 Document"`
	FIToFICstmrCdtTrf FIToFICstmrCdtTrf `xml:"FIToFICstmrCdtTrf"`
}

type FIToFICstmrCdtTrf struct {
	GrpHdr      GroupHeader          `xml:"GrpHdr"`
	CdtTrfTxInf CreditTransferTxInfo `xml:"CdtTrfTxInf"`
}

type GroupHeader struct {
	MsgId   string    `xml:"MsgId"`
	CreDtTm time.Time `xml:"CreDtTm"`
	NbOfTxs int       `xml:"NbOfTxs"`
}

type CreditTransferTxInfo struct {
	PmtId    PaymentIdentification `xml:"PmtId"`
	IntrBkSttlmAmt Amount          `xml:"IntrBkSttlmAmt"`
	Dbtr     PartyIdentification   `xml:"Dbtr"`
	Cdtr     PartyIdentification   `xml:"Cdtr"`
}

type PaymentIdentification struct {
	InstrId string `xml:"InstrId"`
	EndToEndId string `xml:"EndToEndId"`
	TxId    string `xml:"TxId"`
}

type Amount struct {
	Ccy   string  `xml:"Ccy,attr"`
	Value float64 `xml:",chardata"`
}

type PartyIdentification struct {
	Nm string `xml:"Nm"`
	Id PartyId `xml:"Id"`
}

type PartyId struct {
	OrgId OrgId `xml:"OrgId"`
}

type OrgId struct {
	AnyBIC string `xml:"AnyBIC"`
}

// GeneratePacs008 generates a mock pacs.008 (Financial Institution to Financial Institution Customer Credit Transfer) XML message
func GeneratePacs008(txID string, sender string, receiver string, amount float64, currency string) (string, error) {
	doc := Document{
		FIToFICstmrCdtTrf: FIToFICstmrCdtTrf{
			GrpHdr: GroupHeader{
				MsgId:   fmt.Sprintf("MSG-%s", txID),
				CreDtTm: time.Now(),
				NbOfTxs: 1,
			},
			CdtTrfTxInf: CreditTransferTxInfo{
				PmtId: PaymentIdentification{
					InstrId:    fmt.Sprintf("INSTR-%s", txID),
					EndToEndId: fmt.Sprintf("E2E-%s", txID),
					TxId:       txID,
				},
				IntrBkSttlmAmt: Amount{
					Ccy:   currency,
					Value: amount,
				},
				Dbtr: PartyIdentification{
					Nm: "Sender Bank", // Simplified
					Id: PartyId{OrgId: OrgId{AnyBIC: "SENDERBIC"}},
				},
				Cdtr: PartyIdentification{
					Nm: "Receiver Bank", // Simplified
					Id: PartyId{OrgId: OrgId{AnyBIC: "RECEIVERBIC"}},
				},
			},
		},
	}

	output, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(output), nil
}
