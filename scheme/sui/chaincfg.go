package sui

import (
	"maps"
	"slices"
)

type NetworkInfo struct {
	Network            string
	NetworkName        string
	NetworkID          string
	ChainDigest        string
	DefaultURLs        []string
	StablecoinTypes    map[string]string
	StablecoinDecimals map[string]uint8
}

func GetNetworkInfo(network string) *NetworkInfo {
	info, ok := networkInfo[network]
	if !ok {
		return nil
	}
	info.DefaultURLs = slices.Clone(info.DefaultURLs)
	info.StablecoinTypes = maps.Clone(info.StablecoinTypes)
	info.StablecoinDecimals = maps.Clone(info.StablecoinDecimals)
	return &info
}

func GetNetworkName(network string) string {
	info := GetNetworkInfo(network)
	if info == nil {
		return ""
	}
	return info.NetworkName
}

func GetNetworkID(network string) string {
	info := GetNetworkInfo(network)
	if info == nil {
		return ""
	}
	return info.NetworkID
}

func GetDefaultURLs(network string) []string {
	info := GetNetworkInfo(network)
	if info == nil {
		return nil
	}
	return slices.Clone(info.DefaultURLs)
}

func GetGaslessStablecoinTypes(network string) []string {
	info := GetNetworkInfo(network)
	if info == nil {
		return nil
	}

	types := make([]string, 0, len(info.StablecoinTypes))
	seen := make(map[string]struct{}, len(info.StablecoinTypes))
	for _, symbol := range DefaultGaslessStablecoinSymbolList {
		if coinType, ok := stablecoinTypeBySymbol(info.StablecoinTypes, symbol); ok {
			types = append(types, coinType)
			seen[NormalizeType(coinType)] = struct{}{}
		}
	}
	symbols := make([]string, 0, len(info.StablecoinTypes))
	for symbol := range info.StablecoinTypes {
		symbols = append(symbols, symbol)
	}
	slices.Sort(symbols)
	for _, symbol := range symbols {
		coinType := info.StablecoinTypes[symbol]
		if _, ok := seen[NormalizeType(coinType)]; !ok {
			types = append(types, coinType)
		}
	}
	return types
}

func GetGaslessStablecoinType(network, asset string) (string, bool) {
	info := GetNetworkInfo(network)
	if info == nil {
		return "", false
	}
	for symbol, coinType := range info.StablecoinTypes {
		if NormalizeType(symbol) == NormalizeType(asset) {
			return coinType, true
		}
	}
	normalizedAsset := NormalizeType(asset)
	for _, coinType := range info.StablecoinTypes {
		if NormalizeType(coinType) == normalizedAsset {
			return coinType, true
		}
	}
	return "", false
}

func GetGaslessStablecoinDecimals(network, asset string) (uint8, bool) {
	info := GetNetworkInfo(network)
	if info == nil {
		return defaultGaslessStablecoinDecimals(asset)
	}
	for symbol, decimals := range info.StablecoinDecimals {
		if NormalizeType(symbol) == NormalizeType(asset) {
			return decimals, true
		}
	}
	normalizedAsset := NormalizeType(asset)
	for symbol, coinType := range info.StablecoinTypes {
		if NormalizeType(coinType) == normalizedAsset {
			decimals, ok := info.StablecoinDecimals[symbol]
			return decimals, ok
		}
	}
	return 0, false
}

func stablecoinTypeInMap(stablecoinTypes map[string]string, coinType string) (string, bool) {
	normalizedCoinType := NormalizeType(coinType)
	for symbol, candidate := range stablecoinTypes {
		if NormalizeType(candidate) == normalizedCoinType {
			return symbol, true
		}
	}
	return "", false
}

var networkInfo = map[string]NetworkInfo{
	"sui:mainnet": {
		Network:            "sui:mainnet",
		NetworkName:        "Sui Mainnet",
		NetworkID:          "mainnet",
		ChainDigest:        "4btiuiMPvEENsttpZC7CZ53DruC3MAgfznDbASZ7DR6S",
		DefaultURLs:        []string{"https://sui-rpc.publicnode.com", "https://fullnode.mainnet.sui.io:443"},
		StablecoinTypes:    defaultStablecoinTypesBySymbol(),
		StablecoinDecimals: defaultStablecoinDecimalsBySymbol(),
	},
	"sui:testnet": {
		Network:            "sui:testnet",
		NetworkName:        "Sui Testnet",
		NetworkID:          "testnet",
		ChainDigest:        "69WiPg3DAQiwdxfncX6wYQ2siKwAe6L9BZthQea3JNMD",
		DefaultURLs:        []string{"https://sui-testnet-rpc.publicnode.com", "https://fullnode.testnet.sui.io:443"},
		StablecoinTypes:    testnetStablecoinTypesBySymbol(),
		StablecoinDecimals: defaultStablecoinDecimalsBySymbol(),
	},
	"sui:localnet": {
		Network:            "sui:localnet",
		NetworkName:        "Sui Localnet",
		NetworkID:          "localnet",
		DefaultURLs:        []string{"http://127.0.0.1:9000"},
		StablecoinTypes:    defaultStablecoinTypesBySymbol(),
		StablecoinDecimals: defaultStablecoinDecimalsBySymbol(),
	},
}

func defaultStablecoinTypesBySymbol() map[string]string {
	return map[string]string{
		"USDC":     USDCType,
		"USDSUI":   USDSUIType,
		"SUI_USDE": SUIUSDEType,
		"USDY":     USDYType,
		"FDUSD":    FDUSDType,
		"AUSD":     AUSDType,
		"USDB":     USDBType,
	}
}

func testnetStablecoinTypesBySymbol() map[string]string {
	types := defaultStablecoinTypesBySymbol()
	types["USDC"] = TestnetUSDCType
	return types
}

func stablecoinTypeBySymbol(stablecoinTypes map[string]string, symbol string) (string, bool) {
	normalizedSymbol := NormalizeType(symbol)
	for candidate, coinType := range stablecoinTypes {
		if NormalizeType(candidate) == normalizedSymbol {
			return coinType, true
		}
	}
	return "", false
}

func defaultGaslessStablecoinDecimals(asset string) (uint8, bool) {
	normalizedAsset := NormalizeType(asset)
	for symbol, decimals := range defaultStablecoinDecimalsBySymbol() {
		if NormalizeType(symbol) == normalizedAsset {
			return decimals, true
		}
	}
	for symbol, coinType := range defaultStablecoinTypesBySymbol() {
		if NormalizeType(coinType) == normalizedAsset {
			decimals, ok := defaultStablecoinDecimalsBySymbol()[symbol]
			return decimals, ok
		}
	}
	return 0, false
}

func defaultStablecoinDecimalsBySymbol() map[string]uint8 {
	return map[string]uint8{
		"USDC":     6,
		"USDSUI":   6,
		"SUI_USDE": 6,
		"USDY":     6,
		"FDUSD":    6,
		"AUSD":     6,
		"USDB":     6,
	}
}
