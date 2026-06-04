package facilitator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	suischeme "github.com/gosuda/x402-facilitator/scheme/sui"
	"github.com/gosuda/x402-facilitator/types"
	"github.com/gosuda/x402-facilitator/utils"
)

var _ Facilitator = (*SuiFacilitator)(nil)

type SuiFacilitator struct {
	scheme              types.Scheme
	network             string
	client              *suiRPCClient
	gaslessStablecoins  map[string]struct{}
	gaslessStableAssets []string
	minTransferAmounts  map[string]*big.Int
}

type SuiFacilitatorOptions struct {
	GaslessStablecoinTypes []string
	MinTransferAmounts     map[string]string
}

func NewSuiFacilitator(network string, url string, privateKeyHex string) (*SuiFacilitator, error) {
	return NewSuiFacilitatorWithOptions(network, url, privateKeyHex, SuiFacilitatorOptions{})
}

func NewSuiFacilitatorWithOptions(network string, url string, privateKeyHex string, opts SuiFacilitatorOptions) (*SuiFacilitator, error) {
	if !strings.HasPrefix(network, "sui:") {
		return nil, fmt.Errorf("unsupported Sui network %q", network)
	}
	networkInfo := suischeme.GetNetworkInfo(network)
	if networkInfo == nil {
		return nil, fmt.Errorf("unsupported Sui network %q", network)
	}
	client := newSuiRPCClientWithEndpoints(url, networkInfo.DefaultURLs)

	assets := suischeme.GetGaslessStablecoinTypes(network)
	if opts.GaslessStablecoinTypes != nil {
		assets = append([]string(nil), opts.GaslessStablecoinTypes...)
	}

	assets, allowlist := gaslessStablecoinAllowlist(assets)
	minTransferAmounts, err := gaslessStablecoinMinTransferAmounts(network, assets, opts.MinTransferAmounts)
	if err != nil {
		return nil, err
	}

	return &SuiFacilitator{
		scheme:              types.Exact,
		network:             network,
		client:              client,
		gaslessStablecoins:  allowlist,
		gaslessStableAssets: assets,
		minTransferAmounts:  minTransferAmounts,
	}, nil
}

func gaslessStablecoinAllowlist(assets []string) ([]string, map[string]struct{}) {
	allowlist := make(map[string]struct{}, len(assets))
	ordered := make([]string, 0, len(assets))
	for _, asset := range assets {
		asset = strings.TrimSpace(asset)
		if asset == "" {
			continue
		}
		normalized := suischeme.NormalizeType(asset)
		if _, ok := allowlist[normalized]; ok {
			continue
		}
		allowlist[normalized] = struct{}{}
		ordered = append(ordered, asset)
	}
	return ordered, allowlist
}

func gaslessStablecoinMinTransferAmounts(network string, assets []string, overrides map[string]string) (map[string]*big.Int, error) {
	minAmounts := make(map[string]*big.Int, len(assets))
	for _, asset := range assets {
		if decimals, ok := suischeme.GetGaslessStablecoinDecimals(network, asset); ok {
			minAmounts[suischeme.NormalizeType(asset)] = suischeme.MinimumGaslessStablecoinAmount(decimals)
		}
	}
	for asset, rawAmount := range overrides {
		amount, ok := new(big.Int).SetString(strings.TrimSpace(rawAmount), 10)
		if !ok || amount.Sign() < 0 {
			return nil, fmt.Errorf("invalid minimum transfer amount for %s: %s", asset, rawAmount)
		}
		setGaslessStablecoinMinTransferAmount(network, minAmounts, asset, amount)
	}
	return minAmounts, nil
}

func setGaslessStablecoinMinTransferAmount(network string, minAmounts map[string]*big.Int, asset string, amount *big.Int) {
	minAmounts[suischeme.NormalizeType(asset)] = new(big.Int).Set(amount)
	if coinType, ok := suischeme.GetGaslessStablecoinType(network, asset); ok {
		minAmounts[suischeme.NormalizeType(coinType)] = new(big.Int).Set(amount)
	}
}

