package sui

import (
	"context"
	stdECDSA "crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/btcsuite/btcutil/bech32"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	secp256k1ecdsa "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	bcs "github.com/iotaledger/bcs-go"
	"golang.org/x/crypto/blake2b"
)

const (
	// Signature scheme flags used by Sui's serialized signature format.
	SignatureSchemeEd25519   byte = 0x00
	SignatureSchemeSecp256k1 byte = 0x01
	SignatureSchemeSecp256r1 byte = 0x02
	SignatureSchemeMultiSig  byte = 0x03
	SignatureSchemeZkLogin   byte = 0x05

	ecdsaSignatureSize           = 64
	compressedECDSAPublicKeySize = 33

	intentScopeTransactionData byte = 0
	intentVersionV0            byte = 0
	intentAppIDSui             byte = 0
)

var (
	ErrInvalidSignature                 = errors.New("invalid_signature")
	ErrUnsupportedSignature             = errors.New("unsupported_signature_scheme")
	ErrSignatureVerificationUnavailable = errors.New("signature_verification_unavailable")
)

// Signer can sign Sui TransactionData bytes and expose the corresponding Sui
// address.
type Signer interface {
	SignTransaction(txBytes []byte) (string, error)
	Address() string
}

type Ed25519Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

type Secp256k1Signer struct {
	privateKey *secp256k1.PrivateKey
	publicKey  []byte
}

type Secp256r1Signer struct {
	privateKey *stdECDSA.PrivateKey
	publicKey  []byte
}

type SignatureVerifyOptions struct {
	ZkLoginVerifier ZkLoginVerifier
}

type ZkLoginVerifier func(ctx context.Context, author string, txBytes []byte, signature string) (bool, error)

func NewEd25519SignerFromPrivateKey(privateKey []byte) (*Ed25519Signer, error) {
	var key ed25519.PrivateKey
	switch len(privateKey) {
	case ed25519.SeedSize:
		key = ed25519.NewKeyFromSeed(privateKey)
	case ed25519.PrivateKeySize:
		key = append(ed25519.PrivateKey(nil), privateKey...)
	default:
		return nil, fmt.Errorf("invalid_ed25519_private_key_length: %d", len(privateKey))
	}

	publicKey, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid_ed25519_public_key")
	}

	return &Ed25519Signer{
		privateKey: key,
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
	}, nil
}

func NewEd25519SignerFromHex(privateKeyHex string) (*Ed25519Signer, error) {
	privateKeyHex = strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x")
	privateKey, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, err
	}
	return NewEd25519SignerFromPrivateKey(privateKey)
}

func NewSignerFromPrivateKeyString(privateKey string) (Signer, error) {
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return nil, errors.New("empty_private_key")
	}
	if strings.HasPrefix(strings.ToLower(privateKey), "suiprivkey1") {
		return newSignerFromSuiPrivateKey(privateKey)
	}
	return NewEd25519SignerFromHex(privateKey)
}

func newSignerFromSuiPrivateKey(privateKey string) (Signer, error) {
	hrp, data, err := bech32.Decode(privateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid_sui_private_key: %w", err)
	}
	if hrp != "suiprivkey" {
		return nil, fmt.Errorf("invalid_sui_private_key_hrp: %s", hrp)
	}

	decoded, err := bech32.ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, fmt.Errorf("invalid_sui_private_key_payload: %w", err)
	}
	if len(decoded) < 2 {
		return nil, fmt.Errorf("invalid_sui_private_key_length: %d", len(decoded))
	}

	scheme := decoded[0]
	key := decoded[1:]
	switch scheme {
	case SignatureSchemeEd25519:
		return NewEd25519SignerFromPrivateKey(key)
	case SignatureSchemeSecp256k1:
		return NewSecp256k1SignerFromPrivateKey(key)
	case SignatureSchemeSecp256r1:
		return NewSecp256r1SignerFromPrivateKey(key)
	default:
		return nil, fmt.Errorf("unsupported_sui_private_key_scheme: %d", scheme)
	}
}

