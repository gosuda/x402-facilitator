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
	case strings.HasPrefix(network, "casper:"):
		return NewCasperFacilitator(network, rpcUrl, privateKeyHex)
	default:
		return nil, fmt.Errorf("unsupported network %q: expected a CAIP-2 identifier (eip155:*, solana:*, sui:*, tron:*, casper:*)", network)
	}
}
