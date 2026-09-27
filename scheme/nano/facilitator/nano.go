// Package facilitator implements the Nano (XNO) x402 "exact" facilitator.
//
// Nano has no smart contracts and no memo field, and the payer broadcasts its
// own signed send block directly onto a feeless DAG ledger — there is no
// facilitator settlement to broadcast and no gas token to carry. The block on
// the ledger IS the spent row, so this facilitator is read-only on the chain:
// Verify checks the block on at least two independent RPC nodes (fail closed)
// and Settle re-verifies then binds the block hash exactly once, so one proof
// yields one resource. The pay-to destination is a per-invoice Nano account,
// which is what binds the payment to a specific invoice in the absence of a
// memo.
package facilitator

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosuda/x402-facilitator/scheme"
	nanoscheme "github.com/gosuda/x402-facilitator/scheme/nano"
	"github.com/gosuda/x402-facilitator/types"
)

var _ scheme.Facilitator = (*NanoFacilitator)(nil)

// NanoFacilitatorURLEnv overrides the default Nano RPC endpoints when no
// endpoints are supplied through configuration.
const NanoFacilitatorURLEnv = "NANO_FACILITATOR_RPC_URLS"

// NanoFacilitator settles x402 "exact" payments on Nano. It verifies a payment
// block on at least two independent RPC nodes and binds it exactly once.
type NanoFacilitator struct {
	scheme      types.Scheme
	network     string
	client      *nanoscheme.Client
	endpoints   []string
	claims      *ClaimStore
	asset       string
	decimals    int
	networkName string
}

// NanoFacilitatorOptions customizes the facilitator's verification endpoints.
type NanoFacilitatorOptions struct {
	// Endpoints are the independent Nano RPC nodes used for verification.
	// When nil, the network's default public endpoints are used.
	Endpoints []string
	// Client overrides the RPC transport (the test seam). When nil, a real
	// HTTP client is built.
	Client *nanoscheme.Client
}

// NewNanoFacilitator builds a Nano facilitator for a CAIP-2 Nano network.
// privateKeyHex is unused: the payer broadcasts its own send block and the
// facilitator never signs, moves or holds funds.
func NewNanoFacilitator(network, rpcURL, privateKeyHex string) (*NanoFacilitator, error) {
	return NewNanoFacilitatorWithOptions(network, rpcURL, privateKeyHex, NanoFacilitatorOptions{})
}

// NewNanoFacilitatorWithOptions builds a Nano facilitator with explicit
// verification endpoints and/or client.
func NewNanoFacilitatorWithOptions(network, rpcURL string, privateKeyHex string, opts NanoFacilitatorOptions) (*NanoFacilitator, error) {
	if !nanoscheme.IsNanoNetwork(network) {
		return nil, fmt.Errorf("unsupported Nano network %q", network)
	}
	info := nanoscheme.GetNetworkInfo(network)
	if info == nil {
		return nil, fmt.Errorf("unsupported Nano network %q", network)
	}

	endpoints := append([]string(nil), info.DefaultURLs...)
	if len(opts.Endpoints) > 0 {
		endpoints = append([]string(nil), opts.Endpoints...)
	}
	if strings.TrimSpace(rpcURL) != "" {
		endpoints = append([]string{strings.TrimSpace(rpcURL)}, endpoints...)
	}

	client := opts.Client
	if client == nil {
		client = nanoscheme.NewClient(0)
	}

	return &NanoFacilitator{
		scheme:      types.Exact,
		network:     network,
		client:      client,
		endpoints:   endpoints,
		claims:      NewClaimStore(),
		asset:       nanoscheme.AssetXNO,
		decimals:    nanoscheme.XNORawDecimals,
		networkName: info.NetworkName,
	}, nil
}

// Verify reports whether a Nano payment payload is valid against the supplied
// requirements. It checks requirement consistency locally, then verifies the
// referenced send block on at least two independent RPC nodes, fail closed.
func (t *NanoFacilitator) Verify(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentVerifyResponse, error) {
	if payload == nil || req == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}

	blockHash, payerFromPayload, invalid := nanoProof(payload)
	if invalid != nil {
		return invalid, nil
	}

	if env := t.validatePaymentEnvelope(payload, req); env != nil {
		return env, nil
	}

	res := nanoscheme.VerifyBlock(ctx, t.client, t.endpoints, blockHash, req.PayTo, req.Amount)
	// The block's real payer (read from the ledger) is authoritative. A
	// non-empty payer supplied in the payload that CONTRADICTS the block is
	// rejected outright rather than trusted; the payload value is never used
	// for payer attribution.
	if res.Payer != "" && payerFromPayload != "" && !strings.EqualFold(strings.TrimSpace(payerFromPayload), strings.TrimSpace(res.Payer)) {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrInvalidPayloadFormat.Error(),
			InvalidMessage: "payment proof payer contradicts the block's payer",
		}, nil
	}
	if !res.OK {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  verifyReason(res.Reason),
			InvalidMessage: res.Reason,
			Payer:          res.Payer,
		}, nil
	}

	return &types.PaymentVerifyResponse{
		IsValid: true,
		Payer:   res.Payer,
	}, nil
}

