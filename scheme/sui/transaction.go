package sui

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"

	bcs "github.com/iotaledger/bcs-go"
)

func BuildGaslessStablecoinTransferTransaction(ctx context.Context, transfer GaslessStablecoinTransfer) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sender := NormalizeAddress(transfer.Sender)
	if sender == "" {
		return nil, errors.New("empty sender")
	}
	recipient := NormalizeAddress(transfer.Recipient)
	if recipient == "" {
		return nil, errors.New("empty recipient")
	}
	coinType, err := resolveGaslessStablecoinAsset(transfer.Network, transfer.Asset)
	if err != nil {
		return nil, err
	}

	amount, err := strconv.ParseUint(strings.TrimSpace(transfer.Amount), 10, 64)
	if err != nil || amount == 0 {
		return nil, fmt.Errorf("invalid amount: %s", transfer.Amount)
	}
	expiration := TransactionExpiration{}
	if transfer.Expiration != nil {
		expiration = *transfer.Expiration
	} else {
		resolved, err := ResolveGaslessStablecoinExpiration(ctx, transfer.Network, transfer.Endpoints)
		if err != nil {
			return nil, err
		}
		expiration = *resolved
	}

	senderAddress, err := ParseAddress(sender)
	if err != nil {
		return nil, err
	}
	recipientAddress, err := ParseAddress(recipient)
	if err != nil {
		return nil, err
	}
	coinTypeTag, err := ParseTypeTag(coinType)
	if err != nil {
		return nil, err
	}
	packageAddress, err := ParseAddress("0x2")
	if err != nil {
		return nil, err
	}
	recipientBytes, err := bcs.Marshal(&recipientAddress)
	if err != nil {
		return nil, err
	}

	withdrawalInput := uint16(0)
	recipientInput := uint16(1)
	redeemResult := uint16(0)
	txData := gaslessStablecoinTransactionData{
		V1: &gaslessStablecoinTransactionDataV1{
			Kind: gaslessStablecoinTransactionKind{
				ProgrammableTransaction: &gaslessStablecoinProgrammableTransaction{
					Inputs: []gaslessStablecoinCallArg{
						{
							FundsWithdrawal: &gaslessStablecoinFundsWithdrawal{
								Reservation: gaslessStablecoinReservation{
									MaxAmountU64: &amount,
								},
								TypeArg: gaslessStablecoinWithdrawalType{
									Balance: &coinTypeTag,
								},
								WithdrawFrom: gaslessStablecoinWithdrawFrom{
									Sender: &struct{}{},
								},
							},
						},
						{
							Pure: &Pure{
								Bytes: recipientBytes,
							},
						},
					},
					Commands: []Command{
						{
							MoveCall: &ProgrammableMoveCall{
								Package:       packageAddress,
								Module:        "balance",
								Function:      "redeem_funds",
								TypeArguments: []TypeTag{coinTypeTag},
								Arguments: []Argument{
									{Input: &withdrawalInput},
								},
							},
						},
						{
							MoveCall: &ProgrammableMoveCall{
								Package:       packageAddress,
								Module:        "balance",
								Function:      "send_funds",
								TypeArguments: []TypeTag{coinTypeTag},
								Arguments: []Argument{
									{Result: &redeemResult},
									{Input: &recipientInput},
								},
							},
						},
					},
				},
			},
			Sender: senderAddress,
			GasData: gaslessStablecoinGasData{
				Payment: []ObjectRef{},
				Owner:   senderAddress,
				Price:   0,
				Budget:  0,
			},
			Expiration: expiration,
		},
	}

	txBytes, err := bcs.Marshal(&txData)
	if err != nil {
		return nil, err
	}
	return txBytes, nil
}

