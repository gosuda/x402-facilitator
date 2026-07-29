// Package erc7710 implements the x402 v2 "exact" scheme's erc7710 asset-transfer method against
// an ERC-7710 delegation manager.
//
// The design premise, and the reason this package is small: the facilitator holds NO policy. It
// does not know what a caveat is, what an identity attestation is, or what a spending cap looks
// like. It builds one execution - transfer(payTo, amount) on the required asset - and asks the
// delegation manager, by simulation, whether the presented permission context authorises it. The
// entire policy engine lives on-chain; the facilitator is an unprivileged broadcaster that can
// neither widen a delegation's scope nor redirect its funds.
//
// It is also the only assetTransferMethod in the x402 v2 exact/EVM spec that supports MULTI-USE:
// an EIP-3009 authorization dies with its nonce, while one delegation keeps paying until its own
// caveats - a spending cap, a time window, whatever else the delegator signed - refuse it.
package erc7710

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gosuda/x402-facilitator/types"
)

// AssetTransferMethod is the exact/EVM scheme extra value this package serves.
// Spec: x402-foundation/x402 specs/schemes/exact/scheme_exact_evm.md
const AssetTransferMethod = "erc7710"

// ERC7710Payload is the scheme-specific payment material for the erc7710 method.
type ERC7710Payload struct {
	// PermissionContext is the opaque bytes redeemed on-chain. Managers following the MetaMask
	// delegation-framework layout encode it as abi.encode(Delegation[]), leaf first and root
	// last - but the facilitator never needs to know that. It decodes only far enough to name
	// the root delegator as Payer and treats the rest as bytes it has no business interpreting.
	PermissionContext string `json:"permissionContext"`
	// DelegationManager is the ERC-7710 manager the context must be redeemed on. It MUST match
	// the facilitator's pinned manager: settling against an arbitrary contract would let anyone
	// drain the fee payer with reverting broadcasts.
	DelegationManager string `json:"delegationManager"`
}

// ParsePayload extracts and validates an ERC7710Payload from the wire payload map.
func ParsePayload(raw map[string]interface{}) (*ERC7710Payload, error) {
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", types.ErrInvalidPayloadFormat, err)
	}
	var p ERC7710Payload
	if err := json.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", types.ErrInvalidPayloadFormat, err)
	}
	if !has0xPrefix(p.PermissionContext) || len(p.PermissionContext) < 4 {
		return nil, fmt.Errorf("%w: permissionContext must be 0x-prefixed hex", types.ErrInvalidPayloadFormat)
	}
	if !common.IsHexAddress(p.DelegationManager) {
		return nil, fmt.Errorf("%w: delegationManager must be an address", types.ErrInvalidPayloadFormat)
	}
	return &p, nil
}

// IsERC7710Requirements reports whether the payment requirements select this method.
func IsERC7710Requirements(req *types.PaymentRequirements) bool {
	if req == nil || req.Extra == nil {
		return false
	}
	v, ok := req.Extra["assetTransferMethod"].(string)
	return ok && v == AssetTransferMethod
}

func has0xPrefix(s string) bool {
	return len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X')
}
