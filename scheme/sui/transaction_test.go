package sui

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"testing"

	rpcv2 "github.com/gosuda/x402-facilitator/scheme/sui/grpc/pb/sui/rpc/v2"
	bcs "github.com/iotaledger/bcs-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestBuildGaslessStablecoinTransferTransactionUsesTestnetUSDC(t *testing.T) {
	txBytes, err := BuildGaslessStablecoinTransferTransaction(context.Background(), GaslessStablecoinTransfer{
		Sender:     "0x123",
		Recipient:  "0xabc",
		Network:    "sui:testnet",
		Asset:      "USDC",
		Amount:     "1000000",
		Expiration: testValidDuringExpiration(t),
	})
	require.NoError(t, err)

	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	require.NoError(t, err)
	require.NotNil(t, txData.V1)

	programmable := txData.V1.Kind.ProgrammableTransaction
	require.NotNil(t, programmable)
	require.Len(t, programmable.Inputs, 2)
	require.NotNil(t, programmable.Inputs[0].FundsWithdrawal)
	require.Equal(t, NormalizeType(TestnetUSDCType), NormalizeType(programmable.Inputs[0].FundsWithdrawal.TypeArg.Balance.String()))
	require.Len(t, programmable.Commands, 2)
	require.NotNil(t, programmable.Commands[0].MoveCall)
	require.NotNil(t, programmable.Commands[1].MoveCall)
	require.Equal(t, "balance", programmable.Commands[0].MoveCall.Module)
	require.Equal(t, "redeem_funds", programmable.Commands[0].MoveCall.Function)
	require.Equal(t, "balance", programmable.Commands[1].MoveCall.Module)
	require.Equal(t, "send_funds", programmable.Commands[1].MoveCall.Function)
	require.Equal(t, NormalizeType(TestnetUSDCType), NormalizeType(programmable.Commands[1].MoveCall.TypeArguments[0].String()))
}

func TestBuildCoinObjectsToAddressBalanceTransferTransactionUsesAllObjects(t *testing.T) {
	firstObjectID, err := ParseAddress("0x1234")
	require.NoError(t, err)
	secondObjectID, err := ParseAddress("0x5678")
	require.NoError(t, err)
	digest, err := ParseDigest("11111111111111111111111111111111")
	require.NoError(t, err)

	coinObjects := []OwnedCoinObject{
		{
			ObjectRef: ObjectRef{ObjectID: firstObjectID, Version: 7, Digest: digest},
			CoinType:  TestnetUSDCType,
			Balance:   400000,
		},
		{
			ObjectRef: ObjectRef{ObjectID: secondObjectID, Version: 8, Digest: digest},
			CoinType:  TestnetUSDCType,
			Balance:   700000,
		},
	}

	txBytes, err := BuildCoinObjectsToAddressBalanceTransferTransaction(context.Background(), CoinObjectsToAddressBalanceTransfer{
		Sender:      "0x123",
		Recipient:   "0xabc",
		Network:     "sui:testnet",
		Asset:       "USDC",
		CoinObjects: coinObjects,
		Expiration:  testValidDuringExpiration(t),
	})
	require.NoError(t, err)

	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	require.NoError(t, err)
	require.NotNil(t, txData.V1)
	require.Equal(t, NormalizeAddress("0x123"), txData.V1.Sender.String())

	programmable := txData.V1.Kind.ProgrammableTransaction
	require.NotNil(t, programmable)
	require.Len(t, programmable.Inputs, 3)
	require.Len(t, programmable.Commands, 2)

	for i, command := range programmable.Commands {
		require.NotNil(t, command.MoveCall)
		require.Equal(t, "coin", command.MoveCall.Module)
		require.Equal(t, "send_funds", command.MoveCall.Function)
		require.Equal(t, NormalizeType(TestnetUSDCType), NormalizeType(command.MoveCall.TypeArguments[0].String()))
		require.Len(t, command.MoveCall.Arguments, 2)
		require.NotNil(t, command.MoveCall.Arguments[0].Input)
		require.Equal(t, uint16(i), *command.MoveCall.Arguments[0].Input)
		require.NotNil(t, command.MoveCall.Arguments[1].Input)
		require.Equal(t, uint16(2), *command.MoveCall.Arguments[1].Input)
	}

	_, err = BuildCoinObjectsToAddressBalanceTransferTransaction(context.Background(), CoinObjectsToAddressBalanceTransfer{
		Sender:      "0x123",
		Recipient:   "0xabc",
		Network:     "sui:testnet",
		Asset:       "0x2::sui::SUI",
		CoinObjects: coinObjects,
		Expiration:  testValidDuringExpiration(t),
	})
	require.ErrorContains(t, err, "not gasless stablecoin allowlisted")
}

