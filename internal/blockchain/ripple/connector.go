// Package ripple provides support for integrating with the Ripple network.
package ripple

import (
	"context"
	"fmt"

	"kyd/internal/domain"
	"kyd/internal/settlement"
)

// Connector provides stub methods for Ripple blockchain integration.
type Connector struct{}

// NewConnector returns a stub connector suitable for local development.
func NewConnector(_, _ string) (*Connector, error) {
	return &Connector{}, nil
}

// SubmitSettlement returns a stub success result for local development.
func (c *Connector) SubmitSettlement(_ context.Context, s *domain.Settlement) (*settlement.SettlementResult, error) {
	return &settlement.SettlementResult{
		TxHash:    fmt.Sprintf("stub-ripple-%s", s.ID.String()),
		Confirmed: true,
	}, nil
}

// CheckConfirmation always returns true in local development.
func (c *Connector) CheckConfirmation(_ context.Context, _ string) (bool, error) {
	return true, nil
}
