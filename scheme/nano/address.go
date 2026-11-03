package nano

import (
	"errors"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const nanoAlphabet = "13456789abcdefghijkmnopqrstuwxyz"

// ErrInvalidAddress is returned for an address that is malformed or fails its
// checksum.
var ErrInvalidAddress = errors.New("invalid_nano_address")

// DecodeAddress parses a nano_ or xrb_ account, verifies its 40-bit checksum
// and returns the 32-byte public key. It never panics on malformed input.
func DecodeAddress(addr string) ([]byte, error) {
	a := strings.ToLower(strings.TrimSpace(addr))
	switch {
	case strings.HasPrefix(a, "nano_"):
		a = a[len("nano_"):]
	case strings.HasPrefix(a, "xrb_"):
		a = a[len("xrb_"):]
	default:
		return nil, ErrInvalidAddress
	}
	if len(a) != 60 {
		return nil, ErrInvalidAddress
	}
	vals := make([]byte, 60)
	for i := 0; i < 60; i++ {
		k := strings.IndexByte(nanoAlphabet, a[i])
		if k < 0 {
			return nil, ErrInvalidAddress
		}
		vals[i] = byte(k)
	}
	if vals[0] > 1 { // the top 4 of the 260 key bits are padding
		return nil, ErrInvalidAddress
	}
	// 52 five-bit groups right-aligned into 264 bits (33 bytes).
	buf := make([]byte, 33)
	pos := 4
	for i := 0; i < 52; i++ {
		for b := 4; b >= 0; b-- {
			bit := (vals[i] >> uint(b)) & 1
			buf[pos/8] |= bit << uint(7-pos%8)
			pos++
		}
	}
	pub := buf[1:]
	sum := checksum(pub)
	var got uint64
	for i := 52; i < 60; i++ {
		got = got<<5 | uint64(vals[i])
	}
	if got != sum {
		return nil, ErrInvalidAddress
	}
	return pub, nil
}

// checksum is blake2b-40 of the key, reversed, read as a big-endian integer.
func checksum(pub []byte) uint64 {
	h, _ := blake2b.New(5, nil)
	h.Write(pub)
	d := h.Sum(nil)
	var v uint64
	for i := len(d) - 1; i >= 0; i-- {
		v = v<<8 | uint64(d[i])
	}
	return v
}

// CanonicalAddress lowercases an address and rewrites the legacy xrb_ prefix
// to nano_, after checking it. The binding message uses this form so a seller
// and a payer that spell the same account differently still sign the same
// bytes.
func CanonicalAddress(addr string) (string, error) {
	if _, err := DecodeAddress(addr); err != nil {
		return "", err
	}
	a := strings.ToLower(strings.TrimSpace(addr))
	if strings.HasPrefix(a, "xrb_") {
		a = "nano_" + a[len("xrb_"):]
	}
	return a, nil
}
