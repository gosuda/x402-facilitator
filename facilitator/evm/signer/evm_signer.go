// Package signer provides EVM signer implementations for the x402 facilitator.
// It implements the FacilitatorEvmSigner interface from the x402 SDK using go-ethereum.
package signer

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/gosuda/x402-facilitator/types"
)

// EVMSigner implements the types.FacilitatorEvmSigner interface.
// It provides EVM blockchain interaction capabilities including:
// - Reading from smart contracts
// - Writing to smart contracts
// - Sending raw transactions
// - Verifying EIP-712 typed data signatures
// - Waiting for transaction receipts
type EVMSigner struct {
	client    *ethclient.Client
	addresses []common.Address
	chainID   *big.Int
	signer    types.Signer
}

// EVMSignerConfig holds configuration for creating a new EVMSigner.
type EVMSignerConfig struct {
	// RpcURL is the Ethereum RPC endpoint URL (e.g., https://sepolia.base.org)
	RpcURL string
	// ChainID is the blockchain network ID (e.g., 84532 for Base Sepolia)
	ChainID int64
	// PrivateKey is an optional hex-encoded private key for signing.
	// If not provided, SignerCallback must be set.
	PrivateKey string
	// Signer is an optional callback function for signing.
	// If provided, it takes precedence over PrivateKey.
	Signer types.Signer
	// Addresses is a list of addresses this signer can use.
	// If not provided, addresses will be derived from the private key or callback.
	Addresses []string
}

// NewEVMSigner creates a new EVMSigner with the given configuration.
// It connects to the RPC endpoint and validates the configuration.
func NewEVMSigner(config *EVMSignerConfig) (*EVMSigner, error) {
	if config.RpcURL == "" {
		return nil, fmt.Errorf("rpc URL is required")
	}

	// Connect to RPC
	client, err := ethclient.Dial(config.RpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	// Verify connection by getting chain ID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	chainID, err := client.NetworkID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get network ID: %w", err)
	}

	// Validate chain ID matches config
	if config.ChainID != 0 && chainID.Int64() != config.ChainID {
		return nil, fmt.Errorf("chain ID mismatch: expected %d, got %d", config.ChainID, chainID.Int64())
	}

	signer := &EVMSigner{
		client:  client,
		chainID: chainID,
	}

	// Set up addresses
	if len(config.Addresses) > 0 {
		for _, addr := range config.Addresses {
			signer.addresses = append(signer.addresses, common.HexToAddress(addr))
		}
	}

	// Set up signing mechanism
	if config.Signer != nil {
		signer.signer = config.Signer
	} else if config.PrivateKey != "" {
		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(config.PrivateKey, "0x"))
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		signer.signer = createPrivateKeySigner(privateKey)

		// Derive address from private key if not provided
		if len(signer.addresses) == 0 {
			publicKey := privateKey.Public()
			publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
			if !ok {
				return nil, fmt.Errorf("failed to cast public key to ECDSA")
			}
			address := crypto.PubkeyToAddress(*publicKeyECDSA)
			signer.addresses = append(signer.addresses, address)
		}
	} else {
		return nil, fmt.Errorf("either PrivateKey or SignerCallback must be provided")
	}

	return signer, nil
}

// createPrivateKeySigner creates a SignerCallback from a private key.
func createPrivateKeySigner(privateKey *ecdsa.PrivateKey) types.Signer {
	return func(digest []byte) ([]byte, error) {
		sig, err := crypto.Sign(digest, privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to sign digest: %w", err)
		}
		return sig, nil
	}
}

// GetAddresses returns all addresses this facilitator can use for signing.
func (s *EVMSigner) GetAddresses() []string {
	addresses := make([]string, len(s.addresses))
	for i, addr := range s.addresses {
		addresses[i] = addr.Hex()
	}
	return addresses
}

