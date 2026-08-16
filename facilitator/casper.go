package facilitator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	casperscheme "github.com/gosuda/x402-facilitator/scheme/casper"
	"github.com/gosuda/x402-facilitator/types"
)

var _ Facilitator = (*CasperFacilitator)(nil)

// CasperFacilitatorURLEnv overrides the Casper facilitator base URL when no
// URL is supplied through configuration.
const CasperFacilitatorURLEnv = "CASPER_FACILITATOR_URL"

// CasperFacilitator settles x402 "exact" payments on Casper. Casper payments
// are CEP-18 (wCSPR, 9 decimals) transfers authorized by the payer and
// broadcast by a Casper facilitator service, so this implementation delegates
// verification and settlement over HTTP rather than signing locally.
type CasperFacilitator struct {
	scheme  types.Scheme
	network string
	client  *casperscheme.Client
	assets  []string
}

// CasperFacilitatorOptions customizes the settlement asset allowlist.
type CasperFacilitatorOptions struct {
	// AssetContracts are CEP-18 contract hashes accepted for settlement. When
	// empty, any syntactically valid contract hash configured by the payment
	// requirements is accepted.
	AssetContracts []string
}

// NewCasperFacilitator builds a Casper facilitator for a CAIP-2 Casper
// network. privateKeyHex is unused: settlement fees are paid by the Casper
// facilitator service.
func NewCasperFacilitator(network string, url string, privateKeyHex string) (*CasperFacilitator, error) {
	return NewCasperFacilitatorWithOptions(network, url, privateKeyHex, CasperFacilitatorOptions{})
}

// NewCasperFacilitatorWithOptions builds a Casper facilitator with an explicit
// settlement asset allowlist.
func NewCasperFacilitatorWithOptions(network string, url string, privateKeyHex string, opts CasperFacilitatorOptions) (*CasperFacilitator, error) {
	if !casperscheme.IsCasperNetwork(network) {
		return nil, fmt.Errorf("unsupported Casper network %q", network)
	}
	networkInfo := casperscheme.GetNetworkInfo(network)
	if networkInfo == nil {
		return nil, fmt.Errorf("unsupported Casper network %q", network)
	}
	if strings.TrimSpace(url) == "" {
		url = strings.TrimSpace(os.Getenv(CasperFacilitatorURLEnv))
	}
	client := casperscheme.NewClientWithEndpoints(url, networkInfo.DefaultURLs)

	assets := casperscheme.GetAssetTypes(network)
	if opts.AssetContracts != nil {
		assets = append([]string(nil), opts.AssetContracts...)
	}
	assets, err := casperAssetAllowlist(assets)
	if err != nil {
		return nil, err
	}

	return &CasperFacilitator{
		scheme:  types.Exact,
		network: network,
		client:  client,
		assets:  assets,
	}, nil
}

// Close releases the underlying HTTP connections.
func (t *CasperFacilitator) Close() error {
	if t == nil || t.client == nil {
		return nil
	}
	return t.client.Close()
}

func casperAssetAllowlist(assets []string) ([]string, error) {
	ordered := make([]string, 0, len(assets))
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		asset = strings.TrimSpace(asset)
		if asset == "" {
			continue
		}
		hash := casperscheme.NormalizeContractHash(asset)
		if hash == "" {
			return nil, fmt.Errorf("invalid Casper asset contract hash: %s", asset)
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		ordered = append(ordered, asset)
	}
	return ordered, nil
}

func (t *CasperFacilitator) Verify(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentVerifyResponse, error) {
	if payload == nil || req == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}

	casperPayload, err := casperscheme.PayloadFromMap(payload.Payload)
	if err != nil {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  invalidCasperPayloadReason(err),
			InvalidMessage: err.Error(),
		}, nil
	}
	payer := casperscheme.NormalizeAddress(casperPayload.Payer)

	if invalid := t.validatePaymentEnvelope(payload, req, payer); invalid != nil {
		return invalid, nil
	}

	if _, err := casperscheme.ParseMotes(req.Amount); err != nil {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrAmountMismatch.Error(),
			InvalidMessage: err.Error(),
			Payer:          payer,
		}, nil
	}

	verified, err := t.client.Verify(ctx, payload, req)
	if err != nil {
		return nil, fmt.Errorf("casper verify failed: %w", err)
	}
	if !verified.IsValid {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  casperInvalidReason(verified.InvalidReason),
			InvalidMessage: verified.InvalidMessage,
			Payer:          casperPayerOrDefault(verified.Payer, payer),
		}, nil
	}

	return &types.PaymentVerifyResponse{
		IsValid: true,
		Payer:   casperPayerOrDefault(verified.Payer, payer),
	}, nil
}

