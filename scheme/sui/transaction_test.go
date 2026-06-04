package sui

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bcs "github.com/iotaledger/bcs-go"
	"github.com/stretchr/testify/require"
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
	privateKey := ""
	from := ""
	to := ""
	rpcURL := "https://sui-testnet-rpc.publicnode.com"
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
	require.Equal(t, paymentAmount, TransactionResultBalanceDelta(result.PaymentTransaction, to, TestnetUSDCType).String())
	fmt.Printf("sent %s testnet USDC address balance to %s: %s\n", paymentAmount, NormalizeAddress(to), result.PaymentTransaction.Digest.String())
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

func TestResolveGaslessStablecoinExpirationUsesRPC(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rpcReq suiTransactionRPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rpcReq))
		methods = append(methods, rpcReq.Method)

		switch rpcReq.Method {
		case "suix_getLatestSuiSystemState":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]interface{}{
					"epoch": "42",
				},
			}))
		default:
			t.Fatalf("unexpected rpc method %s", rpcReq.Method)
		}
	}))
	defer server.Close()

	expiration, err := ResolveGaslessStablecoinExpiration(context.Background(), "sui:mainnet", []string{"http://127.0.0.1:1", server.URL})
	require.NoError(t, err)
	require.NotNil(t, expiration.ValidDuring)
	require.Equal(t, uint64(42), *expiration.ValidDuring.MinEpoch)
	require.Equal(t, uint64(43), *expiration.ValidDuring.MaxEpoch)
	require.Len(t, expiration.ValidDuring.Chain, 32)
	require.Equal(t, []string{"suix_getLatestSuiSystemState"}, methods)
}

func TestListOwnedGaslessStablecoinCoinObjects(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rpcReq suiTransactionRPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rpcReq))
		methods = append(methods, rpcReq.Method)

		switch rpcReq.Method {
		case "suix_getCoins":
			require.Len(t, rpcReq.Params, 4)
			require.Equal(t, NormalizeAddress("0x123"), rpcReq.Params[0])
			require.Equal(t, TestnetUSDCType, rpcReq.Params[1])
			require.Nil(t, rpcReq.Params[2])
			require.Equal(t, float64(suiCoinObjectPageLimit), rpcReq.Params[3])
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]interface{}{
					"data": []map[string]interface{}{
						suiCoinObjectRPCResult("0x1234", "7", "400000", TestnetUSDCType),
						suiCoinObjectRPCResult("0x5678", "8", "700000", TestnetUSDCType),
					},
					"nextCursor":  nil,
					"hasNextPage": false,
				},
			}))
		default:
			t.Fatalf("unexpected rpc method %s", rpcReq.Method)
		}
	}))
	defer server.Close()

	coinObjects, err := ListOwnedGaslessStablecoinCoinObjects(context.Background(), "sui:testnet", "0x123", "USDC", []string{server.URL})
	require.NoError(t, err)
	require.Equal(t, []string{"suix_getCoins"}, methods)
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

func suiCoinObjectRPCResult(objectID string, version string, balance string, coinType string) map[string]interface{} {
	return map[string]interface{}{
		"coinType":     coinType,
		"coinObjectId": objectID,
		"version":      version,
		"digest":       "11111111111111111111111111111111",
		"balance":      balance,
	}
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