func BuildCoinObjectsToAddressBalanceTransferTransaction(ctx context.Context, transfer CoinObjectsToAddressBalanceTransfer) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sender := NormalizeAddress(transfer.Sender)
	if sender == "" {
		return nil, errors.New("empty sender")
	}
	recipient := NormalizeAddress(transfer.Recipient)
	if recipient == "" {
		return nil, errors.New("empty recipient")
	}
	coinType, err := resolveGaslessStablecoinAsset(transfer.Network, transfer.Asset)
	if err != nil {
		return nil, err
	}
	if len(transfer.CoinObjects) == 0 {
		return nil, errors.New("coin objects are required")
	}

	expiration := TransactionExpiration{}
	if transfer.Expiration != nil {
		expiration = *transfer.Expiration
	} else {
		resolved, err := ResolveGaslessStablecoinExpiration(ctx, transfer.Network, transfer.Endpoints)
		if err != nil {
			return nil, err
		}
		expiration = *resolved
	}

	coinObjects := make([]ObjectRef, 0, len(transfer.CoinObjects))
	coinObjectIDs := make(map[Address]struct{}, len(transfer.CoinObjects))
	for i, object := range transfer.CoinObjects {
		if err := validateObjectRef(fmt.Sprintf("coin object %d", i), object.ObjectRef); err != nil {
			return nil, err
		}
		if _, ok := coinObjectIDs[object.ObjectRef.ObjectID]; ok {
			return nil, fmt.Errorf("duplicate coin object %s", object.ObjectRef.ObjectID.String())
		}
		coinObjectIDs[object.ObjectRef.ObjectID] = struct{}{}
		if strings.TrimSpace(object.CoinType) != "" && normalizeSuiType(object.CoinType) != normalizeSuiType(coinType) {
			return nil, fmt.Errorf("coin object %d type %q does not match %q", i, object.CoinType, coinType)
		}
		coinObjects = append(coinObjects, object.ObjectRef)
	}

	senderAddress, err := ParseAddress(sender)
	if err != nil {
		return nil, err
	}
	recipientAddress, err := ParseAddress(recipient)
	if err != nil {
		return nil, err
	}
	coinTypeTag, err := ParseTypeTag(coinType)
	if err != nil {
		return nil, err
	}

	if len(coinObjects) >= 1<<16 {
		return nil, fmt.Errorf("too many coin objects: %d", len(coinObjects))
	}
	packageAddress, err := ParseAddress("0x2")
	if err != nil {
		return nil, err
	}
	recipientBytes, err := bcs.Marshal(&recipientAddress)
	if err != nil {
		return nil, err
	}

	inputs := make([]gaslessStablecoinCallArg, 0, len(coinObjects)+1)
	for _, coinObject := range coinObjects {
		objectRef := coinObject
		inputs = append(inputs, gaslessStablecoinCallArg{
			Object: &ObjectArg{
				ImmOrOwnedObject: &objectRef,
			},
		})
	}
	inputs = append(inputs, gaslessStablecoinCallArg{
		Pure: &Pure{
			Bytes: recipientBytes,
		},
	})

	recipientInput := uint16(len(coinObjects))
	commands := make([]Command, 0, len(coinObjects))
	for i := range coinObjects {
		coinInput := uint16(i)
		commands = append(commands, Command{
			MoveCall: &ProgrammableMoveCall{
				Package:       packageAddress,
				Module:        "coin",
				Function:      "send_funds",
				TypeArguments: []TypeTag{coinTypeTag},
				Arguments: []Argument{
					{Input: &coinInput},
					{Input: &recipientInput},
				},
			},
		})
	}

	txData := gaslessStablecoinTransactionData{
		V1: &gaslessStablecoinTransactionDataV1{
			Kind: gaslessStablecoinTransactionKind{
				ProgrammableTransaction: &gaslessStablecoinProgrammableTransaction{
					Inputs:   inputs,
					Commands: commands,
				},
			},
			Sender: senderAddress,
			GasData: gaslessStablecoinGasData{
				Payment: []ObjectRef{},
				Owner:   senderAddress,
				Price:   0,
				Budget:  0,
			},
			Expiration: expiration,
		},
	}

	txBytes, err := bcs.Marshal(&txData)
	if err != nil {
		return nil, err
	}
	return txBytes, nil
}

