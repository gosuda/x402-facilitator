package nano

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ErrInvalidAmount is returned for an XNO amount that is not a positive
// decimal number with at most XNORawDecimals fractional digits.
var ErrInvalidAmount = errors.New("invalid_xno_amount")

// XNOToRaw converts a human XNO amount, such as "0.01", to its raw atomic
// decimal string (1 XNO = 1e30 raw). The amount must be positive. Amounts
// with more than XNORawDecimals fractional digits are rejected rather than
// truncated, and so are exponents, signs, and a trailing decimal point.
func XNOToRaw(amount string) (string, error) {
	amount = strings.TrimSpace(amount)
	whole, frac, hasFrac := strings.Cut(amount, ".")
	if whole == "" {
		whole = "0"
	}
	if !isDecimalDigits(whole) || (hasFrac && !isDecimalDigits(frac)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidAmount, amount)
	}
	if len(frac) > XNORawDecimals {
		return "", fmt.Errorf("%w: %q has more than %d decimals", ErrInvalidAmount, amount, XNORawDecimals)
	}
	raw, ok := new(big.Int).SetString(whole+frac+strings.Repeat("0", XNORawDecimals-len(frac)), 10)
	if !ok || raw.Sign() <= 0 {
		return "", fmt.Errorf("%w: %q", ErrInvalidAmount, amount)
	}
	return raw.String(), nil
}

func isDecimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