func (t *SuiFacilitator) Verify(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentVerifyResponse, error) {
	if payload == nil || req == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}

	parsed, err := t.parseAndVerifySuiPayload(ctx, payload.Payload)
	if err != nil {
		if errors.Is(err, suischeme.ErrSignatureVerificationUnavailable) {
			return nil, err
		}
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  invalidPayloadReason(err),
			InvalidMessage: err.Error(),
		}, nil
	}
	if suischeme.NormalizeAddress(parsed.Payer) != suischeme.NormalizeAddress(parsed.Sender) {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrInvalidSignature.Error(),
			InvalidMessage: fmt.Sprintf("signature payer %s does not match transaction sender %s", parsed.Payer, parsed.Sender),
			Payer:          parsed.Payer,
		}, nil
	}

	if invalid := t.validatePaymentEnvelope(payload, req, parsed.Payer); invalid != nil {
		return invalid, nil
	}

	reqAmount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok || reqAmount.Sign() <= 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAmountMismatch.Error(),
			Payer:         parsed.Payer,
		}, nil
	}
	if minAmount, ok := t.minTransferAmount(req.Asset); ok && reqAmount.Cmp(minAmount) < 0 {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrAmountMismatch.Error(),
			InvalidMessage: fmt.Sprintf("amount %s is below minimum %s for %s", reqAmount.String(), minAmount.String(), req.Asset),
			Payer:          parsed.Payer,
		}, nil
	}

	dryRun, err := t.client.dryRunTransactionBlock(ctx, parsed.Payload.Transaction)
	if err != nil {
		return nil, fmt.Errorf("dry run transaction failed: %w", err)
	}
	if !dryRun.Success() {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrTransactionFailed.Error(),
			InvalidMessage: dryRun.StatusError(),
			Payer:          parsed.Payer,
		}, nil
	}
	asset, ok := t.canonicalGaslessStablecoin(req.Asset)
	if !ok {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidToken.Error(),
			Payer:         parsed.Payer,
		}, nil
	}

	if err := dryRun.ValidateGaslessStablecoinPayment(asset); err != nil {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrInvalidTransaction.Error(),
			InvalidMessage: err.Error(),
			Payer:          parsed.Payer,
		}, nil
	}

	received := dryRun.BalanceDelta(req.PayTo, asset)
	if received.Cmp(reqAmount) != 0 {
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  types.ErrAmountMismatch.Error(),
			InvalidMessage: fmt.Sprintf("expected payTo balance delta %s, got %s", reqAmount.String(), received.String()),
			Payer:          parsed.Payer,
		}, nil
	}

	return &types.PaymentVerifyResponse{
		IsValid: true,
		Payer:   parsed.Payer,
	}, nil
}

func (t *SuiFacilitator) Settle(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentSettleResponse, error) {
	network := types.Network("")
	if req != nil {
		network = types.Network(req.Network)
	}

	verified, err := t.Verify(ctx, payload, req)
	if err != nil {
		return nil, err
	}
	if !verified.IsValid {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  verified.InvalidReason,
			ErrorMessage: verified.InvalidMessage,
			Payer:        verified.Payer,
			Network:      network,
		}, nil
	}

	suiPayload, payloadDigest, err := parseSuiPayloadForSettlement(payload.Payload)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  invalidPayloadReason(err),
			ErrorMessage: err.Error(),
			Payer:        verified.Payer,
			Network:      network,
		}, nil
	}

	executed, err := t.client.executeTransactionBlock(ctx, suiPayload.Transaction, []string{suiPayload.Signature})
	if err != nil {
		if settled, lookupErr := t.settledTransactionResponse(ctx, payloadDigest, req, verified.Payer, network); lookupErr == nil && settled != nil {
			return settled, nil
		}
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        verified.Payer,
			Network:      network,
		}, nil
	}
	transactionDigest := executed.Digest.String()
	if transactionDigest == "" {
		transactionDigest = payloadDigest
	}
	if !executed.IsSuccess() {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: suischeme.TransactionResultStatusError(executed, "transaction failed"),
			Payer:        verified.Payer,
			Transaction:  transactionDigest,
			Network:      network,
		}, nil
	}

	return &types.PaymentSettleResponse{
		Success:     true,
		Payer:       verified.Payer,
		Transaction: transactionDigest,
		Network:     network,
	}, nil
}

func (t *SuiFacilitator) Supported() *types.SupportedResponse {
	return &types.SupportedResponse{
		Kinds: []types.SupportedKind{{
			X402Version: int(types.X402VersionV2),
			Scheme:      string(t.scheme),
			Network:     t.network,
			Extra: map[string]interface{}{
				"assetTransferMethod": "sui-gasless-stablecoin-address-balance",
				"assets":              t.gaslessStableAssets,
				"networkId":           suischeme.GetNetworkID(t.network),
				"networkName":         suischeme.GetNetworkName(t.network),
			},
		}},
		Extensions: []string{},
		Signers:    map[string][]string{},
	}
}

