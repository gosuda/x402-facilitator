// Package paywall composes x402 payment gates for a set of path-prefix
// routes from plain configuration: a CAIP-2 network, a pay-to address, and a
// human amount per paid route.
//
// It is the optional composition layer for applications that take the
// payment network as configuration instead of code, such as a tunnel or a
// reverse proxy. The application keeps its own router and wraps it once:
//
//	pw, err := paywall.New(cfg, routes)
//	if err != nil { ... }
//	defer pw.Close()
//	handler := pw.Wrap(router)
//
// Everything chain-specific stays here and in scheme/<chain>: address and
// amount conversion, facilitator construction, per-network options, claim
// storage and its lifecycle, and the payer-facing helper endpoints. The
// application never names a chain. The per-request protocol (402 challenge,
// settlement, receipts, header stripping) is resource/http's chain-blind
// Gate; this package only decides which Gate guards which request.
//
// Applications that compose one chain in code should use resource/http and
// the scheme/<chain> packages directly instead.
package paywall

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"

	x402http "github.com/gosuda/x402-facilitator/resource/http"
	"github.com/gosuda/x402-facilitator/types"
)

const (
	// PreparePath is the helper endpoint a payer posts {sender, method,
	// path} to. A network with an unsigned-transaction prepare flow (Sui)
	// answers with the transactions to sign; every other network answers
	// with the route's 402 challenge.
	PreparePath = "/x402/prepare"

	// ClientPath serves the browser wallet client for the prepare flow.
	ClientPath = "/x402/client.js"

	prepareBodyLimit = 64 << 10

	maxTimeoutSeconds = 60
	resourceMimeType  = "text/html"
)

// Config is the payment contract shared by every paid route.
type Config struct {
	// Network is the CAIP-2 network, such as "sui:mainnet",
	// "casper:casper-test", or "nano:mainnet". Empty selects Sui mainnet,
	// or Sui testnet when Testnet is set.
	Network string
	// Testnet selects the default testnet when Network is empty.
	Testnet bool
	// Asset is the settlement asset where the network needs one named
	// (the wCSPR CEP-18 contract hash on Casper). Networks with a fixed
	// asset ignore an empty value.
	Asset string
	// PayTo is the recipient address on Network. Required when a route is
	// paid.
	PayTo string
	// Endpoints are chain RPC nodes or a hosted facilitator URL; their
	// meaning is the network's (see the README).
	Endpoints []string
	// FacilitatorToken authorizes a hosted facilitator (Casper).
	FacilitatorToken string
	// Options carries network-specific settings by name. Every key must be
	// one the selected network understands; an unknown key is an error, so
	// a misspelled option is never silently dropped.
	Options map[string]string
}

// Route is one path prefix of the application's router.
type Route struct {
	// Prefix is the path prefix, such as "/api" or "/". Requests are
	// matched to the longest prefix, on path segment boundaries, after the
	// path is cleaned.
	Prefix string
	// Amount is the human price in the network's settlement asset, such as
	// "0.01". Empty leaves the route unpaid.
	Amount string
	// Methods limits payment to these HTTP methods; empty means every
	// method. Methods require an Amount.
	Methods []string
}

// Paywall gates the paid routes of one application handler.
type Paywall struct {
	routes   []route
	network  network
	clientJS http.Handler
}

type route struct {
	prefix  string
	methods map[string]struct{}
	policy  *policy
}

// policy is the payment of one paid route.
type policy struct {
	prefix       string
	requirements types.PaymentRequirements
	gate         *x402http.Gate
	// prepare serves PreparePath for this route; nil answers with the
	// route's 402 challenge.
	prepare func(w http.ResponseWriter, r *http.Request, sender string)
}

// network builds the policies of one CAIP-2 network and owns the runtime
// they share (facilitator clients, claim stores).
type network interface {
	policy(prefix, amount string) (*policy, error)
	clientJS() http.Handler
	close() error
}

