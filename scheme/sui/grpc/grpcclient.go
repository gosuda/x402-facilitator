package suigrpc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	rpcv2 "github.com/gosuda/x402-facilitator/scheme/sui/grpc/pb/sui/rpc/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	BcsNameTransactionData = "TransactionData"
	BcsNameUserSignature   = "UserSignature"
)

var (
	simulateTransactionReadMask = readMask(
		"effects.status",
		"effects.changed_objects",
		"balance_changes",
	)
	executeTransactionReadMask = readMask(
		"digest",
		"effects.status",
		"balance_changes",
		"checkpoint",
		"timestamp",
	)
	getTransactionReadMask = readMask(
		"digest",
		"transaction",
		"effects.status",
		"balance_changes",
		"checkpoint",
		"timestamp",
	)
	checkpointDigestReadMask = readMask("digest")
)

type GRPCClient struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

func NewGRPCClient() *GRPCClient {
	return &GRPCClient{
		conns: make(map[string]*grpc.ClientConn),
	}
}

func (c *GRPCClient) SimulateTransaction(ctx context.Context, endpoint string, txBytes []byte) (*rpcv2.SimulateTransactionResponse, error) {
	if len(txBytes) == 0 {
		return nil, errors.New("empty transaction")
	}
	var response *rpcv2.SimulateTransactionResponse
	err := c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewTransactionExecutionServiceClient(conn)
		req := &rpcv2.SimulateTransactionRequest{
			Transaction: transactionFromBCS(txBytes),
			ReadMask:    simulateTransactionReadMask,
		}
		var err error
		response, err = client.SimulateTransaction(ctx, req)
		return err
	})
	return response, err
}

func (c *GRPCClient) ExecuteTransaction(ctx context.Context, endpoint string, txBytes []byte, signatures [][]byte) (*rpcv2.ExecuteTransactionResponse, error) {
	if len(txBytes) == 0 {
		return nil, errors.New("empty transaction")
	}
	if len(signatures) == 0 {
		return nil, errors.New("empty signatures")
	}
	var response *rpcv2.ExecuteTransactionResponse
	err := c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewTransactionExecutionServiceClient(conn)
		req := &rpcv2.ExecuteTransactionRequest{
			Transaction: transactionFromBCS(txBytes),
			Signatures:  userSignatures(signatures),
			ReadMask:    executeTransactionReadMask,
		}
		var err error
		response, err = client.ExecuteTransaction(ctx, req)
		return err
	})
	return response, err
}

func (c *GRPCClient) GetTransaction(ctx context.Context, endpoint string, digest string) (*rpcv2.GetTransactionResponse, error) {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil, errors.New("empty transaction digest")
	}
	var response *rpcv2.GetTransactionResponse
	err := c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewLedgerServiceClient(conn)
		req := &rpcv2.GetTransactionRequest{
			Digest:   &digest,
			ReadMask: getTransactionReadMask,
		}
		var err error
		response, err = client.GetTransaction(ctx, req)
		return err
	})
	return response, err
}

func (c *GRPCClient) GetServiceInfo(ctx context.Context, endpoint string) (*rpcv2.GetServiceInfoResponse, error) {
	var response *rpcv2.GetServiceInfoResponse
	err := c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewLedgerServiceClient(conn)
		var err error
		response, err = client.GetServiceInfo(ctx, &rpcv2.GetServiceInfoRequest{})
		return err
	})
	return response, err
}

func (c *GRPCClient) GetCheckpoint(ctx context.Context, endpoint string, sequence uint64) (*rpcv2.GetCheckpointResponse, error) {
	var response *rpcv2.GetCheckpointResponse
	err := c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewLedgerServiceClient(conn)
		req := &rpcv2.GetCheckpointRequest{
			CheckpointId: &rpcv2.GetCheckpointRequest_SequenceNumber{SequenceNumber: sequence},
			ReadMask:     checkpointDigestReadMask,
		}
		var err error
		response, err = client.GetCheckpoint(ctx, req)
		return err
	})
	return response, err
}

func (c *GRPCClient) GetBalance(ctx context.Context, endpoint string, owner string, coinType string) (*rpcv2.GetBalanceResponse, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("empty owner")
	}
	coinType = strings.TrimSpace(coinType)
	if coinType == "" {
		return nil, errors.New("empty coin type")
	}
	var response *rpcv2.GetBalanceResponse
	err := c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewStateServiceClient(conn)
		req := &rpcv2.GetBalanceRequest{
			Owner:    &owner,
			CoinType: &coinType,
		}
		var err error
		response, err = client.GetBalance(ctx, req)
		return err
	})
	return response, err
}

