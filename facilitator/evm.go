package facilitator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/gosuda/x402-facilitator/scheme/evm"
	"github.com/gosuda/x402-facilitator/scheme/evm/eip3009"
	"github.com/gosuda/x402-facilitator/scheme/evm/permit2"
	"github.com/gosuda/x402-facilitator/types"
	"github.com/gosuda/x402-facilitator/utils"
)

var _ Facilitator = (*EVMFacilitator)(nil)

type EVMFacilitator struct {
	mu        sync.RWMutex
	scheme    types.Scheme
	network   string
	networkID *big.Int
	endpoint  string
	endpoints []string

	signer  types.Signer
	address common.Address
}

const eip3009DeadlineBuffer int64 = 6

func NewEVMFacilitator(network string, url string, privateKeyHex string) (*EVMFacilitator, error) {
	if network == "" && url == "" {
		return nil, fmt.Errorf("network or rpc url must be provided")
	}

	chainInfo := evm.GetChainInfo(network)
	if chainInfo == nil && url == "" {
		return nil, fmt.Errorf("unsupported network name: %s", network)
	}
	defaultURLs := []string(nil)
	if chainInfo != nil {
		defaultURLs = chainInfo.DefaultURLs
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	networkId, selectedURL, err := selectEVMEndpoint(dialCtx, network, url, defaultURLs)
	if err != nil {
		return nil, err
	}

	privateKey, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, err
	}
	signer := evm.NewRawPrivateSigner(privateKey)
	address, err := evm.GetAddrssFromPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get address from private key: %w", err)
	}

	return &EVMFacilitator{
		scheme:    types.Exact,
		network:   network,
		networkID: networkId,
		endpoint:  selectedURL,
		endpoints: utils.EndpointCandidates(append([]string{selectedURL}, defaultURLs...)),

		signer:  signer,
		address: address,
	}, nil
}

func selectEVMEndpoint(ctx context.Context, network string, priorityURL string, defaultURLs []string) (*big.Int, string, error) {
	candidates := utils.EndpointCandidates(append([]string{priorityURL}, defaultURLs...))
	var selectedNetworkID *big.Int
	selectedURL, err := utils.SelectEndpoint(ctx, candidates, func(ctx context.Context, endpoint string) error {
		client, networkID, err := dialEVMEndpoint(ctx, network, endpoint)
		if err != nil {
			return err
		}
		selectedNetworkID = networkID
		client.Close()
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to Ethereum endpoints: %w", err)
	}

	return selectedNetworkID, selectedURL, nil
}

func dialEVMEndpoint(ctx context.Context, network string, endpoint string) (*ethclient.Client, *big.Int, error) {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return nil, nil, err
	}
	networkID, err := client.NetworkID(ctx)
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("failed to get network ID: %w", err)
	}
	chainName := evm.GetChainName(networkID)
	if chainName == "" || chainName != network {
		client.Close()
		return nil, nil, fmt.Errorf("network mismatch: endpoint is %s, expected %s", chainName, network)
	}

	return client, networkID, nil
}

func (t *EVMFacilitator) evmEndpointCandidates() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return utils.EndpointCandidates(append([]string{t.endpoint}, t.endpoints...))
}

func (t *EVMFacilitator) setActiveEVMEndpoint(endpoint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.endpoint = endpoint
	t.endpoints = utils.EndpointCandidates(append([]string{endpoint}, t.endpoints...))
}

func (t *EVMFacilitator) activeEVMEndpoint() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.endpoint
}

func (t *EVMFacilitator) verifyWithEndpointFallback(ctx context.Context, operation func(client *ethclient.Client) (*types.PaymentVerifyResponse, error)) (*types.PaymentVerifyResponse, error) {
	var response *types.PaymentVerifyResponse
	candidates := t.evmEndpointCandidates()
	_, err := utils.DoWithEndpoint(ctx, candidates, func(ctx context.Context, endpoint string) error {
		client, _, err := dialEVMEndpoint(ctx, t.network, endpoint)
		if err != nil {
			return err
		}
		defer client.Close()

		resp, err := operation(client)
		if err != nil {
			return err
		}

		response = resp
		t.setActiveEVMEndpoint(endpoint)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to verify through Ethereum endpoints %s: %w", strings.Join(candidates, ", "), err)
	}
	return response, nil
}

// Verify detects the payload type and routes to the appropriate verification method.
func (t *EVMFacilitator) Verify(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentVerifyResponse, error) {
	raw, err := json.Marshal(payload.Payload)
	if err != nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}
	if evm.IsPermit2PayloadJSON(raw) {
		return t.verifyWithEndpointFallback(ctx, func(client *ethclient.Client) (*types.PaymentVerifyResponse, error) {
			return t.verifyPermit2(ctx, payload, req, raw, client)
		})
	}
	return t.verifyWithEndpointFallback(ctx, func(client *ethclient.Client) (*types.PaymentVerifyResponse, error) {
		return t.verifyEIP3009(ctx, payload, req, raw, client)
	})
}

