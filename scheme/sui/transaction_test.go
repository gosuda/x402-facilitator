package sui

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	bcs "github.com/iotaledger/bcs-go"
	"github.com/stretchr/testify/require"
)

func TestTransactionBlockDataParsesProgrammableCommands(t *testing.T) {
	raw := []byte(`{
		"transaction": {
			"kind": "ProgrammableTransaction",
			"inputs": [{"Pure": [1, 2, 3]}],
			"transactions": [
				{"MoveCall": {
					"package": "0x2",
					"module": "balance",
					"function": "send_funds",
					"type_arguments": ["` + USDCType + `"],
					"arguments": [{"Input": 0}]
				}},
				{"TransferObjects": {"objects": [{"Result": 0}], "address": {"Input": 1}}},
				{"SplitCoins": {"coin": {"Input": 0}, "amounts": [{"Input": 1}]}},
				{"MergeCoins": {"destination": {"Input": 0}, "sources": [{"Input": 1}]}},
				{"Publish": {"modules": ["AA=="], "dependencies": ["0x1"]}},
				{"MakeMoveVec": {"type": "0x2::sui::SUI", "elements": [{"Input": 0}]}},
				{"Upgrade": {"modules": ["AA=="], "dependencies": ["0x1"], "package": "0x2", "ticket": {"Input": 0}}}
			]
		},
		"gasData": {
			"payment": [],
			"owner": "0xabc",
			"price": "0",
			"budget": "0"
		}
	}`)

	parsed, err := ParseTransactionBlockData(raw)
	require.NoError(t, err)
	require.Equal(t, TransactionKindProgrammable, parsed.Transaction.Kind)
	require.Equal(t, "0", parsed.GasData.Price)
	require.Equal(t, "0", parsed.GasData.Budget)

	commands := TransactionCommands(parsed.Transaction)
	require.Len(t, commands, 7)
	require.Equal(t, CommandKindMoveCall, commands[0].Kind)
	require.Equal(t, "0x2", commands[0].MoveCall.Package)
	require.Equal(t, "balance", commands[0].MoveCall.Module)
	require.Equal(t, "send_funds", commands[0].MoveCall.Function)
	require.Equal(t, []string{USDCType}, commands[0].MoveCall.TypeArguments)
	require.Equal(t, CommandKindTransferObjects, commands[1].Kind)
	require.Equal(t, CommandKindSplitCoins, commands[2].Kind)
	require.Equal(t, CommandKindMergeCoins, commands[3].Kind)
	require.Equal(t, CommandKindPublish, commands[4].Kind)
	require.Equal(t, CommandKindMakeMoveVec, commands[5].Kind)
	require.Equal(t, CommandKindUpgrade, commands[6].Kind)
}

func TestTransactionCommandParsesKindFormAndTarget(t *testing.T) {
	moveCall, err := ParseTransactionCommand([]byte(`{
		"kind": "MoveCall",
		"target": "0x2::balance::send_funds",
		"typeArguments": ["` + USDCType + `"],
		"arguments": []
	}`))
	require.NoError(t, err)
	require.Equal(t, CommandKindMoveCall, moveCall.Kind)
	require.Equal(t, "0x2", moveCall.MoveCall.Package)
	require.Equal(t, "balance", moveCall.MoveCall.Module)
	require.Equal(t, "send_funds", moveCall.MoveCall.Function)
	require.Equal(t, []string{USDCType}, moveCall.MoveCall.TypeArguments)

	splitCoins, err := ParseTransactionCommand([]byte(`{
		"$kind": "SplitCoins",
		"coin": {"Input": 0},
		"amounts": [{"Input": 1}]
	}`))
	require.NoError(t, err)
	require.Equal(t, CommandKindSplitCoins, splitCoins.Kind)
}

func TestTransactionCommandKeepsUnknownWrappedKind(t *testing.T) {
	command, err := ParseTransactionCommand([]byte(`{"MysteryCommand": {"value": 1}}`))
	require.NoError(t, err)
	require.Equal(t, "MysteryCommand", command.Kind)
	require.NotEmpty(t, command.Raw)
}