func TestManualSuiTestnetObjectToBalanceThenBalanceToRecipientBroadcast(t *testing.T) {
	// privateKey := ""
	// privateKey := ""
	privateKey := ""
	from := ""
	to := ""
	rpcURL := "https://fullnode.testnet.sui.io:443"
	paymentAmount := "10000"

	if strings.TrimSpace(privateKey) == "" || strings.TrimSpace(to) == "" {
		t.Skip("fill privateKey and to to broadcast this manual Sui testnet test")
	}

	ctx := context.Background()
	signer, err := NewSignerFromPrivateKeyString(privateKey)
	require.NoError(t, err)
	if strings.TrimSpace(from) == "" {
		from = signer.Address()
	} else {
		require.Equal(t, NormalizeAddress(from), NormalizeAddress(signer.Address()))
	}

	result, err := ExecuteGaslessStablecoinObjectBalancePayment(ctx, GaslessStablecoinObjectBalancePayment{
		Sender:    from,
		Recipient: to,
		Network:   "sui:testnet",
		Asset:     "USDC",
		Amount:    paymentAmount,
		Endpoints: []string{rpcURL},
	}, signer)
	require.NoError(t, err)
	if result.PrepareTransaction == nil {
		fmt.Println("no USDC coin objects to prepare; paid from existing address balance")
	} else {
		fmt.Printf("prepared %d objects totaling %s into address balance: %s\n", len(result.CoinObjects), result.PreparedAmount, result.PrepareTransaction.Digest.String())
	}
	require.NotNil(t, result.PaymentTransaction)
	paymentDelta := TransactionResultBalanceDelta(result.PaymentTransaction, to, TestnetUSDCType)
	if NormalizeAddress(from) == NormalizeAddress(to) {
		require.Zero(t, paymentDelta.Sign())
	} else {
		require.Equal(t, paymentAmount, paymentDelta.String())
	}
	fmt.Printf("sent %s testnet USDC address balance to %s: %s\n", paymentAmount, NormalizeAddress(to), result.PaymentTransaction.Digest.String())
}

