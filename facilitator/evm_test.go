package facilitator

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/gosuda/x402-facilitator/scheme/evm"
	"github.com/gosuda/x402-facilitator/types"
	"github.com/stretchr/testify/require"
)

const (
	PrivateKey = ""
	Network    = "eip155:84532"
	Token      = "USDC"
	Amount     = "10000"
	PayTo      = "0x1234567890123456789012345678901234567890"
)

func TestEVMVerify(t *testing.T) {
	facilitator, err := NewEVMFacilitator(Network, "", PrivateKey)
	require.NoError(t, err)

	privKey, err := hex.DecodeString("")
	require.NoError(t, err)
	evmPayload, err := evm.NewEVMPayload(Network, Token,
		"", "", Amount, evm.NewRawPrivateSigner(privKey))
	require.NoError(t, err)

	evmPayloadJson, err := json.Marshal(evmPayload)
	require.NoError(t, err)
	var payloadMap map[string]interface{}
	require.NoError(t, json.Unmarshal(evmPayloadJson, &payloadMap))

	req := &types.PaymentRequirements{
		Scheme:  string(types.Exact),
		Network: Network,
		Asset:   Token,
		Amount:  Amount,
	}
	payload := &types.PaymentPayload{
		X402Version: int(types.X402VersionV2),
		Payload:     payloadMap,
		Accepted:    *req,
	}

	res, err := facilitator.Verify(t.Context(), payload, req)
	require.NoError(t, err)
	jsonRes, err := json.MarshalIndent(res, "", "\t")
	require.NoError(t, err)
	fmt.Println(string(jsonRes))
}

func TestEVMSettle(t *testing.T) {
	facilitator, err := NewEVMFacilitator(Network, "", PrivateKey)
	require.NoError(t, err)

	privKey, err := hex.DecodeString("")
	require.NoError(t, err)
	evmPayload, err := evm.NewEVMPayload(Network, Token,
		"", "", Amount, evm.NewRawPrivateSigner(privKey))
	require.NoError(t, err)
	evmPayloadJson, err := json.Marshal(evmPayload)
	require.NoError(t, err)
	var payloadMap map[string]interface{}
	require.NoError(t, json.Unmarshal(evmPayloadJson, &payloadMap))

	req := &types.PaymentRequirements{
		Scheme:  string(types.Exact),
		Network: Network,
		Asset:   Token,
		Amount:  Amount,
	}
	payload := &types.PaymentPayload{
		X402Version: int(types.X402VersionV2),
		Payload:     payloadMap,
		Accepted:    *req,
	}

	res, err := facilitator.Settle(t.Context(), payload, req)
	require.NoError(t, err)

	jsonRes, err := json.MarshalIndent(res, "", "\t")
	require.NoError(t, err)
	fmt.Println(string(jsonRes))
}

func TestPayloadDetection(t *testing.T) {
	t.Run("detects EIP3009 payload", func(t *testing.T) {
		eip3009Json := []byte(`{"signature":"0xabc","authorization":{"from":"0x1234"}}`)
		require.False(t, evm.IsPermit2PayloadJSON(eip3009Json))
	})

	t.Run("detects Permit2 payload", func(t *testing.T) {
		permit2Json := []byte(`{"signature":"0xabc","permit2Authorization":{"from":"0x1234"}}`)
		require.True(t, evm.IsPermit2PayloadJSON(permit2Json))
	})

	t.Run("returns false for invalid JSON", func(t *testing.T) {
		require.False(t, evm.IsPermit2PayloadJSON([]byte(`{invalid`)))
	})

	t.Run("returns false for empty payload", func(t *testing.T) {
		require.False(t, evm.IsPermit2PayloadJSON([]byte(`{}`)))
	})
}

func TestEIP3009VerifyRejectsAuthorizationBindingMismatch(t *testing.T) {
	facilitator := testEVMFacilitator()
	req := testEIP3009Requirement()

	t.Run("recipient mismatch", func(t *testing.T) {
		payload, raw := testEIP3009Payload(t, "0x2345678901234567890123456789012345678901", Amount)

		res, err := facilitator.verifyEIP3009(t.Context(), payload, req, raw, nil)
		require.NoError(t, err)
		require.False(t, res.IsValid)
		require.Equal(t, types.ErrRecipientMismatch.Error(), res.InvalidReason)
	})

	t.Run("amount mismatch", func(t *testing.T) {
		payload, raw := testEIP3009Payload(t, PayTo, "1")

		res, err := facilitator.verifyEIP3009(t.Context(), payload, req, raw, nil)
		require.NoError(t, err)
		require.False(t, res.IsValid)
		require.Equal(t, types.ErrAmountMismatch.Error(), res.InvalidReason)
	})
}

