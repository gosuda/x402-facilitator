package paywall

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	x402http "github.com/gosuda/x402-facilitator/resource/http"
	"github.com/gosuda/x402-facilitator/types"
)

const (
	nanoPayTo     = "nano_3t6k35gi95xu6tergt6p69ck76ogmitsa8mnijtpxm9fkcm736xtoncuohr3"
	nanoPeer      = "nano_1111111111111111111111111111111111111111111111111111hifc8npp"
	rawFor001XNO  = "10000000000000000000000000000"
	paidBlockHash = "ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113"
)

// unreachable endpoints for paywalls whose RPC nodes are never dialed.
var unreachable = []string{"http://127.0.0.1:1", "http://127.0.0.1:2"}

func newNanoNode(t *testing.T, ledger map[string]map[string]any) string {
	t.Helper()
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
			Hash   string `json:"hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Action != "block_info" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		block, ok := ledger[req.Hash]
		if !ok {
			block = map[string]any{"error": "Block not found"}
		}
		_ = json.NewEncoder(w).Encode(block)
	}))
	t.Cleanup(node.Close)
	return node.URL
}

func sendBlock(destination, amount string) map[string]any {
	return map[string]any{
		"block_account": nanoPeer,
		"amount":        amount,
		"confirmed":     "true",
		"subtype":       "send",
		"contents":      map[string]any{"type": "state", "link_as_account": destination},
	}
}

func mustNew(t *testing.T, cfg Config, routes []Route) *Paywall {
	t.Helper()
	pw, err := New(cfg, routes)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = pw.Close() })
	return pw
}

// echo answers with the request URI, so a test can tell the application ran.
var echo = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = io.WriteString(w, "app "+r.URL.RequestURI())
})

func serve(h http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "https://tenant.example"+target, body))
	return rec
}

func challengeOf(t *testing.T, rec *httptest.ResponseRecorder) (types.ResourceInfo, types.PaymentRequirements) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(rec.Header().Get(x402http.HeaderPaymentRequired))
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	var challenge struct {
		Resource types.ResourceInfo          `json:"resource"`
		Accepts  []types.PaymentRequirements `json:"accepts"`
	}
	if err := json.Unmarshal(raw, &challenge); err != nil || len(challenge.Accepts) != 1 {
		t.Fatalf("challenge = %s, %v; want one accepted requirement", raw, err)
	}
	return challenge.Resource, challenge.Accepts[0]
}

func pay(h http.Handler, accepted types.PaymentRequirements, target string, proof map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(types.PaymentPayload{X402Version: int(types.X402VersionV2), Accepted: accepted, Payload: proof})
	req := httptest.NewRequest(http.MethodGet, "https://tenant.example"+target, nil)
	req.Header.Set(x402http.HeaderPaymentSignature, base64.StdEncoding.EncodeToString(raw))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The wrapper must charge exactly the requests the application router would
// send to a paid route: longest prefix, segment boundaries, cleaned paths,
// and per-method policies, with a longer unpaid prefix shadowing a paid one.
func TestWrapChargesExactlyTheSelectedRoute(t *testing.T) {
	t.Parallel()
	pw := mustNew(t, Config{Network: "nano:mainnet", PayTo: nanoPayTo, Endpoints: unreachable}, []Route{
		{Prefix: "/pay", Amount: "0.01"},
		{Prefix: "/pay/free/"},
		{Prefix: "/pay/free/locked", Amount: "0.01", Methods: []string{"post"}},
		{Prefix: "/"},
	})
	h := pw.Wrap(echo)
	for _, tc := range []struct {
		method, path string
		paid         bool
	}{
		{http.MethodGet, "/pay", true},
		{http.MethodGet, "/pay/item", true},
		{http.MethodGet, "/pay/free/item?q=1", false},
		{http.MethodGet, "/pay/free/locked/item", false},
		{http.MethodPost, "/pay/free/locked/item", true},
		{http.MethodGet, "/pay/free/../item", true},
		{http.MethodGet, "/pay//item", true},
		{http.MethodGet, "/payment", false},
		{http.MethodGet, "/", false},
	} {
		rec := serve(h, tc.method, tc.path, nil)
		if gotPaid := rec.Code == http.StatusPaymentRequired; gotPaid != tc.paid {
			t.Errorf("%s %s: status %d body %q, paid = %v want %v", tc.method, tc.path, rec.Code, rec.Body.String(), gotPaid, tc.paid)
		}
	}
}

func TestUnpaidPaywallIsTransparent(t *testing.T) {
	t.Parallel()
	pw := mustNew(t, Config{}, []Route{{Prefix: "/"}})
	h := pw.Wrap(echo)
	for _, target := range []string{"/", ClientPath, PreparePath} {
		if rec := serve(h, http.MethodGet, target, nil); rec.Code != http.StatusOK || rec.Body.String() != "app "+target {
			t.Errorf("%s: status %d body %q; want the application", target, rec.Code, rec.Body.String())
		}
	}
}

func TestHelperEndpointsAreOwnedWhenARouteIsPaid(t *testing.T) {
	t.Parallel()
	pw := mustNew(t, Config{Network: "nano:mainnet", PayTo: nanoPayTo, Endpoints: unreachable}, []Route{
		{Prefix: "/", Amount: "0.01"},
	})
	h := pw.Wrap(echo)
	if rec := serve(h, http.MethodGet, ClientPath, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("client.js: status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := serve(h, http.MethodGet, PreparePath, nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET prepare: status %d, want 405", rec.Code)
	}
}

func TestPrepareFollowsRouteSelection(t *testing.T) {
	t.Parallel()
	pw := mustNew(t, Config{Network: "nano:mainnet", PayTo: nanoPayTo, Endpoints: unreachable}, []Route{
		{Prefix: "/pay", Amount: "0.01"},
		{Prefix: "/pay/free"},
		{Prefix: "/pay/free/locked", Amount: "0.01", Methods: []string{"POST"}},
	})
	h := pw.Wrap(echo)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"path":"/pay/item"}`, http.StatusPaymentRequired},
		{`{"path":"/pay/free/item"}`, http.StatusNotFound},
		{`{"path":"/pay/free/locked/item","method":"GET"}`, http.StatusNotFound},
		{`{"path":"/pay/free/locked/item","method":"post"}`, http.StatusPaymentRequired},
		{`{"path":"/payment"}`, http.StatusNotFound},
		{`{"method":"GET"}`, http.StatusBadRequest},
		{`not json`, http.StatusBadRequest},
		{`{"path":"/pay"} {"path":"/pay"}`, http.StatusBadRequest},
	} {
		rec := serve(h, http.MethodPost, PreparePath, strings.NewReader(tc.body))
		if rec.Code != tc.status {
			t.Errorf("prepare %s: status %d body %q, want %d", tc.body, rec.Code, rec.Body.String(), tc.status)
		}
	}
	rec := serve(h, http.MethodPost, PreparePath, strings.NewReader(`{"path":"/pay/item"}`))
	resource, accepted := challengeOf(t, rec)
	if resource.URL != "https://tenant.example/pay" || accepted.Amount != rawFor001XNO {
		t.Fatalf("prepare challenge resource %q amount %q", resource.URL, accepted.Amount)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("prepare challenge is cacheable")
	}
}

