package facilitator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gosuda/x402-facilitator/scheme/evm/erc7710"
	"github.com/gosuda/x402-facilitator/types"
	"github.com/rs/zerolog/log"
)

// ERC7710Facilitator serves the x402 v2 "exact" scheme's erc7710 asset-transfer method against a
// single pinned ERC-7710 delegation manager.
//
// It deliberately shares nothing with EVMFacilitator: the eip3009/permit2 paths verify token-level
// signatures and therefore need per-chain token registries and EIP-712 domains, while the erc7710
// path needs none of that - verification IS simulation of the on-chain manager, which runs every
// caveat (spending caps, payee allowlists, time windows, identity attestations) exactly as
// settlement will. One code path for policy, on-chain, or none.
//
// Trust model: the facilitator's key pays gas and nothing else. It cannot widen a delegation's
// scope, cannot redirect funds (the transfer target and amount are baked into the execution it
// builds from the payment requirements), and holds no custody at any point. The delegation
// manager address is pinned at construction because settlement broadcasts calldata to it -
// accepting an attacker-supplied manager would turn the fee payer into a gas faucet.
type ERC7710Facilitator struct {
	network   string
	networkID *big.Int
	manager   common.Address

	client     *ethclient.Client
	privateKey *ecdsa.PrivateKey
	address    common.Address

	// settleGasLimit is fixed rather than estimated: GIWA's public RPC load-balances across
	// backends that lag each other, so estimateGas can disagree with the sequencer's canonical
	// state and fail spuriously. 1.5M covers a full 4-caveat redemption several times over.
	settleGasLimit uint64
}

var _ Facilitator = (*ERC7710Facilitator)(nil)

// NewERC7710Facilitator connects to an EVM chain by CAIP-2 id and pins the delegation manager.
func NewERC7710Facilitator(network, url, privateKeyHex, managerHex string) (*ERC7710Facilitator, error) {
	var chainID uint64
	if _, err := fmt.Sscanf(network, "eip155:%d", &chainID); err != nil {
		return nil, fmt.Errorf("erc7710 facilitator requires an eip155 CAIP-2 network, got %q", network)
	}
	if !common.IsHexAddress(managerHex) {
		return nil, fmt.Errorf("invalid delegation manager address %q", managerHex)
	}

	client, err := ethclient.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}
	networkID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get network ID: %w", err)
	}
	if networkID.Uint64() != chainID {
		return nil, fmt.Errorf("rpc chain id %d does not match configured %s", networkID, network)
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return &ERC7710Facilitator{
		network:        network,
		networkID:      networkID,
		manager:        common.HexToAddress(managerHex),
		client:         client,
		privateKey:     privateKey,
		address:        crypto.PubkeyToAddress(privateKey.PublicKey),
		settleGasLimit: 1_500_000,
	}, nil
}

// Verify checks a payment by simulating the redemption from the facilitator's own address.
// If the simulation succeeds, settlement will succeed under the same state - the manager runs
// the identical validation and caveat pipeline in both.
func (m *ERC7710Facilitator) Verify(
	ctx context.Context, payment *types.PaymentPayload, req *types.PaymentRequirements,
) (*types.PaymentVerifyResponse, error) {
	calldata, payer, resp := m.prepare(payment, req)
	if resp != nil {
		return resp, nil
	}

	if _, err := m.client.CallContract(ctx, ethereum.CallMsg{
		From: m.address,
		To:   &m.manager,
		Data: calldata,
	}, nil); err != nil {
		reason, message := erc7710.DecodeRevert(revertData(err))
		return &types.PaymentVerifyResponse{
			IsValid:        false,
			InvalidReason:  reason,
			InvalidMessage: message,
			Payer:          payer,
		}, nil
	}

	return &types.PaymentVerifyResponse{IsValid: true, Payer: payer}, nil
}