func (s *Ed25519Signer) SignTransaction(txBytes []byte) (string, error) {
	if s == nil {
		return "", errors.New("nil_signer")
	}
	if len(txBytes) == 0 {
		return "", ErrEmptyTransaction
	}

	rawSig := ed25519.Sign(s.privateKey, TransactionIntentDigest(txBytes))
	serialized := make([]byte, 0, 1+len(rawSig)+len(s.publicKey))
	serialized = append(serialized, SignatureSchemeEd25519)
	serialized = append(serialized, rawSig...)
	serialized = append(serialized, s.publicKey...)

	return base64.StdEncoding.EncodeToString(serialized), nil
}

func (s *Ed25519Signer) Address() string {
	if s == nil {
		return ""
	}
	return AddressFromPublicKey(SignatureSchemeEd25519, s.publicKey)
}

func NewSecp256k1SignerFromPrivateKey(privateKey []byte) (*Secp256k1Signer, error) {
	if err := validateECDSAPrivateKey(privateKey, secp256k1.Params().N); err != nil {
		return nil, fmt.Errorf("invalid_secp256k1_private_key: %w", err)
	}

	key := secp256k1.PrivKeyFromBytes(privateKey)
	return &Secp256k1Signer{
		privateKey: key,
		publicKey:  key.PubKey().SerializeCompressed(),
	}, nil
}

func NewSecp256k1SignerFromHex(privateKeyHex string) (*Secp256k1Signer, error) {
	privateKey, err := privateKeyFromHex(privateKeyHex)
	if err != nil {
		return nil, err
	}
	return NewSecp256k1SignerFromPrivateKey(privateKey)
}

func (s *Secp256k1Signer) SignTransaction(txBytes []byte) (string, error) {
	if s == nil {
		return "", errors.New("nil_signer")
	}
	if len(txBytes) == 0 {
		return "", ErrEmptyTransaction
	}

	signature := secp256k1CompactSignature(secp256k1ecdsa.Sign(s.privateKey, TransactionIntentDigest(txBytes)))
	return serializedKeypairSignature(SignatureSchemeSecp256k1, signature, s.publicKey), nil
}

func (s *Secp256k1Signer) Address() string {
	if s == nil {
		return ""
	}
	return AddressFromPublicKey(SignatureSchemeSecp256k1, s.publicKey)
}

func NewSecp256r1SignerFromPrivateKey(privateKey []byte) (*Secp256r1Signer, error) {
	curve := elliptic.P256()
	if err := validateECDSAPrivateKey(privateKey, curve.Params().N); err != nil {
		return nil, fmt.Errorf("invalid_secp256r1_private_key: %w", err)
	}

	x, y := curve.ScalarBaseMult(privateKey)
	key := &stdECDSA.PrivateKey{
		PublicKey: stdECDSA.PublicKey{
			Curve: curve,
			X:     x,
			Y:     y,
		},
		D: new(big.Int).SetBytes(privateKey),
	}
	return &Secp256r1Signer{
		privateKey: key,
		publicKey:  elliptic.MarshalCompressed(curve, x, y),
	}, nil
}

func NewSecp256r1SignerFromHex(privateKeyHex string) (*Secp256r1Signer, error) {
	privateKey, err := privateKeyFromHex(privateKeyHex)
	if err != nil {
		return nil, err
	}
	return NewSecp256r1SignerFromPrivateKey(privateKey)
}

func (s *Secp256r1Signer) SignTransaction(txBytes []byte) (string, error) {
	if s == nil {
		return "", errors.New("nil_signer")
	}
	if len(txBytes) == 0 {
		return "", ErrEmptyTransaction
	}

	r, sigS, err := stdECDSA.Sign(rand.Reader, s.privateKey, TransactionIntentDigest(txBytes))
	if err != nil {
		return "", err
	}
	signature := fixedECDSASignature(r, normalizeECDSALowS(sigS, s.privateKey.Curve.Params().N))
	return serializedKeypairSignature(SignatureSchemeSecp256r1, signature, s.publicKey), nil
}

func (s *Secp256r1Signer) Address() string {
	if s == nil {
		return ""
	}
	return AddressFromPublicKey(SignatureSchemeSecp256r1, s.publicKey)
}

func VerifySignature(signature string, txBytes []byte) (string, error) {
	return VerifySignatureWithOptions(context.Background(), signature, txBytes, SignatureVerifyOptions{})
}

