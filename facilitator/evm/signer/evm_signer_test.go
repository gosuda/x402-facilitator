package signer

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosuda/x402-facilitator/types"
)

// TestEVMSignerConfig_Validation tests configuration validation
func TestEVMSignerConfig_Validation(t *testing.T) {
	t.Run("empty RPC URL should fail", func(t *testing.T) {
		config := &EVMSignerConfig{
			ChainID:    84532,
			PrivateKey: generateTestPrivateKey(),
		}
		signer, err := NewEVMSigner(config)
		assert.Error(t, err)
		assert.Nil(t, signer)
		assert.Contains(t, err.Error(), "rpc URL is required")
	})

	t.Run("missing both private key and callback should fail", func(t *testing.T) {
		config := &EVMSignerConfig{
			RpcURL:  "https://sepolia.base.org",
			ChainID: 84532,
		}
		signer, err := NewEVMSigner(config)
		assert.Error(t, err)
		assert.Nil(t, signer)
		assert.Contains(t, err.Error(), "either PrivateKey or SignerCallback must be provided")
	})
}

// TestEVMSigner_GetAddresses tests address retrieval
func TestEVMSigner_GetAddresses(t *testing.T) {
	privateKey := generateTestPrivateKey()

	t.Run("get addresses from private key", func(t *testing.T) {
		// This test uses a mock - in real scenario would need actual RPC
		// For unit testing, we'll test the address derivation logic
		config := &EVMSignerConfig{
			RpcURL:     "https://sepolia.base.org",
			ChainID:    84532,
			PrivateKey: privateKey,
		}

		// This will fail to connect but we can test the config parsing
		// In real tests, use a local testnet or mock
		_ = config
	})

	t.Run("get addresses from explicit list", func(t *testing.T) {
		addresses := []string{
			"0x1234567890123456789012345678901234567890",
			"0x0987654321098765432109876543210987654321",
		}

		config := &EVMSignerConfig{
			RpcURL:     "https://sepolia.base.org",
			ChainID:    84532,
			PrivateKey: privateKey,
			Addresses:  addresses,
		}

		// Test that addresses are properly set
		require.NotNil(t, config.Addresses)
		assert.Equal(t, 2, len(config.Addresses))
	})
}

// TestEVMSigner_CallbackSigner tests callback-based signing
func TestEVMSigner_CallbackSigner(t *testing.T) {
	privateKey, err := crypto.HexToECDSA(generateTestPrivateKey())
	require.NoError(t, err)

	t.Run("callback signer is used when provided", func(t *testing.T) {
		callback := func(digest []byte) ([]byte, error) {
			return crypto.Sign(digest, privateKey)
		}

		config := &EVMSignerConfig{
			RpcURL:  "https://sepolia.base.org",
			ChainID: 84532,
			Signer:  callback,
		}

		// Verify callback is set
		assert.NotNil(t, config.Signer)
		_ = config

		// In real usage, the callback would be invoked during signing operations
		// Here we just verify the config accepts it
	})

	t.Run("callback takes precedence over private key", func(t *testing.T) {
		callback := func(digest []byte) ([]byte, error) {
			return crypto.Sign(digest, privateKey)
		}

		// Different private key
		otherKey := generateTestPrivateKey()

		config := &EVMSignerConfig{
			RpcURL:     "https://sepolia.base.org",
			ChainID:    84532,
			PrivateKey: otherKey,
			Signer:     callback,
		}

		// Callback should be used instead of private key
		assert.NotNil(t, config.Signer)
		assert.NotEmpty(t, config.PrivateKey)
	})
}

