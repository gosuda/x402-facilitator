package sui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	bcs "github.com/iotaledger/bcs-go"
	"github.com/stretchr/testify/require"
)

func TestEd25519SignerSignsAndVerifiesTransaction(t *testing.T) {
	signer := newTestSigner(t)
	txBytes := []byte("sui transaction data")

	payload, err := NewSignedPayload(txBytes, signer)
	require.NoError(t, err)
	require.NotEmpty(t, payload.Signature)
	require.NotEmpty(t, payload.Transaction)

	decodedTx, err := payload.DecodeTransaction()
	require.NoError(t, err)
	require.Equal(t, txBytes, decodedTx)

	payer, err := VerifySignature(payload.Signature, decodedTx)
	require.NoError(t, err)
	require.Equal(t, signer.Address(), payer)
}

func TestNewSignerFromSuiPrivateKey(t *testing.T) {
	signer, err := NewSignerFromPrivateKeyString("suiprivkey1qqc6q82g50fuezxyvycy64fanyk7gh6un8cq4l6wshv97sz04uemyeu6jtx")
	require.NoError(t, err)
	require.Equal(t, NormalizeAddress("0x614dd2a3d669fb753003f49ca7c0e01a0676efb5241ed291c57028160113d245"), NormalizeAddress(signer.Address()))
}

func TestPayloadJSONRoundTrip(t *testing.T) {
	signer := newTestSigner(t)
	payload, err := NewSignedPayload([]byte{0x01, 0x02, 0x03}, signer)
	require.NoError(t, err)

	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	parsed, err := ParsePayload(raw)
	require.NoError(t, err)
	require.Equal(t, payload.Signature, parsed.Signature)
	require.Equal(t, payload.Transaction, parsed.Transaction)
}

func TestVerifySignatureRejectsUnsupportedScheme(t *testing.T) {
	signer := newTestSigner(t)
	txBytes := []byte("sui transaction data")
	payload, err := NewSignedPayload(txBytes, signer)
	require.NoError(t, err)

	serialized, err := base64.StdEncoding.DecodeString(payload.Signature)
	require.NoError(t, err)
	serialized[0] = 0x04

	_, err = VerifySignature(base64.StdEncoding.EncodeToString(serialized), txBytes)
	require.ErrorIs(t, err, ErrUnsupportedSignature)
}

func TestSecp256k1SignerSignsAndVerifiesTransaction(t *testing.T) {
	privateKey := testPrivateKey(11)
	signer, err := NewSecp256k1SignerFromPrivateKey(privateKey)
	require.NoError(t, err)

	txBytes := []byte("sui transaction data")
	signature, err := signer.SignTransaction(txBytes)
	require.NoError(t, err)

	payer, err := VerifySignature(signature, txBytes)
	require.NoError(t, err)
	require.Equal(t, signer.Address(), payer)
}

func TestSecp256r1SignerSignsAndVerifiesTransaction(t *testing.T) {
	privateKey := testPrivateKey(23)
	signer, err := NewSecp256r1SignerFromPrivateKey(privateKey)
	require.NoError(t, err)

	txBytes := []byte("sui transaction data")
	signature, err := signer.SignTransaction(txBytes)
	require.NoError(t, err)

	payer, err := VerifySignature(signature, txBytes)
	require.NoError(t, err)
	require.Equal(t, signer.Address(), payer)
}