func TestGaslessStablecoinPaymentRejectsUnsupportedMoveCall(t *testing.T) {
	dryRun := DryRunTransactionBlock{
		Input: TransactionBlockData{
			Transaction: &TransactionKind{
				Transactions: []json.RawMessage{
					moveCallTransactionCommand("0x2", "transfer", "public_transfer", []string{USDCType}),
				},
			},
			GasData: &GasData{
				Payment: nil,
				Price:   "0",
				Budget:  "0",
			},
		},
		Effects: TransactionEffects{Status: &TransactionExecutionStatus{Status: "success"}},
	}

	require.ErrorContains(t, dryRun.ValidateGaslessStablecoinPayment(USDCType), "unsupported Move call")
}

func TestGaslessStablecoinPaymentAllowsCoinObjectCommands(t *testing.T) {
	dryRun := DryRunTransactionBlock{
		Input: TransactionBlockData{
			Transaction: &TransactionKind{
				Transactions: []json.RawMessage{
					splitCoinsTransactionCommand(),
					moveCallTransactionCommand("0x2", "coin", "send_funds", []string{USDCType}),
				},
			},
			GasData: &GasData{
				Payment: nil,
				Price:   "0",
				Budget:  "0",
			},
		},
		Effects: TransactionEffects{
			Status:  &TransactionExecutionStatus{Status: "success"},
			Mutated: []ObjectOwnerResult{{}},
		},
		ObjectChanges: []ObjectChange{{Type: "mutated"}},
	}

	require.NoError(t, dryRun.ValidateGaslessStablecoinPayment(USDCType))
}

func TestGaslessStablecoinPaymentRejectsGasPayment(t *testing.T) {
	dryRun := DryRunTransactionBlock{
		Input: TransactionBlockData{
			Transaction: &TransactionKind{
				Transactions: []json.RawMessage{
					moveCallTransactionCommand("0x2", "balance", "send_funds", []string{USDCType}),
				},
			},
			GasData: &GasData{
				Payment: []ObjectRefResult{{}},
				Price:   "1000",
				Budget:  "1000000",
			},
		},
		Effects: TransactionEffects{Status: &TransactionExecutionStatus{Status: "success"}},
	}

	require.ErrorContains(t, dryRun.ValidateGaslessStablecoinPayment(USDCType), "transaction is not gasless")
}

func TestOwnerAddressRejectsEmptyInputs(t *testing.T) {
	require.Empty(t, NormalizeAddress(""))
	require.Empty(t, NormalizeAddress("0x"))
	require.Empty(t, OwnerAddress(nil))
	require.Empty(t, OwnerAddress(json.RawMessage("null")))
}

func TestSuiAddressAndTypeTagParsing(t *testing.T) {
	address, err := ParseAddress("0xabc")
	require.NoError(t, err)
	require.Equal(t, NormalizeAddress("0xabc"), address.String())

	tag, err := ParseTypeTag("0x2::coin::Coin<" + USDCType + ">")
	require.NoError(t, err)
	require.Equal(t, NormalizeType(NormalizeAddress("0x2")+"::coin::Coin<"+USDCType+">"), NormalizeType(tag.String()))

	vector, err := ParseTypeTag("vector<0x2::sui::SUI>")
	require.NoError(t, err)
	require.Equal(t, NormalizeType("vector<"+NormalizeAddress("0x2")+"::sui::SUI>"), NormalizeType(vector.String()))
}

func TestBuildGaslessStablecoinTransferTransaction(t *testing.T) {
	signer := newTestSigner(t)

	txBytes, err := BuildGaslessStablecoinTransferTransaction(context.Background(), GaslessStablecoinTransfer{
		Sender:     signer.Address(),
		Recipient:  "0xabc",
		Network:    "sui:mainnet",
		Asset:      "USDC",
		Amount:     "1000000",
		Expiration: testValidDuringExpiration(t),
	})
	require.NoError(t, err)
	require.NotEmpty(t, txBytes)
}

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
	require.Len(t, programmable.Commands, 1)
	require.NotNil(t, programmable.Commands[0].MoveCall)
	require.Equal(t, NormalizeType(TestnetUSDCType), NormalizeType(programmable.Commands[0].MoveCall.TypeArguments[0].String()))
}

