package facilitator

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	nanoscheme "github.com/gosuda/x402-facilitator/scheme/nano"
	"github.com/gosuda/x402-facilitator/types"
)

// Binding vectors: see scheme/nano/binding_test.go for how they were made.
const (
	bPayer = "nano_3ar3porks6g19gucmephzczmn6iygpu1ehraap87qij7idnef4h6cdhc4oit"
	bSig   = "CBB87DB622B6C2AD82A8A2C714AEAA07097D74019DBCE58930F04FF399B33ABA34FA3AF7B1FA0C3F2AFD9A56FD8E8815958D9D7C41A78297BA315FA6A0AD3D0C"
	bNonce = "6a1b2c3d.0123456789abcdef0123456789abcdef"
	bRes   = "https://api.example.test/v1/answer"
	bOther = "nano_3fao47pnt8sske1m8xun7g4d93jhr9ejwj5w7xwt66i4cq64oq1ttw1ophfe"
)

// ledgerPaidBy answers every block_info as a confirmed send from account.
func ledgerPaidBy(account string) nanoscheme.Callback {
	return func(endpoint string, _ map[string]interface{}) (map[string]interface{}, error) {
		b, _ := realBlock(endpoint, nil)
		b["block_account"] = account
		return b, nil
	}
}

func boundReq() *types.PaymentRequirements {
	r := req()
	r.Extra = map[string]interface{}{"binding": map[string]interface{}{
		"scheme": nanoscheme.BindingScheme, "nonce": bNonce, "resource": bRes,
	}}
	return r
}

func boundPayload(nonce, sig string) *types.PaymentPayload {
	r := boundReq()
	return &types.PaymentPayload{
		X402Version: 2,
		Payload: map[string]interface{}{
			"blockHash": goodBlock,
			"binding":   map[string]interface{}{"nonce": nonce, "signature": sig},
		},
		Accepted: *r,
	}
}

