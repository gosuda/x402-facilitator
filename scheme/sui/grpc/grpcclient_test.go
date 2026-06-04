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

func TestNormalizeEndpointPreservesGRPCResolverTargets(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		wantTarget string
		wantTLS    bool
	}{
		{
			name:       "dns with explicit port",
			endpoint:   "dns:///example.com:443",
			wantTarget: "dns:///example.com:443",
			wantTLS:    true,
		},
		{
			name:       "dns with default port",
			endpoint:   "dns:///example.com",
			wantTarget: "dns:///example.com:443",
			wantTLS:    true,
		},
		{
			name:       "passthrough localhost with explicit port",
			endpoint:   "passthrough:///127.0.0.1:50051",
			wantTarget: "passthrough:///127.0.0.1:50051",
			wantTLS:    false,
		},
		{
			name:       "passthrough localhost with default port",
			endpoint:   "passthrough:///localhost",
			wantTarget: "passthrough:///localhost:443",
			wantTLS:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, useTLS, err := normalizeEndpoint(tt.endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if target != tt.wantTarget {
				t.Fatalf("expected target %q, got %q", tt.wantTarget, target)
			}
			if useTLS != tt.wantTLS {
				t.Fatalf("expected TLS %t, got %t", tt.wantTLS, useTLS)
			}
		})
	}
}

func TestNormalizeEndpointRejectsEmptyResolverTarget(t *testing.T) {
	if _, _, err := normalizeEndpoint("dns:///"); err == nil {
		t.Fatal("expected error")
	}
}
