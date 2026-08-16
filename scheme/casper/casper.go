// Package casper implements the x402 v2 "exact" scheme primitives for the
// Casper network family (CAIP-2 `casper:casper` and `casper:casper-test`).
//
// Settlement uses wCSPR, a CEP-18 token with 9 decimals; the atomic unit is a
// mote. All amounts crossing the wire are integer motes encoded as decimal
// strings, mirroring the rest of this facilitator.
package casper

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

const (
	// MoteDecimals is the number of decimals of CSPR and wCSPR. One CSPR is
	// 10^9 motes.
	MoteDecimals uint8 = 9

	// AccountHashPrefix is the textual prefix of a Casper account hash.
	AccountHashPrefix = "account-hash-"
	// ContractHashPrefix is the textual prefix of a Casper contract hash.
	ContractHashPrefix = "hash-"

	// WCSPRSymbol is the symbol of the settlement asset.
	WCSPRSymbol = "WCSPR"
)

var (
	ErrEmptyPayload      = errors.New("empty_payload")
	ErrEmptySignature    = errors.New("empty_signature")
	ErrEmptyTransaction  = errors.New("empty_transaction")
	ErrInvalidAddress    = errors.New("invalid_address")
	ErrInvalidAsset      = errors.New("invalid_asset")
	ErrInvalidAmount     = errors.New("invalid_amount")
	ErrInvalidNetwork    = errors.New("invalid_network")
	ErrUnsupportedScheme = errors.New("unsupported_scheme")

	// ErrSubMotePrecision is returned when a decimal CSPR amount carries more
	// precision than a mote can represent. Truncating would silently change
	// the payment amount, so the conversion fails instead.
	ErrSubMotePrecision = errors.New("sub_mote_precision")
)

// Payload is the x402 Casper "exact" scheme payload. Deploy is the JSON
// encoded signed Casper Deploy (or Transaction) the payer authorized, and
// Signature is the payer signature in hex, as produced by the Casper x402
// client SDKs.
type Payload struct {
	Signature string          `json:"signature"`
	Payer     string          `json:"payer"`
	Deploy    json.RawMessage `json:"deploy"`
}

// ParsePayload decodes and validates a raw x402 Casper payload.
func ParsePayload(raw []byte) (*Payload, error) {
	if len(raw) == 0 {
		return nil, ErrEmptyPayload
	}
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid_payload: %w", err)
	}
	if strings.TrimSpace(payload.Signature) == "" {
		return nil, ErrEmptySignature
	}
	if len(payload.Deploy) == 0 || strings.TrimSpace(string(payload.Deploy)) == "null" {
		return nil, ErrEmptyTransaction
	}
	if NormalizeAddress(payload.Payer) == "" {
		return nil, ErrInvalidAddress
	}
	return &payload, nil
}

// PayloadFromMap decodes an x402 payload carried as a generic JSON object.
func PayloadFromMap(value map[string]interface{}) (*Payload, error) {
	if value == nil {
		return nil, ErrEmptyPayload
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return ParsePayload(raw)
}

// NormalizeAsset lower-cases an asset identifier for comparison.
func NormalizeAsset(asset string) string {
	return strings.ToLower(strings.TrimSpace(asset))
}

// NormalizeAddress validates and canonicalizes a Casper payer or recipient
// identifier. Accepted forms are an `account-hash-<64 hex>` string and a hex
// encoded public key (ed25519 `01` + 64 hex, secp256k1 `02` + 66 hex). The
// empty string is returned when the input is not a valid Casper address.
func NormalizeAddress(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(address, AccountHashPrefix); ok {
		if !isHex(rest, 64) {
			return ""
		}
		return AccountHashPrefix + rest
	}
	switch {
	case strings.HasPrefix(address, "01") && isHex(address, 66):
		return address
	case strings.HasPrefix(address, "02") && isHex(address, 68):
		return address
	default:
		return ""
	}
}

// NormalizeContractHash validates and canonicalizes a CEP-18 contract hash.
// Both the bare 64 hex character form and the `hash-` prefixed form are
// accepted; the bare lower-case form is returned.
func NormalizeContractHash(contract string) string {
	contract = strings.ToLower(strings.TrimSpace(contract))
	contract = strings.TrimPrefix(contract, ContractHashPrefix)
	contract = strings.TrimPrefix(contract, "contract-")
	if !isHex(contract, 64) {
		return ""
	}
	return contract
}

// ParseMotes parses an integer mote amount encoded as a decimal string.
func ParseMotes(amount string) (*big.Int, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return nil, ErrInvalidAmount
	}
	motes, ok := new(big.Int).SetString(amount, 10)
	if !ok || motes.Sign() < 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAmount, amount)
	}
	return motes, nil
}

// CSPRToMotes converts a decimal CSPR amount to motes. Amounts carrying more
// than MoteDecimals fractional digits are rejected with ErrSubMotePrecision
// rather than truncated, so a payment can never be silently under-settled.
func CSPRToMotes(amount string) (*big.Int, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return nil, ErrInvalidAmount
	}
	if strings.HasPrefix(amount, "-") {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAmount, amount)
	}
	whole, frac, hasFrac := strings.Cut(amount, ".")
	if whole == "" {
		whole = "0"
	}
	if !isDigits(whole) || (hasFrac && !isDigits(frac)) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAmount, amount)
	}
	if len(frac) > int(MoteDecimals) {
		if strings.Trim(frac[MoteDecimals:], "0") != "" {
			return nil, fmt.Errorf("%w: %s has more than %d decimals", ErrSubMotePrecision, amount, MoteDecimals)
		}
		frac = frac[:MoteDecimals]
	}
	frac += strings.Repeat("0", int(MoteDecimals)-len(frac))
	motes, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrInvalidAmount, amount)
	}
	return motes, nil
}

// MotesToCSPR renders an integer mote amount as a decimal CSPR string. The
// conversion is exact: no rounding is applied and trailing fractional zeros
// are trimmed.
func MotesToCSPR(motes *big.Int) string {
	if motes == nil {
		return "0"
	}
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(MoteDecimals)), nil)
	whole, frac := new(big.Int).QuoRem(new(big.Int).Abs(motes), unit, new(big.Int))
	sign := ""
	if motes.Sign() < 0 {
		sign = "-"
	}
	if frac.Sign() == 0 {
		return sign + whole.String()
	}
	digits := strings.TrimRight(fmt.Sprintf("%0*s", MoteDecimals, frac.String()), "0")
	return sign + whole.String() + "." + digits
}

func isHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
