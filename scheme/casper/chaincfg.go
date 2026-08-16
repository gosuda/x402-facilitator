package casper

import (
	"maps"
	"slices"
	"strings"
)

const (
	// NetworkMainnet is the CAIP-2 identifier of Casper mainnet.
	NetworkMainnet = "casper:casper"
	// NetworkTestnet is the CAIP-2 identifier of Casper testnet.
	NetworkTestnet = "casper:casper-test"

	// NetworkNamespace is the CAIP-2 namespace of the Casper network family.
	NetworkNamespace = "casper"

	// DefaultFacilitatorURL is the hosted Casper x402 facilitator operated on
	// cspr.cloud. Deployments can override it via configuration.
	DefaultFacilitatorURL = "https://x402-facilitator.cspr.cloud"
)

// NetworkInfo describes a supported Casper network.
type NetworkInfo struct {
	Network     string
	NetworkName string
	// NetworkID is the Casper chain name used in deploy headers.
	NetworkID string
	// DefaultURLs are the facilitator endpoints tried in order.
	DefaultURLs []string
	// AssetContracts maps an asset symbol to its CEP-18 contract hash. It is
	// empty by default: CEP-18 tokens are addressed by contract hash and the
	// wCSPR hash is supplied per deployment through configuration, so no
	// contract hash is hard-coded here.
	AssetContracts map[string]string
	// AssetDecimals maps an asset symbol to its decimals.
	AssetDecimals map[string]uint8
}

// IsCasperNetwork reports whether the CAIP-2 identifier belongs to the Casper
// namespace.
func IsCasperNetwork(network string) bool {
	return strings.HasPrefix(strings.TrimSpace(network), NetworkNamespace+":")
}

// ParseNetwork splits a CAIP-2 Casper identifier into its namespace and
// reference, validating that the network is supported.
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

// GetNetworkInfo returns a copy of the configuration for a Casper network, or
// nil when the network is not supported.
func GetNetworkInfo(network string) *NetworkInfo {
	info, ok := networkInfo[strings.TrimSpace(network)]
	if !ok {
		return nil
	}
	info.DefaultURLs = slices.Clone(info.DefaultURLs)
	info.AssetContracts = maps.Clone(info.AssetContracts)
	info.AssetDecimals = maps.Clone(info.AssetDecimals)
	return &info
}

// GetNetworkName returns the human readable name of a Casper network.
func GetNetworkName(network string) string {
	info := GetNetworkInfo(network)
	if info == nil {
		return ""
	}
	return info.NetworkName
}

// GetNetworkID returns the Casper chain name of a network.
func GetNetworkID(network string) string {
	info := GetNetworkInfo(network)
	if info == nil {
		return ""
	}
	return info.NetworkID
}

// GetDefaultURLs returns the default facilitator endpoints for a network.
func GetDefaultURLs(network string) []string {
	info := GetNetworkInfo(network)
	if info == nil {
		return nil
	}
	return slices.Clone(info.DefaultURLs)
}

// GetAssetTypes returns the settlement assets supported on a network, as
// CEP-18 contract hashes.
func GetAssetTypes(network string) []string {
	info := GetNetworkInfo(network)
	if info == nil {
		return nil
	}
	symbols := make([]string, 0, len(info.AssetContracts))
	for symbol := range info.AssetContracts {
		symbols = append(symbols, symbol)
	}
	slices.Sort(symbols)
	assets := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		assets = append(assets, info.AssetContracts[symbol])
	}
	return assets
}

// GetAssetContract resolves an asset symbol or contract hash to the canonical
// CEP-18 contract hash configured for the network.
func GetAssetContract(network, asset string) (string, bool) {
	info := GetNetworkInfo(network)
	if info == nil {
		return "", false
	}
	normalized := NormalizeAsset(asset)
	for symbol, contract := range info.AssetContracts {
		if NormalizeAsset(symbol) == normalized {
			return contract, true
		}
	}
	hash := NormalizeContractHash(asset)
	if hash == "" {
		return "", false
	}
	for _, contract := range info.AssetContracts {
		if NormalizeContractHash(contract) == hash {
			return contract, true
		}
	}
	// No symbol mapping is configured for this contract hash. CEP-18 tokens
	// are addressed by contract hash on Casper, so a syntactically valid hash
	// is returned as-is and the deployment's asset allowlist decides whether
	// it is accepted.
	return hash, true
}

// GetAssetDecimals resolves the decimals of an asset symbol or contract hash.
func GetAssetDecimals(network, asset string) (uint8, bool) {
	info := GetNetworkInfo(network)
	if info == nil {
		return 0, false
	}
	normalized := NormalizeAsset(asset)
	for symbol, decimals := range info.AssetDecimals {
		if NormalizeAsset(symbol) == normalized {
			return decimals, true
		}
	}
	hash := NormalizeContractHash(asset)
	if hash == "" {
		return 0, false
	}
	for symbol, contract := range info.AssetContracts {
		if NormalizeContractHash(contract) == hash {
			decimals, ok := info.AssetDecimals[symbol]
			return decimals, ok
		}
	}
	return 0, false
}

var networkInfo = map[string]NetworkInfo{
	NetworkMainnet: {
		Network:        NetworkMainnet,
		NetworkName:    "Casper Mainnet",
		NetworkID:      "casper",
		DefaultURLs:    []string{DefaultFacilitatorURL},
		AssetContracts: map[string]string{},
		AssetDecimals:  map[string]uint8{WCSPRSymbol: MoteDecimals},
	},
	NetworkTestnet: {
		Network:        NetworkTestnet,
		NetworkName:    "Casper Testnet",
		NetworkID:      "casper-test",
		DefaultURLs:    []string{DefaultFacilitatorURL},
		AssetContracts: map[string]string{},
		AssetDecimals:  map[string]uint8{WCSPRSymbol: MoteDecimals},
	},
}
