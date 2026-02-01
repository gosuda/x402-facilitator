package tron

import (
	"context"
	"fmt"

	x402types "github.com/coinbase/x402/go/types"

	"github.com/gosuda/x402-facilitator/types"
)

type Facilitator struct {
}

func NewFacilitator(network string, url string, privateKeyHex string) (*Facilitator, error) {
	return &Facilitator{}, nil
}

func (t *Facilitator) Verify(ctx context.Context, payload x402types.PaymentPayload, req x402types.PaymentRequirements) (*types.VerifyResponse, error) {
	return nil, fmt.Errorf("tron verify not implemented")
}

func (t *Facilitator) Settle(ctx context.Context, payload x402types.PaymentPayload, req x402types.PaymentRequirements) (*types.SettleResponse, error) {
	return nil, fmt.Errorf("tron settle not implemented")
}

// Scheme returns the scheme identifier for this facilitator.
func (t *Facilitator) Scheme() string {
	return string(types.Tron)
}

// CaipFamily returns the CAIP family pattern this facilitator supports.
func (t *Facilitator) CaipFamily() string {
	return "tron:*"
}

// GetExtra returns mechanism-specific extra data for the supported kinds endpoint.
func (t *Facilitator) GetExtra(network types.Network) map[string]interface{} {
	return nil
}

// GetSigners returns signer addresses used by this facilitator for a given network.
func (t *Facilitator) GetSigners(network types.Network) []string {
	return []string{}
}
