package casper

import (
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAddress(t *testing.T) {
	const accountHash = "account-hash-1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000"
	const ed25519Key = "011b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000"
	const secp256k1Key = "021b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000ab"

	tests := []struct {
		name    string
		address string
		want    string
	}{
		{"account hash", accountHash, accountHash},
		{"account hash upper case", "ACCOUNT-HASH-1B2C3D4E5F60718293A4B5C6D7E8F90112233445566778899AABBCCDDEEFF000", accountHash},
		{"account hash padded", "  " + accountHash + "  ", accountHash},
		{"ed25519 public key", ed25519Key, ed25519Key},
		{"secp256k1 public key", secp256k1Key, secp256k1Key},
		{"empty", "", ""},
		{"account hash too short", "account-hash-1b2c3d", ""},
		{"account hash not hex", "account-hash-zz2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000", ""},
		{"unknown key prefix", "031b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000", ""},
		{"ed25519 wrong length", "0117e5a20d", ""},
		{"evm address", "0x1234567890123456789012345678901234567890", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NormalizeAddress(tt.address))
		})
	}
}

func TestNormalizeContractHash(t *testing.T) {
	const hash = "1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff001"

	tests := []struct {
		name     string
		contract string
		want     string
	}{
		{"bare hash", hash, hash},
		{"hash prefixed", "hash-" + hash, hash},
		{"contract prefixed", "contract-" + hash, hash},
		{"upper case", "HASH-1B2C3D4E5F60718293A4B5C6D7E8F90112233445566778899AABBCCDDEEFF001", hash},
		{"empty", "", ""},
		{"too short", "1b2c3d", ""},
		{"not hex", "zz2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff001", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NormalizeContractHash(tt.contract))
		})
	}
}

func TestParseMotes(t *testing.T) {
	tests := []struct {
		name    string
		amount  string
		want    string
		wantErr error
	}{
		{name: "zero", amount: "0", want: "0"},
		{name: "one mote", amount: "1", want: "1"},
		{name: "one cspr", amount: "1000000000", want: "1000000000"},
		{name: "padded", amount: " 2500000000 ", want: "2500000000"},
		{name: "large", amount: "340282366920938463463374607431768211455", want: "340282366920938463463374607431768211455"},
		{name: "empty", amount: "", wantErr: ErrInvalidAmount},
		{name: "negative", amount: "-1", wantErr: ErrInvalidAmount},
		{name: "decimal", amount: "1.5", wantErr: ErrInvalidAmount},
		{name: "not a number", amount: "abc", wantErr: ErrInvalidAmount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			motes, err := ParseMotes(tt.amount)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, motes.String())
		})
	}
}

func TestCSPRToMotes(t *testing.T) {
	tests := []struct {
		name    string
		amount  string
		want    string
		wantErr error
	}{
		{name: "whole", amount: "1", want: "1000000000"},
		{name: "zero", amount: "0", want: "0"},
		{name: "fractional", amount: "1.5", want: "1500000000"},
		{name: "one mote", amount: "0.000000001", want: "1"},
		{name: "leading dot", amount: ".25", want: "250000000"},
		{name: "trailing zeros beyond precision", amount: "1.5000000000000", want: "1500000000"},
		{name: "max precision", amount: "12.123456789", want: "12123456789"},
		{name: "sub-mote precision rejected", amount: "0.0000000001", wantErr: ErrSubMotePrecision},
		{name: "sub-mote precision rejected on large amount", amount: "1.1234567891", wantErr: ErrSubMotePrecision},
		{name: "negative", amount: "-1", wantErr: ErrInvalidAmount},
		{name: "empty", amount: "", wantErr: ErrInvalidAmount},
		{name: "not a number", amount: "1.2.3", wantErr: ErrInvalidAmount},
		{name: "letters", amount: "1e9", wantErr: ErrInvalidAmount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			motes, err := CSPRToMotes(tt.amount)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, motes)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, motes.String())
		})
	}
}

func TestMotesToCSPRRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		motes string
		want  string
	}{
		{"zero", "0", "0"},
		{"one mote", "1", "0.000000001"},
		{"one cspr", "1000000000", "1"},
		{"fractional", "1500000000", "1.5"},
		{"max precision", "12123456789", "12.123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			motes, ok := new(big.Int).SetString(tt.motes, 10)
			require.True(t, ok)
			cspr := MotesToCSPR(motes)
			require.Equal(t, tt.want, cspr)

			back, err := CSPRToMotes(cspr)
			require.NoError(t, err)
			require.Equal(t, tt.motes, back.String())
		})
	}

	require.Equal(t, "0", MotesToCSPR(nil))
}

func TestParsePayload(t *testing.T) {
	const payer = "account-hash-1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000"

	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{
			name: "valid",
			raw:  `{"signature":"01aabb","payer":"` + payer + `","deploy":{"hash":"aabb"}}`,
		},
		{
			name:    "empty signature",
			raw:     `{"signature":"","payer":"` + payer + `","deploy":{"hash":"aabb"}}`,
			wantErr: ErrEmptySignature,
		},
		{
			name:    "missing deploy",
			raw:     `{"signature":"01aabb","payer":"` + payer + `"}`,
			wantErr: ErrEmptyTransaction,
		},
		{
			name:    "null deploy",
			raw:     `{"signature":"01aabb","payer":"` + payer + `","deploy":null}`,
			wantErr: ErrEmptyTransaction,
		},
		{
			name:    "invalid payer",
			raw:     `{"signature":"01aabb","payer":"not-an-address","deploy":{"hash":"aabb"}}`,
			wantErr: ErrInvalidAddress,
		},
		{
			name:    "empty",
			raw:     ``,
			wantErr: ErrEmptyPayload,
		},
		{
			name: "malformed json",
			raw:  `{"signature":`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := ParsePayload([]byte(tt.raw))
			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, payload)
			case tt.name == "malformed json":
				require.Error(t, err)
				require.Nil(t, payload)
			default:
				require.NoError(t, err)
				require.Equal(t, payer, NormalizeAddress(payload.Payer))
				require.NotEmpty(t, payload.Signature)
			}
		})
	}
}

func TestPayloadFromMap(t *testing.T) {
	const payer = "account-hash-1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000"

	payload, err := PayloadFromMap(map[string]interface{}{
		"signature": "01aabb",
		"payer":     payer,
		"deploy":    map[string]interface{}{"hash": "aabb"},
	})
	require.NoError(t, err)
	require.Equal(t, "01aabb", payload.Signature)
	require.Equal(t, payer, payload.Payer)
	require.JSONEq(t, `{"hash":"aabb"}`, string(payload.Deploy))

	_, err = PayloadFromMap(nil)
	require.ErrorIs(t, err, ErrEmptyPayload)

	_, err = PayloadFromMap(map[string]interface{}{"signature": "01aabb"})
	require.ErrorIs(t, err, ErrEmptyTransaction)
}

func TestPayloadJSONRoundTrip(t *testing.T) {
	const payer = "account-hash-1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000"

	original := Payload{
		Signature: "01aabb",
		Payer:     payer,
		Deploy:    json.RawMessage(`{"hash":"aabb"}`),
	}
	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	decoded, err := ParsePayload(encoded)
	require.NoError(t, err)
	require.Equal(t, original.Signature, decoded.Signature)
	require.Equal(t, original.Payer, decoded.Payer)
	require.JSONEq(t, string(original.Deploy), string(decoded.Deploy))
}

func TestNormalizeAsset(t *testing.T) {
	require.Equal(t, "wcspr", NormalizeAsset("  WCSPR "))
	require.Equal(t, "", NormalizeAsset("   "))
}

func TestErrorsAreDistinct(t *testing.T) {
	require.False(t, errors.Is(ErrSubMotePrecision, ErrInvalidAmount))
	require.False(t, errors.Is(ErrInvalidAddress, ErrInvalidAsset))
}
