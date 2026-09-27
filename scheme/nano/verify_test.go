package nano

import (
	"context"
	"testing"
)

// realBlock is the canonical confirmed-send shape both endpoints return in the
// happy path, modelled on a verified live Mainnet send block.
func realBlock(endpoint string, _ map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"block_account": "nano_3t6k35gi95xu6tergt6p69ck76ogmitsa8mnijtpxm9fkcm736xtoncuohr3",
		"amount":        "205676479000000000000000000000000000000",
		"confirmed":     "true",
		"contents": map[string]interface{}{
			"type":        "send",
			"destination": "nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		},
	}, nil
}

func TestVerifyBlockHappyPath(t *testing.T) {
	client := NewClientWithCallback(realBlock)
	endpoints := []string{"https://a", "https://b"}
	res := VerifyBlock(context.Background(), client, endpoints,
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		"205676479000000000000000000000000000000")
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
	if res.ConfirmedSends < MinIndependentEndpoints {
		t.Fatalf("confirmed sends %d < %d", res.ConfirmedSends, MinIndependentEndpoints)
	}
	if res.Payer != "nano_3t6k35gi95xu6tergt6p69ck76ogmitsa8mnijtpxm9fkcm736xtoncuohr3" {
		t.Fatalf("payer = %q", res.Payer)
	}
}

func TestVerifyBlockFailClosedInsufficientEndpoints(t *testing.T) {
	client := NewClientWithCallback(realBlock)
	res := VerifyBlock(context.Background(), client, []string{"https://a"},
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		"205676479000000000000000000000000000000")
	if res.OK {
		t.Fatalf("must fail closed below 2 endpoints")
	}
	if res.Reason == "" {
		t.Fatalf("expected a reason")
	}
}

func TestVerifyBlockFailClosedOneBrokenEndpoint(t *testing.T) {
	// One endpoint down must fail the WHOLE verification: a single healthy
	// endpoint cannot manufacture acceptance.
	calls := 0
	client := NewClientWithCallback(func(endpoint string, body map[string]interface{}) (map[string]interface{}, error) {
		calls++
		if endpoint == "https://broken" {
			return nil, &RPCError{Endpoint: endpoint, Status: 500, Body: "boom"}
		}
		return realBlock(endpoint, body)
	})
	res := VerifyBlock(context.Background(), client,
		[]string{"https://good", "https://broken"},
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		"205676479000000000000000000000000000000")
	if res.OK {
		t.Fatalf("must fail closed when an endpoint errors")
	}
	// Both endpoints were consulted (first good, then broken); the broken one
	// is what fails the whole verification. A single healthy endpoint must not
	// be able to manufacture an acceptance.
	if calls != 2 {
		t.Fatalf("expected both endpoints consulted, contacted %d", calls)
	}
}

func TestVerifyBlockFailClosedWrongAmount(t *testing.T) {
	client := NewClientWithCallback(realBlock)
	res := VerifyBlock(context.Background(), client,
		[]string{"https://a", "https://b"},
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		"999")
	if res.OK {
		t.Fatalf("wrong amount must fail")
	}
}

func TestVerifyBlockFailClosedWrongDestination(t *testing.T) {
	client := NewClientWithCallback(realBlock)
	res := VerifyBlock(context.Background(), client,
		[]string{"https://a", "https://b"},
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_3t6k35gi95xu6tergt6p69ck76ogmitsa8mnijtpxm9fkcm736xtoncuohr3",
		"205676479000000000000000000000000000000")
	if res.OK {
		t.Fatalf("wrong destination must fail")
	}
}

func TestVerifyBlockFailClosedUnconfirmed(t *testing.T) {
	cb := func(endpoint string, _ map[string]interface{}) (map[string]interface{}, error) {
		b, _ := realBlock(endpoint, nil)
		b["confirmed"] = "false"
		return b, nil
	}
	client := NewClientWithCallback(cb)
	res := VerifyBlock(context.Background(), client,
		[]string{"https://a", "https://b"},
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		"205676479000000000000000000000000000000")
	if res.OK {
		t.Fatalf("unconfirmed block must fail")
	}
}

func TestVerifyBlockFailClosedNotSend(t *testing.T) {
	cb := func(endpoint string, _ map[string]interface{}) (map[string]interface{}, error) {
		b, _ := realBlock(endpoint, nil)
		b["contents"] = map[string]interface{}{"type": "receive", "destination": "nano_1111111111111111111111111111111111111111111111111111hifc8npp"}
		return b, nil
	}
	client := NewClientWithCallback(cb)
	res := VerifyBlock(context.Background(), client,
		[]string{"https://a", "https://b"},
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		"205676479000000000000000000000000000000")
	if res.OK {
		t.Fatalf("non-send block must fail")
	}
}

func TestVerifyBlockDedupesDuplicateEndpoints(t *testing.T) {
	// Duplicate or whitespace-padded copies of the same endpoint must count
	// as ONE independent node, not several. Passing one real endpoint twice
	// must fail closed (only 1 independent endpoint), not pass as if 2.
	client := NewClientWithCallback(realBlock)
	res := VerifyBlock(context.Background(), client,
		[]string{"https://a", "https://a", "  https://a	"},
		"ECCB8CB65CD3106EDA8CE9AA893FEAD497A91BCA903890CBD7A5C59F06AB9113",
		"nano_1111111111111111111111111111111111111111111111111111hifc8npp",
		"205676479000000000000000000000000000000")
	if res.OK {
		t.Fatalf("duplicate endpoints must dedupe to 1 independent node and fail closed")
	}
	if res.Reason == "" {
		t.Fatalf("expected an insufficient-endpoints reason, got %+v", res)
	}
}
