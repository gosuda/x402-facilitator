package casper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gosuda/x402-facilitator/types"
	"github.com/gosuda/x402-facilitator/utils"
)

const (
	// VerifyPath is the Casper facilitator verification endpoint.
	VerifyPath = "/verify"
	// SettlePath is the Casper facilitator settlement endpoint.
	SettlePath = "/settle"
	// SupportedPath is the Casper facilitator discovery endpoint.
	SupportedPath = "/supported"

	defaultRequestTimeout = 30 * time.Second
	maxResponseBytes      = 4 << 20
)

// Client owns Casper facilitator endpoint failover and JSON transport. It
// deliberately depends only on the standard library so that deployments do
// not have to pull in a Casper SDK to settle payments.
type Client struct {
	mu         sync.RWMutex
	url        string
	endpoints  []string
	httpClient *http.Client
}

// NewClientWithEndpoints builds a client that prefers url and falls back to
// the supplied endpoints in order.
func NewClientWithEndpoints(url string, endpoints []string) *Client {
	candidates := utils.EndpointCandidates(append([]string{url}, endpoints...))
	activeURL := ""
	if len(candidates) > 0 {
		activeURL = candidates[0]
	}
	return &Client{
		url:        activeURL,
		endpoints:  candidates,
		httpClient: &http.Client{Timeout: defaultRequestTimeout},
	}
}

// NewClientForNetwork builds a client for a supported Casper network.
func NewClientForNetwork(network string, endpoints []string) (*Client, error) {
	info := GetNetworkInfo(network)
	if info == nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidNetwork, network)
	}
	candidates := utils.EndpointCandidates(append(append([]string(nil), endpoints...), info.DefaultURLs...))
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no endpoints configured for %s", network)
	}
	return NewClientWithEndpoints("", candidates), nil
}

// ActiveEndpoint returns the endpoint that most recently served a request.
func (c *Client) ActiveEndpoint() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.url
}

// Close releases idle connections held by the client.
func (c *Client) Close() error {
	if c == nil || c.httpClient == nil {
		return nil
	}
	c.httpClient.CloseIdleConnections()
	return nil
}

// Verify asks the Casper facilitator whether a payment payload satisfies the
// supplied requirements.
func (c *Client) Verify(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*VerifyResponse, error) {
	if payload == nil || req == nil {
		return nil, ErrEmptyPayload
	}
	body := VerifyRequest{
		X402Version:         int(types.X402VersionV2),
		PaymentPayload:      *payload,
		PaymentRequirements: *req,
	}
	var response VerifyResponse
	if err := c.do(ctx, http.MethodPost, VerifyPath, body, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// Settle asks the Casper facilitator to broadcast and settle a payment.
func (c *Client) Settle(ctx context.Context, payload *types.PaymentPayload, req *types.PaymentRequirements) (*SettleResponse, error) {
	if payload == nil || req == nil {
		return nil, ErrEmptyPayload
	}
	body := SettleRequest{
		X402Version:         int(types.X402VersionV2),
		PaymentPayload:      *payload,
		PaymentRequirements: *req,
	}
	var response SettleResponse
	if err := c.do(ctx, http.MethodPost, SettlePath, body, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// Supported returns the (scheme, network) pairs the facilitator accepts.
func (c *Client) Supported(ctx context.Context) (*SupportedResponse, error) {
	var response SupportedResponse
	if err := c.do(ctx, http.MethodGet, SupportedPath, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	if c == nil {
		return ErrEmptyPayload
	}
	var encoded []byte
	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}
	return c.call(ctx, func(ctx context.Context, endpoint string) error {
		return c.request(ctx, method, endpoint+path, encoded, out)
	})
}

func (c *Client) request(ctx context.Context, method, url string, body []byte, out interface{}) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("casper facilitator %s: %s", response.Status, errorMessage(payload))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) call(ctx context.Context, action utils.EndpointOperation) error {
	c.mu.RLock()
	candidates := append([]string(nil), c.endpoints...)
	active := c.url
	c.mu.RUnlock()

	if active != "" {
		candidates = utils.EndpointCandidates(append([]string{active}, candidates...))
	}
	endpoint, err := utils.DoWithEndpoint(ctx, candidates, action)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.url = endpoint
	c.mu.Unlock()
	return nil
}

func errorMessage(payload []byte) string {
	var response ErrorResponse
	if err := json.Unmarshal(payload, &response); err == nil {
		if message := strings.TrimSpace(response.Message); message != "" {
			return message
		}
		if message := strings.TrimSpace(response.Error); message != "" {
			return message
		}
	}
	if message := strings.TrimSpace(string(payload)); message != "" {
		return message
	}
	return "empty response body"
}