func TestBuildGaslessStablecoinTransferTransactionUsesCoinObjectWhenProvided(t *testing.T) {
	coinObject := testObjectRef(t, "0x1234", 7)

	txBytes, err := BuildGaslessStablecoinTransferTransaction(context.Background(), GaslessStablecoinTransfer{
		Sender:     "0x123",
		Recipient:  "0xabc",
		Network:    "sui:testnet",
		Asset:      "USDC",
		Amount:     "1000000",
		CoinObject: &coinObject,
		Expiration: testValidDuringExpiration(t),
	})
	require.NoError(t, err)

	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	require.NoError(t, err)
	require.NotNil(t, txData.V1)
	require.Empty(t, txData.V1.GasData.Payment)
	require.Equal(t, uint64(0), txData.V1.GasData.Price)
	require.Equal(t, uint64(0), txData.V1.GasData.Budget)
	require.NotNil(t, txData.V1.Expiration.ValidDuring)

	programmable := txData.V1.Kind.ProgrammableTransaction
	require.NotNil(t, programmable)
	require.Len(t, programmable.Inputs, 3)
	require.NotNil(t, programmable.Inputs[0].Object)
	require.Equal(t, coinObject, *programmable.Inputs[0].Object.ImmOrOwnedObject)
	require.NotNil(t, programmable.Inputs[1].Pure)
	require.NotNil(t, programmable.Inputs[2].Pure)

	require.Len(t, programmable.Commands, 2)
	require.NotNil(t, programmable.Commands[0].SplitCoins)
	require.NotNil(t, programmable.Commands[1].MoveCall)
	moveCall := programmable.Commands[1].MoveCall
	require.Equal(t, "coin", moveCall.Module)
	require.Equal(t, "send_funds", moveCall.Function)
	require.Equal(t, NormalizeType(TestnetUSDCType), NormalizeType(moveCall.TypeArguments[0].String()))
	require.NotNil(t, moveCall.Arguments[0].NestedResult)
	require.Equal(t, uint16(0), moveCall.Arguments[0].NestedResult.Index)
	require.Equal(t, uint16(0), moveCall.Arguments[0].NestedResult.ResultIndex)
	require.NotNil(t, moveCall.Arguments[1].Input)
	require.Equal(t, uint16(2), *moveCall.Arguments[1].Input)
}

func TestBuildCoinObjectToAddressBalanceTransferTransactionSendsWholeCoinObject(t *testing.T) {
	coinObject := testObjectRef(t, "0x1234", 7)

	txBytes, err := BuildCoinObjectToAddressBalanceTransferTransaction(context.Background(), CoinObjectToAddressBalanceTransfer{
		Sender:     "0x123",
		Recipient:  "0xabc",
		Network:    "sui:testnet",
		Asset:      "USDC",
		CoinObject: coinObject,
	})
	require.NoError(t, err)

	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	require.NoError(t, err)
	require.NotNil(t, txData.V1)
	require.Equal(t, NormalizeAddress("0x123"), txData.V1.Sender.String())
	require.Empty(t, txData.V1.GasData.Payment)
	require.Equal(t, uint64(0), txData.V1.GasData.Price)
	require.Equal(t, uint64(0), txData.V1.GasData.Budget)
	require.NotNil(t, txData.V1.Expiration.None)

	programmable := txData.V1.Kind.ProgrammableTransaction
	require.NotNil(t, programmable)
	require.Len(t, programmable.Inputs, 2)
	require.NotNil(t, programmable.Inputs[0].Object)
	require.Equal(t, coinObject, *programmable.Inputs[0].Object.ImmOrOwnedObject)
	require.NotNil(t, programmable.Inputs[1].Pure)

	require.Len(t, programmable.Commands, 1)
	require.NotNil(t, programmable.Commands[0].MoveCall)
	moveCall := programmable.Commands[0].MoveCall
	require.Equal(t, NormalizeAddress("0x2"), moveCall.Package.String())
	require.Equal(t, "coin", moveCall.Module)
	require.Equal(t, "send_funds", moveCall.Function)
	require.Equal(t, NormalizeType(TestnetUSDCType), NormalizeType(moveCall.TypeArguments[0].String()))
	require.Len(t, moveCall.Arguments, 2)
	require.NotNil(t, moveCall.Arguments[0].Input)
	require.Equal(t, uint16(0), *moveCall.Arguments[0].Input)
	require.NotNil(t, moveCall.Arguments[1].Input)
	require.Equal(t, uint16(1), *moveCall.Arguments[1].Input)
}

