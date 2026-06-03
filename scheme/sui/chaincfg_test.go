package sui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetNetworkInfoIncludesDefaultPublicNodeEndpoints(t *testing.T) {
	tests := []struct {
		network     string
		networkName string
		networkID   string
		defaultURL  string
	}{
		{"sui:mainnet", "Sui Mainnet", "mainnet", "https://sui-rpc.publicnode.com"},
		{"sui:testnet", "Sui Testnet", "testnet", "https://sui-testnet-rpc.publicnode.com"},
		{"sui:localnet", "Sui Localnet", "localnet", "http://127.0.0.1:9000"},
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
}

func TestGetGaslessStablecoinType(t *testing.T) {
	coinType, ok := GetGaslessStablecoinType("sui:mainnet", "USDC")
	require.True(t, ok)
	require.Equal(t, USDCType, coinType)

	coinType, ok = GetGaslessStablecoinType("sui:mainnet", USDCType)
	require.True(t, ok)
	require.Equal(t, USDCType, coinType)

	coinType, ok = GetGaslessStablecoinType("sui:testnet", "USDC")
	require.True(t, ok)
	require.Equal(t, TestnetUSDCType, coinType)

	coinType, ok = GetGaslessStablecoinType("sui:testnet", TestnetUSDCType)
	require.True(t, ok)
	require.Equal(t, TestnetUSDCType, coinType)

	_, ok = GetGaslessStablecoinType("sui:testnet", USDCType)
	require.False(t, ok)

	require.Len(t, GetGaslessStablecoinTypes("sui:mainnet"), len(DefaultGaslessStablecoinTypeList))
	require.Equal(t, TestnetUSDCType, GetGaslessStablecoinTypes("sui:testnet")[0])
	_, ok = GetGaslessStablecoinType("sui:mainnet", "NOT_A_TOKEN")
	require.False(t, ok)
	require.Nil(t, GetNetworkInfo("sui:unknown"))
}

func TestGetGaslessStablecoinDecimals(t *testing.T) {
	decimals, ok := GetGaslessStablecoinDecimals("sui:mainnet", "USDC")
	require.True(t, ok)
	require.Equal(t, uint8(6), decimals)

	decimals, ok = GetGaslessStablecoinDecimals("sui:mainnet", USDCType)
	require.True(t, ok)
	require.Equal(t, uint8(6), decimals)
	require.Equal(t, "10000", MinimumGaslessStablecoinAmount(decimals).String())

	decimals, ok = GetGaslessStablecoinDecimals("sui:testnet", TestnetUSDCType)
	require.True(t, ok)
	require.Equal(t, uint8(6), decimals)

	_, ok = GetGaslessStablecoinDecimals("sui:mainnet", "NOT_A_TOKEN")
	require.False(t, ok)
}
