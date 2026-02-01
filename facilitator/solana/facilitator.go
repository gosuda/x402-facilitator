package solana

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/blocto/solana-go-sdk/client"
	solTypes "github.com/blocto/solana-go-sdk/types"
	x402types "github.com/coinbase/x402/go/types"

	"github.com/gosuda/x402-facilitator/types"
)

type Facilitator struct {
	scheme   types.Scheme
	client   *client.Client
	feePayer solTypes.Account
}

func NewFacilitator(network string, url string, privateKeyHex string) (*Facilitator, error) {
	client := client.NewClient(url)

	privKey, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hex private key: %w", err)
	}

	feePayer, err := solTypes.AccountFromBytes(privKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key format: %w", err)
	}

	return &Facilitator{
		scheme:   types.Solana,
		client:   client,
		feePayer: feePayer,
	}, nil
}

func (t *Facilitator) Verify(ctx context.Context, payload x402types.PaymentPayload, req x402types.PaymentRequirements) (*types.VerifyResponse, error) {
	return nil, fmt.Errorf("solana verify not implemented")
}

func (t *Facilitator) Settle(ctx context.Context, payload x402types.PaymentPayload, req x402types.PaymentRequirements) (*types.SettleResponse, error) {
	return nil, fmt.Errorf("solana settle not implemented")
}

// Scheme returns the scheme identifier for this facilitator.
func (t *Facilitator) Scheme() string {
	return string(types.Solana)
}

// CaipFamily returns the CAIP family pattern this facilitator supports.
func (t *Facilitator) CaipFamily() string {
	return "solana:*"
}

// GetExtra returns mechanism-specific extra data for the supported kinds endpoint.
func (t *Facilitator) GetExtra(network types.Network) map[string]interface{} {
	// SVM schemes return feePayer address
	return map[string]interface{}{
		"feePayer": t.feePayer.PublicKey.ToBase58(),
	}
}

// GetSigners returns signer addresses used by this facilitator for a given network.
func (t *Facilitator) GetSigners(network types.Network) []string {
	return []string{t.feePayer.PublicKey.ToBase58()}
}