func (t *CasperFacilitator) Settle(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentSettleResponse, error) {
	network := types.Network("")
	if req != nil {
		network = types.Network(req.Network)
	}

	verified, err := t.Verify(ctx, payload, req)
	if err != nil {
		return nil, err
	}
	if !verified.IsValid {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  verified.InvalidReason,
			ErrorMessage: verified.InvalidMessage,
			Payer:        verified.Payer,
			Network:      network,
		}, nil
	}

	settled, err := t.client.Settle(ctx, payload, req)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        verified.Payer,
			Network:      network,
		}, nil
	}
	if !settled.Success {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  casperErrorReason(settled.ErrorReason),
			ErrorMessage: settled.ErrorMessage,
			Payer:        casperPayerOrDefault(settled.Payer, verified.Payer),
			Transaction:  settled.Transaction,
			Network:      network,
		}, nil
	}

	return &types.PaymentSettleResponse{
		Success:     true,
		Payer:       casperPayerOrDefault(settled.Payer, verified.Payer),
		Transaction: settled.Transaction,
		Network:     network,
	}, nil
}

func (t *CasperFacilitator) Supported() *types.SupportedResponse {
	return &types.SupportedResponse{
		Kinds: []types.SupportedKind{{
			X402Version: int(types.X402VersionV2),
			Scheme:      string(t.scheme),
			Network:     t.network,
			Extra: map[string]interface{}{
				"assetTransferMethod": "casper-cep18-facilitated-transfer",
				"assets":              t.assets,
				"decimals":            casperscheme.MoteDecimals,
				"networkId":           casperscheme.GetNetworkID(t.network),
				"networkName":         casperscheme.GetNetworkName(t.network),
			},
		}},
		Extensions: []string{},
		Signers:    map[string][]string{},
	}
}

func (t *CasperFacilitator) validatePaymentEnvelope(payload *types.PaymentPayload, req *types.PaymentRequirements, payer string) *types.PaymentVerifyResponse {
	if payload.X402Version != int(types.X402VersionV2) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
			Payer:         payer,
		}
	}
	if payload.Accepted.Scheme != string(t.scheme) || req.Scheme != string(t.scheme) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrIncompatibleScheme.Error(),
			Payer:         payer,
		}
	}
	if payload.Accepted.Network != t.network || req.Network != t.network {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrNetworkMismatch.Error(),
			Payer:         payer,
		}
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Accepted.Asset), strings.TrimSpace(req.Asset)) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrTokenMismatch.Error(),
			Payer:         payer,
		}
	}
	if payload.Accepted.Amount != req.Amount {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAmountMismatch.Error(),
			Payer:         payer,
		}
	}
	if casperscheme.NormalizeAddress(payload.Accepted.PayTo) != casperscheme.NormalizeAddress(req.PayTo) ||
		casperscheme.NormalizeAddress(req.PayTo) == "" {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrRecipientMismatch.Error(),
			Payer:         payer,
		}
	}
	if !t.isSupportedAsset(req.Asset) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidToken.Error(),
			Payer:         payer,
		}
	}
	return nil
}

func (t *CasperFacilitator) isSupportedAsset(asset string) bool {
	_, ok := t.canonicalAsset(asset)
	return ok
}

func (t *CasperFacilitator) canonicalAsset(asset string) (string, bool) {
	if t == nil {
		return "", false
	}
	hash := casperscheme.NormalizeContractHash(asset)
	if hash == "" {
		return "", false
	}
	if len(t.assets) == 0 {
		return hash, true
	}
	for _, candidate := range t.assets {
		if casperscheme.NormalizeContractHash(candidate) == hash {
			return candidate, true
		}
	}
	return "", false
}

func invalidCasperPayloadReason(err error) string {
	if errors.Is(err, casperscheme.ErrEmptySignature) {
		return types.ErrInvalidSignature.Error()
	}
	return types.ErrInvalidPayloadFormat.Error()
}

func casperInvalidReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return types.ErrInvalidTransaction.Error()
	}
	return reason
}

func casperErrorReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return types.ErrTransactionFailed.Error()
	}
	return reason
}

func casperPayerOrDefault(payer, fallback string) string {
	if strings.TrimSpace(payer) != "" {
		return payer
	}
	return fallback
}
