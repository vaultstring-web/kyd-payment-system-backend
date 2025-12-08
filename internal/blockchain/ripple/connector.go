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

// NewConnector returns a stub error for now (ripple disabled).
func NewConnector(_, _ string) (*Connector, error) {
	return &Connector{}, nil
}

// SubmitSettlement is a stub implementation for Ripple Settle -- currently returns a not implemented error.
func (c *Connector) SubmitSettlement(_ context.Context, _ *domain.Settlement) (*settlement.SettlementResult, error) {
	return nil, fmt.Errorf("Ripple connector temporarily disabled: settlement not submitted")
}

// CheckConfirmation is a stub implementation for Ripple Settle -- currently returns a not implemented error.
func (c *Connector) CheckConfirmation(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("Ripple connector temporarily disabled: confirmation unavailable")
}