func ExecuteGaslessStablecoinObjectBalancePayment(ctx context.Context, payment GaslessStablecoinObjectBalancePayment, signer Signer) (*GaslessStablecoinObjectBalancePaymentResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if signer == nil {
		return nil, errors.New("nil signer")
	}

	sender := NormalizeAddress(payment.Sender)
	if sender == "" {
		sender = signer.Address()
	}
	if sender == "" {
		return nil, errors.New("empty sender")
	}
	if NormalizeAddress(signer.Address()) != sender {
		return nil, fmt.Errorf("signer address %s does not match sender %s", signer.Address(), sender)
	}
	recipient := NormalizeAddress(payment.Recipient)
	if recipient == "" {
		return nil, errors.New("empty recipient")
	}

	paymentAmount, err := strconv.ParseUint(strings.TrimSpace(payment.Amount), 10, 64)
	if err != nil || paymentAmount == 0 {
		return nil, fmt.Errorf("invalid amount: %s", payment.Amount)
	}

	coinType, err := resolveGaslessStablecoinAsset(payment.Network, payment.Asset)
	if err != nil {
		return nil, err
	}

	result := &GaslessStablecoinObjectBalancePaymentResult{
		PaymentAmount:  strconv.FormatUint(paymentAmount, 10),
		PreparedAmount: "0",
	}

	client, err := NewClientForNetwork(payment.Network, payment.Endpoints)
	if err != nil {
		return nil, err
	}

	balance, err := client.Balance(ctx, sender, coinType)
	if err != nil {
		return nil, err
	}
	var nonZeroCoinObjects []OwnedCoinObject
	var prepareAmount uint64
	if balance.AddressBalance < paymentAmount {
		if balance.CoinBalance < paymentAmount-balance.AddressBalance {
			return nil, fmt.Errorf("insufficient %s balance: need %d, address balance %d, coin object balance %d", coinType, paymentAmount, balance.AddressBalance, balance.CoinBalance)
		}

		coinObjects, err := client.ListOwnedCoinObjects(ctx, sender, coinType)
		if err != nil {
			return nil, err
		}
		nonZeroCoinObjects = make([]OwnedCoinObject, 0, len(coinObjects))
		for _, coinObject := range coinObjects {
			if coinObject.Balance == 0 {
				continue
			}
			nonZeroCoinObjects = append(nonZeroCoinObjects, coinObject)
		}
		result.CoinObjects = nonZeroCoinObjects

		for _, coinObject := range nonZeroCoinObjects {
			if prepareAmount > ^uint64(0)-coinObject.Balance {
				return nil, errors.New("coin object balance sum overflows uint64")
			}
			prepareAmount += coinObject.Balance
		}

		if prepareAmount < paymentAmount-balance.AddressBalance {
			return nil, fmt.Errorf("insufficient %s balance: need %d, address balance %d, coin object balance %d", coinType, paymentAmount, balance.AddressBalance, prepareAmount)
		}
	}

	info := GetNetworkInfo(payment.Network)
	if info == nil {
		return nil, fmt.Errorf("unsupported Sui network %q", payment.Network)
	}
	expiration, err := client.ResolveGaslessStablecoinExpiration(ctx, info.ChainDigest)
	if err != nil {
		return nil, err
	}

	if prepareAmount > 0 {
		result.PreparedAmount = strconv.FormatUint(prepareAmount, 10)
		prepareTxBytes, err := BuildCoinObjectsToAddressBalanceTransferTransaction(ctx, CoinObjectsToAddressBalanceTransfer{
			Sender:      sender,
			Recipient:   sender,
			Network:     payment.Network,
			Asset:       payment.Asset,
			CoinObjects: nonZeroCoinObjects,
			Endpoints:   payment.Endpoints,
			Expiration:  expiration,
		})
		if err != nil {
			return nil, err
		}
		preparePayload, err := NewSignedPayload(prepareTxBytes, signer)
		if err != nil {
			return nil, err
		}
		prepareResult, err := client.ExecuteTransactionBlock(ctx, preparePayload.Transaction, []string{preparePayload.Signature})
		if err != nil {
			return nil, err
		}
		if !prepareResult.IsSuccess() {
			return nil, fmt.Errorf("prepare transaction failed: %s", TransactionResultStatusError(prepareResult, "transaction failed"))
		}
		result.PrepareTransaction = prepareResult
	}

	paymentTxBytes, err := BuildGaslessStablecoinTransferTransaction(ctx, GaslessStablecoinTransfer{
		Sender:     sender,
		Recipient:  recipient,
		Network:    payment.Network,
		Asset:      payment.Asset,
		Amount:     result.PaymentAmount,
		Endpoints:  payment.Endpoints,
		Expiration: expiration,
	})
	if err != nil {
		return nil, err
	}
	paymentPayload, err := NewSignedPayload(paymentTxBytes, signer)
	if err != nil {
		return nil, err
	}
	paymentResult, err := client.ExecuteTransactionBlock(ctx, paymentPayload.Transaction, []string{paymentPayload.Signature})
	if err != nil {
		return nil, err
	}
	if !paymentResult.IsSuccess() {
		return nil, fmt.Errorf("payment transaction failed: %s", TransactionResultStatusError(paymentResult, "transaction failed"))
	}
	result.PaymentTransaction = paymentResult

	return result, nil
}

