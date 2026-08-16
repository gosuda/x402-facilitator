package facilitator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	casperscheme "github.com/gosuda/x402-facilitator/scheme/casper"
	"github.com/gosuda/x402-facilitator/types"
	"github.com/stretchr/testify/require"
)

const (
	casperTestPayer    = "account-hash-1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000"
	casperTestPayTo    = "account-hash-aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	casperTestContract = "1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff001"
)

func casperRequirements() *types.PaymentRequirements {
	return &types.PaymentRequirements{
		Scheme:            string(types.Exact),
		Network:           casperscheme.NetworkTestnet,
		Asset:             casperTestContract,
		Amount:            "1000000000",
		PayTo:             casperTestPayTo,
		MaxTimeoutSeconds: 60,
	}
}

func casperPayload() *types.PaymentPayload {
	return &types.PaymentPayload{
		X402Version: int(types.X402VersionV2),
		Payload: map[string]interface{}{
			"signature": "01aabb",
			"payer":     casperTestPayer,
			"deploy":    map[string]interface{}{"hash": "aabb"},
		},
		Accepted: *casperRequirements(),
	}
}

func newCasperTestFacilitator(t *testing.T, handler http.HandlerFunc) *CasperFacilitator {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	facilitator, err := NewCasperFacilitatorWithOptions(
		casperscheme.NetworkTestnet,
		server.URL,
		"",
		CasperFacilitatorOptions{AssetContracts: []string{casperTestContract}},
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = facilitator.Close() })
	return facilitator
}

func TestNewCasperFacilitator(t *testing.T) {
	tests := []struct {
		name    string
		network string
		wantErr bool
	}{
		{name: "mainnet", network: casperscheme.NetworkMainnet},
		{name: "testnet", network: casperscheme.NetworkTestnet},
		{name: "unsupported casper network", network: "casper:casper-dev", wantErr: true},
		{name: "wrong namespace", network: "eip155:8453", wantErr: true},
		{name: "empty", network: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facilitator, err := NewCasperFacilitator(tt.network, "", "")
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, facilitator)
				return
			}
			require.NoError(t, err)
			require.NoError(t, facilitator.Close())
		})
	}
}

func TestNewCasperFacilitatorUsesEnvURLWhenUnset(t *testing.T) {
	t.Setenv(CasperFacilitatorURLEnv, "https://casper.example.invalid")

	facilitator, err := NewCasperFacilitator(casperscheme.NetworkMainnet, "", "")
	require.NoError(t, err)
	defer facilitator.Close()
	require.Equal(t, "https://casper.example.invalid", facilitator.client.ActiveEndpoint())
}