func VerifySignatureWithOptions(ctx context.Context, signature string, txBytes []byte, opts SignatureVerifyOptions) (string, error) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return "", ErrEmptySignature
	}
	if len(txBytes) == 0 {
		return "", ErrEmptyTransaction
	}

	serialized, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	if len(serialized) == 0 {
		return "", fmt.Errorf("%w: invalid_length", ErrInvalidSignature)
	}

	digest := TransactionIntentDigest(txBytes)
	switch serialized[0] {
	case SignatureSchemeEd25519:
		return verifySerializedKeypairSignature(serialized, ed25519.SignatureSize, ed25519.PublicKeySize, digest)
	case SignatureSchemeSecp256k1:
		return verifySerializedKeypairSignature(serialized, ecdsaSignatureSize, secp256k1.PubKeyBytesLenCompressed, digest)
	case SignatureSchemeSecp256r1:
		return verifySerializedKeypairSignature(serialized, ecdsaSignatureSize, compressedECDSAPublicKeySize, digest)
	case SignatureSchemeMultiSig:
		return verifySerializedMultiSig(ctx, serialized, txBytes, digest, opts)
	case SignatureSchemeZkLogin:
		return verifySerializedZkLogin(ctx, serialized, txBytes, opts)
	default:
		return "", ErrUnsupportedSignature
	}
}

func verifySerializedKeypairSignature(serialized []byte, signatureSize int, publicKeySize int, digest []byte) (string, error) {
	if len(serialized) != 1+signatureSize+publicKeySize {
		return "", fmt.Errorf("%w: invalid_length", ErrInvalidSignature)
	}

	signature := serialized[1 : 1+signatureSize]
	publicKey := serialized[1+signatureSize:]
	if !verifyKeypairSignature(serialized[0], signature, publicKey, digest) {
		return "", ErrInvalidSignature
	}

	address := AddressFromPublicKey(serialized[0], publicKey)
	if address == "" {
		return "", ErrInvalidSignature
	}
	return address, nil
}

func TransactionIntentDigest(txBytes []byte) []byte {
	if len(txBytes) > math.MaxInt-3 {
		panic("transaction bytes too large")
	}
	intentMessage := make([]byte, 0, 3+len(txBytes))
	intentMessage = append(intentMessage, intentScopeTransactionData, intentVersionV0, intentAppIDSui)
	intentMessage = append(intentMessage, txBytes...)
	digest := blake2b.Sum256(intentMessage)
	return digest[:]
}

func AddressFromPublicKey(signatureScheme byte, publicKey []byte) string {
	if !validPublicKey(signatureScheme, publicKey) {
		return ""
	}
	if len(publicKey) > math.MaxInt-1 {
		panic("public key too large")
	}

	material := make([]byte, 0, 1+len(publicKey))
	material = append(material, signatureScheme)
	material = append(material, publicKey...)
	digest := blake2b.Sum256(material)
	return "0x" + hex.EncodeToString(digest[:])
}

func AddressFromZkLoginSignature(signature string) (string, error) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return "", ErrEmptySignature
	}

	serialized, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	if len(serialized) == 0 || serialized[0] != SignatureSchemeZkLogin {
		return "", ErrUnsupportedSignature
	}
	return addressFromSerializedZkLogin(serialized)
}

func verifySerializedMultiSig(ctx context.Context, serialized []byte, txBytes []byte, digest []byte, opts SignatureVerifyOptions) (string, error) {
	if len(serialized) < 2 {
		return "", fmt.Errorf("%w: invalid_length", ErrInvalidSignature)
	}

	multisig, err := bcs.Unmarshal[multiSig](serialized[1:])
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	if err := validateMultiSigPublicKey(multisig.MultisigPK); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}

	indices, err := multiSigBitmapIndices(multisig.Bitmap)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	if len(indices) != len(multisig.Sigs) {
		return "", fmt.Errorf("%w: multisig signature bitmap mismatch", ErrInvalidSignature)
	}

	weight := 0
	for i, sig := range multisig.Sigs {
		pkIndex := indices[i]
		if pkIndex >= len(multisig.MultisigPK.PKMap) {
			return "", fmt.Errorf("%w: multisig public key index out of range", ErrInvalidSignature)
		}

		signatureScheme, signature, err := compressedSignatureParts(sig)
		if err != nil {
			return "", err
		}
		publicKeyScheme, publicKey, err := multiSigPublicKeyParts(multisig.MultisigPK.PKMap[pkIndex].PubKey)
		if err != nil {
			return "", err
		}
		if signatureScheme != publicKeyScheme {
			return "", ErrInvalidSignature
		}
		if err := verifySignatureParts(ctx, signatureScheme, signature, publicKey, txBytes, digest, opts); err != nil {
			return "", err
		}
		weight += int(multisig.MultisigPK.PKMap[pkIndex].Weight)
	}

	if weight < int(multisig.MultisigPK.Threshold) {
		return "", ErrInvalidSignature
	}
	return addressFromMultiSigPublicKey(multisig.MultisigPK), nil
}