// Settle re-verifies the payment then binds the block hash exactly once (the
// single-use claim). Nano needs no broadcast: the payer already sent, and the
// block on the ledger is the receipt. The returned Transaction is the block
// hash itself.
func (t *NanoFacilitator) Settle(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentSettleResponse, error) {
	network := types.Network("")
	if req != nil {
		network = types.Network(req.Network)
	}

	// Never trust a prior /verify: settle re-verifies the block fresh.
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

	blockHash, _, _ := nanoProof(payload)
	if t.claims == nil || !t.claims.Claim(blockHash) {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrAuthorizationAlreadyUsed.Error(),
			ErrorMessage: "payment proof already settled",
			Payer:        verified.Payer,
			Network:      network,
		}, nil
	}

	return &types.PaymentSettleResponse{
		Success:     true,
		Payer:       verified.Payer,
		Transaction: blockHash,
		Network:     network,
	}, nil
}

// Supported returns the (scheme, network) pair this facilitator settles.
func (t *NanoFacilitator) Supported() *types.SupportedResponse {
	return &types.SupportedResponse{
		Kinds: []types.SupportedKind{{
			X402Version: int(types.X402VersionV2),
			Scheme:      string(t.scheme),
			Network:     t.network,
			Extra: map[string]interface{}{
				"assetTransferMethod": "nano-native-send",
				"assets":              []string{t.asset},
				"decimals":            t.decimals,
				"networkId":           t.network,
				"networkName":         t.networkName,
			},
		}},
		Extensions: []string{},
		Signers:    map[string][]string{},
	}
}

// nanoProof extracts the send-block hash (the payment proof) from a payload.
// On Nano the payer broadcasts the send and includes its block hash so the
// facilitator can verify it on the ledger. The returned hash is normalized to
// uppercase so case variants share one canonical claim key and one
// RPC-compatible hash.
func nanoProof(payload *types.PaymentPayload) (blockHash string, payer string, invalid *types.PaymentVerifyResponse) {
	if payload.Payload["blockHash"] == nil && payload.Payload["transaction"] == nil && payload.Payload["paymentProof"] == nil {
		return "", "", &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrInvalidPayloadFormat.Error(),
			InvalidMessage: "nano payment payload requires a blockHash",
		}
	}
	var h string
	for _, k := range []string{"blockHash", "paymentProof", "transaction"} {
		if s, ok := payload.Payload[k].(string); ok && s != "" {
			h = s
			break
		}
	}
	h = strings.ToUpper(strings.TrimSpace(h))
	if !nanoscheme.IsBlockHash(h) {
		return "", "", &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrInvalidTransaction.Error(),
			InvalidMessage: "payment proof is not a 64-hex Nano block hash",
		}
	}
	p, _ := payload.Payload["payer"].(string)
	return h, strings.TrimSpace(p), nil
}

func (t *NanoFacilitator) validatePaymentEnvelope(payload *types.PaymentPayload, req *types.PaymentRequirements) *types.PaymentVerifyResponse {
	if payload.X402Version != int(types.X402VersionV2) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}
	}
	if payload.Accepted.Scheme != string(t.scheme) || req.Scheme != string(t.scheme) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrIncompatibleScheme.Error(),
		}
	}
	if payload.Accepted.Network != t.network || req.Network != t.network {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrNetworkMismatch.Error(),
		}
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Accepted.Asset), strings.TrimSpace(req.Asset)) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrTokenMismatch.Error(),
		}
	}
	if payload.Accepted.Amount != req.Amount {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAmountMismatch.Error(),
		}
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Accepted.PayTo), strings.TrimSpace(req.PayTo)) ||
		strings.TrimSpace(req.PayTo) == "" {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrRecipientMismatch.Error(),
		}
	}
	return nil
}

func verifyReason(reason string) string {
	switch {
	case strings.Contains(reason, "block is not confirmed"):
		return types.ErrInvalidTransaction.Error()
	case strings.Contains(reason, "destination does not match"):
		return types.ErrRecipientMismatch.Error()
	case strings.Contains(reason, "amount does not match"):
		return types.ErrAmountMismatch.Error()
	case strings.Contains(reason, "not a send"):
		return types.ErrInvalidTransaction.Error()
	default:
		if strings.TrimSpace(reason) == "" {
			return types.ErrInvalidTransaction.Error()
		}
		return reason
	}
}