func TestNanoPaymentSettlesOncePerBlockAcrossRoutes(t *testing.T) {
	t.Parallel()
	ledger := map[string]map[string]any{
		paidBlockHash:           sendBlock(nanoPayTo, rawFor001XNO),
		strings.Repeat("1", 64): sendBlock(nanoPayTo, "9999999999999999999999999999"),
		strings.Repeat("2", 64): sendBlock(nanoPeer, rawFor001XNO),
	}
	var hits atomic.Int32
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "paid content")
	})
	pw := mustNew(t, Config{
		Network:   "nano:mainnet",
		PayTo:     strings.ToUpper(nanoPayTo[:1]) + nanoPayTo[1:],
		Endpoints: []string{newNanoNode(t, ledger), newNanoNode(t, ledger)},
	}, []Route{{Prefix: "/paid", Amount: "0.01"}, {Prefix: "/also-paid", Amount: "0.01"}})
	h := pw.Wrap(app)

	resource, accepted := challengeOf(t, serve(h, http.MethodGet, "/paid/item", nil))
	if resource.URL != "/paid" || accepted.Network != "nano:mainnet" || accepted.Asset != "XNO" ||
		accepted.Amount != rawFor001XNO || accepted.PayTo != nanoPayTo || accepted.Extra["paymentFlow"] != "upfront" {
		t.Fatalf("challenge resource %+v accepted %+v", resource, accepted)
	}
	if _, offered := accepted.Extra["binding"]; offered {
		t.Fatal("binding offered without nano_require_binding")
	}
	for _, hash := range []string{strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("4", 64)} {
		if rec := pay(h, accepted, "/paid/item", map[string]any{"blockHash": hash}); rec.Code != http.StatusPaymentRequired {
			t.Fatalf("invalid block %s: status %d", hash[:4], rec.Code)
		}
	}
	if rec := pay(h, accepted, "/paid/item", map[string]any{"blockHash": paidBlockHash}); rec.Code != http.StatusOK ||
		rec.Header().Get(x402http.HeaderPaymentResponse) == "" {
		t.Fatalf("valid payment: status %d receipt %q", rec.Code, rec.Header().Get(x402http.HeaderPaymentResponse))
	}
	for _, target := range []string{"/paid/item", "/also-paid/item"} {
		if rec := pay(h, accepted, target, map[string]any{"blockHash": paidBlockHash}); rec.Code != http.StatusPaymentRequired {
			t.Fatalf("replay on %s: status %d", target, rec.Code)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("application ran %d times, want once", got)
	}
}

