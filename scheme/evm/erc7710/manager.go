package erc7710

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Calldata construction for ERC-7710 redemption. Layouts are pinned by unit tests against vectors
// produced independently by a Solidity reference implementation, not merely round-tripped here.

// redeemDelegations(bytes[],bytes32[],bytes[]) - selector 0xcef6d209, mandated by ERC-7710.
var redeemSelector = crypto.Keccak256([]byte("redeemDelegations(bytes[],bytes32[],bytes[])"))[:4]

// erc20 transfer(address,uint256) - selector 0xa9059cbb.
var transferSelector = crypto.Keccak256([]byte("transfer(address,uint256)"))[:4]

var (
	bytesTy   = mustType("bytes")
	bytes32Ty = mustType("bytes32")
	addressTy = mustType("address")
	uint256Ty = mustType("uint256")

	bytesArrTy   = mustType("bytes[]")
	bytes32ArrTy = mustType("bytes32[]")

	redeemArgs = abi.Arguments{{Type: bytesArrTy}, {Type: bytes32ArrTy}, {Type: bytesArrTy}}

	// Delegation[] - the MetaMask delegation-framework layout, which compatible managers adopt
	// byte-for-byte. Decoded only to surface the root delegator as Payer, never re-encoded.
	delegationsTy = mustTupleArray()
	delegationArg = abi.Arguments{{Type: delegationsTy}}
)

func mustType(t string) abi.Type {
	ty, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return ty
}

func mustTupleArray() abi.Type {
	ty, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "delegate", Type: "address"},
		{Name: "delegator", Type: "address"},
		{Name: "authority", Type: "bytes32"},
		{Name: "caveats", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
			{Name: "enforcer", Type: "address"},
			{Name: "terms", Type: "bytes"},
			{Name: "args", Type: "bytes"},
		}},
		{Name: "salt", Type: "uint256"},
		{Name: "signature", Type: "bytes"},
	})
	if err != nil {
		panic(err)
	}
	return ty
}

// EncodeRedeemCalldata builds the full redeemDelegations calldata for a single-payment redemption:
// one context, the simple-single mode word (zero), one packed transfer execution.
func EncodeRedeemCalldata(permissionContext []byte, token, payTo common.Address, amount *big.Int) ([]byte, error) {
	transferData, err := abi.Arguments{{Type: addressTy}, {Type: uint256Ty}}.Pack(payTo, amount)
	if err != nil {
		return nil, fmt.Errorf("pack transfer args: %w", err)
	}
	callData := append(append([]byte{}, transferSelector...), transferData...)

	// ERC-7579 single execution, tightly packed: target(20) | value(32) | callData.
	execution := make([]byte, 0, 52+len(callData))
	execution = append(execution, token.Bytes()...)
	execution = append(execution, make([]byte, 32)...) // value = 0
	execution = append(execution, callData...)

	packed, err := redeemArgs.Pack(
		[][]byte{permissionContext},
		[][32]byte{{}}, // mode: simple single, default semantics = zero word
		[][]byte{execution},
	)
	if err != nil {
		return nil, fmt.Errorf("pack redeem args: %w", err)
	}
	return append(append([]byte{}, redeemSelector...), packed...), nil
}

// RootDelegator decodes abi.encode(Delegation[]) far enough to report who ultimately pays:
// the LAST delegation's delegator (root). Used only for the Payer field in responses.
func RootDelegator(permissionContext []byte) (common.Address, error) {
	vals, err := delegationArg.Unpack(permissionContext)
	if err != nil {
		return common.Address{}, fmt.Errorf("decode Delegation[]: %w", err)
	}
	// go-ethereum materialises the tuple array as a slice of anonymous structs.
	chain, ok := vals[0].([]struct {
		Delegate  common.Address `json:"delegate"`
		Delegator common.Address `json:"delegator"`
		Authority [32]byte       `json:"authority"`
		Caveats   []struct {
			Enforcer common.Address `json:"enforcer"`
			Terms    []byte         `json:"terms"`
			Args     []byte         `json:"args"`
		} `json:"caveats"`
		Salt      *big.Int `json:"salt"`
		Signature []byte   `json:"signature"`
	})
	if !ok || len(chain) == 0 {
		return common.Address{}, fmt.Errorf("permission context holds no delegations")
	}
	return chain[len(chain)-1].Delegator, nil
}

