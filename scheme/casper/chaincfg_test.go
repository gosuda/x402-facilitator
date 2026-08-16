package casper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetNetworkInfoIncludesDefaultFacilitatorEndpoints(t *testing.T) {
	tests := []struct {
		network     string
		networkName string
		networkID   string
		defaultURL  string
	}{
		{NetworkMainnet, "Casper Mainnet", "casper", DefaultFacilitatorURL},
		{NetworkTestnet, "Casper Testnet", "casper-test", DefaultFacilitatorURL},
	}

	for _, tt := range tests {
		t.Run(tt.network, func(t *testing.T) {
			info := GetNetworkInfo(tt.network)
			require.NotNil(t, info)
			require.Equal(t, tt.network, info.Network)
			require.Equal(t, tt.networkName, info.NetworkName)
			require.Equal(t, tt.networkID, info.NetworkID)
			require.NotEmpty(t, info.DefaultURLs)
			require.Equal(t, tt.defaultURL, info.DefaultURLs[0])
			require.Equal(t, tt.networkName, GetNetworkName(tt.network))
			require.Equal(t, tt.networkID, GetNetworkID(tt.network))
			require.Equal(t, info.DefaultURLs, GetDefaultURLs(tt.network))
		})
	}

	require.Nil(t, GetNetworkInfo("casper:unknown"))
	require.Empty(t, GetNetworkName("casper:unknown"))
	require.Empty(t, GetNetworkID("casper:unknown"))
	require.Nil(t, GetDefaultURLs("casper:unknown"))
}

func TestIsCasperNetwork(t *testing.T) {
	tests := []struct {
		network string
		want    bool
	}{
		{NetworkMainnet, true},
		{NetworkTestnet, true},
		{"casper:unknown", true},
		{" casper:casper", true},
		{"eip155:8453", false},
		{"sui:mainnet", false},
		{"solana:mainnet", false},
		{"casper", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.network, func(t *testing.T) {
			require.Equal(t, tt.want, IsCasperNetwork(tt.network))
		})
	}
}

func TestParseNetwork(t *testing.T) {
	tests := []struct {
		name          string
		network       string
		wantNamespace string
		wantReference string
		wantErr       bool
	}{
		{name: "mainnet", network: NetworkMainnet, wantNamespace: "casper", wantReference: "casper"},
		{name: "testnet", network: NetworkTestnet, wantNamespace: "casper", wantReference: "casper-test"},
		{name: "padded", network: "  casper:casper-test  ", wantNamespace: "casper", wantReference: "casper-test"},
		{name: "unsupported reference", network: "casper:casper-dev", wantErr: true},
		{name: "wrong namespace", network: "eip155:8453", wantErr: true},
		{name: "missing reference", network: "casper:", wantErr: true},
		{name: "not caip2", network: "casper", wantErr: true},
		{name: "empty", network: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, reference, err := ParseNetwork(tt.network)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidNetwork)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantNamespace, namespace)
			require.Equal(t, tt.wantReference, reference)
		})
	}
}

func TestGetAssetContract(t *testing.T) {
	const contract = "1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff001"

	tests := []struct {
		name    string
		network string
		asset   string
		want    string
		wantOK  bool
	}{
		{name: "bare contract hash", network: NetworkMainnet, asset: contract, want: contract, wantOK: true},
		{name: "hash prefixed contract", network: NetworkTestnet, asset: "hash-" + contract, want: contract, wantOK: true},
		{name: "unknown symbol", network: NetworkMainnet, asset: "NOT_A_TOKEN", wantOK: false},
		{name: "malformed hash", network: NetworkMainnet, asset: "hash-1b2c", wantOK: false},
		{name: "unsupported network", network: "casper:unknown", asset: contract, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := GetAssetContract(tt.network, tt.asset)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetAssetDecimals(t *testing.T) {
	decimals, ok := GetAssetDecimals(NetworkMainnet, WCSPRSymbol)
	require.True(t, ok)
	require.Equal(t, MoteDecimals, decimals)
	require.Equal(t, uint8(9), decimals)

	decimals, ok = GetAssetDecimals(NetworkTestnet, "wcspr")
	require.True(t, ok)
	require.Equal(t, MoteDecimals, decimals)

	_, ok = GetAssetDecimals(NetworkMainnet, "NOT_A_TOKEN")
	require.False(t, ok)

	_, ok = GetAssetDecimals("casper:unknown", WCSPRSymbol)
	require.False(t, ok)
}

func TestGetAssetTypesIsDeploymentConfigured(t *testing.T) {
	// CEP-18 contract hashes are supplied per deployment, so no contract hash
	// is hard-coded for either network.
	require.Empty(t, GetAssetTypes(NetworkMainnet))
	require.Empty(t, GetAssetTypes(NetworkTestnet))
	require.Nil(t, GetAssetTypes("casper:unknown"))
}

func TestGetNetworkInfoReturnsCopy(t *testing.T) {
	info := GetNetworkInfo(NetworkMainnet)
	require.NotNil(t, info)
	info.DefaultURLs[0] = "http://mutated.invalid"
	info.AssetDecimals[WCSPRSymbol] = 0

	fresh := GetNetworkInfo(NetworkMainnet)
	require.NotNil(t, fresh)
	require.Equal(t, DefaultFacilitatorURL, fresh.DefaultURLs[0])
	require.Equal(t, MoteDecimals, fresh.AssetDecimals[WCSPRSymbol])
}
