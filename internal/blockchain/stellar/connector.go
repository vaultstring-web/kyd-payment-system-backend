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

// NewConnector returns a stub connector (Stellar integration disabled).
func NewConnector(_, _ string, _ bool) (*Connector, error) {
	return &Connector{}, nil
}

// SubmitSettlement is a stub implementation for Stellar settlement.
func (c *Connector) SubmitSettlement(_ context.Context, _ *domain.Settlement) (*settlement.SettlementResult, error) {
	return nil, fmt.Errorf("Stellar connector temporarily disabled: settlement not submitted")
}

// CheckConfirmation is a stub implementation for Stellar confirmation.
func (c *Connector) CheckConfirmation(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("Stellar connector temporarily disabled: confirmation unavailable")
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