// The claim log is owned by the Paywall: a block settled by one Paywall is
// refused by the next one on the same file, and Close releases the file.
func TestNanoClaimsFileSurvivesRestartAndIsClosed(t *testing.T) {
	t.Parallel()
	ledger := map[string]map[string]any{paidBlockHash: sendBlock(nanoPayTo, rawFor001XNO)}
	endpoints := []string{newNanoNode(t, ledger), newNanoNode(t, ledger)}
	path := filepath.Join(t.TempDir(), "claims.jsonl")
	cfg := Config{Network: "nano:mainnet", PayTo: nanoPayTo, Endpoints: endpoints, Options: map[string]string{OptionNanoClaimsFile: path}}
	routes := []Route{{Prefix: "/", Amount: "0.01"}}

	first, err := New(cfg, routes)
	if err != nil {
		t.Fatal(err)
	}
	_, accepted := challengeOf(t, serve(first.Wrap(echo), http.MethodGet, "/", nil))
	if rec := pay(first.Wrap(echo), accepted, "/", map[string]any{"blockHash": paidBlockHash}); rec.Code != http.StatusOK {
		t.Fatalf("first payment: status %d", rec.Code)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if rec := pay(first.Wrap(echo), accepted, "/", map[string]any{"blockHash": strings.ToLower(paidBlockHash)}); rec.Code == http.StatusOK {
		t.Fatal("a closed paywall accepted a payment it could not record")
	}

	second := mustNew(t, cfg, routes)
	if rec := pay(second.Wrap(echo), accepted, "/", map[string]any{"blockHash": paidBlockHash}); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("replay after restart: status %d", rec.Code)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("claim log not written: %v", err)
	}
}

func TestNanoClaimsFileReleasedWhenALaterRouteFails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "claims.jsonl")
	_, err := New(Config{Network: "nano:mainnet", PayTo: nanoPayTo, Endpoints: unreachable, Options: map[string]string{OptionNanoClaimsFile: path}},
		[]Route{{Prefix: "/a", Amount: "0.01"}, {Prefix: "/b", Amount: "1e-3"}})
	if err == nil {
		t.Fatal("New() accepted an invalid amount")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("claim log still held or missing: %v", err)
	}
}