// Settle detects the payload type and routes to the appropriate settlement method.
func (t *EVMFacilitator) Settle(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*types.PaymentSettleResponse, error) {
	raw, err := json.Marshal(payload.Payload)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}
	if evm.IsPermit2PayloadJSON(raw) {
		return t.settlePermit2(ctx, payload, req, raw)
	}
	return t.settleEIP3009(ctx, payload, req, raw)
}

func (t *EVMFacilitator) validateEVMPaymentEnvelope(payload *types.PaymentPayload, req *types.PaymentRequirements, payer string) *types.PaymentVerifyResponse {
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
	if !evmAddressMatches(payload.Accepted.PayTo, req.PayTo) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrRecipientMismatch.Error(),
			Payer:         payer,
		}
	}
	return nil
}

func validateEIP3009Authorization(auth *evm.Authorization, req *types.PaymentRequirements, payer string) *types.PaymentVerifyResponse {
	if auth == nil || auth.Value == nil || auth.ValidAfter == nil || auth.ValidBefore == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
			Payer:         payer,
		}
	}

	if !common.IsHexAddress(strings.TrimSpace(req.PayTo)) || auth.To != common.HexToAddress(strings.TrimSpace(req.PayTo)) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrRecipientMismatch.Error(),
			Payer:         payer,
		}
	}

	reqAmount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok || reqAmount.Sign() <= 0 || auth.Value.Cmp(reqAmount) != 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAmountMismatch.Error(),
			Payer:         payer,
		}
	}

	now := time.Now().Unix()
	if auth.ValidBefore.Cmp(big.NewInt(now+eip3009DeadlineBuffer)) < 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAuthorizationExpired.Error(),
			Payer:         payer,
		}
	}
	if auth.ValidAfter.Cmp(big.NewInt(now)) > 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAuthorizationNotYetValid.Error(),
			Payer:         payer,
		}
	}

	return nil
}

func evmAddressMatches(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if !common.IsHexAddress(left) || !common.IsHexAddress(right) {
		return false
	}
	return common.HexToAddress(left) == common.HexToAddress(right)
}

func evmSettleResponseFromInvalid(invalid *types.PaymentVerifyResponse, network types.Network) *types.PaymentSettleResponse {
	return &types.PaymentSettleResponse{
		Success:      false,
		ErrorReason:  invalid.InvalidReason,
		ErrorMessage: invalid.InvalidMessage,
		Payer:        invalid.Payer,
		Network:      network,
	}
}

func (t *EVMFacilitator) Supported() *types.SupportedResponse {
	return &types.SupportedResponse{
		Kinds: []types.SupportedKind{{
			X402Version: int(types.X402VersionV2),
			Scheme:      string(t.scheme),
			Network:     t.network,
		}},
		Extensions: []string{},
		Signers: map[string][]string{
			"eip155:*": {t.address.Hex()},
		},
	}
}