// Settle broadcasts the redemption. The facilitator pays gas; funds move directly from the root
// delegator's account to the payee. The payer needs no ETH at all.
func (m *ERC7710Facilitator) Settle(
	ctx context.Context, payment *types.PaymentPayload, req *types.PaymentRequirements,
) (*types.PaymentSettleResponse, error) {
	calldata, payer, vresp := m.prepare(payment, req)
	if vresp != nil {
		return &types.PaymentSettleResponse{
			Success:      false,
			ErrorReason:  vresp.InvalidReason,
			ErrorMessage: vresp.InvalidMessage,
			Network:      types.Network(m.network),
		}, nil
	}

	// Simulate first: a revert here costs nothing, a reverted broadcast costs the fee payer gas.
	if _, err := m.client.CallContract(ctx, ethereum.CallMsg{
		From: m.address, To: &m.manager, Data: calldata,
	}, nil); err != nil {
		reason, message := erc7710.DecodeRevert(revertData(err))
		return &types.PaymentSettleResponse{
			Success: false, ErrorReason: reason, ErrorMessage: message,
			Payer: payer, Network: types.Network(m.network),
		}, nil
	}

	nonce, err := m.client.PendingNonceAt(ctx, m.address)
	if err != nil {
		return nil, fmt.Errorf("fetch nonce: %w", err)
	}
	gasPrice, err := m.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("suggest gas price: %w", err)
	}

	tx := gethtypes.NewTx(&gethtypes.DynamicFeeTx{
		ChainID:   m.networkID,
		Nonce:     nonce,
		GasTipCap: gasPrice,
		GasFeeCap: new(big.Int).Mul(gasPrice, big.NewInt(2)),
		Gas:       m.settleGasLimit,
		To:        &m.manager,
		Data:      calldata,
	})
	signed, err := gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(m.networkID), m.privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign settlement: %w", err)
	}
	if err := m.client.SendTransaction(ctx, signed); err != nil {
		return nil, fmt.Errorf("broadcast settlement: %w", err)
	}

	log.Info().
		Str("tx", signed.Hash().Hex()).
		Str("payer", payer).
		Str("payTo", req.PayTo).
		Str("amount", req.Amount).
		Msg("erc7710 settlement broadcast")

	return &types.PaymentSettleResponse{
		Success:     true,
		Payer:       payer,
		Transaction: signed.Hash().Hex(),
		Network:     types.Network(m.network),
	}, nil
}

// Supported advertises the erc7710 method, the pinned manager, and - crucially - the settlement
// address. Clients append a leaf delegation to that address so the facilitator can redeem;
// this is what the spec's `signers` field exists for.
func (m *ERC7710Facilitator) Supported() *types.SupportedResponse {
	return &types.SupportedResponse{
		Kinds: []types.SupportedKind{{
			X402Version: int(types.X402VersionV2),
			Scheme:      string(types.Exact),
			Network:     m.network,
			Extra: map[string]interface{}{
				"assetTransferMethods": []string{erc7710.AssetTransferMethod},
				"delegationManager":    m.manager.Hex(),
			},
		}},
		Extensions: []string{},
		Signers:    map[string][]string{"eip155:*": {m.address.Hex()}},
	}
}

// prepare validates the request shape and builds the redemption calldata. A non-nil response
// means validation failed before any chain interaction.
func (m *ERC7710Facilitator) prepare(
	payment *types.PaymentPayload, req *types.PaymentRequirements,
) (calldata []byte, payer string, errResp *types.PaymentVerifyResponse) {
	fail := func(reason, message string) ([]byte, string, *types.PaymentVerifyResponse) {
		return nil, payer, &types.PaymentVerifyResponse{IsValid: false, InvalidReason: reason, InvalidMessage: message}
	}

	if req.Scheme != string(types.Exact) {
		return fail(types.ErrIncompatibleScheme.Error(), fmt.Sprintf("scheme %q not supported", req.Scheme))
	}
	if req.Network != m.network {
		return fail(types.ErrNetworkMismatch.Error(), fmt.Sprintf("network %q, facilitator serves %s", req.Network, m.network))
	}
	if !erc7710.IsERC7710Requirements(req) {
		return fail(types.ErrInvalidPayloadFormat.Error(), `extra.assetTransferMethod must be "erc7710"`)
	}
	if !common.IsHexAddress(req.Asset) || !common.IsHexAddress(req.PayTo) {
		return fail(types.ErrInvalidPayloadFormat.Error(), "asset and payTo must be addresses")
	}

	amount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok || amount.Sign() <= 0 {
		return fail(types.ErrInvalidPayloadFormat.Error(), fmt.Sprintf("invalid amount %q", req.Amount))
	}

	p, err := erc7710.ParsePayload(payment.Payload)
	if err != nil {
		return fail(types.ErrInvalidPayloadFormat.Error(), err.Error())
	}
	if common.HexToAddress(p.DelegationManager) != m.manager {
		return fail("delegation_manager_mismatch",
			fmt.Sprintf("payload manager %s, facilitator pins %s", p.DelegationManager, m.manager.Hex()))
	}

	contextBytes, err := hexutil.Decode(p.PermissionContext)
	if err != nil {
		return fail(types.ErrInvalidPayloadFormat.Error(), "permissionContext is not valid hex")
	}
	if root, err := erc7710.RootDelegator(contextBytes); err == nil {
		payer = root.Hex()
	}

	data, err := erc7710.EncodeRedeemCalldata(
		contextBytes, common.HexToAddress(req.Asset), common.HexToAddress(req.PayTo), amount,
	)
	if err != nil {
		return fail(types.ErrInvalidPayloadFormat.Error(), err.Error())
	}
	return data, payer, nil
}

// revertData digs the raw revert bytes out of a go-ethereum RPC error by unwrapping until a
// carrier of ErrorData() is found (go-ethereum's rpc.DataError).
func revertData(err error) []byte {
	for err != nil {
		if de, ok := err.(interface{ ErrorData() interface{} }); ok {
			if s, ok := de.ErrorData().(string); ok {
				if b, decodeErr := hexutil.Decode(s); decodeErr == nil {
					return b
				}
			}
			return nil
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = u.Unwrap()
	}
	return nil
}