func TestNanoRequireBindingOffersPerRouteNonce(t *testing.T) {
	t.Parallel()
	pw := mustNew(t, Config{Network: "nano:mainnet", PayTo: nanoPayTo, Endpoints: unreachable, Options: map[string]string{OptionNanoRequireBinding: "true"}},
		[]Route{{Prefix: "/a", Amount: "0.01"}, {Prefix: "/b", Amount: "0.01"}})
	h := pw.Wrap(echo)
	nonces := map[string]bool{}
	for _, target := range []string{"/a", "/b"} {
		_, accepted := challengeOf(t, serve(h, http.MethodGet, target, nil))
		binding, _ := accepted.Extra["binding"].(map[string]any)
		nonce, _ := binding["nonce"].(string)
		if binding["scheme"] != "nano-block-sig/1" || binding["resource"] != target || len(nonce) != 32 {
			t.Fatalf("%s binding = %+v", target, binding)
		}
		nonces[nonce] = true
	}
	if len(nonces) != 2 {
		t.Fatal("routes share a binding nonce")
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	paid := []Route{{Prefix: "/", Amount: "0.01"}}
	nano := func(mut func(*Config)) Config {
		cfg := Config{Network: "nano:mainnet", PayTo: nanoPayTo, Endpoints: unreachable}
		mut(&cfg)
		return cfg
	}
	for name, tc := range map[string]struct {
		cfg    Config
		routes []Route
	}{
		"unsupported network":    {Config{Network: "eip155:8453", PayTo: "0x1"}, paid},
		"unsupported sui":        {Config{Network: "sui:devnet", PayTo: "0x1"}, paid},
		"bad sui pay-to":         {Config{PayTo: "nano_x"}, paid},
		"casper without asset":   {Config{Network: "casper:casper-test", PayTo: "account-hash-" + strings.Repeat("a", 64)}, paid},
		"casper bad pay-to":      {Config{Network: "casper:casper-test", Asset: "hash-" + strings.Repeat("b", 64), PayTo: "0x1"}, paid},
		"casper hosted no key":   {Config{Network: "casper:casper-test", Asset: strings.Repeat("b", 64), PayTo: "account-hash-" + strings.Repeat("a", 64)}, paid},
		"casper token over http": {Config{Network: "casper:casper-test", Asset: strings.Repeat("b", 64), PayTo: "account-hash-" + strings.Repeat("a", 64), Endpoints: []string{"http://facilitator.example"}, FacilitatorToken: "k"}, paid},
		"nano bad checksum":      {nano(func(c *Config) { c.PayTo = nanoPayTo[:len(nanoPayTo)-1] + "1" }), paid},
		"nano one endpoint":      {nano(func(c *Config) { c.Endpoints = []string{"http://a", " http://a "} }), paid},
		"nano sub-raw amount":    {nano(func(*Config) {}), []Route{{Prefix: "/", Amount: "0." + strings.Repeat("0", 30) + "1"}}},
		"nano zero amount":       {nano(func(*Config) {}), []Route{{Prefix: "/", Amount: "0"}}},
		"nano bad binding flag":  {nano(func(c *Config) { c.Options = map[string]string{OptionNanoRequireBinding: "maybe"} }), paid},
		"nano damaged claims":    {nano(func(c *Config) { c.Options = map[string]string{OptionNanoClaimsFile: t.TempDir()} }), paid},
		"unknown option":         {nano(func(c *Config) { c.Options = map[string]string{"nano_claim_file": "x"} }), paid},
		"option on sui":          {Config{PayTo: "0x1", Options: map[string]string{OptionNanoRequireBinding: "true"}}, paid},
		"option without paid":    {Config{Options: map[string]string{OptionNanoRequireBinding: "true"}}, []Route{{Prefix: "/"}}},
		"methods without price":  {Config{}, []Route{{Prefix: "/", Methods: []string{"POST"}}}},
		"empty method":           {nano(func(*Config) {}), []Route{{Prefix: "/", Amount: "0.01", Methods: []string{" "}}}},
		"relative prefix":        {Config{}, []Route{{Prefix: "api"}}},
		"duplicate prefix":       {Config{}, []Route{{Prefix: "/a/"}, {Prefix: "/a"}}},
	} {
		if pw, err := New(tc.cfg, tc.routes); err == nil {
			_ = pw.Close()
			t.Errorf("%s: New() error = nil", name)
		}
	}
}

func TestNanoAcceptsLegacyXRBPayTo(t *testing.T) {
	t.Parallel()
	pw := mustNew(t, Config{Network: "nano:mainnet", PayTo: "xrb_" + nanoPayTo[len("nano_"):], Endpoints: unreachable}, []Route{{Prefix: "/", Amount: "0.01"}})
	if _, accepted := challengeOf(t, serve(pw.Wrap(echo), http.MethodGet, "/", nil)); accepted.PayTo != nanoPayTo {
		t.Fatalf("pay-to %q, want canonical %q", accepted.PayTo, nanoPayTo)
	}
}

func TestCasperRouteChallenge(t *testing.T) {
	t.Parallel()
	asset := strings.Repeat("b", 64)
	payTo := "account-hash-" + strings.Repeat("a", 64)
	pw := mustNew(t, Config{Network: "casper:casper-test", Asset: strings.ToUpper(asset), PayTo: payTo, Endpoints: []string{"http://127.0.0.1:1"}, FacilitatorToken: "k"},
		[]Route{{Prefix: "/", Amount: "1.5"}})
	_, accepted := challengeOf(t, serve(pw.Wrap(echo), http.MethodGet, "/", nil))
	if accepted.Network != "casper:casper-test" || accepted.Asset != asset || accepted.Amount != "1500000000" || accepted.PayTo != payTo {
		t.Fatalf("accepted = %+v", accepted)
	}
}

func TestSuiDefaultsToMainnetAndTestnet(t *testing.T) {
	t.Parallel()
	payTo := "0x" + strings.Repeat("a", 64)
	for testnet, want := range map[bool]string{false: suiMainnet, true: suiTestnet} {
		pw := mustNew(t, Config{Testnet: testnet, PayTo: payTo}, []Route{{Prefix: "/", Amount: "0.01"}})
		_, accepted := challengeOf(t, serve(pw.Wrap(echo), http.MethodGet, "/", nil))
		if accepted.Network != want || accepted.Amount != "10000" || accepted.PayTo != payTo {
			t.Errorf("testnet=%v accepted = %+v", testnet, accepted)
		}
	}
}

func TestCleanPath(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"": "/", "/": "/", "a": "/a", "/a/": "/a", "/a/../b": "/b", "//evil": "/evil", "/\\evil": "/", " /a/b/ ": "/a/b",
	} {
		if got := CleanPath(raw); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", raw, got, want)
		}
	}
}
