package evm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEVMFacilitatorCreation tests facilitator creation
func TestEVMFacilitatorCreation(t *testing.T) {
	t.Run("missing network should fail", func(t *testing.T) {
		_, err := NewFacilitator("", "https://rpc.example.com", "0x1234")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "network is required")
	})

	t.Run("missing rpc URL should fail", func(t *testing.T) {
		_, err := NewFacilitator("base-sepolia", "", "0x1234")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rpc URL is required")
	})

	t.Run("missing private key should fail", func(t *testing.T) {
		_, err := NewFacilitator("base-sepolia", "https://rpc.example.com", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "private key is required")
	})
}

// Integration tests require actual RPC connection and private key
// func TestEVMFacilitatorVerifyIntegration(t *testing.T) {
// 	t.Skip("Integration test - requires actual RPC connection")
// 	// TODO: Implement SDK-based integration test
// }
//
// func TestEVMFacilitatorSettleIntegration(t *testing.T) {
// 	t.Skip("Integration test - requires actual RPC connection")
// 	// TODO: Implement SDK-based integration test
// }