// verification steps:
//   - ✅ verify payload format
//   - ✅ verify payload version
//   - ✅ verify usdc address is correct for the chain
//   - ✅ verify permit signature
//   - ✅ verify deadline
//   - verify nonce is current
//   - ✅ verify client has enough funds to cover paymentRequirements.maxAmountRequired
//   - ✅ verify value in payload is enough to cover paymentRequirements.maxAmountRequired
//   - check min amount is above some threshold we think is reasonable for covering gas
//   - verify resource is not already paid for (next version)
func (t *EVMFacilitator) verifyEIP3009(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements, raw []byte, client *ethclient.Client) (*types.PaymentVerifyResponse, error) {
	// Step 1: Payload format
	var evmPayload evm.EVMPayload
	if err := json.Unmarshal(raw, &evmPayload); err != nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}
	if evmPayload.Authorization == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}
	auth := evmPayload.Authorization
	payer := auth.From.String()

	// Step 2: Bind client-echoed requirements to the server requirements.
	if invalid := t.validateEVMPaymentEnvelope(payload, req, payer); invalid != nil {
		return invalid, nil
	}
	if invalid := validateEIP3009Authorization(auth, req, payer); invalid != nil {
		return invalid, nil
	}

	// Step 3: Network info and Contract info
	chainID := evm.GetChainID(req.Network)
	if chainID == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidNetwork.Error(),
			Payer:         payer,
		}, nil
	}
	if chainID.Cmp(t.networkID) != 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrNetworkIDMismatch.Error(),
			Payer:         payer,
		}, nil
	}
	domainConfig := evm.GetDomainConfig(req.Network, req.Asset)
	if domainConfig == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrTokenMismatch.Error(),
			Payer:         payer,
		}, nil
	}

	// Step 4: Verify signature (EIP-712)
	sig, err := evm.ParseSignature(evmPayload.Signature)
	if err != nil {
		return nil, err
	}
	digest := evm.HashEip3009(auth, domainConfig)
	pubkey, err := evm.Ecrecover(digest, sig)
	if err != nil {
		return nil, err
	}
	if valid := evm.VerifySignature(pubkey, digest, sig[:64]); !valid {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidSignature.Error(),
			Payer:         payer,
		}, nil
	}
	if evm.PubkeyToAddress(pubkey) != auth.From {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidSignature.Error(),
			Payer:         payer,
		}, nil
	}

	// Step 5: Check nonce freshness
	contract, err := eip3009.NewEip3009(domainConfig.VerifyingContract, client)
	if err != nil {
		return nil, fmt.Errorf("contract bind failed: %w", err)
	}
	used, err := contract.AuthorizationState(&bind.CallOpts{Context: ctx}, auth.From, auth.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization state: %w", err)
	}
	if used {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrAuthorizationAlreadyUsed.Error(),
			Payer:         payer,
		}, nil
	}

	// Step 6: Check ERC20 balance
	balance, err := contract.BalanceOf(&bind.CallOpts{Context: ctx}, auth.From)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	if balance.Cmp(auth.Value) < 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInsufficientBalance.Error(),
			Payer:         payer,
		}, nil
	}

	// Step 7: TODO: Check minimum payment threshold (e.g. for gas overhead)

	// Step 8: TODO: Check if resource already paid (next version)

	// ✅ All checks passed
	return &types.PaymentVerifyResponse{
		IsValid: true,
		Payer:   payer,
	}, nil
}

func (t *EVMFacilitator) settleEIP3009(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements, raw []byte) (*types.PaymentSettleResponse, error) {
	network := types.Network(req.Network)

	var evmPayload evm.EVMPayload
	if err := json.Unmarshal(raw, &evmPayload); err != nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrInvalidPayloadFormat.Error(),
			Network:     network,
		}, nil
	}
	if evmPayload.Authorization == nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrInvalidPayloadFormat.Error(),
			Network:     network,
		}, nil
	}
	payer := evmPayload.Authorization.From.String()

	if invalid := t.validateEVMPaymentEnvelope(payload, req, payer); invalid != nil {
		return evmSettleResponseFromInvalid(invalid, network), nil
	}
	if invalid := validateEIP3009Authorization(evmPayload.Authorization, req, payer); invalid != nil {
		return evmSettleResponseFromInvalid(invalid, network), nil
	}

	networkID := evm.GetChainID(req.Network)
	if networkID == nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrInvalidNetwork.Error(),
			Payer:       payer,
			Network:     network,
		}, nil
	}

	domainConfig := evm.GetDomainConfig(req.Network, req.Asset)
	if domainConfig == nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrTokenMismatch.Error(),
			Payer:       payer,
			Network:     network,
		}, nil
	}
	client, _, err := dialEVMEndpoint(ctx, t.network, t.activeEVMEndpoint())
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}
	defer client.Close()

	contract, err := eip3009.NewEip3009(domainConfig.VerifyingContract, client)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrContractBindFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}
	used, err := contract.AuthorizationState(&bind.CallOpts{Context: ctx}, evmPayload.Authorization.From, evmPayload.Authorization.Nonce)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}
	if used {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrAuthorizationAlreadyUsed.Error(),
			Payer:       payer,
			Network:     network,
		}, nil
	}
	clientSig, err := evm.ParseSignature(evmPayload.Signature) // client signature
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrInvalidSignatureFormat.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}

	tx, err := contract.TransferWithAuthorization(
		&bind.TransactOpts{
			Context: ctx,
			Signer:  evm.ToGethSigner(t.signer, networkID), // facilitator signature
			From:    t.address,
		},
		evmPayload.Authorization.From,
		evmPayload.Authorization.To,
		evmPayload.Authorization.Value,
		evmPayload.Authorization.ValidAfter,
		evmPayload.Authorization.ValidBefore,
		evmPayload.Authorization.Nonce,
		clientSig,
	)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}

	return &types.PaymentSettleResponse{
		Success:     true,
		Payer:       payer,
		Transaction: tx.Hash().Hex(),
		Network:     network,
	}, nil
}