func (t *SuiFacilitator) validatePaymentEnvelope(payload *types.PaymentPayload, req *types.PaymentRequirements, payer string) *types.PaymentVerifyResponse {
	if payload.X402Version != int(types.X402VersionV2) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
			Payer:         payer,
		}
	}
	if payload.Accepted.Scheme != string(t.scheme) || req.Scheme != string(t.scheme) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrIncompatibleScheme.Error(),
			Payer:         payer,
		}
	}
	if payload.Accepted.Network != t.network || req.Network != t.network {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrNetworkMismatch.Error(),
			Payer:         payer,
		}
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Accepted.Asset), strings.TrimSpace(req.Asset)) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrTokenMismatch.Error(),
			Payer:         payer,
		}
	}
	if payload.Accepted.Amount != req.Amount {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAmountMismatch.Error(),
			Payer:         payer,
		}
	}
	if suischeme.NormalizeAddress(payload.Accepted.PayTo) != suischeme.NormalizeAddress(req.PayTo) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrRecipientMismatch.Error(),
			Payer:         payer,
		}
	}
	if !t.isGaslessStablecoin(req.Asset) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidToken.Error(),
			Payer:         payer,
		}
	}
	return nil
}

func (t *SuiFacilitator) isGaslessStablecoin(asset string) bool {
	_, ok := t.canonicalGaslessStablecoin(asset)
	return ok
}

func (t *SuiFacilitator) canonicalGaslessStablecoin(asset string) (string, bool) {
	if t == nil {
		return "", false
	}
	if _, ok := t.gaslessStablecoins[suischeme.NormalizeType(asset)]; ok {
		return asset, true
	}
	coinType, ok := suischeme.GetGaslessStablecoinType(t.network, asset)
	if !ok {
		return "", false
	}
	if _, ok := t.gaslessStablecoins[suischeme.NormalizeType(coinType)]; !ok {
		return "", false
	}
	return coinType, true
}

func (t *SuiFacilitator) minTransferAmount(asset string) (*big.Int, bool) {
	if t == nil {
		return nil, false
	}
	minAmount, ok := t.minTransferAmounts[suischeme.NormalizeType(asset)]
	if !ok {
		if coinType, resolved := t.canonicalGaslessStablecoin(asset); resolved {
			minAmount, ok = t.minTransferAmounts[suischeme.NormalizeType(coinType)]
		}
	}
	if !ok {
		return nil, false
	}
	return new(big.Int).Set(minAmount), true
}

func (t *SuiFacilitator) settledTransactionResponse(ctx context.Context, digest string, req *types.PaymentRequirements, payer string, network types.Network) (*types.PaymentSettleResponse, error) {
	if strings.TrimSpace(digest) == "" {
		return nil, errors.New("empty transaction digest")
	}
	executed, err := t.client.getTransactionBlock(ctx, digest)
	if err != nil {
		return nil, err
	}
	if !executed.IsSuccess() {
		return nil, fmt.Errorf("transaction %s is not successful: %s", digest, suischeme.TransactionResultStatusError(executed, "transaction failed"))
	}
	reqAmount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok || reqAmount.Sign() <= 0 {
		return nil, fmt.Errorf("invalid amount: %s", req.Amount)
	}
	asset, ok := t.canonicalGaslessStablecoin(req.Asset)
	if !ok {
		return nil, fmt.Errorf("invalid asset: %s", req.Asset)
	}
	received := suischeme.TransactionResultBalanceDelta(executed, req.PayTo, asset)
	if received.Cmp(reqAmount) != 0 {
		return nil, fmt.Errorf("settled transaction %s balance delta mismatch: expected %s, got %s", digest, reqAmount.String(), received.String())
	}

	return &types.PaymentSettleResponse{
		Success:     true,
		Payer:       payer,
		Transaction: digest,
		Network:     network,
	}, nil
}

type verifiedSuiPayload struct {
	Payload           *suischeme.Payload
	Payer             string
	Sender            string
	TransactionDigest string
}

func (t *SuiFacilitator) parseAndVerifySuiPayload(ctx context.Context, payload map[string]interface{}) (*verifiedSuiPayload, error) {
	suiPayload, err := suischeme.PayloadFromMap(payload)
	if err != nil {
		return nil, err
	}

	txBytes, err := suiPayload.DecodeTransaction()
	if err != nil {
		return nil, err
	}
	payer, err := suischeme.VerifySignatureWithOptions(ctx, suiPayload.Signature, txBytes, suischeme.SignatureVerifyOptions{
		ZkLoginVerifier: t.client.verifyZkLoginSignature,
	})
	if err != nil {
		return nil, err
	}
	sender, err := suischeme.TransactionSender(txBytes)
	if err != nil {
		return nil, err
	}
	digest, err := suischeme.TransactionDigest(txBytes)
	if err != nil {
		return nil, err
	}

	return &verifiedSuiPayload{
		Payload:           suiPayload,
		Payer:             payer,
		Sender:            sender,
		TransactionDigest: digest,
	}, nil
}