func verifySerializedZkLogin(ctx context.Context, serialized []byte, txBytes []byte, opts SignatureVerifyOptions) (string, error) {
	author, err := addressFromSerializedZkLogin(serialized)
	if err != nil {
		return "", err
	}
	if opts.ZkLoginVerifier == nil {
		return "", ErrUnsupportedSignature
	}

	ok, err := opts.ZkLoginVerifier(ctx, author, txBytes, base64.StdEncoding.EncodeToString(serialized))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSignatureVerificationUnavailable, err)
	}
	if !ok {
		return "", ErrInvalidSignature
	}
	return author, nil
}

func verifySignatureParts(ctx context.Context, scheme byte, signature []byte, publicKey []byte, txBytes []byte, digest []byte, opts SignatureVerifyOptions) error {
	switch scheme {
	case SignatureSchemeEd25519, SignatureSchemeSecp256k1, SignatureSchemeSecp256r1:
		if !verifyKeypairSignature(scheme, signature, publicKey, digest) {
			return ErrInvalidSignature
		}
		return nil
	case SignatureSchemeZkLogin:
		author := AddressFromPublicKey(SignatureSchemeZkLogin, publicKey)
		if author == "" {
			return ErrInvalidSignature
		}
		if opts.ZkLoginVerifier == nil {
			return ErrUnsupportedSignature
		}
		if len(signature) == 0 || signature[0] != SignatureSchemeZkLogin {
			return ErrInvalidSignature
		}
		ok, err := opts.ZkLoginVerifier(ctx, author, txBytes, base64.StdEncoding.EncodeToString(signature))
		if err != nil {
			return fmt.Errorf("%w: %v", ErrSignatureVerificationUnavailable, err)
		}
		if !ok {
			return ErrInvalidSignature
		}
		return nil
	default:
		return ErrUnsupportedSignature
	}
}

func verifyKeypairSignature(scheme byte, signature []byte, publicKey []byte, digest []byte) bool {
	switch scheme {
	case SignatureSchemeEd25519:
		return len(signature) == ed25519.SignatureSize &&
			len(publicKey) == ed25519.PublicKeySize &&
			ed25519.Verify(ed25519.PublicKey(publicKey), digest, signature)
	case SignatureSchemeSecp256k1:
		return verifySecp256k1Signature(signature, publicKey, digest)
	case SignatureSchemeSecp256r1:
		return verifySecp256r1Signature(signature, publicKey, digest)
	default:
		return false
	}
}

func verifySecp256k1Signature(signature []byte, publicKey []byte, digest []byte) bool {
	if len(signature) != ecdsaSignatureSize || len(publicKey) != secp256k1.PubKeyBytesLenCompressed {
		return false
	}
	pubKey, err := secp256k1.ParsePubKey(publicKey)
	if err != nil {
		return false
	}

	var r, sigS secp256k1.ModNScalar
	if r.SetByteSlice(signature[:32]) || sigS.SetByteSlice(signature[32:]) {
		return false
	}
	if r.IsZero() || sigS.IsZero() || sigS.IsOverHalfOrder() {
		return false
	}
	return secp256k1ecdsa.NewSignature(&r, &sigS).Verify(digest, pubKey)
}

func verifySecp256r1Signature(signature []byte, publicKey []byte, digest []byte) bool {
	if len(signature) != ecdsaSignatureSize || len(publicKey) != compressedECDSAPublicKeySize {
		return false
	}
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), publicKey)
	if x == nil || y == nil {
		return false
	}

	curveOrder := elliptic.P256().Params().N
	r := new(big.Int).SetBytes(signature[:32])
	sigS := new(big.Int).SetBytes(signature[32:])
	if !validECDSAValue(r, curveOrder) || !validECDSAValue(sigS, curveOrder) {
		return false
	}
	if sigS.Cmp(halfOrder(curveOrder)) > 0 {
		return false
	}
	return stdECDSA.Verify(&stdECDSA.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest, r, sigS)
}