// ReadContract reads data from a smart contract using eth_call.
// Parameters:
//   - ctx: Context for the call
//   - address: Contract address
//   - abiJSON: Contract ABI as JSON string
//   - functionName: Name of the function to call
//   - args: Function arguments
func (s *EVMSigner) ReadContract(ctx context.Context, address string, abiJSON []byte, functionName string, args ...interface{}) (interface{}, error) {
	contractAddr := common.HexToAddress(address)

	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(string(abiJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Pack the function call data
	data, err := parsedABI.Pack(functionName, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack function call: %w", err)
	}

	// Create the call message
	msg := ethereum.CallMsg{
		To:   &contractAddr,
		Data: data,
	}

	// Execute the call
	result, err := s.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call contract: %w", err)
	}

	// Unpack the result
	method, ok := parsedABI.Methods[functionName]
	if !ok {
		return nil, fmt.Errorf("function %s not found in ABI", functionName)
	}

	unpacked, err := method.Outputs.Unpack(result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack result: %w", err)
	}

	return unpacked, nil
}

// WriteContract executes a smart contract transaction.
// Parameters:
//   - ctx: Context for the transaction
//   - address: Contract address
//   - abiJSON: Contract ABI as JSON string
//   - functionName: Name of the function to call
//   - args: Function arguments
//
// Returns the transaction hash.
func (s *EVMSigner) WriteContract(ctx context.Context, address string, abiJSON []byte, functionName string, args ...interface{}) (string, error) {
	if len(s.addresses) == 0 {
		return "", fmt.Errorf("no signer addresses available")
	}

	contractAddr := common.HexToAddress(address)
	fromAddr := s.addresses[0] // Use first address

	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(string(abiJSON)))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Pack the function call data
	data, err := parsedABI.Pack(functionName, args...)
	if err != nil {
		return "", fmt.Errorf("failed to pack function call: %w", err)
	}

	// Get nonce
	nonce, err := s.client.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := s.client.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to suggest gas price: %w", err)
	}

	// Estimate gas
	gasLimit, err := s.client.EstimateGas(ctx, ethereum.CallMsg{
		From: fromAddr,
		To:   &contractAddr,
		Data: data,
	})
	if err != nil {
		return "", fmt.Errorf("failed to estimate gas: %w", err)
	}

	// Create transaction
	tx := ethTypes.NewTransaction(nonce, contractAddr, big.NewInt(0), gasLimit, gasPrice, data)

	// Sign transaction
	signer := ethTypes.LatestSignerForChainID(s.chainID)
	digest := signer.Hash(tx).Bytes()

	sig, err := s.signer(digest)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	signedTx, err := tx.WithSignature(signer, sig)
	if err != nil {
		return "", fmt.Errorf("failed to apply signature: %w", err)
	}

	// Send transaction
	if err := s.client.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx.Hash().Hex(), nil
}

