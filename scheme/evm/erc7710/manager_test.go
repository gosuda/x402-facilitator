package erc7710

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Expected calldata generated INDEPENDENTLY with `cast calldata` (foundry), not with this
// package's own encoder - a test that round-trips its own encoding proves nothing.
//
//	cast calldata "redeemDelegations(bytes[],bytes32[],bytes[])" '[0xdeadbeef]' '[0x00..00]' '[<exec>]'
//
// where <exec> is token(20) || value(32,zero) || transfer(payTo, 30000).
const redeemVector = "0xcef6d209000000000000000000000000000000000000000000000000000000000000006000000000000000000000000000000000000000000000000000000000000000e00000000000000000000000000000000000000000000000000000000000000120000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000004deadbeef00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000788bd74916e3427b4ef8bed3d2f49241056e5e4f2b0000000000000000000000000000000000000000000000000000000000000000a9059cbb0000000000000000000000008acd1cb724aae8cdcd737ac97a5af4414012a61700000000000000000000000000000000000000000000000000000000000075300000000000000000"

func TestEncodeRedeemCalldata_MatchesCastVector(t *testing.T) {
	got, err := EncodeRedeemCalldata(
		hexutil.MustDecode("0xdeadbeef"),
		common.HexToAddress("0x8bd74916E3427B4eF8Bed3D2F49241056E5e4F2B"),
		common.HexToAddress("0x8ACD1cB724AAe8CDCD737aC97A5aF4414012a617"),
		big.NewInt(30_000),
	)
	if err != nil {
		t.Fatal(err)
	}
	if hexutil.Encode(got) != redeemVector {
		t.Fatalf("calldata drifts from cast vector\n got %s\nwant %s", hexutil.Encode(got), redeemVector)
	}
}

// The permission-context fixture is emitted by the Solidity reference
// (script/GenFixtures.s.sol) - the same fixture the TypeScript SDK is pinned against, making this
// a three-language byte-parity chain: Solidity -> TS -> Go.
func TestRootDelegator_FromSolidityFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/erc7710-encoding.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PermissionContext string `json:"permissionContext"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}

	root, err := RootDelegator(hexutil.MustDecode(fixture.PermissionContext))
	if err != nil {
		t.Fatal(err)
	}
	want := common.HexToAddress("0x4444444444444444444444444444444444444444")
	if root != want {
		t.Fatalf("root delegator: got %s want %s", root, want)
	}
}

func TestDecodeRevert(t *testing.T) {
	cases := []struct {
		name       string
		data       string
		wantReason string
	}{
		// cast abi-encode of each error, prefixed with its selector
		{"NotDojangVerified",
			"0x5ca03b860000000000000000000000002875b01abf0e5eb98253274d62db08fa7630b783aa92f8c143657dde575de430aecaea6ca91f2e6072339b16932d426895d8d678",
			"identity_not_verified"},
		{"CannotUseADisabledDelegation", "0x05baa052", "delegation_disabled"},
		{"unknown selector", "0x12345678", "delegation_rejected"},
		{"empty", "0x", "delegation_rejected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, _ := DecodeRevert(hexutil.MustDecode(c.data))
			if reason != c.wantReason {
				t.Fatalf("got %s want %s", reason, c.wantReason)
			}
		})
	}
}

func TestDecodeRevert_ErrorString(t *testing.T) {
	// abi.encodeWithSignature("Error(string)", "ERC20PeriodTransferEnforcer:transfer-amount-exceeded")
	data := "0x08c379a" +
		"00000000000000000000000000000000000000000000000000000000000000020" +
		"0000000000000000000000000000000000000000000000000000000000000035" +
		"4552433230506572696f645472616e73666572456e666f726365723a7472616e73" +
		"6665722d616d6f756e742d6578636565646564" +
		"0000000000000000000000"
	reason, msg := DecodeRevert(hexutil.MustDecode(data))
	if reason != "delegation_cap_exceeded" {
		t.Fatalf("reason: got %s, msg %q", reason, msg)
	}
}
