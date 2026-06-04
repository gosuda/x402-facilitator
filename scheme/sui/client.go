package sui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	suigrpc "github.com/gosuda/x402-facilitator/scheme/sui/grpc"
	rpcv2 "github.com/gosuda/x402-facilitator/scheme/sui/grpc/pb/sui/rpc/v2"
	"github.com/gosuda/x402-facilitator/utils"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Client owns Sui endpoint failover, gRPC transport calls, and response conversion.
type Client struct {
	mu        sync.RWMutex
	url       string
	endpoints []string
	grpc      *suigrpc.GRPCClient
}

type BalanceSummary struct {
	Balance        uint64
	AddressBalance uint64
	CoinBalance    uint64
}

func NewClientWithEndpoints(url string, endpoints []string) *Client {
	candidates := utils.EndpointCandidates(append([]string{url}, endpoints...))
	activeURL := ""
	if len(candidates) > 0 {
		activeURL = candidates[0]
	}
	return &Client{
		url:       activeURL,
		endpoints: candidates,
		grpc:      suigrpc.NewGRPCClient(),
	}
}

func NewClientForNetwork(network string, endpoints []string) (*Client, error) {
	candidates, err := endpointCandidatesForNetwork(network, endpoints)
	if err != nil {
		return nil, err
	}
	return NewClientWithEndpoints("", candidates), nil
}

func (c *Client) ActiveEndpoint() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.url
}

func (c *Client) Close() error {
	if c == nil || c.grpc == nil {
		return nil
	}
	return c.grpc.Close()
}

func (c *Client) DryRunTransactionBlock(ctx context.Context, txBytesBase64 string) (*DryRunTransactionBlock, error) {
	txBytesBase64 = strings.TrimSpace(txBytesBase64)
	if txBytesBase64 == "" {
		return nil, ErrEmptyTransaction
	}
	txBytes, err := base64.StdEncoding.DecodeString(txBytesBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction encoding: %w", err)
	}
	if len(txBytes) == 0 {
		return nil, ErrEmptyTransaction
	}
	var result *DryRunTransactionBlock
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.SimulateTransaction(ctx, endpoint, txBytes)
		if err != nil {
			return err
		}
		result, err = dryRunTransactionBlockFromGRPC(txBytes, response)
		return err
	}); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("missing Sui transaction dry run result")
	}
	return result, nil
}

func (c *Client) ExecuteTransactionBlock(ctx context.Context, txBytesBase64 string, signatures []string) (*ExecuteTransactionBlock, error) {
	txBytesBase64 = strings.TrimSpace(txBytesBase64)
	if txBytesBase64 == "" {
		return nil, ErrEmptyTransaction
	}
	txBytes, err := base64.StdEncoding.DecodeString(txBytesBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction encoding: %w", err)
	}
	if len(txBytes) == 0 {
		return nil, ErrEmptyTransaction
	}

	signatureBytes := make([][]byte, 0, len(signatures))
	for _, signature := range signatures {
		signature = strings.TrimSpace(signature)
		if signature == "" {
			return nil, ErrEmptySignature
		}
		decoded, err := base64.StdEncoding.DecodeString(signature)
		if err != nil {
			return nil, fmt.Errorf("invalid signature encoding: %w", err)
		}
		if len(decoded) == 0 {
			return nil, ErrEmptySignature
		}
		signatureBytes = append(signatureBytes, decoded)
	}
	if len(signatureBytes) == 0 {
		return nil, ErrEmptySignature
	}

	var result *ExecuteTransactionBlock
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.ExecuteTransaction(ctx, endpoint, txBytes, signatureBytes)
		if err != nil {
			return err
		}
		result, err = executeTransactionBlockFromGRPC(response.GetTransaction())
		return err
	}); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("missing Sui transaction execution result")
	}
	return result, nil
}

func (c *Client) GetTransactionBlock(ctx context.Context, digest string) (*ExecuteTransactionBlock, error) {
	var result *ExecuteTransactionBlock
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.GetTransaction(ctx, endpoint, digest)
		if err != nil {
			return err
		}
		result, err = executeTransactionBlockFromGRPC(response.GetTransaction())
		return err
	}); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("missing Sui transaction")
	}
	return result, nil
}

func (c *Client) VerifyZkLoginSignature(ctx context.Context, author string, txBytes []byte, signature string) (bool, error) {
	var result bool
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		ok, err := c.grpc.VerifySignature(ctx, endpoint, author, txBytes, signature)
		if err != nil {
			return err
		}
		result = ok
		return nil
	}); err != nil {
		return false, err
	}
	return result, nil
}

func (c *Client) ListOwnedCoinObjects(ctx context.Context, owner string, coinType string) ([]OwnedCoinObject, error) {
	var coinObjects []OwnedCoinObject
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		fetched, err := c.listOwnedCoinObjectsFromEndpoint(ctx, endpoint, owner, coinType)
		if err != nil {
			return err
		}
		coinObjects = fetched
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list Sui coin objects: %w", err)
	}
	return coinObjects, nil
}