func TestEIP3009VerifyRejectsAcceptedRequirementDrift(t *testing.T) {
	facilitator := testEVMFacilitator()
	req := testEIP3009Requirement()

	t.Run("accepted amount drift", func(t *testing.T) {
		payload, raw := testEIP3009Payload(t, PayTo, Amount)
		payload.Accepted.Amount = "1"

		res, err := facilitator.verifyEIP3009(t.Context(), payload, req, raw, nil)
		require.NoError(t, err)
		require.False(t, res.IsValid)
		require.Equal(t, types.ErrAmountMismatch.Error(), res.InvalidReason)
	})

	t.Run("accepted recipient drift", func(t *testing.T) {
		payload, raw := testEIP3009Payload(t, PayTo, Amount)
		payload.Accepted.PayTo = "0x2345678901234567890123456789012345678901"

		res, err := facilitator.verifyEIP3009(t.Context(), payload, req, raw, nil)
		require.NoError(t, err)
		require.False(t, res.IsValid)
		require.Equal(t, types.ErrRecipientMismatch.Error(), res.InvalidReason)
	})
}

func TestEIP3009VerifyRejectsAuthorizationTimeBounds(t *testing.T) {
	facilitator := testEVMFacilitator()
	req := testEIP3009Requirement()

	t.Run("expired", func(t *testing.T) {
		payload, raw := testEIP3009PayloadWithMutation(t, PayTo, Amount, func(auth *evm.Authorization) {
			auth.ValidBefore = big.NewInt(time.Now().Unix() - 1)
		})

		res, err := facilitator.verifyEIP3009(t.Context(), payload, req, raw, nil)
		require.NoError(t, err)
		require.False(t, res.IsValid)
		require.Equal(t, types.ErrAuthorizationExpired.Error(), res.InvalidReason)
	})

	t.Run("not yet valid", func(t *testing.T) {
		payload, raw := testEIP3009PayloadWithMutation(t, PayTo, Amount, func(auth *evm.Authorization) {
			auth.ValidAfter = big.NewInt(time.Now().Unix() + 60)
		})

		res, err := facilitator.verifyEIP3009(t.Context(), payload, req, raw, nil)
		require.NoError(t, err)
		require.False(t, res.IsValid)
		require.Equal(t, types.ErrAuthorizationNotYetValid.Error(), res.InvalidReason)
	})
}

func TestEIP3009SettleRejectsAuthorizationBindingMismatchBeforeDial(t *testing.T) {
	facilitator := testEVMFacilitator()
	req := testEIP3009Requirement()
	payload, _ := testEIP3009Payload(t, "0x2345678901234567890123456789012345678901", Amount)

	res, err := facilitator.Settle(t.Context(), payload, req)
	require.NoError(t, err)
	require.False(t, res.Success)
	require.Equal(t, types.ErrRecipientMismatch.Error(), res.ErrorReason)
}

func testEVMFacilitator() *EVMFacilitator {
	return &EVMFacilitator{
		scheme:    types.Exact,
		network:   Network,
		networkID: evm.GetChainID(Network),
	}
}

func testEIP3009Requirement() *types.PaymentRequirements {
	return &types.PaymentRequirements{
		Scheme:  string(types.Exact),
		Network: Network,
		Asset:   Token,
		Amount:  Amount,
		PayTo:   PayTo,
	}
}

func testEIP3009Payload(t *testing.T, to string, amount string) (*types.PaymentPayload, []byte) {
	t.Helper()
	return testEIP3009PayloadWithMutation(t, to, amount, nil)
}

func testEIP3009PayloadWithMutation(t *testing.T, to string, amount string, mutate func(*evm.Authorization)) (*types.PaymentPayload, []byte) {
	t.Helper()

	privKey, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	from, err := evm.GetAddrssFromPrivateKey(privKey.Serialize())
	require.NoError(t, err)
	evmPayload, err := evm.NewEVMPayload(Network, Token, from.Hex(), to, amount, evm.NewRawPrivateSigner(privKey.Serialize()))
	require.NoError(t, err)
	if mutate != nil {
		mutate(evmPayload.Authorization)
		evmPayload.Signature, err = evm.SignEip3009(
			evmPayload.Authorization,
			evm.GetDomainConfig(Network, Token),
			evm.NewRawPrivateSigner(privKey.Serialize()),
		)
		require.NoError(t, err)
	}

	evmPayloadJSON, err := json.Marshal(evmPayload)
	require.NoError(t, err)
	var payloadMap map[string]interface{}
	require.NoError(t, json.Unmarshal(evmPayloadJSON, &payloadMap))

	req := testEIP3009Requirement()
	payload := &types.PaymentPayload{
		X402Version: int(types.X402VersionV2),
		Payload:     payloadMap,
		Accepted:    *req,
	}
	raw, err := json.Marshal(payload.Payload)
	require.NoError(t, err)
	return payload, raw
}