func TestNewCasperFacilitatorRejectsInvalidAssetContract(t *testing.T) {
	_, err := NewCasperFacilitatorWithOptions(
		casperscheme.NetworkTestnet,
		"https://casper.example.invalid",
		"",
		CasperFacilitatorOptions{AssetContracts: []string{"not-a-contract-hash"}},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid Casper asset contract hash")
}

func TestCasperFacilitatorSupported(t *testing.T) {
	facilitator, err := NewCasperFacilitatorWithOptions(
		casperscheme.NetworkMainnet,
		"https://casper.example.invalid",
		"",
		CasperFacilitatorOptions{AssetContracts: []string{casperTestContract}},
	)
	require.NoError(t, err)
	defer facilitator.Close()

	supported := facilitator.Supported()
	require.NotNil(t, supported)
	require.Len(t, supported.Kinds, 1)

	kind := supported.Kinds[0]
	require.Equal(t, int(types.X402VersionV2), kind.X402Version)
	require.Equal(t, string(types.Exact), kind.Scheme)
	require.Equal(t, casperscheme.NetworkMainnet, kind.Network)
	require.Equal(t, "casper", kind.Extra["networkId"])
	require.Equal(t, "Casper Mainnet", kind.Extra["networkName"])
	require.Equal(t, casperscheme.MoteDecimals, kind.Extra["decimals"])
	require.Equal(t, []string{casperTestContract}, kind.Extra["assets"])
	require.NotNil(t, supported.Extensions)
	require.NotNil(t, supported.Signers)
}

func TestCasperFacilitatorVerifyEnvelopeValidation(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(payload *types.PaymentPayload, req *types.PaymentRequirements)
		wantReason string
	}{
		{
			name:       "x402 version mismatch",
			mutate:     func(p *types.PaymentPayload, _ *types.PaymentRequirements) { p.X402Version = 1 },
			wantReason: types.ErrInvalidPayloadFormat.Error(),
		},
		{
			name: "scheme mismatch",
			mutate: func(p *types.PaymentPayload, r *types.PaymentRequirements) {
				p.Accepted.Scheme = "upto"
				r.Scheme = "upto"
			},
			wantReason: types.ErrIncompatibleScheme.Error(),
		},
		{
			name: "network mismatch",
			mutate: func(p *types.PaymentPayload, r *types.PaymentRequirements) {
				p.Accepted.Network = casperscheme.NetworkMainnet
				r.Network = casperscheme.NetworkMainnet
			},
			wantReason: types.ErrNetworkMismatch.Error(),
		},
		{
			name:       "asset mismatch",
			mutate:     func(p *types.PaymentPayload, _ *types.PaymentRequirements) { p.Accepted.Asset = "hash-" + casperTestContract },
			wantReason: types.ErrTokenMismatch.Error(),
		},
		{
			name:       "amount mismatch",
			mutate:     func(p *types.PaymentPayload, _ *types.PaymentRequirements) { p.Accepted.Amount = "2000000000" },
			wantReason: types.ErrAmountMismatch.Error(),
		},
		{
			name:       "recipient mismatch",
			mutate:     func(p *types.PaymentPayload, _ *types.PaymentRequirements) { p.Accepted.PayTo = casperTestPayer },
			wantReason: types.ErrRecipientMismatch.Error(),
		},
		{
			name: "invalid recipient",
			mutate: func(p *types.PaymentPayload, r *types.PaymentRequirements) {
				p.Accepted.PayTo = "not-an-address"
				r.PayTo = "not-an-address"
			},
			wantReason: types.ErrRecipientMismatch.Error(),
		},
		{
			name: "unsupported asset",
			mutate: func(p *types.PaymentPayload, r *types.PaymentRequirements) {
				other := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff0"
				p.Accepted.Asset = other
				r.Asset = other
			},
			wantReason: types.ErrInvalidToken.Error(),
		},
		{
			name: "invalid amount",
			mutate: func(p *types.PaymentPayload, r *types.PaymentRequirements) {
				p.Accepted.Amount = "1.5"
				r.Amount = "1.5"
			},
			wantReason: types.ErrAmountMismatch.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facilitator := newCasperTestFacilitator(t, func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("remote facilitator should not be called for locally invalid payments")
			})

			payload, req := casperPayload(), casperRequirements()
			tt.mutate(payload, req)

			response, err := facilitator.Verify(context.Background(), payload, req)
			require.NoError(t, err)
			require.False(t, response.IsValid)
			require.Equal(t, tt.wantReason, response.InvalidReason)
		})
	}
}

func TestCasperFacilitatorVerifyRejectsNilArguments(t *testing.T) {
	facilitator := newCasperTestFacilitator(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("remote facilitator should not be called")
	})

	response, err := facilitator.Verify(context.Background(), nil, casperRequirements())
	require.NoError(t, err)
	require.False(t, response.IsValid)
	require.Equal(t, types.ErrInvalidPayloadFormat.Error(), response.InvalidReason)

	response, err = facilitator.Verify(context.Background(), casperPayload(), nil)
	require.NoError(t, err)
	require.False(t, response.IsValid)
	require.Equal(t, types.ErrInvalidPayloadFormat.Error(), response.InvalidReason)
}

func TestCasperFacilitatorVerifyRejectsMalformedPayload(t *testing.T) {
	tests := []struct {
		name       string
		payload    map[string]interface{}
		wantReason string
	}{
		{
			name:       "missing signature",
			payload:    map[string]interface{}{"payer": casperTestPayer, "deploy": map[string]interface{}{"hash": "aabb"}},
			wantReason: types.ErrInvalidSignature.Error(),
		},
		{
			name:       "missing deploy",
			payload:    map[string]interface{}{"signature": "01aabb", "payer": casperTestPayer},
			wantReason: types.ErrInvalidPayloadFormat.Error(),
		},
		{
			name:       "invalid payer",
			payload:    map[string]interface{}{"signature": "01aabb", "payer": "nope", "deploy": map[string]interface{}{"hash": "aabb"}},
			wantReason: types.ErrInvalidPayloadFormat.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facilitator := newCasperTestFacilitator(t, func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("remote facilitator should not be called for malformed payloads")
			})

			payload := casperPayload()
			payload.Payload = tt.payload

			response, err := facilitator.Verify(context.Background(), payload, casperRequirements())
			require.NoError(t, err)
			require.False(t, response.IsValid)
			require.Equal(t, tt.wantReason, response.InvalidReason)
		})
	}
}

