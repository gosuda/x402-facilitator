package facilitator

import (
	"context"
	"testing"

	nanoscheme "github.com/gosuda/x402-facilitator/scheme/nano"
)

// TestVerifyLiveRealSend proves the whole verification path against TWO real,
// independent public Nano RPC endpoints using a real, confirmed Mainnet send
// block (height 42, sent to the nano_111 burn account). If either endpoint is
// down this test skips rather than flakes the suite — the in-memory tests
// above are the deterministic guarantee; this one is the live evidence.
func TestVerifyLiveRealSend(t *testing.T) {
	eps := nanoscheme.DefaultMainnetEndpoints
	for _, e := range eps {
		reachable := nanoscheme.NewClient(0)
		if _, err := reachable.BlockInfo(context.Background(), e, goodBlock); err != nil {
			t.Skipf("live endpoint %s unreachable (%v); skipping live proof", e, err)
		}
	}
	fac, err := NewNanoFacilitatorWithOptions(nanoscheme.NetworkMainnet, "", "",
		NanoFacilitatorOptions{Endpoints: eps})
	if err != nil {
		t.Fatal(err)
	}
	v, err := fac.Verify(context.Background(), payload(), req())
	if err != nil {
		t.Fatal(err)
	}
	if !v.IsValid {
		t.Fatalf("live real send must verify true, got %+v", v)
	}
	if v.Payer != payer {
		t.Fatalf("live payer = %q, want %q", v.Payer, payer)
	}
}