func TestExecuteGaslessStablecoinObjectBalancePaymentResolvesExpirationOnce(t *testing.T) {
	signer := newTestSigner(t)
	recipient := "0xabc"
	var methods []string
	executeCount := 0
	endpoint, closeServer := newSuiTransactionGRPCTestServer(t, &suiTransactionGRPCTestServer{
		methods: &methods,
		getServiceInfo: func(ctx context.Context, req *rpcv2.GetServiceInfoRequest) (*rpcv2.GetServiceInfoResponse, error) {
			return &rpcv2.GetServiceInfoResponse{Epoch: ptr(uint64(42))}, nil
		},
		getBalance: func(ctx context.Context, req *rpcv2.GetBalanceRequest) (*rpcv2.GetBalanceResponse, error) {
			require.Equal(t, signer.Address(), req.GetOwner())
			require.Equal(t, TestnetUSDCType, req.GetCoinType())
			return suiBalanceGRPCResult(TestnetUSDCType, 0, 400000), nil
		},
		listOwnedObjects: func(ctx context.Context, req *rpcv2.ListOwnedObjectsRequest) (*rpcv2.ListOwnedObjectsResponse, error) {
			require.Equal(t, signer.Address(), req.GetOwner())
			return &rpcv2.ListOwnedObjectsResponse{
				Objects: []*rpcv2.Object{
					suiCoinObjectGRPCResult("0x1234", 7, 400000, TestnetUSDCType),
				},
			}, nil
		},
		execute: func(ctx context.Context, req *rpcv2.ExecuteTransactionRequest) (*rpcv2.ExecuteTransactionResponse, error) {
			executeCount++
			requireReadMask(t, req.GetReadMask(), "digest", "effects.status", "balance_changes", "checkpoint", "timestamp")
			return &rpcv2.ExecuteTransactionResponse{
				Transaction: suiExecutedTransactionGRPCResult("11111111111111111111111111111111", recipient, TestnetUSDCType, "10000"),
			}, nil
		},
	})
	defer closeServer()

	result, err := ExecuteGaslessStablecoinObjectBalancePayment(context.Background(), GaslessStablecoinObjectBalancePayment{
		Sender:    signer.Address(),
		Recipient: recipient,
		Network:   "sui:testnet",
		Asset:     "USDC",
		Amount:    "10000",
		Endpoints: []string{endpoint},
	}, signer)
	require.NoError(t, err)
	require.NotNil(t, result.PrepareTransaction)
	require.NotNil(t, result.PaymentTransaction)
	require.Equal(t, 2, executeCount)
	require.Equal(t, []string{
		"GetBalance",
		"ListOwnedObjects",
		"GetServiceInfo",
		"ExecuteTransaction",
		"ExecuteTransaction",
	}, methods)
}

func TestExecuteGaslessStablecoinObjectBalancePaymentSkipsPrepareWhenAddressBalanceIsEnough(t *testing.T) {
	signer := newTestSigner(t)
	recipient := "0xabc"
	var methods []string
	endpoint, closeServer := newSuiTransactionGRPCTestServer(t, &suiTransactionGRPCTestServer{
		methods: &methods,
		getServiceInfo: func(ctx context.Context, req *rpcv2.GetServiceInfoRequest) (*rpcv2.GetServiceInfoResponse, error) {
			return &rpcv2.GetServiceInfoResponse{Epoch: ptr(uint64(42))}, nil
		},
		getBalance: func(ctx context.Context, req *rpcv2.GetBalanceRequest) (*rpcv2.GetBalanceResponse, error) {
			require.Equal(t, signer.Address(), req.GetOwner())
			require.Equal(t, TestnetUSDCType, req.GetCoinType())
			return suiBalanceGRPCResult(TestnetUSDCType, 10000, 400000), nil
		},
		listOwnedObjects: func(ctx context.Context, req *rpcv2.ListOwnedObjectsRequest) (*rpcv2.ListOwnedObjectsResponse, error) {
			t.Fatal("coin object lookup should be skipped when address balance is sufficient")
			return nil, nil
		},
		execute: func(ctx context.Context, req *rpcv2.ExecuteTransactionRequest) (*rpcv2.ExecuteTransactionResponse, error) {
			return &rpcv2.ExecuteTransactionResponse{
				Transaction: suiExecutedTransactionGRPCResult("11111111111111111111111111111111", recipient, TestnetUSDCType, "10000"),
			}, nil
		},
	})
	defer closeServer()

	result, err := ExecuteGaslessStablecoinObjectBalancePayment(context.Background(), GaslessStablecoinObjectBalancePayment{
		Sender:    signer.Address(),
		Recipient: recipient,
		Network:   "sui:testnet",
		Asset:     "USDC",
		Amount:    "10000",
		Endpoints: []string{endpoint},
	}, signer)
	require.NoError(t, err)
	require.Nil(t, result.PrepareTransaction)
	require.NotNil(t, result.PaymentTransaction)
	require.Empty(t, result.CoinObjects)
	require.Equal(t, []string{
		"GetBalance",
		"GetServiceInfo",
		"ExecuteTransaction",
	}, methods)
}