func TestBuildCoinObjectToAddressBalanceTransferTransactionSplitsAmount(t *testing.T) {
	coinObject := testObjectRef(t, "0x1234", 7)
	gasObject := testObjectRef(t, "0x5678", 8)

	txBytes, err := BuildCoinObjectToAddressBalanceTransferTransaction(context.Background(), CoinObjectToAddressBalanceTransfer{
		Sender:     "0x123",
		Recipient:  "0xabc",
		Network:    "sui:mainnet",
		Asset:      "USDC",
		CoinObject: coinObject,
		Amount:     "1000000",
		GasPayment: []ObjectRef{gasObject},
		GasPrice:   1000,
		GasBudget:  5000000,
	})
	require.NoError(t, err)

	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	require.NoError(t, err)
	require.NotNil(t, txData.V1)
	require.Equal(t, []ObjectRef{gasObject}, txData.V1.GasData.Payment)
	require.Equal(t, uint64(1000), txData.V1.GasData.Price)
	require.Equal(t, uint64(5000000), txData.V1.GasData.Budget)
	require.NotNil(t, txData.V1.Expiration.None)

	programmable := txData.V1.Kind.ProgrammableTransaction
	require.NotNil(t, programmable)
	require.Len(t, programmable.Inputs, 3)
	require.NotNil(t, programmable.Inputs[0].Object)
	require.NotNil(t, programmable.Inputs[1].Pure)
	require.NotNil(t, programmable.Inputs[2].Pure)

	require.Len(t, programmable.Commands, 2)
	require.NotNil(t, programmable.Commands[0].SplitCoins)
	split := programmable.Commands[0].SplitCoins
	require.NotNil(t, split.Coin.Input)
	require.Equal(t, uint16(0), *split.Coin.Input)
	require.Len(t, split.Amounts, 1)
	require.NotNil(t, split.Amounts[0].Input)
	require.Equal(t, uint16(1), *split.Amounts[0].Input)

	require.NotNil(t, programmable.Commands[1].MoveCall)
	moveCall := programmable.Commands[1].MoveCall
	require.Equal(t, "coin", moveCall.Module)
	require.Equal(t, "send_funds", moveCall.Function)
	require.Equal(t, NormalizeType(USDCType), NormalizeType(moveCall.TypeArguments[0].String()))
	require.Len(t, moveCall.Arguments, 2)
	require.NotNil(t, moveCall.Arguments[0].NestedResult)
	require.Equal(t, uint16(0), moveCall.Arguments[0].NestedResult.Index)
	require.Equal(t, uint16(0), moveCall.Arguments[0].NestedResult.ResultIndex)
	require.NotNil(t, moveCall.Arguments[1].Input)
	require.Equal(t, uint16(2), *moveCall.Arguments[1].Input)
}

func TestBuildCoinObjectToAddressBalanceTransferTransactionRejectsNonAllowlistedGaslessAsset(t *testing.T) {
	_, err := BuildCoinObjectToAddressBalanceTransferTransaction(context.Background(), CoinObjectToAddressBalanceTransfer{
		Sender:     "0x123",
		Recipient:  "0xabc",
		Network:    "sui:mainnet",
		Asset:      "0x2::sui::SUI",
		CoinObject: testObjectRef(t, "0x1234", 7),
	})
	require.ErrorContains(t, err, "gasless stablecoin allowlisted asset")
}

