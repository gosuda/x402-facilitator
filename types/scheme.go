package types

// Scheme represents the blockchain scheme type
type Scheme string

const (
	EVM    Scheme = "evm"
	Solana Scheme = "solana"
	Sui    Scheme = "sui"
	Tron   Scheme = "tron"
)

// Signer is a function type for signing digests
type Signer func(digest []byte) (signature []byte, err error)
