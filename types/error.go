package types

import "errors"

var (
	ErrInvalidPayloadFormat     = errors.New("invalid_payload_format")
	ErrIncompatibleScheme       = errors.New("incompatible_payload_scheme")
	ErrNetworkMismatch          = errors.New("network_mismatch")
	ErrInvalidNetwork           = errors.New("invalid_network")
	ErrNetworkIDMismatch        = errors.New("network_id_mismatch")
	ErrInvalidSignature         = errors.New("invalid_signature")
	ErrInvalidSignatureFormat   = errors.New("invalid_signature_format")
	ErrInvalidToken             = errors.New("invalid_token")
	ErrTokenMismatch            = errors.New("token_mismatch")
	ErrAmountMismatch           = errors.New("amount_mismatch")
	ErrRecipientMismatch        = errors.New("recipient_mismatch")
	ErrAuthorizationExpired     = errors.New("authorization_expired")
	ErrAuthorizationNotYetValid = errors.New("authorization_not_yet_valid")
	ErrAuthorizationAlreadyUsed = errors.New("authorization_already_used")
	ErrInvalidTransaction       = errors.New("invalid_transaction")
	ErrInsufficientBalance      = errors.New("insufficient_balance")
	ErrContractBindFailed       = errors.New("contract_bind_failed")
	ErrTransactionFailed        = errors.New("transaction_failed")

	// Permit2 errors
	ErrPermit2InvalidSpender    = errors.New("permit2_invalid_spender")
	ErrPermit2RecipientMismatch = errors.New("permit2_recipient_mismatch")
	ErrPermit2DeadlineExpired   = errors.New("permit2_deadline_expired")
	ErrPermit2NotYetValid       = errors.New("permit2_not_yet_valid")
	ErrPermit2AmountMismatch    = errors.New("permit2_amount_mismatch")
	ErrPermit2TokenMismatch     = errors.New("permit2_token_mismatch")
	ErrPermit2InvalidSignature  = errors.New("permit2_invalid_signature")
)