func (c *Client) Balance(ctx context.Context, owner string, coinType string) (BalanceSummary, error) {
	var balance BalanceSummary
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.GetBalance(ctx, endpoint, owner, coinType)
		if err != nil {
			return err
		}
		if value := response.GetBalance(); value != nil {
			balance = BalanceSummary{
				Balance:        value.GetBalance(),
				AddressBalance: value.GetAddressBalance(),
				CoinBalance:    value.GetCoinBalance(),
			}
		}
		return nil
	}); err != nil {
		return BalanceSummary{}, fmt.Errorf("failed to get Sui balance: %w", err)
	}
	return balance, nil
}

func (c *Client) AddressBalance(ctx context.Context, owner string, coinType string) (uint64, error) {
	balance, err := c.Balance(ctx, owner, coinType)
	if err != nil {
		return 0, err
	}
	return balance.AddressBalance, nil
}

func (c *Client) ResolveGaslessStablecoinExpiration(ctx context.Context, chainDigest string) (*TransactionExpiration, error) {
	var expiration *TransactionExpiration
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		resolved, err := c.resolveGaslessStablecoinExpirationFromEndpoint(ctx, endpoint, chainDigest)
		if err != nil {
			return err
		}
		expiration = resolved
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to resolve gasless stablecoin expiration: %w", err)
	}
	if expiration == nil {
		return nil, errors.New("failed to resolve gasless stablecoin expiration")
	}
	return expiration, nil
}

func (c *Client) call(ctx context.Context, action func(context.Context, string) error) error {
	if c == nil {
		return errors.New("nil Sui client")
	}
	if c.grpc == nil {
		return errors.New("Sui gRPC client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	candidates := c.endpointCandidates()
	if len(candidates) == 0 {
		return errors.New("no Sui gRPC endpoints")
	}
	selected, err := utils.DoWithEndpoint(ctx, candidates, func(ctx context.Context, endpoint string) error {
		return action(ctx, endpoint)
	})
	if err != nil {
		return err
	}
	c.setActiveEndpoint(selected)
	return nil
}

func (c *Client) endpointCandidates() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return utils.EndpointCandidates(append([]string{c.url}, c.endpoints...))
}

func (c *Client) setActiveEndpoint(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.url = endpoint
	c.endpoints = utils.EndpointCandidates(append([]string{endpoint}, c.endpoints...))
}

func (c *Client) listOwnedCoinObjectsFromEndpoint(ctx context.Context, endpoint string, owner string, coinType string) ([]OwnedCoinObject, error) {
	var coinObjects []OwnedCoinObject
	var pageToken []byte

	for {
		page, err := c.grpc.ListOwnedObjects(ctx, endpoint, listOwnedCoinObjectsRequest(owner, coinType, pageToken))
		if err != nil {
			return nil, err
		}
		for i, object := range page.GetObjects() {
			coinObject, err := ownedCoinObjectFromGRPC(object, coinType)
			if err != nil {
				return nil, fmt.Errorf("invalid coin object %d: %w", len(coinObjects)+i, err)
			}
			coinObjects = append(coinObjects, coinObject)
		}
		if len(page.GetNextPageToken()) == 0 {
			break
		}
		pageToken = page.GetNextPageToken()
	}

	return coinObjects, nil
}

func (c *Client) resolveGaslessStablecoinExpirationFromEndpoint(ctx context.Context, endpoint string, chainDigest string) (*TransactionExpiration, error) {
	info, err := c.grpc.GetServiceInfo(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	epoch := info.GetEpoch()
	if epoch == ^uint64(0) {
		return nil, errors.New("Sui epoch is too large")
	}
	if strings.TrimSpace(chainDigest) == "" {
		chainDigest = info.GetChainId()
	}

	if strings.TrimSpace(chainDigest) == "" {
		checkpoint, err := c.grpc.GetCheckpoint(ctx, endpoint, 0)
		if err != nil {
			return nil, err
		}
		chainDigest = checkpoint.GetCheckpoint().GetDigest()
	}

	nonce, err := randomUint32()
	if err != nil {
		return nil, err
	}
	return TransactionExpirationValidDuring(chainDigest, epoch, epoch+1, nonce)
}

func endpointCandidatesForNetwork(network string, endpoints []string) ([]string, error) {
	endpointInput := append([]string{}, endpoints...)
	if info := GetNetworkInfo(network); info != nil {
		endpointInput = append(endpointInput, info.DefaultURLs...)
	}
	candidates := utils.EndpointCandidates(endpointInput)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no Sui gRPC endpoints for network %q", network)
	}
	return candidates, nil
}

func listOwnedCoinObjectsRequest(owner string, coinType string, pageToken []byte) *rpcv2.ListOwnedObjectsRequest {
	objectType := coinObjectType(coinType)
	return &rpcv2.ListOwnedObjectsRequest{
		Owner:      &owner,
		PageSize:   ptr(uint32(suiCoinObjectPageLimit)),
		PageToken:  pageToken,
		ObjectType: &objectType,
		ReadMask: &fieldmaskpb.FieldMask{Paths: []string{
			"object_id",
			"version",
			"digest",
			"object_type",
			"balance",
		}},
	}
}