func TestExecuteGaslessStablecoinObjectBalancePaymentRejectsInsufficientBalanceBeforePrepareLookup(t *testing.T) {
	signer := newTestSigner(t)
	var methods []string
	endpoint, closeServer := newSuiTransactionGRPCTestServer(t, &suiTransactionGRPCTestServer{
		methods: &methods,
		getBalance: func(ctx context.Context, req *rpcv2.GetBalanceRequest) (*rpcv2.GetBalanceResponse, error) {
			require.Equal(t, signer.Address(), req.GetOwner())
			require.Equal(t, TestnetUSDCType, req.GetCoinType())
			return suiBalanceGRPCResult(TestnetUSDCType, 1000, 2000), nil
		},
		getServiceInfo: func(ctx context.Context, req *rpcv2.GetServiceInfoRequest) (*rpcv2.GetServiceInfoResponse, error) {
			t.Fatal("expiration should not be resolved when total balance is insufficient")
			return nil, nil
		},
		listOwnedObjects: func(ctx context.Context, req *rpcv2.ListOwnedObjectsRequest) (*rpcv2.ListOwnedObjectsResponse, error) {
			t.Fatal("coin objects should not be listed when total balance is insufficient")
			return nil, nil
		},
		execute: func(ctx context.Context, req *rpcv2.ExecuteTransactionRequest) (*rpcv2.ExecuteTransactionResponse, error) {
			t.Fatal("transaction should not be executed when total balance is insufficient")
			return nil, nil
		},
	})
	defer closeServer()

	result, err := ExecuteGaslessStablecoinObjectBalancePayment(context.Background(), GaslessStablecoinObjectBalancePayment{
		Sender:    signer.Address(),
		Recipient: "0xabc",
		Network:   "sui:testnet",
		Asset:     "USDC",
		Amount:    "10000",
		Endpoints: []string{endpoint},
	}, signer)
	require.Nil(t, result)
	require.ErrorContains(t, err, "insufficient")
	require.Equal(t, []string{"GetBalance"}, methods)
}

func TestBuildGaslessStablecoinTransferTransactionRejectsInvalidInput(t *testing.T) {
	signer := newTestSigner(t)

	_, err := BuildGaslessStablecoinTransferTransaction(context.Background(), GaslessStablecoinTransfer{
		Sender:     signer.Address(),
		Recipient:  "0xabc",
		Network:    "sui:mainnet",
		Asset:      "0x2::sui::SUI",
		Amount:     "1000000",
		Expiration: testValidDuringExpiration(t),
	})
	require.ErrorContains(t, err, "not gasless stablecoin allowlisted")

	_, err = BuildGaslessStablecoinTransferTransaction(context.Background(), GaslessStablecoinTransfer{
		Sender:     signer.Address(),
		Recipient:  "0xabc",
		Asset:      USDCType,
		Amount:     "0",
		Expiration: testValidDuringExpiration(t),
	})
	require.ErrorContains(t, err, "invalid amount")
}

func TestResolveGaslessStablecoinExpirationUsesGRPC(t *testing.T) {
	var methods []string
	endpoint, closeServer := newSuiTransactionGRPCTestServer(t, &suiTransactionGRPCTestServer{
		methods: &methods,
		getServiceInfo: func(ctx context.Context, req *rpcv2.GetServiceInfoRequest) (*rpcv2.GetServiceInfoResponse, error) {
			return &rpcv2.GetServiceInfoResponse{Epoch: ptr(uint64(42))}, nil
		},
	})
	defer closeServer()

	expiration, err := ResolveGaslessStablecoinExpiration(context.Background(), "sui:mainnet", []string{"http://127.0.0.1:1", endpoint})
	require.NoError(t, err)
	require.NotNil(t, expiration.ValidDuring)
	require.Equal(t, uint64(42), *expiration.ValidDuring.MinEpoch)
	require.Equal(t, uint64(43), *expiration.ValidDuring.MaxEpoch)
	require.Len(t, expiration.ValidDuring.Chain, 32)
	require.Equal(t, []string{"GetServiceInfo"}, methods)
}