// New validates cfg and routes and builds the paywall. Every route the
// application serves should be listed, unpaid ones too: a longer unpaid
// prefix must shadow a shorter paid one exactly as the router does.
func New(cfg Config, routes []Route) (*Paywall, error) {
	p := &Paywall{}
	seen := make(map[string]struct{}, len(routes))
	for _, raw := range routes {
		r, amount, err := parseRoute(raw)
		if err != nil {
			_ = p.Close()
			return nil, err
		}
		if _, dup := seen[r.prefix]; dup {
			_ = p.Close()
			return nil, fmt.Errorf("paywall: duplicate route prefix %q", r.prefix)
		}
		seen[r.prefix] = struct{}{}
		if amount != "" {
			if p.network == nil {
				if p.network, err = newNetwork(cfg); err != nil {
					return nil, err
				}
			}
			if r.policy, err = p.network.policy(r.prefix, amount); err != nil {
				_ = p.Close()
				return nil, fmt.Errorf("paywall: route %q: %w", r.prefix, err)
			}
		}
		p.routes = append(p.routes, r)
	}
	if p.network == nil {
		if err := checkUnpaidConfig(cfg); err != nil {
			return nil, err
		}
	} else {
		p.clientJS = p.network.clientJS()
	}
	slices.SortFunc(p.routes, func(a, b route) int {
		if len(a.prefix) != len(b.prefix) {
			return len(b.prefix) - len(a.prefix)
		}
		return strings.Compare(a.prefix, b.prefix)
	})
	return p, nil
}

// checkUnpaidConfig refuses network-specific options when no route is paid:
// they would otherwise be accepted and never applied.
func checkUnpaidConfig(cfg Config) error {
	for key := range cfg.Options {
		return fmt.Errorf("paywall: option %q requires a paid route", key)
	}
	return nil
}

func parseRoute(raw Route) (route, string, error) {
	prefix := strings.TrimSpace(raw.Prefix)
	if !strings.HasPrefix(prefix, "/") {
		return route{}, "", fmt.Errorf("paywall: route prefix %q must start with /", raw.Prefix)
	}
	prefix = CleanPath(prefix)
	amount := strings.TrimSpace(raw.Amount)
	if amount == "" && len(raw.Methods) > 0 {
		return route{}, "", fmt.Errorf("paywall: route %q payment methods require an amount", prefix)
	}
	methods := make(map[string]struct{}, len(raw.Methods))
	for _, method := range raw.Methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			return route{}, "", fmt.Errorf("paywall: route %q has an empty payment method", prefix)
		}
		methods[method] = struct{}{}
	}
	return route{prefix: prefix, methods: methods}, amount, nil
}

func newNetwork(cfg Config) (network, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Network))
	if name == "" {
		name = suiMainnet
		if cfg.Testnet {
			name = suiTestnet
		}
	}
	namespace, _, _ := strings.Cut(name, ":")
	var (
		n   network
		err error
	)
	switch namespace {
	case "sui":
		n, err = newSuiNetwork(name, cfg)
	case "casper":
		n, err = newCasperNetwork(name, cfg)
	case "nano":
		n, err = newNanoNetwork(name, cfg)
	default:
		return nil, fmt.Errorf("paywall: unsupported x402 network %q", name)
	}
	if err != nil {
		return nil, fmt.Errorf("paywall: %s: %w", name, err)
	}
	return n, nil
}

// Wrap returns next guarded by the paywall. Requests to a paid route and
// method must pay before next runs; every other request reaches next
// unchanged. When any route is paid, PreparePath and ClientPath are served
// here and never reach next.
func (p *Paywall) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := "/"
		if r.URL != nil {
			reqPath = r.URL.Path
		}
		reqPath = CleanPath(reqPath)
		if p.network != nil {
			switch reqPath {
			case ClientPath:
				p.clientJS.ServeHTTP(w, r)
				return
			case PreparePath:
				p.servePrepare(w, r)
				return
			}
		}
		if pol := p.paidPolicy(reqPath, r.Method); pol != nil {
			pol.gate.Wrap(next).ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// paidPolicy returns the policy that must be paid for method on the cleaned
// path, or nil when the request is free.
func (p *Paywall) paidPolicy(cleanPath, method string) *policy {
	r, ok := p.match(cleanPath)
	if !ok || r.policy == nil {
		return nil
	}
	if len(r.methods) > 0 {
		if _, paid := r.methods[strings.ToUpper(strings.TrimSpace(method))]; !paid {
			return nil
		}
	}
	return r.policy
}

func (p *Paywall) match(cleanPath string) (route, bool) {
	for _, r := range p.routes {
		if r.prefix == "/" || cleanPath == r.prefix || strings.HasPrefix(cleanPath, r.prefix+"/") {
			return r, true
		}
	}
	return route{}, false
}

type prepareRequest struct {
	Sender string `json:"sender"`
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
}

func (p *Paywall) servePrepare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req prepareRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, prepareBodyLimit))
	if err := decoder.Decode(&req); err != nil || decoder.More() {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid payment prepare request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	method := http.MethodGet
	if raw := strings.ToUpper(strings.TrimSpace(req.Method)); raw != "" {
		method = raw
	}
	pol := p.paidPolicy(CleanPath(req.Path), method)
	if pol == nil {
		http.Error(w, "x402 payment is not enabled for path", http.StatusNotFound)
		return
	}
	if pol.prepare != nil {
		pol.prepare(w, r, req.Sender)
		return
	}
	pol.writeChallenge(w, r)
}