func validPublicKey(scheme byte, publicKey []byte) bool {
	switch scheme {
	case SignatureSchemeEd25519:
		return len(publicKey) == ed25519.PublicKeySize
	case SignatureSchemeSecp256k1, SignatureSchemeSecp256r1:
		return len(publicKey) == compressedECDSAPublicKeySize
	case SignatureSchemeZkLogin:
		return validZkLoginPublicIdentifier(publicKey)
	default:
		return false
	}
}

func validZkLoginPublicIdentifier(publicKey []byte) bool {
	return len(publicKey) >= 1+32 && len(publicKey) == 1+int(publicKey[0])+32
}

func validateECDSAPrivateKey(privateKey []byte, order *big.Int) error {
	if len(privateKey) != 32 {
		return fmt.Errorf("invalid length %d", len(privateKey))
	}
	d := new(big.Int).SetBytes(privateKey)
	if !validECDSAValue(d, order) {
		return errors.New("private key must be in [1, n)")
	}
	return nil
}

func validECDSAValue(value *big.Int, order *big.Int) bool {
	return value.Sign() > 0 && value.Cmp(order) < 0
}

func halfOrder(order *big.Int) *big.Int {
	return new(big.Int).Rsh(new(big.Int).Set(order), 1)
}

func normalizeECDSALowS(sigS *big.Int, order *big.Int) *big.Int {
	if sigS.Cmp(halfOrder(order)) <= 0 {
		return sigS
	}
	return new(big.Int).Sub(order, sigS)
}

func fixedECDSASignature(r *big.Int, sigS *big.Int) []byte {
	signature := make([]byte, ecdsaSignatureSize)
	r.FillBytes(signature[:32])
	sigS.FillBytes(signature[32:])
	return signature
}

func secp256k1CompactSignature(signature *secp256k1ecdsa.Signature) []byte {
	r := signature.R()
	sigS := signature.S()
	if sigS.IsOverHalfOrder() {
		sigS.Negate()
	}

	serialized := make([]byte, ecdsaSignatureSize)
	r.PutBytesUnchecked(serialized[:32])
	sigS.PutBytesUnchecked(serialized[32:])
	return serialized
}

func serializedKeypairSignature(scheme byte, signature []byte, publicKey []byte) string {
	serialized := make([]byte, 0, 1+len(signature)+len(publicKey))
	serialized = append(serialized, scheme)
	serialized = append(serialized, signature...)
	serialized = append(serialized, publicKey...)
	return base64.StdEncoding.EncodeToString(serialized)
}

func privateKeyFromHex(privateKeyHex string) ([]byte, error) {
	privateKeyHex = strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x")
	return hex.DecodeString(privateKeyHex)
}

type signature64 [64]byte
type publicKey32 [32]byte
type publicKey33 [33]byte

type compressedSignature struct {
	ED25519   *signature64
	Secp256k1 *signature64
	Secp256r1 *signature64
	ZkLogin   *[]byte
	Passkey   *[]byte
}

func (compressedSignature) IsBcsEnum() {}

type multiSigPublicKeyEnum struct {
	ED25519   *publicKey32
	Secp256k1 *publicKey33
	Secp256r1 *publicKey33
	ZkLogin   *[]byte
	Passkey   *publicKey33
}

func (multiSigPublicKeyEnum) IsBcsEnum() {}

type multiSigPublicKeyMap struct {
	PubKey multiSigPublicKeyEnum
	Weight uint8
}

type multiSigPublicKey struct {
	PKMap     []multiSigPublicKeyMap
	Threshold uint16
}

type multiSig struct {
	Sigs       []compressedSignature
	Bitmap     uint16
	MultisigPK multiSigPublicKey
}

