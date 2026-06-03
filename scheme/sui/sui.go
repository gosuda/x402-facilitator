package sui

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// USDCType is the canonical native USDC type currently supported by Sui's
	// gasless stablecoin transfer flow.
	USDCType        = "0xdba34672e30cb065b1f93e3ab55318768fd6fef66c15942c9f7cb846e2f900e7::usdc::USDC"
	TestnetUSDCType = "0xa1ec7fc00a6f40db9693ad1415d0c193ad3906494428cf252621037bd7117e29::usdc::USDC"
	USDSUIType      = "0x44f838219cf67b058f3b37907b655f226153c18e33dfcd0da559a844fea9b1c1::usdsui::USDSUI"
	SUIUSDEType     = "0x41d587e5336f1c86cad50d38a7136db99333bb9bda91cea4ba69115defeb1402::sui_usde::SUI_USDE"
	USDYType        = "0x960b531667636f39e85867775f52f6b1f220a058c4de786905bdf761e06a56bb::usdy::USDY"
	FDUSDType       = "0xf16e6b723f242ec745dfd7634ad072c42d5c1d9ac9d62a39c381303eaa57693a::fdusd::FDUSD"
	AUSDType        = "0x2053d08c1e2bd02791056171aab0fd12bd7cd7efad2ab8f6b9c8902f14df2ff2::ausd::AUSD"
	USDBType        = "0xe14726c336e81b32328e92afc37345d159f5b550b09fa92bd43640cfdd0a0cfd::usdb::USDB"
)

var (
	ErrEmptyTransaction = errors.New("empty_transaction")

	ErrEmptySignature = errors.New("empty_signature")

	// DefaultGaslessStablecoinTypeList mirrors the Sui gasless stablecoin
	// allowlist from the public docs. The protocol configuration can change, so
	// facilitator deployments should treat this as a default policy snapshot.
	DefaultGaslessStablecoinSymbolList = []string{
		"USDC",
		"USDSUI",
		"SUI_USDE",
		"USDY",
		"FDUSD",
		"AUSD",
		"USDB",
	}
	DefaultGaslessStablecoinTypeList = []string{
		USDCType,
		USDSUIType,
		SUIUSDEType,
		USDYType,
		FDUSDType,
		AUSDType,
		USDBType,
	}
	DefaultGaslessStablecoinTypes = defaultGaslessStablecoinTypes()
)

// Payload is the x402 Sui "exact" scheme payload. Transaction is the base64
// encoded Sui TransactionData bytes. Signature is the base64 encoded Sui
// serialized signature, containing the scheme flag, signature, and public key.
type Payload struct {
	Signature   string `json:"signature"`
	Transaction string `json:"transaction"`
}

func NewPayload(signature string, txBytes []byte) (*Payload, error) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return nil, ErrEmptySignature
	}
	if len(txBytes) == 0 {
		return nil, ErrEmptyTransaction
	}

	return &Payload{
		Signature:   signature,
		Transaction: base64.StdEncoding.EncodeToString(txBytes),
	}, nil
}

func NewSignedPayload(txBytes []byte, signer Signer) (*Payload, error) {
	if signer == nil {
		return nil, errors.New("nil_signer")
	}
	signature, err := signer.SignTransaction(txBytes)
	if err != nil {
		return nil, err
	}
	return NewPayload(signature, txBytes)
}

func ParsePayload(raw []byte) (*Payload, error) {
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid_payload: %w", err)
	}
	if strings.TrimSpace(payload.Signature) == "" {
		return nil, ErrEmptySignature
	}
	if strings.TrimSpace(payload.Transaction) == "" {
		return nil, ErrEmptyTransaction
	}
	return &payload, nil
}

func PayloadFromMap(value map[string]interface{}) (*Payload, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return ParsePayload(raw)
}

func (p *Payload) DecodeTransaction() ([]byte, error) {
	if p == nil || strings.TrimSpace(p.Transaction) == "" {
		return nil, ErrEmptyTransaction
	}
	txBytes, err := base64.StdEncoding.DecodeString(p.Transaction)
	if err != nil {
		return nil, fmt.Errorf("invalid_transaction_encoding: %w", err)
	}
	if len(txBytes) == 0 {
		return nil, ErrEmptyTransaction
	}
	return txBytes, nil
}

func NormalizeType(asset string) string {
	return strings.ToLower(strings.TrimSpace(asset))
}

func NormalizeAddress(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" || address == "0x" {
		return ""
	}
	address = strings.TrimPrefix(address, "0x")
	if address == "" || len(address) > 64 {
		return ""
	}
	if len(address)%2 != 0 {
		address = "0" + address
	}
	if _, err := hex.DecodeString(address); err != nil {
		return ""
	}
	if len(address) < 64 {
		address = strings.Repeat("0", 64-len(address)) + address
	}
	return "0x" + address
}

func IsDefaultGaslessStablecoinType(asset string) bool {
	_, ok := DefaultGaslessStablecoinTypes[NormalizeType(asset)]
	return ok
}

func defaultGaslessStablecoinTypes() map[string]struct{} {
	types := make(map[string]struct{}, len(DefaultGaslessStablecoinTypeList))
	for _, asset := range DefaultGaslessStablecoinTypeList {
		types[NormalizeType(asset)] = struct{}{}
	}
	return types
}