func ResolveGaslessStablecoinExpiration(ctx context.Context, network string, endpoints []string) (*TransactionExpiration, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info := GetNetworkInfo(network)
	if info == nil {
		return nil, fmt.Errorf("unsupported Sui network %q", network)
	}

	client, err := NewClientForNetwork(network, endpoints)
	if err != nil {
		return nil, err
	}
	return client.ResolveGaslessStablecoinExpiration(ctx, info.ChainDigest)
}

func ListOwnedGaslessStablecoinCoinObjects(ctx context.Context, network string, owner string, asset string, endpoints []string) ([]OwnedCoinObject, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	owner = NormalizeAddress(owner)
	if owner == "" {
		return nil, errors.New("empty owner")
	}
	coinType, err := resolveGaslessStablecoinAsset(network, asset)
	if err != nil {
		return nil, err
	}

	client, err := NewClientForNetwork(network, endpoints)
	if err != nil {
		return nil, err
	}
	return client.ListOwnedCoinObjects(ctx, owner, coinType)
}

func ExecuteSignedTransactionBlock(ctx context.Context, network string, endpoints []string, payload *Payload) (*ExecuteTransactionBlock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, errors.New("nil payload")
	}
	if strings.TrimSpace(payload.Transaction) == "" {
		return nil, ErrEmptyTransaction
	}
	if strings.TrimSpace(payload.Signature) == "" {
		return nil, ErrEmptySignature
	}

	client, err := NewClientForNetwork(network, endpoints)
	if err != nil {
		return nil, err
	}
	return client.ExecuteTransactionBlock(ctx, payload.Transaction, []string{payload.Signature})
}

func randomUint32() (uint32, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

func resolveGaslessStablecoinAsset(network string, asset string) (string, error) {
	asset = strings.TrimSpace(asset)
	if asset == "" {
		return "", errors.New("empty asset")
	}
	if network != "" {
		coinType, ok := GetGaslessStablecoinType(network, asset)
		if !ok {
			return "", fmt.Errorf("asset is not gasless stablecoin allowlisted on %s: %s", network, asset)
		}
		return coinType, nil
	}

	normalizedAsset := NormalizeType(asset)
	for symbol, coinType := range defaultStablecoinTypesBySymbol() {
		if NormalizeType(symbol) == normalizedAsset || NormalizeType(coinType) == normalizedAsset {
			return coinType, nil
		}
	}
	return "", fmt.Errorf("asset is not gasless stablecoin allowlisted: %s", asset)
}