func compressedSignatureParts(signature compressedSignature) (byte, []byte, error) {
	switch {
	case signature.ED25519 != nil:
		return SignatureSchemeEd25519, signature.ED25519[:], nil
	case signature.Secp256k1 != nil:
		return SignatureSchemeSecp256k1, signature.Secp256k1[:], nil
	case signature.Secp256r1 != nil:
		return SignatureSchemeSecp256r1, signature.Secp256r1[:], nil
	case signature.ZkLogin != nil:
		return SignatureSchemeZkLogin, *signature.ZkLogin, nil
	case signature.Passkey != nil:
		return 0, nil, ErrUnsupportedSignature
	default:
		return 0, nil, ErrInvalidSignature
	}
}

func multiSigPublicKeyParts(publicKey multiSigPublicKeyEnum) (byte, []byte, error) {
	switch {
	case publicKey.ED25519 != nil:
		return SignatureSchemeEd25519, publicKey.ED25519[:], nil
	case publicKey.Secp256k1 != nil:
		return SignatureSchemeSecp256k1, publicKey.Secp256k1[:], nil
	case publicKey.Secp256r1 != nil:
		return SignatureSchemeSecp256r1, publicKey.Secp256r1[:], nil
	case publicKey.ZkLogin != nil:
		return SignatureSchemeZkLogin, *publicKey.ZkLogin, nil
	case publicKey.Passkey != nil:
		return 0, nil, ErrUnsupportedSignature
	default:
		return 0, nil, ErrInvalidSignature
	}
}

func validateMultiSigPublicKey(publicKey multiSigPublicKey) error {
	if publicKey.Threshold == 0 {
		return errors.New("multisig threshold is zero")
	}
	if len(publicKey.PKMap) == 0 || len(publicKey.PKMap) > 10 {
		return fmt.Errorf("invalid multisig signer count %d", len(publicKey.PKMap))
	}

	seen := make(map[string]struct{}, len(publicKey.PKMap))
	totalWeight := 0
	for _, entry := range publicKey.PKMap {
		scheme, key, err := multiSigPublicKeyParts(entry.PubKey)
		if err != nil {
			return err
		}
		if entry.Weight == 0 {
			return errors.New("multisig weight is zero")
		}
		if !validPublicKey(scheme, key) {
			return errors.New("invalid multisig public key length")
		}
		identity := string(append([]byte{scheme}, key...))
		if _, ok := seen[identity]; ok {
			return errors.New("duplicate multisig public key")
		}
		seen[identity] = struct{}{}
		totalWeight += int(entry.Weight)
	}
	if int(publicKey.Threshold) > totalWeight {
		return errors.New("multisig threshold exceeds total weight")
	}
	return nil
}

func multiSigBitmapIndices(bitmap uint16) ([]int, error) {
	if bitmap > 1024 {
		return nil, errors.New("invalid multisig bitmap")
	}

	indices := make([]int, 0, 10)
	for i := 0; i < 10; i++ {
		if bitmap&(1<<i) != 0 {
			indices = append(indices, i)
		}
	}
	return indices, nil
}

func addressFromMultiSigPublicKey(publicKey multiSigPublicKey) string {
	material := make([]byte, 0, 1+2+len(publicKey.PKMap)*(1+65+1))
	material = append(material, SignatureSchemeMultiSig)
	material = binary.LittleEndian.AppendUint16(material, publicKey.Threshold)
	for _, entry := range publicKey.PKMap {
		scheme, key, err := multiSigPublicKeyParts(entry.PubKey)
		if err != nil {
			return ""
		}
		material = append(material, scheme)
		material = append(material, key...)
		material = append(material, entry.Weight)
	}
	digest := blake2b.Sum256(material)
	return "0x" + hex.EncodeToString(digest[:])
}

type zkLoginSignature struct {
	Inputs        zkLoginSignatureInputs
	MaxEpoch      uint64
	UserSignature []byte
}

type zkLoginSignatureInputs struct {
	ProofPoints      zkLoginSignatureProofPoints
	IssBase64Details zkLoginSignatureClaim
	HeaderBase64     string
	AddressSeed      string
}

type zkLoginSignatureProofPoints struct {
	A []string
	B [][]string
	C []string
}

type zkLoginSignatureClaim struct {
	Value     string
	IndexMod4 uint8
}