func (c *GRPCClient) ListOwnedObjects(ctx context.Context, endpoint string, req *rpcv2.ListOwnedObjectsRequest) (*rpcv2.ListOwnedObjectsResponse, error) {
	if req == nil {
		return nil, errors.New("nil list owned objects request")
	}
	var response *rpcv2.ListOwnedObjectsResponse
	err := c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewStateServiceClient(conn)
		var err error
		response, err = client.ListOwnedObjects(ctx, req)
		return err
	})
	return response, err
}

func (c *GRPCClient) VerifySignature(ctx context.Context, endpoint string, author string, txBytes []byte, signature string) (bool, error) {
	author = strings.TrimSpace(author)
	if author == "" {
		return false, errors.New("empty author")
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false, err
	}
	var response *rpcv2.VerifySignatureResponse
	err = c.withConn(ctx, endpoint, func(conn *grpc.ClientConn) error {
		client := rpcv2.NewSignatureVerificationServiceClient(conn)
		req := &rpcv2.VerifySignatureRequest{
			Message: &rpcv2.Bcs{
				Name:  ptr(BcsNameTransactionData),
				Value: txBytes,
			},
			Signature: userSignature(signatureBytes),
			Address:   &author,
		}
		var err error
		response, err = client.VerifySignature(ctx, req)
		return err
	})
	if err != nil {
		return false, err
	}
	return response != nil && response.GetIsValid(), nil
}

func (c *GRPCClient) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	conns := c.conns
	c.conns = make(map[string]*grpc.ClientConn)
	c.mu.Unlock()

	var errs []error
	for _, conn := range conns {
		if err := conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *GRPCClient) withConn(ctx context.Context, endpoint string, fn func(*grpc.ClientConn) error) error {
	conn, err := c.conn(endpoint)
	if err != nil {
		return err
	}
	return fn(conn)
}

func (c *GRPCClient) conn(endpoint string) (*grpc.ClientConn, error) {
	if c == nil {
		return nil, errors.New("nil Sui gRPC client")
	}

	target, useTLS, err := normalizeEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	key := connCacheKey(target, useTLS)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conns == nil {
		c.conns = make(map[string]*grpc.ClientConn)
	}
	if conn := c.conns[key]; conn != nil {
		return conn, nil
	}

	opts := []grpc.DialOption{}
	if useTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	c.conns[key] = conn
	return conn, nil
}

func connCacheKey(target string, useTLS bool) string {
	if useTLS {
		return "tls://" + target
	}
	return "plain://" + target
}

func normalizeEndpoint(endpoint string) (string, bool, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", false, errors.New("empty Sui gRPC endpoint")
	}

	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", false, err
		}
		switch parsed.Scheme {
		case "http":
			return hostWithDefaultPort(parsed.Host, "80"), false, nil
		case "https":
			return hostWithDefaultPort(parsed.Host, "443"), true, nil
		case "grpc":
			return hostWithDefaultPort(parsed.Host, "443"), false, nil
		case "grpcs":
			return hostWithDefaultPort(parsed.Host, "443"), true, nil
		default:
			return "", false, fmt.Errorf("unsupported Sui gRPC endpoint scheme %q", parsed.Scheme)
		}
	}

	host := endpoint
	if strings.HasPrefix(endpoint, "dns:///") || strings.HasPrefix(endpoint, "passthrough:///") {
		host = strings.TrimPrefix(strings.TrimPrefix(endpoint, "dns:///"), "passthrough:///")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "443")
	}
	return host, !isLocalhost(host), nil
}

func hostWithDefaultPort(host string, port string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, port)
}

func isLocalhost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func transactionFromBCS(txBytes []byte) *rpcv2.Transaction {
	return &rpcv2.Transaction{
		Bcs: &rpcv2.Bcs{
			Name:  ptr(BcsNameTransactionData),
			Value: txBytes,
		},
	}
}

func userSignatures(signatures [][]byte) []*rpcv2.UserSignature {
	result := make([]*rpcv2.UserSignature, 0, len(signatures))
	for _, signature := range signatures {
		result = append(result, userSignature(signature))
	}
	return result
}

func userSignature(signature []byte) *rpcv2.UserSignature {
	return &rpcv2.UserSignature{
		Bcs: &rpcv2.Bcs{
			Name:  ptr(BcsNameUserSignature),
			Value: signature,
		},
	}
}

func readMask(paths ...string) *fieldmaskpb.FieldMask {
	return &fieldmaskpb.FieldMask{Paths: paths}
}

func ptr[T any](value T) *T {
	return &value
}
