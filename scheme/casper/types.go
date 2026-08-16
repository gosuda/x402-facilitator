package casper

import "github.com/gosuda/x402-facilitator/types"

// VerifyRequest is the body of POST /verify on the Casper facilitator. It
// mirrors the x402 v2 wire format used by this repository.
type VerifyRequest struct {
	X402Version         int                       `json:"x402Version"`
	PaymentPayload      types.PaymentPayload      `json:"paymentPayload"`
	PaymentRequirements types.PaymentRequirements `json:"paymentRequirements"`
}

// VerifyResponse is the body returned by POST /verify.
type VerifyResponse struct {
	IsValid        bool   `json:"isValid"`
	InvalidReason  string `json:"invalidReason,omitempty"`
	InvalidMessage string `json:"invalidMessage,omitempty"`
	Payer          string `json:"payer,omitempty"`
}

// SettleRequest is the body of POST /settle on the Casper facilitator.
type SettleRequest struct {
	X402Version         int                       `json:"x402Version"`
	PaymentPayload      types.PaymentPayload      `json:"paymentPayload"`
	PaymentRequirements types.PaymentRequirements `json:"paymentRequirements"`
}

// SettleResponse is the body returned by POST /settle.
type SettleResponse struct {
	Success      bool   `json:"success"`
	ErrorReason  string `json:"errorReason,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	Payer        string `json:"payer,omitempty"`
	Transaction  string `json:"transaction"`
	Network      string `json:"network"`
}

// SupportedKind is one entry of the GET /supported response.
type SupportedKind struct {
	X402Version int                    `json:"x402Version"`
	Scheme      string                 `json:"scheme"`
	Network     string                 `json:"network"`
	Extra       map[string]interface{} `json:"extra,omitempty"`
}

// SupportedResponse is the body returned by GET /supported.
type SupportedResponse struct {
	Kinds []SupportedKind `json:"kinds"`
}

// ErrorResponse is the error body returned by the Casper facilitator for
// non-2xx responses.
type ErrorResponse struct {
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}
