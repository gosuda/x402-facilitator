package paywall

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	facilitatorclient "github.com/gosuda/x402-facilitator/api/client"
	casperscheme "github.com/gosuda/x402-facilitator/scheme/casper"
	suihttp "github.com/gosuda/x402-facilitator/scheme/sui/http"
)

// casperNetwork pays in wCSPR through a hosted Casper facilitator.
type casperNetwork struct {
	network     string
	asset       string
	payTo       string
	facilitator *facilitatorclient.Client
}

func newCasperNetwork(name string, cfg Config) (network, error) {
	if casperscheme.GetNetworkInfo(name) == nil {
		return nil, fmt.Errorf("unsupported Casper network %q", name)
	}
	if err := rejectOptions(cfg.Options); err != nil {
		return nil, err
	}
	asset := strings.ToLower(strings.TrimSpace(cfg.Asset))
	if asset == "" {
		return nil, errors.New("x402 wCSPR payment requires the wCSPR CEP-18 contract hash")
	}
	payTo := casperscheme.NormalizeAddress(cfg.PayTo)
	if payTo == "" {
		return nil, errors.New("x402 wCSPR payment requires a Casper pay-to address")
	}
	facilitator, err := newCasperFacilitatorClient(cfg)
	if err != nil {
		return nil, err
	}
	return &casperNetwork{network: name, asset: asset, payTo: payTo, facilitator: facilitator}, nil
}

func newCasperFacilitatorClient(cfg Config) (*facilitatorclient.Client, error) {
	endpoint := casperscheme.DefaultFacilitatorURL
	if endpoints := compactEndpoints(cfg.Endpoints); len(endpoints) > 0 {
		endpoint = endpoints[0]
	}
	token := strings.TrimSpace(cfg.FacilitatorToken)
	if token == "" && strings.EqualFold(strings.TrimRight(endpoint, "/"), casperscheme.DefaultFacilitatorURL) {
		return nil, errors.New("CSPR.cloud x402 facilitator requires an authorization token")
	}
	if token != "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse x402 facilitator endpoint %q: %w", endpoint, err)
		}
		if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("x402 facilitator endpoint %q sends the authorization token over plaintext http; use https or a loopback endpoint", endpoint)
		}
	}
	client, err := facilitatorclient.NewClient(endpoint)
	if err != nil {
		return nil, fmt.Errorf("create casper x402 facilitator: %w", err)
	}
	if token != "" {
		client.CreateAuthHeader = func() (map[string]map[string]string, error) {
			return map[string]map[string]string{
				"verify": {"Authorization": token},
				"settle": {"Authorization": token},
			}, nil
		}
	}
	return client, nil
}

func isLoopbackHost(hostname string) bool {
	if hostname == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(strings.Trim(hostname, "[]"))
	return err == nil && net.IP(addr.AsSlice()).IsLoopback()
}

func (n *casperNetwork) policy(prefix, amount string) (*policy, error) {
	motes, err := casperscheme.CSPRToMotes(amount)
	if err != nil {
		return nil, err
	}
	req := requirements(n.network, n.asset, motes.String(), n.payTo, map[string]any{
		"asset":               casperscheme.WCSPRSymbol,
		"assetTransferMethod": "casper-cep18-transfer",
		"decimals":            casperscheme.MoteDecimals,
	})
	return newPolicy(prefix, req, n.facilitator)
}

// clientJS keeps serving the shared browser client on every network so a
// page that imports it loads; only Sui routes answer its prepare call with
// transactions.
func (n *casperNetwork) clientJS() http.Handler { return suihttp.ClientHandler() }

func (n *casperNetwork) close() error { return nil }
