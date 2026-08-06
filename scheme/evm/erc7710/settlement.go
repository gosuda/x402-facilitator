package erc7710

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Confirming that a settlement actually settled.
//
// A receipt with status 1 says the delegation manager did not revert. It does not say the money
// arrived, and the difference is not academic: a manager could succeed while an inner call moved
// a different amount, or moved it somewhere else, and a facilitator reporting success on status
// alone would be repeating a claim it never checked. So settlement is confirmed against the
// token's own Transfer log - the same event a block explorer would show the payee.

// transferTopic is topic0 of ERC-20 Transfer(address,address,uint256).
var transferTopic = common.BytesToHash(crypto.Keccak256([]byte("Transfer(address,address,uint256)")))

// FindSettlementTransfer reports whether the receipt contains a Transfer of exactly `amount` of
// `token` to `payTo`. The sender is returned so the caller can confirm the funds left the account
// the permission context named, rather than some other account that happened to pay.
//
// An exact-amount match is deliberate. Accepting "at least the amount" would let a settlement
// whose execution was tampered with pass as long as it overpaid, and overpayment out of someone
// else's account is not a lesser failure than underpayment.
func FindSettlementTransfer(
	receipt *types.Receipt, token, payTo common.Address, amount *big.Int,
) (from common.Address, found bool) {
	if receipt == nil {
		return common.Address{}, false
	}
	for _, l := range receipt.Logs {
		if l == nil || l.Address != token || len(l.Topics) != 3 || l.Topics[0] != transferTopic {
			continue
		}
		if common.BytesToAddress(l.Topics[2].Bytes()) != payTo {
			continue
		}
		if new(big.Int).SetBytes(l.Data).Cmp(amount) != 0 {
			continue
		}
		return common.BytesToAddress(l.Topics[1].Bytes()), true
	}
	return common.Address{}, false
}