func facWith(t *testing.T, cb nanoscheme.Callback, o NanoFacilitatorOptions) *NanoFacilitator {
	t.Helper()
	o.Endpoints = []string{"https://a", "https://b"}
	o.Client = nanoscheme.NewClientWithCallback(cb)
	f, err := NewNanoFacilitatorWithOptions(nanoscheme.NetworkMainnet, "", "", o)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBoundPaymentIsValid(t *testing.T) {
	f := facWith(t, ledgerPaidBy(bPayer), NanoFacilitatorOptions{RequireBinding: true})
	v, _ := f.Verify(context.Background(), boundPayload(bNonce, bSig), boundReq())
	if !v.IsValid {
		t.Fatalf("expected valid: %+v", v)
	}
}

// The scenario the maintainer named: someone who only saw the block hash.
func TestBlockHashAloneIsRefusedWhenBindingRequired(t *testing.T) {
	f := facWith(t, ledgerPaidBy(bPayer), NanoFacilitatorOptions{RequireBinding: true})
	p := payload()
	p.Payload = map[string]interface{}{"blockHash": goodBlock}
	v, _ := f.Verify(context.Background(), p, boundReq())
	if v.IsValid || v.InvalidReason != nanoscheme.ErrBindingMissing.Error() {
		t.Fatalf("bearer proof accepted: %+v", v)
	}
}

func TestBlockHashAloneStillWorksWhenBindingNotRequired(t *testing.T) {
	f := facWith(t, realBlock, NanoFacilitatorOptions{})
	v, _ := f.Verify(context.Background(), payload(), req())
	if !v.IsValid {
		t.Fatalf("default behaviour changed: %+v", v)
	}
}

func TestBindingSignedByAnotherAccountIsRefused(t *testing.T) {
	// The ledger says bOther made the payment; the signature is bPayer's.
	f := facWith(t, ledgerPaidBy(bOther), NanoFacilitatorOptions{RequireBinding: true})
	v, _ := f.Verify(context.Background(), boundPayload(bNonce, bSig), boundReq())
	if v.IsValid || v.InvalidReason != nanoscheme.ErrBindingInvalid.Error() {
		t.Fatalf("thief's binding accepted: %+v", v)
	}
}

func TestPresentBindingIsCheckedEvenWhenNotRequired(t *testing.T) {
	f := facWith(t, ledgerPaidBy(bOther), NanoFacilitatorOptions{})
	v, _ := f.Verify(context.Background(), boundPayload(bNonce, bSig), boundReq())
	if v.IsValid {
		t.Fatalf("a wrong binding must not pass just because binding is optional: %+v", v)
	}
}

func TestBindingRefusalReasons(t *testing.T) {
	f := facWith(t, ledgerPaidBy(bPayer), NanoFacilitatorOptions{RequireBinding: true})
	cases := []struct {
		name string
		mut  func(*types.PaymentPayload, *types.PaymentRequirements)
		want error
	}{
		{"nonce not the seller's", func(p *types.PaymentPayload, r *types.PaymentRequirements) {
			p.Payload["binding"] = map[string]interface{}{"nonce": "ffffffff.00", "signature": bSig}
		}, nanoscheme.ErrBindingNonce},
		{"seller offered no binding", func(p *types.PaymentPayload, r *types.PaymentRequirements) { r.Extra = nil }, nanoscheme.ErrBindingNotOffered},
		{"resource changed by the seller", func(p *types.PaymentPayload, r *types.PaymentRequirements) {
			r.Extra["binding"].(map[string]interface{})["resource"] = bRes + "/other"
		}, nanoscheme.ErrBindingInvalid},
		{"binding is a string", func(p *types.PaymentPayload, r *types.PaymentRequirements) { p.Payload["binding"] = "x" }, nanoscheme.ErrBindingMalformed},
		{"empty signature", func(p *types.PaymentPayload, r *types.PaymentRequirements) {
			p.Payload["binding"] = map[string]interface{}{"nonce": bNonce, "signature": ""}
		}, nanoscheme.ErrBindingMalformed},
		{"signature not hex", func(p *types.PaymentPayload, r *types.PaymentRequirements) {
			p.Payload["binding"] = map[string]interface{}{"nonce": bNonce, "signature": "nothex"}
		}, nanoscheme.ErrBindingMalformed},
	}
	for _, c := range cases {
		p, r := boundPayload(bNonce, bSig), boundReq()
		c.mut(p, r)
		p.Accepted = *r
		v, _ := f.Verify(context.Background(), p, r)
		if v.IsValid || v.InvalidReason != c.want.Error() {
			t.Errorf("%s: got valid=%v reason=%q want %q", c.name, v.IsValid, v.InvalidReason, c.want)
		}
	}
}

func TestSupportedAdvertisesBinding(t *testing.T) {
	f := facWith(t, realBlock, NanoFacilitatorOptions{RequireBinding: true})
	x := f.Supported().Kinds[0].Extra
	if x["binding"] != nanoscheme.BindingScheme || x["bindingRequired"] != true {
		t.Fatalf("extra = %v", x)
	}
}

func TestSettleOfBoundPaymentClaimsOnce(t *testing.T) {
	f := facWith(t, ledgerPaidBy(bPayer), NanoFacilitatorOptions{RequireBinding: true})
	s1, _ := f.Settle(context.Background(), boundPayload(bNonce, bSig), boundReq())
	s2, _ := f.Settle(context.Background(), boundPayload(bNonce, bSig), boundReq())
	if !s1.Success || s2.Success {
		t.Fatalf("first=%v second=%v", s1.Success, s2.Success)
	}
}

// --- claim history survives a restart ---------------------------------

func TestFileClaimStoreSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.jsonl")
	s, err := NewFileClaimStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Claim(goodBlock) || s.Claim(goodBlock) {
		t.Fatal("first claim must win, second must lose")
	}
	_ = s.Close()

	s2, err := NewFileClaimStore(path) // the "restart"
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if !s2.Spent(goodBlock) || s2.Claim(goodBlock) {
		t.Fatal("a spent block became spendable after restart")
	}
}

