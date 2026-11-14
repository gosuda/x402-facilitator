package paywall

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	nanoscheme "github.com/gosuda/x402-facilitator/scheme/nano"
	nanofacilitator "github.com/gosuda/x402-facilitator/scheme/nano/facilitator"
	suihttp "github.com/gosuda/x402-facilitator/scheme/sui/http"
)

// Nano options, set through Config.Options.
const (
	// OptionNanoClaimsFile names a claim log that keeps settled blocks
	// spent across restarts (nanofacilitator.FileClaimStore). Without it
	// claims are kept in memory for the life of the Paywall.
	OptionNanoClaimsFile = "nano_claims_file"
	// OptionNanoRequireBinding ("true"/"false") accepts a payment only with
	// a nano-block-sig/1 binding signed by the paying account. Each route
	// then publishes extra.binding with a fresh nonce and its prefix as the
	// resource.
	OptionNanoRequireBinding = "nano_require_binding"
)

// nanoNetwork pays in XNO; every route shares one facilitator so a block
// pays for one request across all routes.
type nanoNetwork struct {
	network        string
	payTo          string
	requireBinding bool
	facilitator    *nanofacilitator.NanoFacilitator
	claims         *nanofacilitator.FileClaimStore
}

func newNanoNetwork(name string, cfg Config) (network, error) {
	if nanoscheme.GetNetworkInfo(name) == nil {
		return nil, fmt.Errorf("unsupported Nano network %q", name)
	}
	if err := rejectOptions(cfg.Options, OptionNanoClaimsFile, OptionNanoRequireBinding); err != nil {
		return nil, err
	}
	payTo, err := nanoscheme.CanonicalAddress(cfg.PayTo)
	if err != nil {
		return nil, errors.New("x402 XNO payment requires a checksummed Nano pay-to address")
	}
	requireBinding := false
	if raw := strings.TrimSpace(cfg.Options[OptionNanoRequireBinding]); raw != "" {
		if requireBinding, err = strconv.ParseBool(raw); err != nil {
			return nil, fmt.Errorf("option %q must be true or false", OptionNanoRequireBinding)
		}
	}
	endpoints := compactEndpoints(cfg.Endpoints)
	if len(endpoints) > 0 && len(endpoints) < nanoscheme.MinIndependentEndpoints {
		return nil, fmt.Errorf("x402 XNO payment requires at least %d independent Nano RPC endpoints", nanoscheme.MinIndependentEndpoints)
	}
	n := &nanoNetwork{network: name, payTo: payTo, requireBinding: requireBinding}
	opts := nanofacilitator.NanoFacilitatorOptions{Endpoints: endpoints, RequireBinding: requireBinding}
	if path := strings.TrimSpace(cfg.Options[OptionNanoClaimsFile]); path != "" {
		if n.claims, err = nanofacilitator.NewFileClaimStore(path); err != nil {
			return nil, fmt.Errorf("x402 XNO claim log: %w", err)
		}
		opts.Claims = n.claims
	}
	if n.facilitator, err = nanofacilitator.NewNanoFacilitatorWithOptions(name, "", "", opts); err != nil {
		_ = n.close()
		return nil, err
	}
	return n, nil
}

func (n *nanoNetwork) policy(prefix, amount string) (*policy, error) {
	raw, err := nanoscheme.XNOToRaw(amount)
	if err != nil {
		return nil, err
	}
	extra := map[string]any{
		"asset":               nanoscheme.AssetXNO,
		"assetTransferMethod": "nano-native-send",
		"decimals":            nanoscheme.XNORawDecimals,
	}
	if n.requireBinding {
		if extra["binding"], err = nanoBindingOffer(prefix); err != nil {
			return nil, err
		}
	}
	return newPolicy(prefix, requirements(n.network, nanoscheme.AssetXNO, raw, n.payTo, extra), n.facilitator)
}

// nanoBindingOffer is the extra.binding a route publishes: the payer signs
// its block hash with this nonce and resource, so a block hash seen on the
// public ledger cannot be redeemed by anyone else. The nonce is fresh for
// each route of each Paywall.
func nanoBindingOffer(resource string) (map[string]any, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("x402 XNO binding nonce: %w", err)
	}
	return map[string]any{
		"scheme":   nanoscheme.BindingScheme,
		"nonce":    hex.EncodeToString(nonce),
		"resource": resource,
	}, nil
}

func (n *nanoNetwork) clientJS() http.Handler { return suihttp.ClientHandler() }

// close releases the claim log. A settle that races it is refused, never
// accepted unrecorded (FileClaimStore.Claim fails closed).
func (n *nanoNetwork) close() error { return n.claims.Close() }