func TestNewGaslessStablecoinPaymentPayload(t *testing.T) {
	signer := newTestSigner(t)

	payload, err := NewGaslessStablecoinPaymentPayloadFromTransfer(context.Background(), GaslessStablecoinTransfer{
		Recipient:  "0xabc",
		Network:    "sui:mainnet",
		Asset:      "USDC",
		Amount:     "1000000",
		Expiration: testValidDuringExpiration(t),
	}, signer)
	require.NoError(t, err)
	require.NotEmpty(t, payload.Transaction)
	require.NotEmpty(t, payload.Signature)

	txBytes, err := payload.DecodeTransaction()
	require.NoError(t, err)
	payer, err := VerifySignature(payload.Signature, txBytes)
	require.NoError(t, err)
	require.Equal(t, signer.Address(), payer)
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

func TestNewGaslessStablecoinPaymentPayloadFromPrivateKeyHex(t *testing.T) {
	privateKey := make([]byte, 32)
	for i := range privateKey {
		privateKey[i] = byte(i + 1)
	}

	payload, err := NewGaslessStablecoinPaymentPayloadFromTransferAndPrivateKeyHex(
		context.Background(),
		GaslessStablecoinTransfer{
			Recipient:  "0xabc",
			Network:    "sui:mainnet",
			Asset:      "USDC",
			Amount:     "1000000",
			Expiration: testValidDuringExpiration(t),
		},
		hex.EncodeToString(privateKey),
	)
	require.NoError(t, err)
	require.NotEmpty(t, payload.Transaction)
	require.NotEmpty(t, payload.Signature)
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

func moveCallTransactionCommand(pkg string, module string, function string, typeArguments []string) json.RawMessage {
	raw, err := json.Marshal(map[string]interface{}{
		"MoveCall": map[string]interface{}{
			"package":        pkg,
			"module":         module,
			"function":       function,
			"type_arguments": typeArguments,
		},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func splitCoinsTransactionCommand() json.RawMessage {
	raw, err := json.Marshal(map[string]interface{}{
		"SplitCoins": map[string]interface{}{
			"coin":    map[string]interface{}{"Input": 0},
			"amounts": []map[string]interface{}{{"Input": 1}},
		},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func testValidDuringExpiration(t *testing.T) *TransactionExpiration {
	t.Helper()
	expiration, err := TransactionExpirationValidDuring("4btiuiMPvEENsttpZC7CZ53DruC3MAgfznDbASZ7DR6S", 1142, 1143, 7)
	require.NoError(t, err)
	return expiration
}

func testObjectRef(t *testing.T, objectID string, version uint64) ObjectRef {
	t.Helper()
	parsedObjectID, err := ParseAddress(objectID)
	require.NoError(t, err)
	digest, err := ParseDigest("11111111111111111111111111111111")
	require.NoError(t, err)
	return ObjectRef{
		ObjectID: parsedObjectID,
		Version:  version,
		Digest:   digest,
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

	require.Len(t, programmable.Commands, 1)
	require.NotNil(t, programmable.Commands[0].MoveCall)
	moveCall := programmable.Commands[0].MoveCall
	require.Equal(t, NormalizeAddress("0x2"), moveCall.Package.String())
	require.Equal(t, "balance", moveCall.Module)
	require.Equal(t, "send_funds", moveCall.Function)
	require.Len(t, moveCall.TypeArguments, 1)
	require.Equal(t, NormalizeType(USDCType), NormalizeType(moveCall.TypeArguments[0].String()))
	require.Len(t, moveCall.Arguments, 2)
	require.NotNil(t, moveCall.Arguments[0].Input)
	require.Equal(t, uint16(0), *moveCall.Arguments[0].Input)
	require.NotNil(t, moveCall.Arguments[1].Input)
	require.Equal(t, uint16(1), *moveCall.Arguments[1].Input)
}
