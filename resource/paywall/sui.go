package paywall

import (
	"errors"
	"fmt"
	"net/http"

	suischeme "github.com/gosuda/x402-facilitator/scheme/sui"
	suifacilitator "github.com/gosuda/x402-facilitator/scheme/sui/facilitator"
	suihttp "github.com/gosuda/x402-facilitator/scheme/sui/http"
)

const (
	suiMainnet = "sui:mainnet"
	suiTestnet = "sui:testnet"
	suiAsset   = "USDC"
)

// suiNetwork pays in gasless USDC and serves the unsigned-transaction
// prepare flow and its browser client.
type suiNetwork struct {
	network     string
	asset       string
	payTo       string
	endpoints   []string
	facilitator *suifacilitator.SuiFacilitator
}

func newSuiNetwork(name string, cfg Config) (network, error) {
	if name != suiMainnet && name != suiTestnet {
		return nil, fmt.Errorf("unsupported Sui network %q", name)
	}
	if err := rejectOptions(cfg.Options); err != nil {
		return nil, err
	}
	asset, ok := suischeme.GetGaslessStablecoinType(name, suiAsset)
	if !ok {
		return nil, fmt.Errorf("%s is not gasless stablecoin allowlisted on %s", suiAsset, name)
	}
	payTo := suischeme.NormalizeAddress(cfg.PayTo)
	if payTo == "" {
		return nil, errors.New("x402 USDC payment requires a Sui pay-to address")
	}
	endpoints := compactEndpoints(cfg.Endpoints)
	rpcURL := ""
	if len(endpoints) > 0 {
		rpcURL = endpoints[0]
	}
	facilitator, err := suifacilitator.NewSuiFacilitatorWithOptions(name, rpcURL, "", suifacilitator.SuiFacilitatorOptions{
		GaslessStablecoinTypes: []string{asset},
	})
	if err != nil {
		return nil, err
	}
	return &suiNetwork{network: name, asset: asset, payTo: payTo, endpoints: endpoints, facilitator: facilitator}, nil
}

func (n *suiNetwork) policy(prefix, amount string) (*policy, error) {
	atomic, err := suischeme.StablecoinAmountToAtomic(n.network, suiAsset, amount)
	if err != nil {
		return nil, err
	}
	req := requirements(n.network, n.asset, atomic, n.payTo, map[string]any{
		"asset":               suiAsset,
		"assetTransferMethod": "sui-gasless-stablecoin-address-balance",
	})
	pol, err := newPolicy(prefix, req, n.facilitator)
	if err != nil {
		return nil, err
	}
	preparer, err := suihttp.NewPreparer(suihttp.Config{
		Requirements:     req,
		ResourcePath:     prefix,
		ResourceMimeType: resourceMimeType,
		Endpoints:        n.endpoints,
	})
	if err != nil {
		return nil, err
	}
	pol.prepare = preparer.WritePrepare
	return pol, nil
}

func (n *suiNetwork) clientJS() http.Handler { return suihttp.ClientHandler() }

func (n *suiNetwork) close() error { return n.facilitator.Close() }