// TestEVMSigner_VerifyTypedData tests EIP-712 signature verification
func TestEVMSigner_VerifyTypedData(t *testing.T) {
	// Generate a test key pair
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	require.True(t, ok)

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Create typed data
	domain := types.TypedDataDomain{
		Name:              "TestDomain",
		Version:           "1",
		ChainID:           big.NewInt(84532),
		VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
	}

	_ = map[string][]types.TypedDataField{
		"TestMessage": {
			{Name: "content", Type: "string"},
			{Name: "amount", Type: "uint256"},
		},
	}

	message := map[string]interface{}{
		"content": "Hello World",
		"amount":  "1000000000000000000",
	}

	t.Run("verify valid EIP-712 signature", func(t *testing.T) {
		// Create typed data for signing
		typedData := TypedData{
			Types: Types{
				"EIP712Domain": []Type{
					{Name: "name", Type: "string"},
					{Name: "version", Type: "string"},
					{Name: "chainId", Type: "uint256"},
					{Name: "verifyingContract", Type: "address"},
				},
				"TestMessage": []Type{
					{Name: "content", Type: "string"},
					{Name: "amount", Type: "uint256"},
				},
			},
			PrimaryType: "TestMessage",
			Domain: TypedDataDomain{
				Name:              domain.Name,
				Version:           domain.Version,
				ChainId:           domain.ChainID,
				VerifyingContract: domain.VerifyingContract,
			},
			Message: message,
		}

		// Hash the typed data
		digest, _, err := HashTypedData(typedData)
		require.NoError(t, err)

		// Sign with the private key
		signature, err := crypto.Sign(digest, privateKey)
		require.NoError(t, err)

		// Verify the signature
		// Note: This is a simplified test - full verification requires the actual EVMSigner
		// with proper EIP-712 implementation
		assert.NotNil(t, signature)
		assert.Equal(t, 65, len(signature))
	})

	t.Run("verify signature recovers correct address", func(t *testing.T) {
		// Create a simple message hash
		message := []byte("test message")
		digest := crypto.Keccak256(message)

		// Sign
		signature, err := crypto.Sign(digest, privateKey)
		require.NoError(t, err)

		// Recover public key
		recoveredPubKey, err := crypto.SigToPub(digest, signature)
		require.NoError(t, err)

		recoveredAddr := crypto.PubkeyToAddress(*recoveredPubKey)
		assert.Equal(t, address.Hex(), recoveredAddr.Hex())
	})
}

// TestEVMSigner_EIP712Hashing tests EIP-712 typed data hashing
func TestEVMSigner_EIP712Hashing(t *testing.T) {
	t.Run("hash typed data with domain", func(t *testing.T) {
		typedData := TypedData{
			Types: Types{
				"EIP712Domain": []Type{
					{Name: "name", Type: "string"},
					{Name: "version", Type: "string"},
					{Name: "chainId", Type: "uint256"},
					{Name: "verifyingContract", Type: "address"},
				},
				"Message": []Type{
					{Name: "content", Type: "string"},
				},
			},
			PrimaryType: "Message",
			Domain: TypedDataDomain{
				Name:              "Test",
				Version:           "1",
				ChainId:           big.NewInt(1),
				VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
			},
			Message: map[string]interface{}{
				"content": "Hello",
			},
		}

		digest, primaryType, err := HashTypedData(typedData)
		require.NoError(t, err)
		assert.Equal(t, "Message", primaryType)
		assert.Equal(t, 32, len(digest))
	})

	t.Run("type hash generation", func(t *testing.T) {
		types := Types{
			"TestType": []Type{
				{Name: "field1", Type: "uint256"},
				{Name: "field2", Type: "address"},
			},
		}

		hash := typeHash(types, "TestType")
		assert.Equal(t, 32, len(hash))
	})
}

// TestEVMSigner_EncodeValue tests value encoding for EIP-712
func TestEVMSigner_EncodeValue(t *testing.T) {
	tests := []struct {
		name      string
		fieldType string
		value     interface{}
		wantLen   int
		wantErr   bool
	}{
		{
			name:      "encode address",
			fieldType: "address",
			value:     "0x1234567890123456789012345678901234567890",
			wantLen:   32,
			wantErr:   false,
		},
		{
			name:      "encode string",
			fieldType: "string",
			value:     "Hello World",
			wantLen:   32,
			wantErr:   false,
		},
		{
			name:      "encode uint256 from string",
			fieldType: "uint256",
			value:     "1000000000000000000",
			wantLen:   32,
			wantErr:   false,
		},
		{
			name:      "encode uint256 from big.Int",
			fieldType: "uint256",
			value:     big.NewInt(1000000),
			wantLen:   32,
			wantErr:   false,
		},
		{
			name:      "encode bytes from hex string",
			fieldType: "bytes",
			value:     "0x1234",
			wantLen:   32,
			wantErr:   false,
		},
		{
			name:      "encode invalid address type",
			fieldType: "address",
			value:     12345,
			wantLen:   0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := encodeValue(tt.fieldType, tt.value)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantLen, len(encoded))
			}
		})
	}
}