func TestListOwnedGaslessStablecoinCoinObjects(t *testing.T) {
	var methods []string
	endpoint, closeServer := newSuiTransactionGRPCTestServer(t, &suiTransactionGRPCTestServer{
		methods: &methods,
		listOwnedObjects: func(ctx context.Context, req *rpcv2.ListOwnedObjectsRequest) (*rpcv2.ListOwnedObjectsResponse, error) {
			require.Equal(t, NormalizeAddress("0x123"), req.GetOwner())
			require.Equal(t, coinObjectType(TestnetUSDCType), req.GetObjectType())
			require.Equal(t, uint32(suiCoinObjectPageLimit), req.GetPageSize())
			require.Empty(t, req.GetPageToken())
			requireReadMask(t, req.GetReadMask(), "object_id", "version", "digest", "object_type", "balance")
			firstObject := suiCoinObjectGRPCResult("0x1234", 7, 400000, TestnetUSDCType)
			firstObject.PreviousTransaction = ptr("11111111111111111111111111111111")
			return &rpcv2.ListOwnedObjectsResponse{
				Objects: []*rpcv2.Object{
					firstObject,
					suiCoinObjectGRPCResult("0x5678", 8, 700000, TestnetUSDCType),
				},
			}, nil
		},
	})
	defer closeServer()

	coinObjects, err := ListOwnedGaslessStablecoinCoinObjects(context.Background(), "sui:testnet", "0x123", "USDC", []string{endpoint})
	require.NoError(t, err)
	require.Equal(t, []string{"ListOwnedObjects"}, methods)
	require.Len(t, coinObjects, 2)
	require.Equal(t, NormalizeAddress("0x1234"), coinObjects[0].ObjectRef.ObjectID.String())
	require.Equal(t, uint64(7), coinObjects[0].ObjectRef.Version)
	require.Equal(t, uint64(400000), coinObjects[0].Balance)
	require.Equal(t, NormalizeType(TestnetUSDCType), NormalizeType(coinObjects[0].CoinType))
}

func testValidDuringExpiration(t *testing.T) *TransactionExpiration {
	t.Helper()
	expiration, err := TransactionExpirationValidDuring("4btiuiMPvEENsttpZC7CZ53DruC3MAgfznDbASZ7DR6S", 1142, 1143, 7)
	require.NoError(t, err)
	return expiration
}

func suiCoinObjectGRPCResult(objectID string, version uint64, balance uint64, coinType string) *rpcv2.Object {
	return &rpcv2.Object{
		ObjectId:            ptr(objectID),
		Version:             ptr(version),
		Digest:              ptr("11111111111111111111111111111111"),
		ObjectType:          ptr(coinObjectType(coinType)),
		Balance:             ptr(balance),
		PreviousTransaction: ptr(""),
	}
}

type suiTransactionGRPCTestServer struct {
	rpcv2.UnimplementedLedgerServiceServer
	rpcv2.UnimplementedStateServiceServer
	rpcv2.UnimplementedTransactionExecutionServiceServer

	methods          *[]string
	getServiceInfo   func(context.Context, *rpcv2.GetServiceInfoRequest) (*rpcv2.GetServiceInfoResponse, error)
	getBalance       func(context.Context, *rpcv2.GetBalanceRequest) (*rpcv2.GetBalanceResponse, error)
	listOwnedObjects func(context.Context, *rpcv2.ListOwnedObjectsRequest) (*rpcv2.ListOwnedObjectsResponse, error)
	execute          func(context.Context, *rpcv2.ExecuteTransactionRequest) (*rpcv2.ExecuteTransactionResponse, error)
}