func addressFromSerializedZkLogin(serialized []byte) (string, error) {
	if len(serialized) < 2 || serialized[0] != SignatureSchemeZkLogin {
		return "", ErrUnsupportedSignature
	}

	zkSig, err := bcs.Unmarshal[zkLoginSignature](serialized[1:])
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	issuer, err := extractZkLoginIssuer(zkSig.Inputs.IssBase64Details)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	publicIdentifier, err := zkLoginPublicIdentifier(zkSig.Inputs.AddressSeed, issuer)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	address := AddressFromPublicKey(SignatureSchemeZkLogin, publicIdentifier)
	if address == "" {
		return "", ErrInvalidSignature
	}
	return address, nil
}

func zkLoginPublicIdentifier(addressSeed string, issuer string) ([]byte, error) {
	seed, ok := new(big.Int).SetString(strings.TrimSpace(addressSeed), 10)
	if !ok || seed.Sign() < 0 || seed.BitLen() > 256 {
		return nil, fmt.Errorf("invalid zkLogin address seed %q", addressSeed)
	}

	issuer = normalizeZkLoginIssuer(issuer)
	issuerBytes := []byte(issuer)
	if len(issuerBytes) == 0 || len(issuerBytes) > math.MaxUint8 {
		return nil, fmt.Errorf("invalid zkLogin issuer length %d", len(issuerBytes))
	}

	seedBytes := make([]byte, 32)
	seed.FillBytes(seedBytes)

	publicIdentifier := make([]byte, 1+len(issuerBytes)+len(seedBytes))
	publicIdentifier[0] = byte(len(issuerBytes))
	copy(publicIdentifier[1:], issuerBytes)
	copy(publicIdentifier[1+len(issuerBytes):], seedBytes)
	return publicIdentifier, nil
}

func normalizeZkLoginIssuer(issuer string) string {
	if issuer == "accounts.google.com" {
		return "https://accounts.google.com"
	}
	return issuer
}

func extractZkLoginIssuer(claim zkLoginSignatureClaim) (string, error) {
	decoded, err := decodeBase64URLTightlyPacked(claim.Value, claim.IndexMod4)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(decoded, "}") && !strings.HasSuffix(decoded, ",") {
		return "", errors.New("invalid zkLogin issuer claim")
	}

	var value map[string]string
	if err := json.Unmarshal([]byte("{"+decoded[:len(decoded)-1]+"}"), &value); err != nil {
		return "", err
	}
	if len(value) != 1 {
		return "", errors.New("invalid zkLogin issuer claim")
	}
	issuer, ok := value["iss"]
	if !ok {
		return "", errors.New("missing zkLogin issuer claim")
	}
	return issuer, nil
}

func decodeBase64URLTightlyPacked(value string, indexMod4 uint8) (string, error) {
	if len(value) < 2 {
		return "", errors.New("invalid zkLogin claim length")
	}
	bits := make([]byte, 0, len(value)*6)
	for _, ch := range value {
		index := strings.IndexRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", ch)
		if index < 0 {
			return "", fmt.Errorf("invalid base64url character %q", ch)
		}
		for bit := 5; bit >= 0; bit-- {
			if index&(1<<bit) == 0 {
				bits = append(bits, 0)
			} else {
				bits = append(bits, 1)
			}
		}
	}

	switch indexMod4 % 4 {
	case 0:
	case 1:
		if len(bits) < 2 {
			return "", errors.New("invalid zkLogin claim offset")
		}
		bits = bits[2:]
	case 2:
		if len(bits) < 4 {
			return "", errors.New("invalid zkLogin claim offset")
		}
		bits = bits[4:]
	default:
		return "", errors.New("invalid zkLogin claim offset")
	}

	lastCharOffset := (int(indexMod4) + len(value) - 1) % 4
	switch lastCharOffset {
	case 3:
	case 2:
		if len(bits) < 2 {
			return "", errors.New("invalid zkLogin claim packing")
		}
		bits = bits[:len(bits)-2]
	case 1:
		if len(bits) < 4 {
			return "", errors.New("invalid zkLogin claim packing")
		}
		bits = bits[:len(bits)-4]
	default:
		return "", errors.New("invalid zkLogin claim packing")
	}
	if len(bits)%8 != 0 {
		return "", errors.New("invalid zkLogin claim bit length")
	}

	decoded := make([]byte, len(bits)/8)
	for i := 0; i < len(decoded); i++ {
		var b byte
		for j := 0; j < 8; j++ {
			b = (b << 1) | bits[i*8+j]
		}
		decoded[i] = b
	}
	return string(decoded), nil
}
