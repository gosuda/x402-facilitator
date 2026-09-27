// Package nano implements the Nano (XNO) network binding for the x402 "exact"
// scheme. Nano is a feeless DAG ledger: the payer broadcasts its own signed
// send block and the block on the ledger IS the spent row. There is no memo
// field, so a payment is bound to a specific invoice by the pay-to destination
// (a per-invoice Nano account) and the amount — both read back from the block,
// never from request bodies. The facilitator only reads the public ledger; it
// never holds, moves or signs anything, because the settlement already happened
// at broadcast time.
package nano

import "strings"

const (
	// NetworkMainnet is the CAIP-2 identifier of Nano mainnet, matching the
	// upstream x402 exact-on-nano spec (x402-foundation/x402#3432).
	NetworkMainnet = "nano:mainnet"

	// NetworkNamespace is the CAIP-2 namespace of the Nano network family.
	NetworkNamespace = "nano"

	// AssetXNO is the settlement asset identifier (a native coin, not a token).
	AssetXNO = "XNO"

	// XNORawDecimals is the exponent of the raw atomic unit (1 XNO = 1e30 raw).
	XNORawDecimals = 30

	// MinIndependentEndpoints is the minimum number of independent RPC nodes a
	// verification must consult. Verification fails closed with fewer.
	MinIndependentEndpoints = 2
)

// NetworkInfo describes a supported Nano network and the independent public
// RPC endpoints used to verify a payment block on it.
type NetworkInfo struct {
	// Network is the CAIP-2 identifier, e.g. "nano:mainnet".
	Network string
	// NetworkName is the human readable network name.
	NetworkName string
	// DefaultURLs are independent public Nano RPC endpoints tried in order.
	// The verifier consults MinIndependentEndpoints of them before it trusts
	// a block.
	DefaultURLs []string
}

// DefaultMainnetEndpoints are two independent public Nano RPC nodes. They are
// deliberately from different operators so a single operator's error cannot
// manufacture a false acceptance.
var DefaultMainnetEndpoints = []string{
	"https://rpc.nano.to",
	"https://rainstorm.city/api",
}

// networkInfo holds the supported Nano networks.
var networkInfo = map[string]*NetworkInfo{
	NetworkMainnet: {
		Network:     NetworkMainnet,
		NetworkName: "Nano mainnet",
		DefaultURLs: append([]string(nil), DefaultMainnetEndpoints...),
	},
}

// IsNanoNetwork reports whether the CAIP-2 identifier belongs to the Nano
// namespace.
func IsNanoNetwork(network string) bool {
	return strings.HasPrefix(strings.TrimSpace(network), NetworkNamespace+":")
}

// ParseNetwork splits a CAIP-2 Nano identifier into namespace and reference,
// validating that the network is supported.
func ParseNetwork(network string) (namespace string, reference string, err error) {
	network = strings.TrimSpace(network)
	namespace, reference, ok := strings.Cut(network, ":")
	if !ok || namespace != NetworkNamespace || reference == "" {
		return "", "", ErrInvalidNetwork
	}
	if _, supported := networkInfo[network]; !supported {
		return "", "", ErrInvalidNetwork
	}
	return namespace, reference, nil
}

// GetNetworkInfo returns a copy of the configuration for a Nano network, or
// nil when the network is not supported.
func GetNetworkInfo(network string) *NetworkInfo {
	info, ok := networkInfo[strings.TrimSpace(network)]
	if !ok {
		return nil
	}
	urls := append([]string(nil), info.DefaultURLs...)
	return &NetworkInfo{Network: info.Network, NetworkName: info.NetworkName, DefaultURLs: urls}
}

// GetNetworkName returns the human readable name of a Nano network.
func GetNetworkName(network string) string {
	info := GetNetworkInfo(network)
	if info == nil {
		return ""
	}
	return info.NetworkName
}