func TestCasperFacilitatorVerifyDelegatesToRemote(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantValid  bool
		wantReason string
		wantPayer  string
	}{
		{
			name:      "remote accepts",
			body:      `{"isValid":true,"payer":"` + casperTestPayer + `"}`,
			wantValid: true,
			wantPayer: casperTestPayer,
		},
		{
			name:       "remote rejects with reason",
			body:       `{"isValid":false,"invalidReason":"insufficient_balance"}`,
			wantReason: "insufficient_balance",
			wantPayer:  casperTestPayer,
		},
		{
			name:       "remote rejects without reason",
			body:       `{"isValid":false}`,
			wantReason: types.ErrInvalidTransaction.Error(),
			wantPayer:  casperTestPayer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facilitator := newCasperTestFacilitator(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, casperscheme.VerifyPath, r.URL.Path)
				_, _ = w.Write([]byte(tt.body))
			})

			response, err := facilitator.Verify(context.Background(), casperPayload(), casperRequirements())
			require.NoError(t, err)
			require.Equal(t, tt.wantValid, response.IsValid)
			require.Equal(t, tt.wantReason, response.InvalidReason)
			require.Equal(t, tt.wantPayer, response.Payer)
		})
	}
}

func TestCasperFacilitatorVerifyPropagatesTransportError(t *testing.T) {
	facilitator := newCasperTestFacilitator(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	response, err := facilitator.Verify(context.Background(), casperPayload(), casperRequirements())
	require.Error(t, err)
	require.Nil(t, response)
	require.Contains(t, err.Error(), "casper verify failed")
}

func TestCasperFacilitatorSettle(t *testing.T) {
	tests := []struct {
		name        string
		verifyBody  string
		settleBody  string
		settleCode  int
		wantSuccess bool
		wantReason  string
		wantTx      string
	}{
		{
			name:        "settles successfully",
			verifyBody:  `{"isValid":true,"payer":"` + casperTestPayer + `"}`,
			settleBody:  `{"success":true,"transaction":"deadbeef","payer":"` + casperTestPayer + `"}`,
			settleCode:  http.StatusOK,
			wantSuccess: true,
			wantTx:      "deadbeef",
		},
		{
			name:       "verification failure short circuits settlement",
			verifyBody: `{"isValid":false,"invalidReason":"insufficient_balance"}`,
			settleCode: http.StatusOK,
			wantReason: "insufficient_balance",
		},
		{
			name:       "remote settlement failure",
			verifyBody: `{"isValid":true,"payer":"` + casperTestPayer + `"}`,
			settleBody: `{"success":false,"errorReason":"transaction_failed","errorMessage":"deploy reverted"}`,
			settleCode: http.StatusOK,
			wantReason: "transaction_failed",
		},
		{
			name:       "remote settlement transport error",
			verifyBody: `{"isValid":true,"payer":"` + casperTestPayer + `"}`,
			settleBody: `{"message":"upstream unavailable"}`,
			settleCode: http.StatusBadGateway,
			wantReason: types.ErrTransactionFailed.Error(),
		},
		{
			name:       "remote settlement failure without reason",
			verifyBody: `{"isValid":true,"payer":"` + casperTestPayer + `"}`,
			settleBody: `{"success":false}`,
			settleCode: http.StatusOK,
			wantReason: types.ErrTransactionFailed.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var settleCalled bool
			facilitator := newCasperTestFacilitator(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case casperscheme.VerifyPath:
					_, _ = w.Write([]byte(tt.verifyBody))
				case casperscheme.SettlePath:
					settleCalled = true
					w.WriteHeader(tt.settleCode)
					_, _ = w.Write([]byte(tt.settleBody))
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			})

			response, err := facilitator.Settle(context.Background(), casperPayload(), casperRequirements())
			require.NoError(t, err)
			require.Equal(t, tt.wantSuccess, response.Success)
			require.Equal(t, tt.wantTx, response.Transaction)
			require.Equal(t, types.Network(casperscheme.NetworkTestnet), response.Network)
			if tt.wantReason != "" {
				require.Equal(t, tt.wantReason, response.ErrorReason)
			}
			if tt.name == "verification failure short circuits settlement" {
				require.False(t, settleCalled)
			}
		})
	}
}

func TestNewFacilitatorRoutesCasperNetworks(t *testing.T) {
	tests := []struct {
		name    string
		scheme  types.Scheme
		network string
		wantErr bool
	}{
		{name: "mainnet", scheme: types.Exact, network: casperscheme.NetworkMainnet},
		{name: "testnet", scheme: types.Exact, network: casperscheme.NetworkTestnet},
		{name: "unsupported casper network", scheme: types.Exact, network: "casper:casper-dev", wantErr: true},
		{name: "unsupported scheme", scheme: types.Scheme("upto"), network: casperscheme.NetworkMainnet, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance, err := NewFacilitator(tt.scheme, tt.network, "https://casper.example.invalid", "")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			casperFacilitator, ok := instance.(*CasperFacilitator)
			require.True(t, ok)
			require.Equal(t, tt.network, casperFacilitator.Supported().Kinds[0].Network)
			require.NoError(t, casperFacilitator.Close())
		})
	}
}
