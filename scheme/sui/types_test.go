package sui

import (
	"encoding/json"
	"testing"

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

func TestDryRunTransactionBlockParsesNumericObjectVersions(t *testing.T) {
	raw := []byte(`{
		"input": {
			"transaction": {
				"transactions": [
					{"MoveCall": {
						"package": "0x2",
						"module": "coin",
						"function": "send_funds",
						"type_arguments": ["` + USDCType + `"]
					}}
				]
			},
			"gasData": {
				"payment": [{"objectId": "0x9", "version": 9, "digest": "11111111111111111111111111111111"}],
				"price": "0",
				"budget": "0"
			}
		},
		"effects": {
			"status": {"status": "success"},
			"mutated": [{
				"reference": {"objectId": "0x1", "version": 7, "digest": "11111111111111111111111111111111"}
			}],
			"deleted": [{"objectId": "0x2", "version": "8", "digest": "11111111111111111111111111111111"}]
		}
	}`)

	parsed, err := ParseDryRunTransactionBlock(raw)
	require.NoError(t, err)
	require.NotNil(t, parsed.Effects.Mutated[0].Reference)
	require.Equal(t, ObjectVersion("7"), parsed.Effects.Mutated[0].Reference.Version)
	require.Equal(t, ObjectVersion("8"), parsed.Effects.Deleted[0].Version)
	require.Equal(t, ObjectVersion("9"), parsed.Input.GasData.Payment[0].Version)
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

func TestGaslessStablecoinPaymentRejectsCoinObjectCommands(t *testing.T) {
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

	require.ErrorContains(t, dryRun.ValidateGaslessStablecoinPayment(USDCType), "transaction writes objects")
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