// Permit2 verification steps:
//   - ✅ verify payload format
//   - ✅ verify scheme matches
//   - ✅ verify network matches
//   - ✅ verify chain ID matches
//   - ✅ verify spender is x402ExactPermit2Proxy
//   - ✅ verify witness.to matches payTo
//   - ✅ verify deadline not expired (with 6-second buffer)
//   - ✅ verify validAfter not in the future
//   - ✅ verify amount matches requirement
//   - ✅ verify token matches requirement asset
//   - ✅ verify EIP-712 signature
//   - ✅ verify client has enough balance
func (t *EVMFacilitator) verifyPermit2(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements, raw []byte, client *ethclient.Client) (*types.PaymentVerifyResponse, error) {
	// Step 1: Parse Permit2 payload
	var permit2Payload evm.Permit2Payload
	if err := json.Unmarshal(raw, &permit2Payload); err != nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}
	auth := permit2Payload.Permit2Authorization
	if auth == nil || auth.Nonce == nil || auth.Deadline == nil ||
		auth.Permitted.Amount == nil || auth.Witness.ValidAfter == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidPayloadFormat.Error(),
		}, nil
	}

	// Step 2: Scheme verification (scheme lives inside payload.Accepted in v2).
	if payload.Accepted.Scheme != string(t.scheme) || req.Scheme != string(t.scheme) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrIncompatibleScheme.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 3: Network verification
	if payload.Accepted.Network != t.network {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrNetworkMismatch.Error(),
			Payer:         auth.From.String(),
		}, nil
	}
	chainID := evm.GetChainID(payload.Accepted.Network)
	if chainID == nil {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInvalidNetwork.Error(),
			Payer:         auth.From.String(),
		}, nil
	}
	if chainID.Cmp(t.networkID) != 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrNetworkIDMismatch.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 4: Verify spender is x402ExactPermit2Proxy
	if auth.Spender != evm.X402ExactPermit2ProxyAddress {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2InvalidSpender.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 5: Verify witness.to matches payTo
	payTo := common.HexToAddress(req.PayTo)
	if auth.Witness.To != payTo {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2RecipientMismatch.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 6: Deadline not expired (with 6-second buffer for block propagation)
	now := time.Now().Unix()
	if auth.Deadline.Cmp(big.NewInt(now+evm.Permit2DeadlineBuffer)) < 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2DeadlineExpired.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 7: ValidAfter not in the future
	if auth.Witness.ValidAfter.Cmp(big.NewInt(now)) > 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2NotYetValid.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 8: Amount must exactly match requirement
	reqAmount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2AmountMismatch.Error(),
			Payer:         auth.From.String(),
		}, nil
	}
	if auth.Permitted.Amount.Cmp(reqAmount) != 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2AmountMismatch.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 9: Token matches requirement asset
	tokenAddr := evm.GetTokenAddress(payload.Accepted.Network, req.Asset)
	if tokenAddr == (common.Address{}) {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2TokenMismatch.Error(),
			Payer:         auth.From.String(),
		}, nil
	}
	if auth.Permitted.Token != tokenAddr {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2TokenMismatch.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 10: EIP-712 signature verification
	sig, err := evm.ParseSignature(permit2Payload.Signature)
	if err != nil {
		return nil, err
	}
	digest := evm.HashPermit2(auth, chainID)
	pubkey, err := evm.Ecrecover(digest, sig)
	if err != nil {
		return nil, err
	}
	if valid := evm.VerifySignature(pubkey, digest, sig[:64]); !valid {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2InvalidSignature.Error(),
			Payer:         auth.From.String(),
		}, nil
	}
	if evm.PubkeyToAddress(pubkey) != auth.From {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrPermit2InvalidSignature.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// Step 11: Check ERC20 balance
	// Bind to the token contract for balanceOf (not the proxy)
	tokenContract, err := permit2.NewPermit2(auth.Permitted.Token, client)
	if err != nil {
		return nil, fmt.Errorf("token contract bind failed: %w", err)
	}
	balance, err := tokenContract.BalanceOf(&bind.CallOpts{Context: ctx}, auth.From)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	if balance.Cmp(auth.Permitted.Amount) < 0 {
		return &types.PaymentVerifyResponse{
			IsValid:       false,
			InvalidReason: types.ErrInsufficientBalance.Error(),
			Payer:         auth.From.String(),
		}, nil
	}

	// ✅ All checks passed
	return &types.PaymentVerifyResponse{
		IsValid: true,
		Payer:   auth.From.String(),
	}, nil
}

