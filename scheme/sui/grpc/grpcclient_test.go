package suigrpc

import "testing"

func TestGRPCClientReusesConnectionByNormalizedEndpoint(t *testing.T) {
	client := NewGRPCClient()
	defer client.Close()

	first, err := client.conn("http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.conn("127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatal("expected normalized endpoint to reuse cached connection")
	}
	if got := len(client.conns); got != 1 {
		t.Fatalf("expected 1 cached connection, got %d", got)
	}
}

func TestGRPCClientCloseClearsConnectionCache(t *testing.T) {
	client := NewGRPCClient()

	if _, err := client.conn("http://127.0.0.1:1234"); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	if got := len(client.conns); got != 0 {
		t.Fatalf("expected empty connection cache, got %d", got)
	}
}