func newSuiTransactionGRPCTestServer(t *testing.T, handler *suiTransactionGRPCTestServer) (string, func()) {
	t.Helper()
	if handler == nil {
		handler = &suiTransactionGRPCTestServer{}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	rpcv2.RegisterLedgerServiceServer(server, handler)
	rpcv2.RegisterStateServiceServer(server, handler)
	rpcv2.RegisterTransactionExecutionServiceServer(server, handler)
	go func() {
		_ = server.Serve(listener)
	}()
	return "http://" + listener.Addr().String(), func() {
		server.Stop()
		_ = listener.Close()
	}
}

func (s *suiTransactionGRPCTestServer) GetServiceInfo(ctx context.Context, req *rpcv2.GetServiceInfoRequest) (*rpcv2.GetServiceInfoResponse, error) {
	s.appendMethod("GetServiceInfo")
	if s.getServiceInfo == nil {
		return nil, status.Error(codes.Unimplemented, "get service info not implemented")
	}
	return s.getServiceInfo(ctx, req)
}

func (s *suiTransactionGRPCTestServer) ListOwnedObjects(ctx context.Context, req *rpcv2.ListOwnedObjectsRequest) (*rpcv2.ListOwnedObjectsResponse, error) {
	s.appendMethod("ListOwnedObjects")
	if s.listOwnedObjects == nil {
		return nil, status.Error(codes.Unimplemented, "list owned objects not implemented")
	}
	return s.listOwnedObjects(ctx, req)
}

func (s *suiTransactionGRPCTestServer) GetBalance(ctx context.Context, req *rpcv2.GetBalanceRequest) (*rpcv2.GetBalanceResponse, error) {
	s.appendMethod("GetBalance")
	if s.getBalance == nil {
		return nil, status.Error(codes.Unimplemented, "get balance not implemented")
	}
	return s.getBalance(ctx, req)
}

func (s *suiTransactionGRPCTestServer) ExecuteTransaction(ctx context.Context, req *rpcv2.ExecuteTransactionRequest) (*rpcv2.ExecuteTransactionResponse, error) {
	s.appendMethod("ExecuteTransaction")
	if s.execute == nil {
		return nil, status.Error(codes.Unimplemented, "execute not implemented")
	}
	return s.execute(ctx, req)
}

func (s *suiTransactionGRPCTestServer) appendMethod(method string) {
	if s.methods != nil {
		*s.methods = append(*s.methods, method)
	}
}

func suiExecutedTransactionGRPCResult(digest string, address string, coinType string, amount string) *rpcv2.ExecutedTransaction {
	success := true
	transaction := &rpcv2.ExecutedTransaction{
		Digest: ptr(digest),
		Effects: &rpcv2.TransactionEffects{
			Status: &rpcv2.ExecutionStatus{Success: &success},
		},
	}
	if amount != "" {
		transaction.BalanceChanges = []*rpcv2.BalanceChange{{
			Address:  ptr(address),
			CoinType: ptr(coinType),
			Amount:   ptr(amount),
		}}
	}
	return transaction
}

func suiBalanceGRPCResult(coinType string, addressBalance uint64, coinBalance uint64) *rpcv2.GetBalanceResponse {
	return &rpcv2.GetBalanceResponse{
		Balance: &rpcv2.Balance{
			CoinType:       ptr(coinType),
			Balance:        ptr(addressBalance + coinBalance),
			AddressBalance: ptr(addressBalance),
			CoinBalance:    ptr(coinBalance),
		},
	}
}

func requireReadMask(t *testing.T, mask *fieldmaskpb.FieldMask, paths ...string) {
	t.Helper()
	require.NotNil(t, mask)
	require.Equal(t, paths, mask.GetPaths())
}

func TestBuildGaslessStablecoinTransferTransactionUsesFundsWithdrawal(t *testing.T) {
	txBytes, err := BuildGaslessStablecoinTransferTransaction(context.Background(), GaslessStablecoinTransfer{
		Sender:     "0x123",
		Recipient:  "0xabc",
		Network:    "sui:mainnet",
		Asset:      "USDC",
		Amount:     "1000000",
		Expiration: testValidDuringExpiration(t),
	})
	require.NoError(t, err)
	require.NotEmpty(t, hex.EncodeToString(txBytes))
	sender, err := TransactionSender(txBytes)
	require.NoError(t, err)
	require.Equal(t, NormalizeAddress("0x123"), sender)
	digest, err := TransactionDigest(txBytes)
	require.NoError(t, err)
	require.NotEmpty(t, digest)

	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	require.NoError(t, err)
	require.NotNil(t, txData.V1)
	require.Equal(t, NormalizeAddress("0x123"), txData.V1.Sender.String())
	require.Empty(t, txData.V1.GasData.Payment)
	require.Equal(t, uint64(0), txData.V1.GasData.Price)
	require.Equal(t, uint64(0), txData.V1.GasData.Budget)
	require.NotNil(t, txData.V1.Expiration.ValidDuring)
	require.Equal(t, uint64(1142), *txData.V1.Expiration.ValidDuring.MinEpoch)
	require.Equal(t, uint64(1143), *txData.V1.Expiration.ValidDuring.MaxEpoch)
	require.Equal(t, uint32(7), txData.V1.Expiration.ValidDuring.Nonce)

	programmable := txData.V1.Kind.ProgrammableTransaction
	require.NotNil(t, programmable)
	require.Len(t, programmable.Inputs, 2)
	require.NotNil(t, programmable.Inputs[0].FundsWithdrawal)
	require.NotNil(t, programmable.Inputs[0].FundsWithdrawal.Reservation.MaxAmountU64)
	require.Equal(t, uint64(1000000), *programmable.Inputs[0].FundsWithdrawal.Reservation.MaxAmountU64)
	require.NotNil(t, programmable.Inputs[0].FundsWithdrawal.TypeArg.Balance)
	require.Equal(t, NormalizeType(USDCType), NormalizeType(programmable.Inputs[0].FundsWithdrawal.TypeArg.Balance.String()))
	require.NotNil(t, programmable.Inputs[0].FundsWithdrawal.WithdrawFrom.Sender)
	require.NotNil(t, programmable.Inputs[1].Pure)

	require.Len(t, programmable.Commands, 2)
	require.NotNil(t, programmable.Commands[0].MoveCall)
	redeemCall := programmable.Commands[0].MoveCall
	require.Equal(t, NormalizeAddress("0x2"), redeemCall.Package.String())
	require.Equal(t, "balance", redeemCall.Module)
	require.Equal(t, "redeem_funds", redeemCall.Function)
	require.Len(t, redeemCall.TypeArguments, 1)
	require.Equal(t, NormalizeType(USDCType), NormalizeType(redeemCall.TypeArguments[0].String()))
	require.Len(t, redeemCall.Arguments, 1)
	require.NotNil(t, redeemCall.Arguments[0].Input)
	require.Equal(t, uint16(0), *redeemCall.Arguments[0].Input)

	require.NotNil(t, programmable.Commands[1].MoveCall)
	sendCall := programmable.Commands[1].MoveCall
	require.Equal(t, NormalizeAddress("0x2"), sendCall.Package.String())
	require.Equal(t, "balance", sendCall.Module)
	require.Equal(t, "send_funds", sendCall.Function)
	require.Len(t, sendCall.TypeArguments, 1)
	require.Equal(t, NormalizeType(USDCType), NormalizeType(sendCall.TypeArguments[0].String()))
	require.Len(t, sendCall.Arguments, 2)
	require.NotNil(t, sendCall.Arguments[0].Result)
	require.Equal(t, uint16(0), *sendCall.Arguments[0].Result)
	require.NotNil(t, sendCall.Arguments[1].Input)
	require.Equal(t, uint16(1), *sendCall.Arguments[1].Input)
}
