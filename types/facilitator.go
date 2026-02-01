package types

import (
	"encoding/json"

	x402types "github.com/coinbase/x402/go/types"
)

// =============================================================================
// PaymentPayload - Embeds SDK type with server-side extensions
// =============================================================================

// PaymentPayload represents the V2 payment payload.
// It embeds the SDK's PaymentPayload and adds server-specific fields.
type PaymentPayload struct {
	x402types.PaymentPayload `json:",inline"`
}

// =============================================================================
// PaymentRequirements - Embeds SDK type with server-side extensions
// =============================================================================

// PaymentRequirements embeds the SDK's PaymentRequirements and adds
// server-side HTTP API fields (Resource, Description, etc.)
type PaymentRequirements struct {
	x402types.PaymentRequirements `json:",inline"`

	// Resource is the URL of the resource to pay for (server-side field)
	Resource string `json:"resource,omitempty"`

	// Description is a human-readable description of the resource (server-side field)
	Description string `json:"description,omitempty"`

	// MimeType is the MIME type of the resource response (server-side field)
	MimeType string `json:"mimeType,omitempty"`

	// OutputSchema is the JSON schema of the resource response (optional, server-side field)
	OutputSchema *json.RawMessage `json:"outputSchema,omitempty"`
}

// ToSDKRequirements converts to SDK PaymentRequirements (strips server-side fields)
func (pr PaymentRequirements) ToSDKRequirements() x402types.PaymentRequirements {
	return pr.PaymentRequirements
}

// =============================================================================
// Request/Response Types
// =============================================================================

// PaymentVerifyRequest is the request body sent to facilitator's /verify endpoint.
type PaymentVerifyRequest struct {
	X402Version         int                 `json:"x402Version"`
	PaymentHeader       PaymentPayload      `json:"paymentHeader"`
	PaymentRequirements PaymentRequirements `json:"paymentRequirements"`
}

// PaymentVerifyResponse is the response returned from the /verify endpoint.
type PaymentVerifyResponse struct {
	// IsValid indicates whether the payment payload is valid
	IsValid bool `json:"isValid"`
	// InvalidReason provides the error message if invalid
	InvalidReason string `json:"invalidReason,omitempty"`
	// Payer is the address of the payer (if verification succeeded)
	Payer string `json:"payer,omitempty"`
}

// PaymentSettleRequest is the request body sent to facilitator's /settle endpoint.
type PaymentSettleRequest struct {
	X402Version         int                 `json:"x402Version"`
	PaymentHeader       PaymentPayload      `json:"paymentHeader"`
	PaymentRequirements PaymentRequirements `json:"paymentRequirements"`
}

// PaymentSettleResponse is the response from the /settle endpoint.
type PaymentSettleResponse struct {
	// Success indicates whether the settlement was successful
	Success bool `json:"success"`
	// Error provides the error message if settlement failed
	Error string `json:"error,omitempty"`
	// TxHash is the transaction hash of the settled payment
	TxHash string `json:"txHash,omitempty"`
	// NetworkId is the network ID where the transaction was submitted
	NetworkId string `json:"networkId,omitempty"`
}

// =============================================================================
// Supporting Types
// =============================================================================

// SupportedKind represents a supported scheme and network pair.
type SupportedKind struct {
	Scheme  string `json:"scheme"`
	Network string `json:"network"`
}

// SupportedResponse is the response from the /supported endpoint.
type SupportedResponse struct {
	Kinds []SupportedKind `json:"kinds"`
}

// X402Version represents the protocol version
type X402Version int

const (
	// X402VersionV2 is the current V2 protocol version
	X402VersionV2 X402Version = 2
)

// IsV2 returns true if the payload is V2 format
func (p PaymentPayload) IsV2() bool {
	return p.X402Version == int(X402VersionV2)
}