func (t *EVMFacilitator) settlePermit2(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements, raw []byte) (*types.PaymentSettleResponse, error) {
	network := types.Network(req.Network)

	var permit2Payload evm.Permit2Payload
	if err := json.Unmarshal(raw, &permit2Payload); err != nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrInvalidPayloadFormat.Error(),
			Network:     network,
		}, nil
	}
	auth := permit2Payload.Permit2Authorization
	if auth == nil || auth.Nonce == nil || auth.Deadline == nil ||
		auth.Permitted.Amount == nil || auth.Witness.ValidAfter == nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrInvalidPayloadFormat.Error(),
			Network:     network,
		}, nil
	}
	payer := auth.From.String()

	networkID := evm.GetChainID(req.Network)
	if networkID == nil {
		return &types.PaymentSettleResponse{
			Success:     false,
			ErrorReason: types.ErrInvalidNetwork.Error(),
			Payer:       payer,
			Network:     network,
		}, nil
	}

	// Bind to x402ExactPermit2Proxy contract
	client, _, err := dialEVMEndpoint(ctx, t.network, t.activeEVMEndpoint())
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}
	defer client.Close()

	proxyContract, err := permit2.NewPermit2(evm.X402ExactPermit2ProxyAddress, client)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrContractBindFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}

	clientSig, err := evm.ParseSignature(permit2Payload.Signature)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrInvalidSignatureFormat.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}

	// Build settle() arguments
	// Note: abigen generates Struct0 for (address, uint256) tuples and Struct1 for the permit tuple.
	// Struct0 is reused for both TokenPermissions (token, amount) and Witness (to, validAfter)
	// because they share the same ABI shape (address, uint256).
	permitArg := permit2.Struct1{
		Permitted: permit2.Struct0{
			Token:  auth.Permitted.Token,
			Amount: auth.Permitted.Amount,
		},
		Nonce:    auth.Nonce,
		Deadline: auth.Deadline,
	}
	// Witness fields map to Struct0: Token→To, Amount→ValidAfter
	witnessArg := permit2.Struct0{
		Token:  auth.Witness.To,
		Amount: auth.Witness.ValidAfter,
	}

	tx, err := proxyContract.Settle(
		&bind.TransactOpts{
			Context: ctx,
			Signer:  evm.ToGethSigner(t.signer, networkID),
			From:    t.address,
		},
		permitArg,
		auth.From,
		witnessArg,
		clientSig,
	)
	if err != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  types.ErrTransactionFailed.Error(),
			ErrorMessage: err.Error(),
			Payer:        payer,
			Network:      network,
		}, nil
	}

	return &types.PaymentSettleResponse{
		Success:     true,
		Payer:       payer,
		Transaction: tx.Hash().Hex(),
		Network:     network,
	}, nil
}
