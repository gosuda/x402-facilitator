package nano

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RPCError is returned when a Nano RPC endpoint answers with an error object
// or a non-2xx status.
type RPCError struct {
	Endpoint string
	Status   int
	Body     string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("nano rpc %s: status %d: %s", e.Endpoint, e.Status, e.Body)
}

// Client is a minimal Nano RPC client that issues one block_info read per
// endpoint. It deliberately depends only on the standard library so
// deployments do not have to pull in a Nano SDK to verify payments — Nano
// already settled at broadcast time and the facilitator only reads.
type Client struct {
	httpClient *http.Client
	// call, when set, replaces the HTTP transport (the test seam). It is
	// called with (endpoint, requestBody) and must return the raw JSON object
	// as decoded from bytes (or an error). When nil, a real POST is issued.
	call func(endpoint string, body map[string]interface{}) (map[string]interface{}, error)
}

// Callback is the injectable transport signature. Tests supply it to drive
// verification against an in-memory store instead of the live node.
type Callback func(endpoint string, body map[string]interface{}) (map[string]interface{}, error)

// NewClient builds a Nano RPC client over real HTTP.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{httpClient: &http.Client{Timeout: timeout}}
}

// NewClientWithCallback builds a Nano RPC client whose transport is the given
// callback (used for offline tests).
func NewClientWithCallback(call Callback) *Client {
	return &Client{call: call}
}

func (c *Client) post(ctx context.Context, endpoint string, body map[string]interface{}) (map[string]interface{}, error) {
	if c != nil && c.call != nil {
		return c.call(endpoint, body)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "x402-nano-facilitator")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &RPCError{Endpoint: endpoint, Status: resp.StatusCode, Body: string(raw)}
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, &RPCError{Endpoint: endpoint, Status: resp.StatusCode, Body: string(raw)}
	}
	if errMsg, ok := obj["error"]; ok {
		return nil, &RPCError{Endpoint: endpoint, Status: resp.StatusCode, Body: fmt.Sprintf("%v", errMsg)}
	}
	return obj, nil
}

// BlockInfo fetches the block_info for a block hash from one endpoint and
// normalizes it into the canonical BlockFields shape.
func (c *Client) BlockInfo(ctx context.Context, endpoint, hash string) (*BlockFields, error) {
	if c == nil {
		return nil, fmt.Errorf("nano client is nil")
	}
	obj, err := c.post(ctx, endpoint, map[string]interface{}{
		"action":     "block_info",
		"json_block": "true",
		"hash":       hash,
	})
	if err != nil {
		return nil, err
	}
	return NormalizeBlockInfo(obj), nil
}

// NormalizeBlockInfo maps a real Nano RPC block_info response onto the
// canonical BlockFields, tolerating the alias key shapes nodes expose. It
// FAILS CLOSED: an object that cannot authoritatively provide the
// amount/confirmed/destination fields yields a BlockFields with those fields
// unset so the caller's fail-closed rules reject it rather than guess.
func NormalizeBlockInfo(raw map[string]interface{}) *BlockFields {
	b := &BlockFields{}
	// Subtype: modern state blocks put the direction at the TOP-LEVEL
	// "subtype" field (send/receive); legacy blocks carry it at
	// contents.type and never as "state". Never treat the block type
	// "state" itself as a subtype.
	contents, _ := raw["contents"].(map[string]interface{})
	subtype := firstString(raw, "subtype")
	if subtype == "" {
		st := firstString(contents, "type")
		if st != "" && st != "state" {
			subtype = st
		} else {
			subtype = firstString(raw, "type")
		}
	}
	b.Subtype = subtype
	if b.Subtype == "state" {
		b.Subtype = ""
	}

	// Destination: contents.destination | contents.link_as_account | contents.link |
	// or top-level link_as_account | destination | link.
	b.Destination = firstString(contents, "destination", "link_as_account", "link")
	if b.Destination == "" {
		b.Destination = firstString(raw, "link_as_account", "destination", "link")
	}

	// Account (payer): block_account | account.
	b.Account = firstString(raw, "block_account", "account")

	// Amount: top-level amount (raw string). If absent, derive from
	// contents.amount / amount_nano is non-raw and unusable for exact match.
	if raw["amount"] != nil {
		b.AmountRaw = toString(raw["amount"])
	} else if contents != nil && contents["amount"] != nil {
		b.AmountRaw = toString(contents["amount"])
	}

	// Confirmed: string "true" (real shape) or bool true, at top level.
	b.Confirmed = isTruthy(raw["confirmed"])
	return b
}

func firstString(m map[string]interface{}, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := toString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return fmt.Sprintf("%.0f", t)
	default:
		if t == nil {
			return ""
		}
		return fmt.Sprintf("%v", t)
	}
}

func isTruthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	default:
		return false
	}
}
