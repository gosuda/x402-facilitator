package nano

import (
	"errors"

	"github.com/gosuda/x402-facilitator/types"
)

var (
	// ErrInvalidNetwork is returned for a malformed or unsupported Nano CAIP-2
	// network identifier.
	ErrInvalidNetwork = errors.New("invalid_nano_network")
	// ErrEmptyPayload is returned when a nil or malformed payment payload is
	// supplied.
	ErrEmptyPayload = errors.New("empty_payment_payload")
	// ErrInvalidBlockHash is returned when the payment proof is not a 64-hex
	// Nano block hash.
	ErrInvalidBlockHash = errors.New("invalid_nano_block_hash")
	// ErrInsufficientEndpoints is returned when fewer than
	// MinIndependentEndpoints RPC endpoints are available to verify a block.
	ErrInsufficientEndpoints = errors.New("insufficient_nano_endpoints")
)

// VerifyRequest mirrors the x402 v2 wire format on the Nano boundary. It is
// the shape a caller sends to a Nano verify/settle helper and exists so the
// scheme has an explicit, named request type like the other chains.
type VerifyRequest struct {
	X402Version         int                       `json:"x402Version"`
	PaymentPayload      types.PaymentPayload      `json:"paymentPayload"`
	PaymentRequirements types.PaymentRequirements `json:"paymentRequirements"`
}

// SupportedKind is one entry of the GET /supported response for a Nano
// network.
type SupportedKind struct {
	X402Version int                    `json:"x402Version"`
	Scheme      string                 `json:"scheme"`
	Network     string                 `json:"network"`
	Extra       map[string]interface{} `json:"extra,omitempty"`
}

// BlockFields is the on-ledger data a verification reads back from a Nano send
// block. It is what actually binds a payment: the account that sent, the
// destination it paid, the exact raw amount, and whether the block is
// confirmed (cemented). None of it is taken from request bodies.
type BlockFields struct {
	// Account is the payer (block_account).
	Account string
	// Subtype is the block subtype (send / receive / change / open).
	Subtype string
	// Destination is the block's link/destination address.
	Destination string
	// AmountRaw is the block amount in raw atomic units.
	AmountRaw string
	// Confirmed reports whether the block is confirmed/cemented.
	Confirmed bool
}