func parseSuiPayloadForSettlement(payload map[string]interface{}) (*suischeme.Payload, string, error) {
	suiPayload, err := suischeme.PayloadFromMap(payload)
	if err != nil {
		return nil, "", err
	}
	txBytes, err := suiPayload.DecodeTransaction()
	if err != nil {
		return nil, "", err
	}
	digest, err := suischeme.TransactionDigest(txBytes)
	if err != nil {
		return nil, "", err
	}
	return suiPayload, digest, nil
}

func invalidPayloadReason(err error) string {
	if errors.Is(err, suischeme.ErrInvalidSignature) ||
		errors.Is(err, suischeme.ErrUnsupportedSignature) ||
		errors.Is(err, suischeme.ErrEmptySignature) {
		return types.ErrInvalidSignature.Error()
	}
	return types.ErrInvalidPayloadFormat.Error()
}

type suiRPCClient struct {
	mu         sync.RWMutex
	url        string
	endpoints  []string
	httpClient *http.Client
}

func newSuiRPCClientWithEndpoints(url string, endpoints []string) *suiRPCClient {
	candidates := utils.EndpointCandidates(append([]string{url}, endpoints...))
	activeURL := ""
	if len(candidates) > 0 {
		activeURL = candidates[0]
	}
	return &suiRPCClient{
		url:       activeURL,
		endpoints: candidates,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *suiRPCClient) dryRunTransactionBlock(ctx context.Context, txBytesBase64 string) (*suischeme.DryRunTransactionBlock, error) {
	var result suischeme.DryRunTransactionBlock
	if err := c.call(ctx, "sui_dryRunTransactionBlock", []interface{}{txBytesBase64}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *suiRPCClient) executeTransactionBlock(ctx context.Context, txBytesBase64 string, signatures []string) (*suischeme.ExecuteTransactionBlock, error) {
	options := map[string]bool{
		"showEffects":        true,
		"showBalanceChanges": true,
	}
	params := []interface{}{txBytesBase64, signatures, options, "WaitForLocalExecution"}

	var result suischeme.ExecuteTransactionBlock
	if err := c.call(ctx, "sui_executeTransactionBlock", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *suiRPCClient) getTransactionBlock(ctx context.Context, digest string) (*suischeme.ExecuteTransactionBlock, error) {
	options := map[string]bool{
		"showEffects":        true,
		"showBalanceChanges": true,
	}
	params := []interface{}{digest, options}

	var result suischeme.ExecuteTransactionBlock
	if err := c.call(ctx, "sui_getTransactionBlock", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *suiRPCClient) verifyZkLoginSignature(ctx context.Context, author string, txBytes []byte, signature string) (bool, error) {
	params := []interface{}{
		base64.StdEncoding.EncodeToString(txBytes),
		signature,
		"TransactionData",
		author,
	}

	var result suiZkLoginVerifyResult
	if err := c.call(ctx, "sui_verifyZkLoginSignature", params, &result); err != nil {
		return false, err
	}
	return result.Success && len(result.Errors) == 0, nil
}

func (c *suiRPCClient) call(ctx context.Context, method string, params []interface{}, result interface{}) error {
	candidates := c.endpointCandidates()
	selected, err := utils.DoWithEndpoint(ctx, candidates, func(ctx context.Context, endpoint string) error {
		return c.callEndpoint(ctx, endpoint, method, params, result)
	})
	if err != nil {
		return err
	}
	c.setActiveEndpoint(selected)
	return nil
}

func (c *suiRPCClient) endpointCandidates() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return utils.EndpointCandidates(append([]string{c.url}, c.endpoints...))
}

func (c *suiRPCClient) setActiveEndpoint(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.url = endpoint
	c.endpoints = utils.EndpointCandidates(append([]string{endpoint}, c.endpoints...))
}

func (c *suiRPCClient) callEndpoint(ctx context.Context, endpoint string, method string, params []interface{}, result interface{}) error {
	reqBody := suiRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("sui rpc http status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var rpcResp suiRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return rpcResp.Error
	}
	if len(rpcResp.Result) == 0 {
		return errors.New("sui rpc missing result")
	}
	return json.Unmarshal(rpcResp.Result, result)
}

type suiRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type suiRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *suiRPCError    `json:"error,omitempty"`
}

type suiZkLoginVerifyResult struct {
	Success bool     `json:"success"`
	Errors  []string `json:"errors,omitempty"`
}

type suiRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *suiRPCError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Data) == 0 {
		return fmt.Sprintf("sui rpc error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("sui rpc error %d: %s: %s", e.Code, e.Message, string(e.Data))
}
