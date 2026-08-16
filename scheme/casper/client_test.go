package casper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosuda/x402-facilitator/types"
	"github.com/stretchr/testify/require"
)

const (
	testPayer    = "account-hash-1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff000"
	testPayTo    = "account-hash-aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	testContract = "1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff001"
)

func testPaymentPayload() *types.PaymentPayload {
	return &types.PaymentPayload{
		X402Version: int(types.X402VersionV2),
		Payload: map[string]interface{}{
			"signature": "01aabb",
			"payer":     testPayer,
			"deploy":    map[string]interface{}{"hash": "aabb"},
		},
		Accepted: *testPaymentRequirements(),
	}
}

func testPaymentRequirements() *types.PaymentRequirements {
	return &types.PaymentRequirements{
		Scheme:            string(types.Exact),
		Network:           NetworkTestnet,
		Asset:             testContract,
		Amount:            "1000000000",
		PayTo:             testPayTo,
		MaxTimeoutSeconds: 60,
	}
}

func TestClientVerify(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantValid  bool
		wantReason string
		wantErr    bool
	}{
		{
			name:      "valid payment",
			status:    http.StatusOK,
			body:      `{"isValid":true,"payer":"` + testPayer + `"}`,
			wantValid: true,
		},
		{
			name:       "invalid payment",
			status:     http.StatusOK,
			body:       `{"isValid":false,"invalidReason":"insufficient_balance","invalidMessage":"payer balance too low"}`,
			wantValid:  false,
			wantReason: "insufficient_balance",
		},
		{
			name:    "server error",
			status:  http.StatusInternalServerError,
			body:    `{"error":"boom"}`,
			wantErr: true,
		},
		{
			name:    "malformed body",
			status:  http.StatusOK,
			body:    `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotRequest VerifyRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, VerifyPath, r.URL.Path)
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				require.NoError(t, json.NewDecoder(r.Body).Decode(&gotRequest))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClientWithEndpoints(server.URL, nil)
			defer client.Close()

			response, err := client.Verify(context.Background(), testPaymentPayload(), testPaymentRequirements())
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, response)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantValid, response.IsValid)
			require.Equal(t, tt.wantReason, response.InvalidReason)

			require.Equal(t, int(types.X402VersionV2), gotRequest.X402Version)
			require.Equal(t, NetworkTestnet, gotRequest.PaymentRequirements.Network)
			require.Equal(t, "1000000000", gotRequest.PaymentRequirements.Amount)
			require.Equal(t, testContract, gotRequest.PaymentRequirements.Asset)
			require.Equal(t, string(types.Exact), gotRequest.PaymentPayload.Accepted.Scheme)
		})
	}
}

func TestClientSettle(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantSuccess bool
		wantTx      string
		wantErr     bool
	}{
		{
			name:        "settled",
			status:      http.StatusOK,
			body:        `{"success":true,"transaction":"deadbeef","network":"` + NetworkTestnet + `","payer":"` + testPayer + `"}`,
			wantSuccess: true,
			wantTx:      "deadbeef",
		},
		{
			name:        "settlement failed",
			status:      http.StatusOK,
			body:        `{"success":false,"errorReason":"transaction_failed","errorMessage":"deploy reverted"}`,
			wantSuccess: false,
		},
		{
			name:    "bad gateway",
			status:  http.StatusBadGateway,
			body:    `{"message":"upstream unavailable"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, SettlePath, r.URL.Path)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClientWithEndpoints(server.URL, nil)
			defer client.Close()

			response, err := client.Settle(context.Background(), testPaymentPayload(), testPaymentRequirements())
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "upstream unavailable")
				require.Nil(t, response)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantSuccess, response.Success)
			require.Equal(t, tt.wantTx, response.Transaction)
		})
	}
}

func TestClientSupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, SupportedPath, r.URL.Path)
		_, _ = w.Write([]byte(`{"kinds":[
			{"x402Version":2,"scheme":"exact","network":"casper:casper"},
			{"x402Version":2,"scheme":"exact","network":"casper:casper-test","extra":{"asset":"WCSPR"}}
		]}`))
	}))
	defer server.Close()

	client := NewClientWithEndpoints(server.URL, nil)
	defer client.Close()

	response, err := client.Supported(context.Background())
	require.NoError(t, err)
	require.Len(t, response.Kinds, 2)
	require.Equal(t, NetworkMainnet, response.Kinds[0].Network)
	require.Equal(t, string(types.Exact), response.Kinds[0].Scheme)
	require.Equal(t, int(types.X402VersionV2), response.Kinds[0].X402Version)
	require.Equal(t, NetworkTestnet, response.Kinds[1].Network)
	require.Equal(t, "WCSPR", response.Kinds[1].Extra["asset"])
}

func TestClientFailsOverToNextEndpoint(t *testing.T) {
	var healthyHits int
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer broken.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyHits++
		_, _ = w.Write([]byte(`{"kinds":[{"x402Version":2,"scheme":"exact","network":"casper:casper"}]}`))
	}))
	defer healthy.Close()

	client := NewClientWithEndpoints(broken.URL, []string{healthy.URL})
	defer client.Close()

	response, err := client.Supported(context.Background())
	require.NoError(t, err)
	require.Len(t, response.Kinds, 1)
	require.Equal(t, 1, healthyHits)
	require.Equal(t, healthy.URL, client.ActiveEndpoint())
}

func TestClientAllEndpointsFail(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()

	client := NewClientWithEndpoints(broken.URL, nil)
	defer client.Close()

	_, err := client.Supported(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "all endpoints failed")
}

func TestNewClientForNetwork(t *testing.T) {
	client, err := NewClientForNetwork(NetworkMainnet, nil)
	require.NoError(t, err)
	require.Equal(t, DefaultFacilitatorURL, client.ActiveEndpoint())
	require.NoError(t, client.Close())

	client, err = NewClientForNetwork(NetworkTestnet, []string{"https://casper.example.invalid"})
	require.NoError(t, err)
	require.Equal(t, "https://casper.example.invalid", client.ActiveEndpoint())
	require.NoError(t, client.Close())

	_, err = NewClientForNetwork("casper:unknown", nil)
	require.ErrorIs(t, err, ErrInvalidNetwork)
}

func TestClientRejectsNilArguments(t *testing.T) {
	client := NewClientWithEndpoints("https://casper.example.invalid", nil)
	defer client.Close()

	_, err := client.Verify(context.Background(), nil, testPaymentRequirements())
	require.ErrorIs(t, err, ErrEmptyPayload)

	_, err = client.Settle(context.Background(), testPaymentPayload(), nil)
	require.ErrorIs(t, err, ErrEmptyPayload)
}
