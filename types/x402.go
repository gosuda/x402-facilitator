package types

// x402 SDK re-exports
// This file provides a single import point for all x402 SDK types and functions.

import (
	x402 "github.com/coinbase/x402/go"
	"github.com/coinbase/x402/go/mechanisms/evm"
	"github.com/coinbase/x402/go/mechanisms/evm/exact/facilitator"
	x402types "github.com/coinbase/x402/go/types"
)

// =============================================================================
// Core Types (V2)
// =============================================================================

type (
	// SDK types (use directly when needed)
	SDKPaymentPayload      = x402types.PaymentPayload
	SDKPaymentRequirements = x402types.PaymentRequirements
	SDKSupportedKind       = x402types.SupportedKind

	// Network is the blockchain network identifier (CAIP-2 format)
	Network = x402.Network

	// VerifyResponse is the response from payment verification
	VerifyResponse = x402.VerifyResponse

	// SettleResponse is the response from payment settlement
	SettleResponse = x402.SettleResponse
)

// =============================================================================
// EVM Types
// =============================================================================

type (
	// ExactEIP3009Authorization represents EIP-3009 TransferWithAuthorization data
	ExactEIP3009Authorization = evm.ExactEIP3009Authorization

	// ExactEIP3009Payload represents the exact payment payload for EVM networks
	ExactEIP3009Payload = evm.ExactEIP3009Payload

	// ExactPermit2Payload represents the Permit2 payment payload
	ExactPermit2Payload = evm.ExactPermit2Payload

	// FacilitatorEvmSigner is the interface for facilitator EVM operations
	FacilitatorEvmSigner = evm.FacilitatorEvmSigner

	// ClientEvmSigner is the interface for client-side EVM signing
	ClientEvmSigner = evm.ClientEvmSigner

	// TypedDataDomain represents the EIP-712 domain separator
	TypedDataDomain = evm.TypedDataDomain

	// TypedDataField represents a field in EIP-712 typed data
	TypedDataField = evm.TypedDataField

	// TransactionReceipt represents the receipt of a mined transaction
	TransactionReceipt = evm.TransactionReceipt

	// ExactEvmSchemeConfig holds configuration for the ExactEvmScheme facilitator
	ExactEvmSchemeConfig = facilitator.ExactEvmSchemeConfig
)

// =============================================================================
// Facilitator Types
// =============================================================================

type (
	// X402Facilitator is the main facilitator type
	X402Facilitator = x402.X402Facilitator

	// SchemeNetworkFacilitator is the interface for V2 facilitator mechanisms
	SchemeNetworkFacilitator = x402.SchemeNetworkFacilitator
)

// =============================================================================
// Functions - Facilitator
// =============================================================================

var (
	// NewFacilitator creates a new x402 facilitator instance
	NewFacilitator = x402.Newx402Facilitator

	// NewExactEvmScheme creates a new EVM exact payment scheme facilitator
	NewExactEvmScheme = facilitator.NewExactEvmScheme
)

// =============================================================================
// Functions - Signature Verification
// =============================================================================

var (
	// VerifyUniversalSignature verifies signatures supporting EOA, EIP-1271, and ERC-6492
	VerifyUniversalSignature = evm.VerifyUniversalSignature

	// VerifyEOASignature verifies ECDSA signatures from EOA wallets
	VerifyEOASignature = evm.VerifyEOASignature

	// VerifyEIP1271Signature verifies signatures from smart contract wallets
	VerifyEIP1271Signature = evm.VerifyEIP1271Signature
)

// =============================================================================
// Functions - EIP-712 Hashing
// =============================================================================

var (
	// HashEIP3009Authorization hashes EIP-3009 TransferWithAuthorization data
	HashEIP3009Authorization = evm.HashEIP3009Authorization

	// HashPermit2Authorization hashes Permit2 authorization data
	HashPermit2Authorization = evm.HashPermit2Authorization
)

// =============================================================================
// Functions - ERC-6492
// =============================================================================

var (
	// ParseERC6492Signature parses an ERC-6492 wrapped signature
	ParseERC6492Signature = evm.ParseERC6492Signature

	// IsERC6492Signature checks if a signature is ERC-6492 wrapped
	IsERC6492Signature = evm.IsERC6492Signature
)

// =============================================================================
// Functions - Payload Detection
// =============================================================================

var (
	// IsPermit2Payload checks if a payload is a Permit2 payload
	IsPermit2Payload = evm.IsPermit2Payload

	// IsEIP3009Payload checks if a payload is an EIP-3009 payload
	IsEIP3009Payload = evm.IsEIP3009Payload

	// PayloadFromMap creates an EIP-3009 payload from a map
	PayloadFromMap = evm.PayloadFromMap

	// Permit2PayloadFromMap creates a Permit2 payload from a map
	Permit2PayloadFromMap = evm.Permit2PayloadFromMap
)

// =============================================================================
// Functions - Network Configuration
// =============================================================================

var (
	// GetNetworkConfig returns the network configuration for a given network
	GetNetworkConfig = evm.GetNetworkConfig

	// GetAssetInfo returns asset information for a given network and asset address
	GetAssetInfo = evm.GetAssetInfo

	// GetEvmChainId extracts the EVM chain ID from a CAIP-2 network identifier
	GetEvmChainId = evm.GetEvmChainId
)

// =============================================================================
// Functions - Utilities
// =============================================================================

var (
	// HexToBytes converts a hex string to bytes
	HexToBytes = evm.HexToBytes

	// CreateNonce generates a random 32-byte nonce for EIP-3009 authorizations
	CreateNonce = evm.CreateNonce

	// ParseAmount converts a decimal amount to the smallest unit
	ParseAmount = evm.ParseAmount

	// FormatAmount converts an amount in smallest unit to decimal
	FormatAmount = evm.FormatAmount
)

// =============================================================================
// Constants
// =============================================================================

const (
	// PERMIT2Address is the canonical Permit2 contract address
	PERMIT2Address = evm.PERMIT2Address

	// SchemeExact is the scheme identifier for exact payments
	SchemeExact = evm.SchemeExact

	// TxStatusSuccess is the status code for successful transactions
	TxStatusSuccess = evm.TxStatusSuccess
)

// =============================================================================
// ABI Constants
// =============================================================================

var (
	// TransferWithAuthorizationVRSABI is the ABI for TransferWithAuthorization (v,r,s version)
	TransferWithAuthorizationVRSABI = evm.TransferWithAuthorizationVRSABI

	// TransferWithAuthorizationBytesABI is the ABI for TransferWithAuthorization (bytes version)
	TransferWithAuthorizationBytesABI = evm.TransferWithAuthorizationBytesABI

	// ERC20BalanceOfABI is the ABI for ERC20 balanceOf
	ERC20BalanceOfABI = evm.ERC20BalanceOfABI

	// AuthorizationStateABI is the ABI for checking authorization state
	AuthorizationStateABI = evm.AuthorizationStateABI
)
