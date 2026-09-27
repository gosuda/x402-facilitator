package main

import (
	"fmt"
	"strings"

	"github.com/gosuda/x402-facilitator/scheme"
	casperfacilitator "github.com/gosuda/x402-facilitator/scheme/casper/facilitator"
	evmfacilitator "github.com/gosuda/x402-facilitator/scheme/evm/facilitator"
	nanofacilitator "github.com/gosuda/x402-facilitator/scheme/nano/facilitator"
	solanafacilitator "github.com/gosuda/x402-facilitator/scheme/solana/facilitator"
	suifacilitator "github.com/gosuda/x402-facilitator/scheme/sui/facilitator"
	tronfacilitator "github.com/gosuda/x402-facilitator/scheme/tron/facilitator"
	"github.com/gosuda/x402-facilitator/types"
)

// newFacilitator is the distribution-level chain dispatch. There is no
// generic factory anymore: library consumers compose the chain facilitator
// they need explicitly from its scheme package, and this registry is where
// the binary composes all of them. Routing eip155 here is what pulls
// go-ethereum into this binary's build — a choice that belongs to the
// distribution, not the library.
func newFacilitator(scheme types.Scheme, network, rpcURL, privateKeyHex string) (scheme.Facilitator, error) {
	if scheme != types.Exact {
		return nil, fmt.Errorf("unsupported scheme %q (only %q is implemented)", scheme, types.Exact)
	}

	// Route by CAIP-2 network prefix
	switch {
	case strings.HasPrefix(network, "eip155:"):
		return evmfacilitator.NewEVMFacilitator(network, rpcURL, privateKeyHex)
	case strings.HasPrefix(network, "solana:"):
		return solanafacilitator.NewSolanaFacilitator(network, rpcURL, privateKeyHex)
	case strings.HasPrefix(network, "sui:"):
		return suifacilitator.NewSuiFacilitator(network, rpcURL, privateKeyHex)
	case strings.HasPrefix(network, "tron:"):
		return tronfacilitator.NewTronFacilitator(network, rpcURL, privateKeyHex)
	case strings.HasPrefix(network, "casper:"):
		return casperfacilitator.NewCasperFacilitator(network, rpcURL, privateKeyHex)
	case strings.HasPrefix(network, "nano:"):
		return nanofacilitator.NewNanoFacilitator(network, rpcURL, privateKeyHex)
	default:
		return nil, fmt.Errorf("unsupported network %q: expected a CAIP-2 identifier (eip155:*, solana:*, sui:*, tron:*, casper:*, nano:*)", network)
	}
}
