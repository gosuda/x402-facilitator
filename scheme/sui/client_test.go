package sui

import (
	"context"
	"strings"
	"testing"
)

func TestClientCallRejectsUninitializedGRPCClient(t *testing.T) {
	client := &Client{url: "127.0.0.1:50051"}

	err := client.call(context.Background(), func(context.Context, string) error {
		t.Fatal("action should not be called")
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected uninitialized gRPC client error, got %v", err)
	}
}