// VerifyTypedData verifies an EIP-712 typed data signature.
// Parameters:
//   - ctx: Context for the verification
//   - address: Expected signer address
//   - domain: EIP-712 domain separator
//   - types: Type definitions for the typed data
//   - primaryType: Primary type name
//   - message: The message data as a map
//   - signature: The signature to verify
func (s *EVMSigner) VerifyTypedData(ctx context.Context, address string, domain types.TypedDataDomain, types map[string][]types.TypedDataField, primaryType string, message map[string]interface{}, signature []byte) (bool, error) {
	// Convert SDK types to go-ethereum types
	typedData := TypedData{
		Types: Types{
			"EIP712Domain": []Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
		},
		PrimaryType: primaryType,
		Domain: TypedDataDomain{
			Name:              domain.Name,
			Version:           domain.Version,
			ChainId:           domain.ChainID,
			VerifyingContract: domain.VerifyingContract,
		},
		Message: message,
	}

	// Add custom types
	for typeName, fields := range types {
		if typeName == "EIP712Domain" {
			continue // Skip domain type
		}
		typedData.Types[typeName] = make([]Type, len(fields))
		for i, field := range fields {
			typedData.Types[typeName][i] = Type{
				Name: field.Name,
				Type: field.Type,
			}
		}
	}

	// Hash the typed data
	digest, _, err := HashTypedData(typedData)
	if err != nil {
		return false, fmt.Errorf("failed to hash typed data: %w", err)
	}

	// Recover public key from signature
	sig := make([]byte, len(signature))
	copy(sig, signature)

	// Adjust V value if needed
	if len(sig) == 65 && sig[64] >= 27 {
		sig[64] -= 27
	}

	pubKey, err := crypto.SigToPub(digest, sig)
	if err != nil {
		return false, fmt.Errorf("failed to recover public key: %w", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	expectedAddr := common.HexToAddress(address)

	return recoveredAddr == expectedAddr, nil
}

// SendTransaction sends a raw transaction with arbitrary calldata.
// Used for smart wallet deployment where calldata is pre-encoded.
// Parameters:
//   - ctx: Context for the transaction
//   - to: Recipient address
//   - data: Transaction calldata
//
// Returns the transaction hash.
func (s *EVMSigner) SendTransaction(ctx context.Context, to string, data []byte) (string, error) {
	if len(s.addresses) == 0 {
		return "", fmt.Errorf("no signer addresses available")
	}

	toAddr := common.HexToAddress(to)
	fromAddr := s.addresses[0] // Use first address

	// Get nonce
	nonce, err := s.client.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := s.client.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to suggest gas price: %w", err)
	}

	// Estimate gas
	gasLimit, err := s.client.EstimateGas(ctx, ethereum.CallMsg{
		From: fromAddr,
		To:   &toAddr,
		Data: data,
	})
	if err != nil {
		return "", fmt.Errorf("failed to estimate gas: %w", err)
	}

	// Create transaction
	tx := ethTypes.NewTransaction(nonce, toAddr, big.NewInt(0), gasLimit, gasPrice, data)

	// Sign transaction
	signer := ethTypes.LatestSignerForChainID(s.chainID)
	digest := signer.Hash(tx).Bytes()

	sig, err := s.signer(digest)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	signedTx, err := tx.WithSignature(signer, sig)
	if err != nil {
		return "", fmt.Errorf("failed to apply signature: %w", err)
	}

	// Send transaction
	if err := s.client.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx.Hash().Hex(), nil
}

// WaitForTransactionReceipt waits for a transaction to be mined and returns the receipt.
// Parameters:
//   - ctx: Context with timeout
//   - txHash: Transaction hash to wait for
func (s *EVMSigner) WaitForTransactionReceipt(ctx context.Context, txHash string) (*types.TransactionReceipt, error) {
	hash := common.HexToHash(txHash)

	receipt, err := bind.WaitMined(ctx, s.client, &ethTypes.Transaction{})
	if err != nil {
		return nil, fmt.Errorf("failed to wait for transaction: %w", err)
	}

	// Get the actual receipt
	receipt, err = s.client.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	return &types.TransactionReceipt{
		Status:      receipt.Status,
		BlockNumber: receipt.BlockNumber.Uint64(),
		TxHash:      receipt.TxHash.Hex(),
	}, nil
}

// GetBalance gets the balance of an address for a specific token.
// For native token (ETH), use tokenAddress "0x0000000000000000000000000000000000000000" or empty string.
func (s *EVMSigner) GetBalance(ctx context.Context, address string, tokenAddress string) (*big.Int, error) {
	addr := common.HexToAddress(address)

	// Native token balance
	if tokenAddress == "" || tokenAddress == "0x0000000000000000000000000000000000000000" {
		balance, err := s.client.BalanceAt(ctx, addr, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get balance: %w", err)
		}
		return balance, nil
	}

	// ERC20 token balance
	tokenAddr := common.HexToAddress(tokenAddress)

	// ERC20 balanceOf function signature: 0x70a08231
	data := append(
		common.Hex2Bytes("70a08231"),
		common.LeftPadBytes(addr.Bytes(), 32)...,
	)

	result, err := s.client.CallContract(ctx, ethereum.CallMsg{
		To:   &tokenAddr,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call balanceOf: %w", err)
	}

	if len(result) < 32 {
		return nil, fmt.Errorf("invalid balanceOf response")
	}

	balance := new(big.Int).SetBytes(result)
	return balance, nil
}

// GetChainID returns the chain ID of the connected network.
func (s *EVMSigner) GetChainID(ctx context.Context) (*big.Int, error) {
	return s.chainID, nil
}

// GetCode returns the bytecode at the given address.
// Returns empty slice if address is an EOA or doesn't exist.
func (s *EVMSigner) GetCode(ctx context.Context, address string) ([]byte, error) {
	addr := common.HexToAddress(address)
	code, err := s.client.CodeAt(ctx, addr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get code: %w", err)
	}
	return code, nil
}

// Close closes the connection to the RPC client.
func (s *EVMSigner) Close() {
	s.client.Close()
}

// EIP-712 Types for typed data hashing

type Type struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Types map[string][]Type

type TypedDataDomain struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	ChainId           *big.Int `json:"chainId"`
	VerifyingContract string   `json:"verifyingContract"`
}

type TypedData struct {
	Types       Types                  `json:"types"`
	PrimaryType string                 `json:"primaryType"`
	Domain      TypedDataDomain        `json:"domain"`
	Message     map[string]interface{} `json:"message"`
}

// HashTypedData hashes the typed data according to EIP-712.
func HashTypedData(typedData TypedData) ([]byte, string, error) {
	domainSeparator, err := hashStruct(typedData.Types, "EIP712Domain", typedData.Domain)
	if err != nil {
		return nil, "", err
	}

	messageHash, err := hashStruct(typedData.Types, typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, "", err
	}

	// Final EIP-712 hash: keccak256("\x19\x01" || domainSeparator || messageHash)
	prefix := []byte{0x19, 0x01}
	digest := crypto.Keccak256(prefix, domainSeparator, messageHash)

	return digest, typedData.PrimaryType, nil
}

func hashStruct(types Types, primaryType string, data interface{}) ([]byte, error) {
	// Encode type data
	encodeData, err := encodeData(types, primaryType, data)
	if err != nil {
		return nil, err
	}

	// Hash the encoded data
	return crypto.Keccak256(encodeData), nil
}

func encodeData(types Types, primaryType string, data interface{}) ([]byte, error) {
	// This is a simplified implementation
	// For full EIP-712 compliance, we need to handle all Solidity types

	// Get type hash
	typeHash := typeHash(types, primaryType)

	// Encode fields
	var encoded []byte
	encoded = append(encoded, typeHash...)

	// Handle different data types
	switch v := data.(type) {
	case TypedDataDomain:
		// Encode domain fields
		encoded = append(encoded, crypto.Keccak256([]byte(v.Name))...)
		encoded = append(encoded, crypto.Keccak256([]byte(v.Version))...)
		encoded = append(encoded, common.LeftPadBytes(v.ChainId.Bytes(), 32)...)
		encoded = append(encoded, common.HexToAddress(v.VerifyingContract).Bytes()...)
	case map[string]interface{}:
		// Encode message fields
		for _, field := range types[primaryType] {
			value := v[field.Name]
			fieldEncoded, err := encodeValue(field.Type, value)
			if err != nil {
				return nil, err
			}
			encoded = append(encoded, fieldEncoded...)
		}
	}

	return encoded, nil
}

func encodeValue(fieldType string, value interface{}) ([]byte, error) {
	switch fieldType {
	case "address":
		addr, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string for address, got %T", value)
		}
		return common.LeftPadBytes(common.HexToAddress(addr).Bytes(), 32), nil
	case "string":
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", value)
		}
		return crypto.Keccak256([]byte(str)), nil
	case "bytes":
		switch v := value.(type) {
		case string:
			if strings.HasPrefix(v, "0x") {
				b := common.Hex2Bytes(v[2:])
				return crypto.Keccak256(b), nil
			}
			return crypto.Keccak256([]byte(v)), nil
		case []byte:
			return crypto.Keccak256(v), nil
		default:
			return nil, fmt.Errorf("expected string or []byte for bytes, got %T", value)
		}
	default:
		// Handle numeric types (uint256, int256, etc.)
		if strings.HasPrefix(fieldType, "uint") || strings.HasPrefix(fieldType, "int") {
			switch v := value.(type) {
			case string:
				n := new(big.Int)
				if _, ok := n.SetString(v, 10); !ok {
					return nil, fmt.Errorf("failed to parse %s as big.Int", v)
				}
				return common.LeftPadBytes(n.Bytes(), 32), nil
			case *big.Int:
				return common.LeftPadBytes(v.Bytes(), 32), nil
			case float64:
				n := big.NewInt(int64(v))
				return common.LeftPadBytes(n.Bytes(), 32), nil
			default:
				return nil, fmt.Errorf("expected numeric type, got %T", value)
			}
		}
		return nil, fmt.Errorf("unsupported type: %s", fieldType)
	}
}

func typeHash(types Types, primaryType string) []byte {
	// Create type string
	var typeStr strings.Builder
	typeStr.WriteString(primaryType)
	typeStr.WriteString("(")

	for i, field := range types[primaryType] {
		if i > 0 {
			typeStr.WriteString(",")
		}
		typeStr.WriteString(field.Type)
		typeStr.WriteString(" ")
		typeStr.WriteString(field.Name)
	}
	typeStr.WriteString(")")

	return crypto.Keccak256([]byte(typeStr.String()))
}

// Helper function for JSON marshaling
func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Helper function to parse hex string
func fromHex(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	b, _ := hex.DecodeString(s)
	return b
}

// Ensure EVMSigner implements the FacilitatorEvmSigner interface
var _ types.FacilitatorEvmSigner = (*EVMSigner)(nil)
