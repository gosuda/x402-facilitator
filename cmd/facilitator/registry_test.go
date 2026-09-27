package main

import (
	"testing"

	"github.com/gosuda/x402-facilitator/types"
	"github.com/stretchr/testify/require"
)

// TestNewFacilitatorRoutesNetworks pins the registry's routing errors, all
// of which originate here now that the registry is the chain dispatch.
// Error paths only — successful construction dials live endpoints and is
// covered by the chain packages' on-chain integration tests.
func TestNewFacilitatorRoutesNetworks(t *testing.T) {
	tests := []struct {
		name        string
		scheme      types.Scheme
		network     string
		errContains []string
		notContains string
	}{
		{
			name:        "unsupported scheme rejected before chain dispatch",
			scheme:      types.Scheme("upto"),
			network:     "eip155:84532",
			errContains: []string{"unsupported scheme", `"upto"`, `"exact"`},
		},
		{
			name:        "non-CAIP-2 network rejected by dispatch",
			scheme:      types.Exact,
			network:     "not-a-caip2-network",
			errContains: []string{"unsupported network", "CAIP-2"},
		},
		{name: "unknown eip155 network rejected by evm constructor", scheme: types.Exact, network: "eip155:999999"},
		// Right prefix, invalid rest: the chain constructor's own
		// validation error proves the dispatch reached it (the default
		// branch would have answered with the CAIP-2 message instead).
		{name: "solana prefix routes to solana constructor", scheme: types.Exact, network: "solana:", errContains: []string{"invalid Solana network"}},
		{name: "sui prefix routes to sui constructor", scheme: types.Exact, network: "sui:", notContains: "expected a CAIP-2 identifier", errContains: []string{"unsupported Sui network"}},
		{name: "tron prefix routes to tron constructor", scheme: types.Exact, network: "tron:mainnet", errContains: []string{"not implemented"}},
		{name: "casper prefix routes to casper constructor", scheme: types.Exact, network: "casper:", notContains: "expected a CAIP-2 identifier"},
		{name: "nano prefix routes to nano constructor", scheme: types.Exact, network: "nano:", notContains: "expected a CAIP-2 identifier", errContains: []string{"unsupported Nano network"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance, err := newFacilitator(tt.scheme, tt.network, "https://example.invalid", "")
			require.Error(t, err)
			require.Nil(t, instance)
			for _, substr := range tt.errContains {
				require.ErrorContains(t, err, substr)
			}
			if tt.notContains != "" {
				require.NotContains(t, err.Error(), tt.notContains)
			}
		})
	}
}
