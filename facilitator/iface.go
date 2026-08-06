package facilitator

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosuda/x402-facilitator/types"
)

type Facilitator interface {
	Verify(ctx context.Context, payment *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentVerifyResponse, error)
	Settle(ctx context.Context, payment *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentSettleResponse, error)
	Supported() *types.SupportedResponse
}

// ReadinessChecker is optional: a facilitator that can say whether it is presently able to settle
// implements it, and /ready reports what it says. Kept out of Facilitator on purpose - adding a
// method there would break every implementation to serve one endpoint, and an implementation that
// cannot meaningfully answer should not be forced to pretend.
//
// Readiness is not liveness. A process can be perfectly healthy and still unable to settle: the
// RPC may be unreachable, or the fee payer may be out of gas. A load balancer needs to know the
// difference, because restarting the first case fixes nothing.
type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

func NewFacilitator(scheme types.Scheme, network, rpcUrl string, privateKeyHex string) (Facilitator, error) {
	if scheme != types.Exact {
		return nil, fmt.Errorf("unsupported scheme %q (only %q is implemented)", scheme, types.Exact)
	}

	// Route by CAIP-2 network prefix
	switch {
	case strings.HasPrefix(network, "eip155:"):
		return NewEVMFacilitator(network, rpcUrl, privateKeyHex)
	case strings.HasPrefix(network, "solana:"):
		return NewSolanaFacilitator(network, rpcUrl, privateKeyHex)
	case strings.HasPrefix(network, "sui:"):
		return NewSuiFacilitator(network, rpcUrl, privateKeyHex)
	case strings.HasPrefix(network, "tron:"):
		return NewTronFacilitator(network, rpcUrl, privateKeyHex)
	default:
		return nil, fmt.Errorf("unsupported network %q: expected a CAIP-2 identifier (eip155:*, solana:*, sui:*, tron:*)", network)
	}
}
