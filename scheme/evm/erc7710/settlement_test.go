package erc7710

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	tokenAddr = common.HexToAddress("0x8bd74916E3427B4eF8Bed3D2F49241056E5e4F2B")
	payerAddr = common.HexToAddress("0x4b6B26Cc68011FB4e9c9B6C0a4E15df040BFc23e")
	payeeAddr = common.HexToAddress("0x2476f2F54451df83B476b7aa51a711dE91094714")
	otherAddr = common.HexToAddress("0x00000000000000000000000000000000000000ff")
)

func transferLog(token, from, to common.Address, amount *big.Int) *types.Log {
	return &types.Log{
		Address: token,
		Topics: []common.Hash{
			transferTopic,
			common.BytesToHash(from.Bytes()),
			common.BytesToHash(to.Bytes()),
		},
		Data: common.LeftPadBytes(amount.Bytes(), 32),
	}
}

// The happy path, and the sender it reports.
func TestFindSettlementTransfer_Matches(t *testing.T) {
	amount := big.NewInt(20_000)
	receipt := &types.Receipt{Logs: []*types.Log{
		transferLog(tokenAddr, payerAddr, payeeAddr, amount),
	}}

	from, found := FindSettlementTransfer(receipt, tokenAddr, payeeAddr, amount)
	if !found {
		t.Fatal("a matching transfer was not found")
	}
	if from != payerAddr {
		t.Fatalf("sender: got %s, want %s", from, payerAddr)
	}
}

// Each of these is a settlement that a status-only check would have reported as success.
func TestFindSettlementTransfer_RejectsNearMisses(t *testing.T) {
	amount := big.NewInt(20_000)

	cases := map[string]*types.Receipt{
		"no logs at all": {},
		"paid to somebody else": {Logs: []*types.Log{
			transferLog(tokenAddr, payerAddr, otherAddr, amount),
		}},
		"wrong amount": {Logs: []*types.Log{
			transferLog(tokenAddr, payerAddr, payeeAddr, big.NewInt(19_999)),
		}},
		"overpaid - not a lesser failure": {Logs: []*types.Log{
			transferLog(tokenAddr, payerAddr, payeeAddr, big.NewInt(20_001)),
		}},
		"a different token entirely": {Logs: []*types.Log{
			transferLog(otherAddr, payerAddr, payeeAddr, amount),
		}},
		"an event that is not Transfer": {Logs: []*types.Log{{
			Address: tokenAddr,
			Topics: []common.Hash{
				common.HexToHash("0xdeadbeef"),
				common.BytesToHash(payerAddr.Bytes()),
				common.BytesToHash(payeeAddr.Bytes()),
			},
			Data: common.LeftPadBytes(amount.Bytes(), 32),
		}}},
	}

	for name, receipt := range cases {
		if _, found := FindSettlementTransfer(receipt, tokenAddr, payeeAddr, amount); found {
			t.Errorf("%s: reported as settled", name)
		}
	}
}

// A settlement can carry unrelated events - the payee's own hooks, another transfer in a batch.
// Finding the right one among them is the point.
func TestFindSettlementTransfer_IgnoresNoise(t *testing.T) {
	amount := big.NewInt(20_000)
	receipt := &types.Receipt{Logs: []*types.Log{
		nil,
		transferLog(tokenAddr, payerAddr, otherAddr, big.NewInt(1)),
		transferLog(otherAddr, payerAddr, payeeAddr, amount),
		transferLog(tokenAddr, payerAddr, payeeAddr, amount),
	}}

	from, found := FindSettlementTransfer(receipt, tokenAddr, payeeAddr, amount)
	if !found || from != payerAddr {
		t.Fatalf("the matching transfer was missed among unrelated logs (found=%v)", found)
	}
}

// A nil receipt is what a caller holds when confirmation timed out. It must not read as success.
func TestFindSettlementTransfer_NilReceipt(t *testing.T) {
	if _, found := FindSettlementTransfer(nil, tokenAddr, payeeAddr, big.NewInt(1)); found {
		t.Fatal("a nil receipt was reported as settled")
	}
}