// Revert decoding: map a revert selector to a stable machine-readable reason, keeping the decoded
// detail as the human-readable message.
//
// A facilitator cannot enumerate every caveat enforcer that will ever exist, and it is not
// supposed to - the refusal is the manager's to make, not its to predict. So this table is a
// courtesy: it names the refusals seen in practice on deployed ERC-7710 managers and their
// enforcers, and everything it does not recognise still returns a well-formed refusal with the
// raw selector attached. Adding an enforcer never requires touching this file.

type revertMapping struct {
	name   string
	reason string
}

var revertSelectors = map[[4]byte]revertMapping{
	// Manager: signature, chain and kill-switch validation, which precedes every caveat.
	sel("CannotUseADisabledDelegation()"): {"CannotUseADisabledDelegation", "delegation_disabled"},
	sel("InvalidDelegate()"):              {"InvalidDelegate", "invalid_delegate"},
	sel("InvalidAuthority()"):             {"InvalidAuthority", "invalid_authority"},
	sel("InvalidEOASignature()"):          {"InvalidEOASignature", "invalid_signature"},
	sel("InvalidERC1271Signature()"):      {"InvalidERC1271Signature", "invalid_signature"},
	sel("BatchDataLengthMismatch()"):      {"BatchDataLengthMismatch", "invalid_payload_format"},
	sel("EmptyDelegationChain()"):         {"EmptyDelegationChain", "invalid_payload_format"},

	// Enforcers observed on GIWA. Amount and window refusals arrive as Error(string) from the
	// vendored MetaMask enforcers and are handled below; these are the custom-error ones.
	sel("PerPaymentCapExceeded(uint256,uint256)"):     {"PerPaymentCapExceeded", "delegation_cap_exceeded"},
	sel("PayeeNotAllowed(address)"):                   {"PayeeNotAllowed", "payee_not_allowed"},
	sel("NotDojangVerified(address,bytes32)"):         {"NotDojangVerified", "identity_not_verified"},
	sel("CodeNotVerified(bytes32,string,bytes32)"):    {"CodeNotVerified", "human_approval_missing"},
	sel("UnknownAccount(address)"):                    {"UnknownAccount", "unknown_account"},
	sel("PrincipalMismatch(address,address,address)"): {"PrincipalMismatch", "principal_mismatch"},
	sel("InvalidMethod(bytes4)"):                      {"InvalidMethod", "unsupported_execution"},
	sel("InvalidExecutionLength(uint256)"):            {"InvalidExecutionLength", "unsupported_execution"},
	sel("DirtyRecipientWord(bytes32)"):                {"DirtyRecipientWord", "invalid_payload_format"},
	sel("InvalidTermsLength(uint256)"):                {"InvalidTermsLength", "invalid_caveat_terms"},
	sel("InvalidZeroCap()"):                           {"InvalidZeroCap", "invalid_caveat_terms"},
}

func sel(sig string) [4]byte {
	var s [4]byte
	copy(s[:], crypto.Keccak256([]byte(sig))[:4])
	return s
}

// DecodeRevert turns raw revert data into (machine reason, human message).
func DecodeRevert(data []byte) (reason string, message string) {
	if len(data) < 4 {
		return "delegation_rejected", "execution reverted without data"
	}
	var s [4]byte
	copy(s[:], data[:4])

	// Error(string) - solidity require strings, e.g. the period enforcer's caps.
	if s == sel("Error(string)") {
		stringArgs := abi.Arguments{{Type: mustType("string")}}
		if vals, err := stringArgs.Unpack(data[4:]); err == nil {
			msg := vals[0].(string)
			if strings.Contains(msg, "transfer-amount-exceeded") {
				return "delegation_cap_exceeded", msg
			}
			if strings.Contains(msg, "expired-delegation") || strings.Contains(msg, "early-delegation") {
				return "delegation_window_closed", msg
			}
			return "delegation_rejected", msg
		}
	}

	if m, ok := revertSelectors[s]; ok {
		return m.reason, m.name + hexSuffix(data)
	}
	return "delegation_rejected", "unrecognised revert 0x" + common.Bytes2Hex(data[:4])
}

func hexSuffix(data []byte) string {
	if len(data) <= 4 {
		return "()"
	}
	return "(0x" + common.Bytes2Hex(data[4:]) + ")"
}
