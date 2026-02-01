package evm

import (
	"context"
	"fmt"

	x402types "github.com/coinbase/x402/go/types"
	"github.com/gosuda/x402-facilitator/facilitator/evm/signer"
	"github.com/gosuda/x402-facilitator/types"
)

var _ types.SchemeNetworkFacilitator = (*Facilitator)(nil)

// Facilitator implements the Facilitator interface using x402 SDK's ExactEvmScheme.
// This is a pure V2 implementation that delegates all operations to the SDK.
type Facilitator struct {
	scheme  types.SchemeNetworkFacilitator
	network string
}

// NewFacilitator creates a new EVM facilitator using the x402 SDK's ExactEvmScheme.
func NewFacilitator(network string, rpcURL string, privateKeyHex string) (*Facilitator, error) {
	if network == "" {
		return nil, fmt.Errorf("network is required")
	}
	if rpcURL == "" {
		return nil, fmt.Errorf("rpc URL is required")
	}
	if privateKeyHex == "" {
		return nil, fmt.Errorf("private key is required")
	}

	// Create EVMSigner with callback-based signing
	config := &signer.EVMSignerConfig{
		RpcURL:     rpcURL,
		PrivateKey: privateKeyHex,
	}

	signerInstance, err := signer.NewEVMSigner(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create EVM signer: %w", err)
	}

	// Create SDK's ExactEvmScheme with the signer
	// Note: SDK's NewExactEvmScheme takes a pointer to config
	schemeConfig := &types.ExactEvmSchemeConfig{
		// Enable smart wallet auto-deployment if needed
		// DeployERC4337WithEIP6492: true,
	}

	scheme := types.NewExactEvmScheme(signerInstance, schemeConfig)
	if scheme == nil {
		signerInstance.Close()
		return nil, fmt.Errorf("failed to create ExactEvmScheme")
	}

	return &Facilitator{
		scheme:  scheme,
		network: network,
	}, nil
}

// NewFacilitatorWithCallback creates a new EVM facilitator with a custom signing callback.
// This allows for external key management (e.g., AWS KMS, HashiCorp Vault).
func NewFacilitatorWithCallback(network string, rpcURL string, callback types.Signer, addresses []string) (*Facilitator, error) {
	if network == "" {
		return nil, fmt.Errorf("network is required")
	}
	if rpcURL == "" {
		return nil, fmt.Errorf("rpc URL is required")
	}
	if callback == nil {
		return nil, fmt.Errorf("signer callback is required")
	}

	// Create EVMSigner with callback
	config := &signer.EVMSignerConfig{
		RpcURL:    rpcURL,
		Signer:    callback,
		Addresses: addresses,
	}

	signerInstance, err := signer.NewEVMSigner(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create EVM signer: %w", err)
	}

	// Create SDK's ExactEvmScheme
	schemeConfig := &types.ExactEvmSchemeConfig{}
	scheme := types.NewExactEvmScheme(signerInstance, schemeConfig)
	if scheme == nil {
		signerInstance.Close()
		return nil, fmt.Errorf("failed to create ExactEvmScheme")
	}

	return &Facilitator{
		scheme:  scheme,
		network: network,
	}, nil
}

// Verify validates a payment payload without executing on-chain.
// It delegates to the SDK's ExactEvmScheme.Verify method.
func (f *Facilitator) Verify(ctx context.Context, payload x402types.PaymentPayload, req x402types.PaymentRequirements) (*types.VerifyResponse, error) {
	// Call SDK's Verify directly with SDK types
	result, err := f.scheme.Verify(ctx, payload, req)
	if err != nil {
		return nil, fmt.Errorf("verify failed: %w", err)
	}

	return result, nil
}

// Settle executes the payment on-chain.
// It delegates to the SDK's ExactEvmScheme.Settle method.
func (f *Facilitator) Settle(ctx context.Context, payload x402types.PaymentPayload, req x402types.PaymentRequirements) (*types.SettleResponse, error) {
	// Call SDK's Settle directly with SDK types
	result, err := f.scheme.Settle(ctx, payload, req)
	if err != nil {
		return nil, fmt.Errorf("settle failed: %w", err)
	}

	return result, nil
}

// Scheme returns the scheme identifier for this facilitator.
func (f *Facilitator) Scheme() string {
	return "exact"
}

// CaipFamily returns the CAIP family pattern this facilitator supports.
func (f *Facilitator) CaipFamily() string {
	return "eip155:*"
}

// GetExtra returns mechanism-specific extra data for the supported kinds endpoint.
func (f *Facilitator) GetExtra(network types.Network) map[string]interface{} {
	// EVM schemes return nil (no extra data needed)
	return nil
}

// GetSigners returns signer addresses used by this facilitator for a given network.
func (f *Facilitator) GetSigners(network types.Network) []string {
	// TODO: Return actual signer addresses from the underlying signer
	return []string{}
}

// GetScheme returns the underlying SDK scheme (for advanced usage).
func (f *Facilitator) GetScheme() types.SchemeNetworkFacilitator {
	return f.scheme
}