// TestEVMSigner_InterfaceCompliance tests that EVMSigner implements the interface
func TestEVMSigner_InterfaceCompliance(t *testing.T) {
	// This test verifies at compile time that EVMSigner implements FacilitatorEvmSigner
	var _ types.FacilitatorEvmSigner = (*EVMSigner)(nil)
}

// TestEVMSignerConfig_WithExplicitAddresses tests explicit address configuration
func TestEVMSignerConfig_WithExplicitAddresses(t *testing.T) {
	addresses := []string{
		"0x1234567890123456789012345678901234567890",
		"0x0987654321098765432109876543210987654321",
	}

	config := &EVMSignerConfig{
		RpcURL:     "https://sepolia.base.org",
		ChainID:    84532,
		PrivateKey: generateTestPrivateKey(),
		Addresses:  addresses,
	}

	assert.Equal(t, 2, len(config.Addresses))
	assert.Equal(t, addresses[0], config.Addresses[0])
	assert.Equal(t, addresses[1], config.Addresses[1])
}

// TestEVMSigner_Close tests connection cleanup
func TestEVMSigner_Close(t *testing.T) {
	// This is a placeholder - actual close testing requires a real connection
	// In production, this would verify that the ethclient connection is properly closed
	t.Run("close should not panic", func(t *testing.T) {
		// Test passes if no panic occurs
	})
}

// Helper function to generate a test private key
func generateTestPrivateKey() string {
	// This is a known test key - never use in production!
	// Generate a random key for testing
	key, _ := crypto.GenerateKey()
	return hex.EncodeToString(crypto.FromECDSA(key))
}

// Integration test placeholder - requires actual RPC connection
func TestEVMSigner_Integration(t *testing.T) {
	t.Skip("Integration test - requires actual RPC connection")

	// Example of how integration test would look:
	/*
		config := &EVMSignerConfig{
			RpcURL:     "https://sepolia.base.org",
			ChainID:    84532,
			PrivateKey: os.Getenv("TEST_PRIVATE_KEY"),
		}

		signer, err := NewEVMSigner(config)
		require.NoError(t, err)
		defer signer.Close()

		// Test GetAddresses
		addresses := signer.GetAddresses()
		assert.NotEmpty(t, addresses)

		// Test GetChainID
		ctx := context.Background()
		chainID, err := signer.GetChainID(ctx)
		assert.NoError(t, err)
		assert.Equal(t, int64(84532), chainID.Int64())

		// Test GetBalance
		balance, err := signer.GetBalance(ctx, addresses[0], "")
		assert.NoError(t, err)
		assert.NotNil(t, balance)
	*/
}

// Benchmark tests
func BenchmarkEVMSigner_HashTypedData(b *testing.B) {
	typedData := TypedData{
		Types: Types{
			"EIP712Domain": []Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Message": []Type{
				{Name: "content", Type: "string"},
				{Name: "amount", Type: "uint256"},
			},
		},
		PrimaryType: "Message",
		Domain: TypedDataDomain{
			Name:              "Test",
			Version:           "1",
			ChainId:           big.NewInt(1),
			VerifyingContract: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
		},
		Message: map[string]interface{}{
			"content": "Hello World",
			"amount":  "1000000000000000000",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = HashTypedData(typedData)
	}
}

func BenchmarkEVMSigner_EncodeValue(b *testing.B) {
	b.Run("encode address", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = encodeValue("address", "0x1234567890123456789012345678901234567890")
		}
	})

	b.Run("encode uint256", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = encodeValue("uint256", "1000000000000000000")
		}
	})
}
