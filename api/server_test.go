package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosuda/x402-facilitator/types"
)

// A facilitator that answers nothing, so these tests exercise the server rather than a chain.
type stubFacilitator struct{ notReady error }

func (s *stubFacilitator) Verify(context.Context, *types.PaymentPayload, *types.PaymentRequirements) (*types.PaymentVerifyResponse, error) {
	return &types.PaymentVerifyResponse{IsValid: true}, nil
}

func (s *stubFacilitator) Settle(context.Context, *types.PaymentPayload, *types.PaymentRequirements) (*types.PaymentSettleResponse, error) {
	return &types.PaymentSettleResponse{Success: true}, nil
}

func (s *stubFacilitator) Supported() *types.SupportedResponse {
	return &types.SupportedResponse{}
}

// readyStub also answers the readiness question; plainStub deliberately does not.
type readyStub struct{ stubFacilitator }

func (r *readyStub) Ready(context.Context) error { return r.notReady }

func get(t *testing.T, s http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthIgnoresReadiness(t *testing.T) {
	// Liveness must not consult the chain: a bad RPC would otherwise be answered with a restart
	// loop, which fixes nothing.
	s := NewServer(&readyStub{stubFacilitator{notReady: errors.New("chain unreachable")}})
	if rec := get(t, s, "/health"); rec.Code != http.StatusOK {
		t.Fatalf("health: got %d, want 200 even while not ready", rec.Code)
	}
}

func TestReadyReportsUnavailableWithReason(t *testing.T) {
	s := NewServer(&readyStub{stubFacilitator{notReady: errors.New("fee payer holds 0 wei")}})
	rec := get(t, s, "/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready: got %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "fee payer holds 0 wei") {
		t.Fatalf("the reason was not passed through: %s", rec.Body.String())
	}
}

func TestReadyPassesWhenAble(t *testing.T) {
	if rec := get(t, NewServer(&readyStub{}), "/ready"); rec.Code != http.StatusOK {
		t.Fatalf("ready: got %d, want 200", rec.Code)
	}
}

func TestReadyDoesNotClaimAnUnrunCheck(t *testing.T) {
	// A facilitator with no readiness of its own must not be reported as verified-ready.
	rec := get(t, NewServer(&stubFacilitator{}), "/ready")
	if rec.Code != http.StatusOK {
		t.Fatalf("ready: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "liveness only") {
		t.Fatalf("it should say what it did not check: %s", rec.Body.String())
	}
}

func TestRateLimiterGuardsTheSpendingPaths(t *testing.T) {
	s := NewServer(&stubFacilitator{}, Options{SettleRateLimit: 1, SettleBurst: 2})

	post := func() int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settle", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.7:5555"
		s.ServeHTTP(rec, req)
		return rec.Code
	}

	// Whatever the first calls answer, a burst of 2 must eventually be refused rather than
	// broadcast - the point is that an unlimited settling endpoint spends real money.
	var limited bool
	for i := 0; i < 8; i++ {
		if post() == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("the settling path was never rate limited")
	}

	// /supported carries no cost and must stay reachable while a client is being throttled.
	if rec := get(t, s, "/supported"); rec.Code == http.StatusTooManyRequests {
		t.Fatal("a free endpoint was caught by the limiter")
	}
}

func TestRateLimiterOffByDefault(t *testing.T) {
	s := NewServer(&stubFacilitator{})
	for i := 0; i < 20; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settle", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		s.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatal("an existing deployment was throttled without asking to be")
		}
	}
}
