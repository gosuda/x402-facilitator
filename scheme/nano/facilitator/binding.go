package facilitator

import (
	"strings"

	nanoscheme "github.com/gosuda/x402-facilitator/scheme/nano"
	"github.com/gosuda/x402-facilitator/types"
)

// checkBinding enforces the requester binding (nano-block-sig/1).
//
// A block hash alone is a bearer proof: the block is public the moment it is
// broadcast, so anyone who sees it can present it first. The binding closes
// that: the payer signs (blockHash, nonce, resource, payTo) with the key of the
// account that made the payment, and the facilitator verifies the signature
// against the block's account as READ FROM THE LEDGER (payerFromLedger), never
// against anything in the request. The seller supplies nonce and resource in
// the 402 (requirements.extra.binding); the facilitator compares the payload's
// nonce to them. The seller owns nonce freshness (it issued the nonce);
// the facilitator owns "was this signed by the payer of this block".
//
// It returns nil when the payment is acceptable.
func (t *NanoFacilitator) checkBinding(payload *types.PaymentPayload, req *types.PaymentRequirements, blockHash, payerFromLedger string) *types.PaymentVerifyResponse {
	fail := func(err error, msg string) *types.PaymentVerifyResponse {
		return &types.PaymentVerifyResponse{IsValid: false, InvalidReason: err.Error(), InvalidMessage: msg}
	}
	raw, present := payload.Payload["binding"]
	if !present || raw == nil {
		if t.requireBind {
			return fail(nanoscheme.ErrBindingMissing, "this facilitator requires a "+nanoscheme.BindingScheme+" binding signed by the paying account")
		}
		return nil
	}
	b, ok := raw.(map[string]interface{})
	if !ok {
		return fail(nanoscheme.ErrBindingMalformed, "binding must be an object with a nonce and a signature")
	}
	nonce, _ := b["nonce"].(string)
	sig, _ := b["signature"].(string)
	if nonce == "" || sig == "" {
		return fail(nanoscheme.ErrBindingMalformed, "binding needs a nonce and a signature")
	}
	offered, _ := req.Extra["binding"].(map[string]interface{})
	wantNonce, _ := offered["nonce"].(string)
	resource, _ := offered["resource"].(string)
	if wantNonce == "" || resource == "" {
		return fail(nanoscheme.ErrBindingNotOffered, "the payment requirements carry no extra.binding nonce and resource to bind to")
	}
	if nonce != wantNonce {
		return fail(nanoscheme.ErrBindingNonce, "binding nonce is not the one the seller issued in the 402")
	}
	msg, err := nanoscheme.BindingMessage(blockHash, nonce, resource, req.PayTo)
	if err != nil {
		return fail(err, "binding message cannot be built from this block hash and pay-to")
	}
	if strings.TrimSpace(payerFromLedger) == "" {
		return fail(nanoscheme.ErrBindingInvalid, "the block's payer could not be read from the ledger")
	}
	if err := nanoscheme.VerifyBinding(payerFromLedger, msg, sig); err != nil {
		return fail(err, "binding signature does not match the account that made the payment")
	}
	return nil
}
