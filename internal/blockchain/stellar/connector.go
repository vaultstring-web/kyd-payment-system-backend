// Package stellar provides support for integrating with the Stellar network.
package stellar

import (
	"context"
	"fmt"

	"github.com/stellar/go/clients/horizonclient"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
	"kyd/internal/domain"
	"kyd/internal/settlement"
)

// Connector provides stub methods for Stellar blockchain integration.
type Connector struct {
	client      *horizonclient.Client
	issuerKP    *keypair.Full
	networkPass string
}

// NewConnector returns a stub connector suitable for local development.
func NewConnector(_, _ string, _ bool) (*Connector, error) {
	return &Connector{}, nil
}

// SubmitSettlement returns a stub success result for local development.
func (c *Connector) SubmitSettlement(_ context.Context, s *domain.Settlement) (*settlement.SettlementResult, error) {
	return &settlement.SettlementResult{
		TxHash:    fmt.Sprintf("stub-stellar-%s", s.ID.String()),
		Confirmed: true,
	}, nil
}

// CheckConfirmation always returns true in local development.
func (c *Connector) CheckConfirmation(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (c *Connector) getAsset(currency domain.Currency) txnbuild.Asset {
	if currency == "XLM" {
		return txnbuild.NativeAsset{}
	}

	return txnbuild.CreditAsset{
		Code:   string(currency),
		Issuer: c.issuerKP.Address(),
	}
}