func TestFacilitatorWithFileStoreRefusesReplayAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.jsonl")
	mk := func() (*NanoFacilitator, *FileClaimStore) {
		cs, err := NewFileClaimStore(path)
		if err != nil {
			t.Fatal(err)
		}
		return facWith(t, ledgerPaidBy(bPayer), NanoFacilitatorOptions{RequireBinding: true, Claims: cs}), cs
	}
	f1, cs1 := mk()
	if s, _ := f1.Settle(context.Background(), boundPayload(bNonce, bSig), boundReq()); !s.Success {
		t.Fatalf("first settle failed: %+v", s)
	}
	_ = cs1.Close()
	f2, cs2 := mk()
	defer cs2.Close()
	s, _ := f2.Settle(context.Background(), boundPayload(bNonce, bSig), boundReq())
	if s.Success || s.ErrorReason != types.ErrAuthorizationAlreadyUsed.Error() {
		t.Fatalf("replay after restart accepted: %+v", s)
	}
}

func TestFileClaimStoreOneWinnerUnderRace(t *testing.T) {
	s, err := NewFileClaimStore(filepath.Join(t.TempDir(), "c.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Claim(goodBlock) {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d winners", wins)
	}
}

func TestFileClaimStoreFailsClosedWhenItCannotWrite(t *testing.T) {
	s, err := NewFileClaimStore(filepath.Join(t.TempDir(), "c.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close() // closed file: every write now fails
	if s.Claim(goodBlock) {
		t.Fatal("claim reported success without being durable")
	}
}

func TestFileClaimStoreToleratesTornTailButNotMidFileDamage(t *testing.T) {
	dir := t.TempDir()
	torn := filepath.Join(dir, "torn.jsonl")
	if err := os.WriteFile(torn, []byte(`{"hash":"AA","at":"x"}`+"\n"+`{"hash":"BB","at"`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewFileClaimStore(torn)
	if err != nil {
		t.Fatalf("torn tail must be tolerated: %v", err)
	}
	if !s.Spent("AA") || s.Spent("BB") {
		t.Fatal("history misread")
	}
	if !s.Claim("CC") { // the next append must not glue onto the torn bytes
		t.Fatal("claim after torn tail failed")
	}
	_ = s.Close()
	s3, err := NewFileClaimStore(torn)
	if err != nil {
		t.Fatalf("log unreadable after append past torn tail: %v", err)
	}
	if !s3.Spent("CC") {
		t.Fatal("CC lost")
	}
	_ = s3.Close()

	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte(`{"hash":"AA"}`+"\nGARBAGE\n"+`{"hash":"CC"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileClaimStore(bad); err == nil {
		t.Fatal("mid-file damage must refuse to start, not silently shrink the history")
	}
}

func TestFileClaimStoreModeAndEmptyPath(t *testing.T) {
	if _, err := NewFileClaimStore(""); err == nil {
		t.Fatal("empty path accepted")
	}
	p := filepath.Join(t.TempDir(), "c.jsonl")
	s, _ := NewFileClaimStore(p)
	defer s.Close()
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestEnvWiring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.jsonl")
	t.Setenv(NanoClaimsFileEnv, path)
	t.Setenv(NanoRequireBindingEnv, "1")
	f, err := NewNanoFacilitator(nanoscheme.NetworkMainnet, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !f.requireBind {
		t.Fatal("binding not required")
	}
	if !f.claims.Claim(goodBlock) {
		t.Fatal("claim failed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("claim log was not created")
	}
	t.Setenv(NanoClaimsFileEnv, t.TempDir()) // a directory: cannot be a log
	if _, err := NewNanoFacilitator(nanoscheme.NetworkMainnet, "", ""); err == nil {
		t.Fatal("unusable claim log must stop startup, not fall back to memory")
	}
}
