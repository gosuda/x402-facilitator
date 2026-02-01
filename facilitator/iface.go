package facilitator

import (
	"fmt"

	evmfacilitator "github.com/gosuda/x402-facilitator/facilitator/evm"
	solanafacilitator "github.com/gosuda/x402-facilitator/facilitator/solana"
	suifacilitator "github.com/gosuda/x402-facilitator/facilitator/sui"
	tronfacilitator "github.com/gosuda/x402-facilitator/facilitator/tron"
	"github.com/gosuda/x402-facilitator/types"
)

// NewFacilitator creates a new facilitator for the given scheme.
// It returns a types.SchemeNetworkFacilitator (SDK interface).
func NewFacilitator(scheme types.Scheme, network, rpcUrl string, privateKeyHex string) (types.SchemeNetworkFacilitator, error) {
	switch scheme {
	case types.EVM:
		return evmfacilitator.NewFacilitator(network, rpcUrl, privateKeyHex)
	case types.Solana:
		return solanafacilitator.NewFacilitator(network, rpcUrl, privateKeyHex)
	case types.Sui:
		return suifacilitator.NewFacilitator(network, rpcUrl, privateKeyHex)
	case types.Tron:
		return tronfacilitator.NewFacilitator(network, rpcUrl, privateKeyHex)
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", scheme)
	}
}
