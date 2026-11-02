package nano

import (
	"encoding/hex"
	"errors"
	"strings"

	"filippo.io/edwards25519"
	"golang.org/x/crypto/blake2b"
)

// BindingScheme names the payer-signature binding carried in the 402 as
// accepts[].extra.binding.scheme.
const BindingScheme = "nano-block-sig/1"

const bindingPrefix = "nano-402-binding/1\n"

// Binding errors. Each is a stable code a resource server can switch on.
var (
	ErrBindingMissing    = errors.New("nano_binding_missing")
	ErrBindingMalformed  = errors.New("nano_binding_malformed")
	ErrBindingNonce      = errors.New("nano_binding_nonce_mismatch")
	ErrBindingInvalid    = errors.New("nano_binding_invalid")
	ErrBindingNotOffered = errors.New("nano_binding_not_offered")
)

// BindingMessage is the exact byte string the payer signs:
//
//	"nano-402-binding/1\n" + blockHash(32 bytes) + "\n" + nonce + "\n" +
//	resource + "\n" + payTo (nano_ form)
//
// The format is the one in dhyabi2/dual-rail binding.py and pay402.py, so a
// payer using either tool needs no change.
func BindingMessage(blockHash, nonce, resource, payTo string) ([]byte, error) {
	h, err := hex.DecodeString(strings.TrimSpace(blockHash))
	if err != nil || len(h) != 32 {
		return nil, ErrBindingMalformed
	}
	pt, err := CanonicalAddress(payTo)
	if err != nil {
		return nil, ErrBindingMalformed
	}
	msg := []byte(bindingPrefix)
	msg = append(msg, h...)
	msg = append(msg, '\n')
	msg = append(msg, nonce...)
	msg = append(msg, '\n')
	msg = append(msg, resource...)
	msg = append(msg, '\n')
	msg = append(msg, pt...)
	return msg, nil
}

// VerifyBinding checks that sigHex is a valid Nano-variant Ed25519 signature
// of msg by payerAddress. payerAddress must come from the LEDGER (the block's
// account), never from the request: the point of the binding is that only the
// holder of the paying account's key can produce it. Only public data is
// touched, so the variable-time parts are harmless here.
func VerifyBinding(payerAddress string, msg []byte, sigHex string) error {
	sig, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(sig) != 64 {
		return ErrBindingMalformed
	}
	pub, err := DecodeAddress(payerAddress)
	if err != nil {
		return ErrBindingMalformed
	}
	if !verifyBlake2bEd25519(msg, sig, pub) {
		return ErrBindingInvalid
	}
	return nil
}

// verifyBlake2bEd25519 is RFC 8032 verification with BLAKE2b-512 in place of
// SHA-512 (Nano's variant): [s]B == R + [k]A, k = H(R || A || msg) mod L.
// Non-canonical S and non-point R or A are refused.
func verifyBlake2bEd25519(msg, sig, pub []byte) bool {
	if len(sig) != 64 || len(pub) != 32 {
		return false
	}
	A, err := new(edwards25519.Point).SetBytes(pub)
	if err != nil {
		return false
	}
	R, err := new(edwards25519.Point).SetBytes(sig[:32])
	if err != nil {
		return false
	}
	s, err := new(edwards25519.Scalar).SetCanonicalBytes(sig[32:])
	if err != nil {
		return false
	}
	h, _ := blake2b.New512(nil)
	h.Write(sig[:32])
	h.Write(pub)
	h.Write(msg)
	k, err := new(edwards25519.Scalar).SetUniformBytes(h.Sum(nil))
	if err != nil {
		return false
	}
	lhs := new(edwards25519.Point).ScalarBaseMult(s)
	rhs := new(edwards25519.Point).ScalarMult(k, A)
	rhs.Add(rhs, R)
	return lhs.Equal(rhs) == 1
}