func TestMultiSigSignatureVerifiesThreshold(t *testing.T) {
	txBytes := []byte("sui transaction data")
	edSigner := newTestSigner(t)
	secpSigner, err := NewSecp256k1SignerFromPrivateKey(testPrivateKey(31))
	require.NoError(t, err)

	edSignature, err := edSigner.SignTransaction(txBytes)
	require.NoError(t, err)
	secpSignature, err := secpSigner.SignTransaction(txBytes)
	require.NoError(t, err)

	_, edRawSignature, _, err := keypairSignatureParts(edSignature, ed25519PublicKeySize)
	require.NoError(t, err)
	_, secpRawSignature, _, err := keypairSignatureParts(secpSignature, compressedECDSAPublicKeySize)
	require.NoError(t, err)

	multisigPK := multiSigPublicKey{
		PKMap: []multiSigPublicKeyMap{
			{PubKey: multiSigPublicKeyEnum{ED25519: publicKey32FromSlice(t, edSigner.publicKey)}, Weight: 1},
			{PubKey: multiSigPublicKeyEnum{Secp256k1: publicKey33FromSlice(t, secpSigner.publicKey)}, Weight: 1},
		},
		Threshold: 2,
	}
	multisig := multiSig{
		Sigs: []compressedSignature{
			{ED25519: signature64FromSlice(t, edRawSignature)},
			{Secp256k1: signature64FromSlice(t, secpRawSignature)},
		},
		Bitmap:     0b11,
		MultisigPK: multisigPK,
	}
	multisigBytes, err := bcs.Marshal(&multisig)
	require.NoError(t, err)
	serialized := append([]byte{SignatureSchemeMultiSig}, multisigBytes...)

	payer, err := VerifySignature(base64.StdEncoding.EncodeToString(serialized), txBytes)
	require.NoError(t, err)
	require.Equal(t, addressFromMultiSigPublicKey(multisigPK), payer)
}

func TestZkLoginSignatureUsesVerifier(t *testing.T) {
	txBytes := []byte("sui transaction data")
	signature := testZkLoginSignature(t, "abc", "1")
	expectedAuthor, err := AddressFromZkLoginSignature(signature)
	require.NoError(t, err)

	called := false
	payer, err := VerifySignatureWithOptions(context.Background(), signature, txBytes, SignatureVerifyOptions{
		ZkLoginVerifier: func(ctx context.Context, author string, gotTxBytes []byte, gotSignature string) (bool, error) {
			called = true
			require.Equal(t, expectedAuthor, author)
			require.Equal(t, txBytes, gotTxBytes)
			require.Equal(t, signature, gotSignature)
			return true, nil
		},
	})
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, expectedAuthor, payer)

	_, err = VerifySignature(signature, txBytes)
	require.ErrorIs(t, err, ErrUnsupportedSignature)
}

func newTestSigner(t *testing.T) *Ed25519Signer {
	t.Helper()

	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}

	signer, err := NewEd25519SignerFromPrivateKey(seed)
	require.NoError(t, err)
	return signer
}

const ed25519PublicKeySize = 32

func testPrivateKey(fill byte) []byte {
	privateKey := make([]byte, 32)
	for i := range privateKey {
		privateKey[i] = fill
	}
	return privateKey
}

func keypairSignatureParts(signature string, publicKeySize int) (byte, []byte, []byte, error) {
	serialized, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return 0, nil, nil, err
	}
	rawSignature := serialized[1 : len(serialized)-publicKeySize]
	publicKey := serialized[len(serialized)-publicKeySize:]
	return serialized[0], rawSignature, publicKey, nil
}

func signature64FromSlice(t *testing.T, value []byte) *signature64 {
	t.Helper()
	require.Len(t, value, 64)
	var out signature64
	copy(out[:], value)
	return &out
}

func publicKey32FromSlice(t *testing.T, value []byte) *publicKey32 {
	t.Helper()
	require.Len(t, value, 32)
	var out publicKey32
	copy(out[:], value)
	return &out
}

func publicKey33FromSlice(t *testing.T, value []byte) *publicKey33 {
	t.Helper()
	require.Len(t, value, 33)
	var out publicKey33
	copy(out[:], value)
	return &out
}

func testZkLoginSignature(t *testing.T, issuer string, addressSeed string) string {
	t.Helper()
	claim := []byte(`"iss":"` + issuer + `",`)
	require.Zero(t, len(base64.RawURLEncoding.EncodeToString(claim))%4)
	signature := zkLoginSignature{
		Inputs: zkLoginSignatureInputs{
			IssBase64Details: zkLoginSignatureClaim{
				Value:     base64.RawURLEncoding.EncodeToString(claim),
				IndexMod4: 0,
			},
			AddressSeed: addressSeed,
		},
		MaxEpoch:      1,
		UserSignature: []byte{1, 2, 3},
	}
	bytes, err := bcs.Marshal(&signature)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(append([]byte{SignatureSchemeZkLogin}, bytes...))
}