// writeChallenge answers a prepare call on a network without a prepare flow
// with the route's 402 challenge, naming the public URL of the route.
func (pol *policy) writeChallenge(w http.ResponseWriter, r *http.Request) {
	body := struct {
		X402Version int                         `json:"x402Version"`
		Error       string                      `json:"error,omitempty"`
		Resource    *types.ResourceInfo         `json:"resource,omitempty"`
		Accepts     []types.PaymentRequirements `json:"accepts"`
	}{
		X402Version: int(types.X402VersionV2),
		Error:       "payment required",
		Resource:    &types.ResourceInfo{URL: publicURLForPath(r, pol.prefix)},
		Accepts:     []types.PaymentRequirements{pol.requirements},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "encode x402 payment requirements", http.StatusInternalServerError)
		return
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set(x402http.HeaderPaymentRequired, encoded)
	w.Header().Set(x402http.HeaderXPaymentRequired, encoded)
	w.WriteHeader(http.StatusPaymentRequired)
	_, _ = w.Write(raw)
}

// publicURLForPath resolves the public absolute URL of path from the
// request's forwarding headers; it returns the bare path when no host is
// known.
func publicURLForPath(r *http.Request, path string) string {
	scheme, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	host, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	host = cmp.Or(strings.TrimSpace(host), strings.TrimSpace(r.Host))
	if host == "" {
		return path
	}
	return scheme + "://" + host + path
}

// Close releases the runtime shared by the paid routes, such as RPC
// connections and claim logs. It is safe on a nil or unpaid Paywall.
func (p *Paywall) Close() error {
	if p == nil || p.network == nil {
		return nil
	}
	return p.network.close()
}

// CleanPath is the path form routes are matched on: cleaned, rooted,
// without a trailing slash, and with an ambiguous "//" or "/\" start
// collapsed to "/".
func CleanPath(raw string) string {
	clean := path.Clean(strings.TrimSpace(raw))
	if clean == "." || clean == "" {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	if len(clean) > 1 && (clean[1] == '/' || clean[1] == '\\') {
		return "/"
	}
	if clean != "/" {
		clean = strings.TrimSuffix(clean, "/")
	}
	return clean
}

// requirements is the common exact-scheme contract of one paid route, in
// the upfront flow the gate publishes.
func requirements(network, asset, amount, payTo string, extra map[string]any) types.PaymentRequirements {
	extra["paymentFlow"] = x402http.PaymentFlowUpfront
	return types.PaymentRequirements{
		Scheme:            string(types.Exact),
		Network:           network,
		Asset:             asset,
		Amount:            amount,
		PayTo:             payTo,
		MaxTimeoutSeconds: maxTimeoutSeconds,
		Extra:             extra,
	}
}

func newPolicy(prefix string, req types.PaymentRequirements, facilitator x402http.Facilitator) (*policy, error) {
	gate, err := x402http.New(x402http.Config{
		Requirements: req,
		Facilitator:  facilitator,
		Resource:     &types.ResourceInfo{URL: prefix, MimeType: resourceMimeType},
	})
	if err != nil {
		return nil, err
	}
	return &policy{prefix: prefix, requirements: req, gate: gate}, nil
}

func compactEndpoints(endpoints []string) []string {
	out := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint = strings.TrimSpace(endpoint); endpoint != "" && !slices.Contains(out, endpoint) {
			out = append(out, endpoint)
		}
	}
	return out
}

// rejectOptions refuses every option key not in known.
func rejectOptions(options map[string]string, known ...string) error {
	for key := range options {
		if !slices.Contains(known, key) {
			return fmt.Errorf("unsupported option %q", key)
		}
	}
	return nil
}
