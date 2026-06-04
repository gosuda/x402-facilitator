package sui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	suigrpc "github.com/gosuda/x402-facilitator/scheme/sui/grpc"
	rpcv2 "github.com/gosuda/x402-facilitator/scheme/sui/grpc/pb/sui/rpc/v2"
	"github.com/gosuda/x402-facilitator/utils"
	bcs "github.com/iotaledger/bcs-go"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Client owns Sui endpoint failover, gRPC transport calls, and response conversion.
type Client struct {
	mu        sync.RWMutex
	url       string
	endpoints []string
	grpc      *suigrpc.GRPCClient

	addressBalanceCoinObjectCache sync.Map
}

type BalanceSummary struct {
	Balance        uint64
	AddressBalance uint64
	CoinBalance    uint64
}

func NewClientWithEndpoints(url string, endpoints []string) *Client {
	candidates := utils.EndpointCandidates(append([]string{url}, endpoints...))
	activeURL := ""
	if len(candidates) > 0 {
		activeURL = candidates[0]
	}
	return &Client{
		url:       activeURL,
		endpoints: candidates,
		grpc:      suigrpc.NewGRPCClient(),
	}
}

func NewClientForNetwork(network string, endpoints []string) (*Client, error) {
	candidates, err := endpointCandidatesForNetwork(network, endpoints)
	if err != nil {
		return nil, err
	}
	return NewClientWithEndpoints("", candidates), nil
}

func (c *Client) ActiveEndpoint() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.url
}

func (c *Client) Close() error {
	if c == nil || c.grpc == nil {
		return nil
	}
	return c.grpc.Close()
}

func (c *Client) DryRunTransactionBlock(ctx context.Context, txBytesBase64 string) (*DryRunTransactionBlock, error) {
	txBytes, err := decodeTransactionBase64(txBytesBase64)
	if err != nil {
		return nil, err
	}
	var result *DryRunTransactionBlock
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.SimulateTransaction(ctx, endpoint, txBytes)
		if err != nil {
			return err
		}
		result, err = dryRunTransactionBlockFromGRPC(txBytes, response)
		return err
	}); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("missing Sui transaction dry run result")
	}
	return result, nil
}

func (c *Client) ExecuteTransactionBlock(ctx context.Context, txBytesBase64 string, signatures []string) (*ExecuteTransactionBlock, error) {
	txBytes, err := decodeTransactionBase64(txBytesBase64)
	if err != nil {
		return nil, err
	}
	signatureBytes, err := decodeSignaturesBase64(signatures)
	if err != nil {
		return nil, err
	}

	var result *ExecuteTransactionBlock
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.ExecuteTransaction(ctx, endpoint, txBytes, signatureBytes)
		if err != nil {
			return err
		}
		result, err = executeTransactionBlockFromGRPC(response.GetTransaction())
		return err
	}); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("missing Sui transaction execution result")
	}
	return result, nil
}

func (c *Client) GetTransactionBlock(ctx context.Context, digest string) (*ExecuteTransactionBlock, error) {
	var result *ExecuteTransactionBlock
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.GetTransaction(ctx, endpoint, digest)
		if err != nil {
			return err
		}
		result, err = executeTransactionBlockFromGRPC(response.GetTransaction())
		return err
	}); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("missing Sui transaction")
	}
	return result, nil
}

func (c *Client) VerifyZkLoginSignature(ctx context.Context, author string, txBytes []byte, signature string) (bool, error) {
	var result bool
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		ok, err := c.grpc.VerifySignature(ctx, endpoint, author, txBytes, signature)
		if err != nil {
			return err
		}
		result = ok
		return nil
	}); err != nil {
		return false, err
	}
	return result, nil
}

func (c *Client) ListOwnedCoinObjects(ctx context.Context, owner string, coinType string) ([]OwnedCoinObject, error) {
	var coinObjects []OwnedCoinObject
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		fetched, err := c.listOwnedCoinObjectsFromEndpoint(ctx, endpoint, owner, coinType)
		if err != nil {
			return err
		}
		filtered, err := c.excludeAddressBalanceCoinObjects(ctx, endpoint, fetched)
		if err != nil {
			return err
		}
		coinObjects = filtered
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list Sui coin objects: %w", err)
	}
	return coinObjects, nil
}

func (c *Client) Balance(ctx context.Context, owner string, coinType string) (BalanceSummary, error) {
	var balance BalanceSummary
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		response, err := c.grpc.GetBalance(ctx, endpoint, owner, coinType)
		if err != nil {
			return err
		}
		if value := response.GetBalance(); value != nil {
			balance = BalanceSummary{
				Balance:        value.GetBalance(),
				AddressBalance: value.GetAddressBalance(),
				CoinBalance:    value.GetCoinBalance(),
			}
		}
		return nil
	}); err != nil {
		return BalanceSummary{}, fmt.Errorf("failed to get Sui balance: %w", err)
	}
	return balance, nil
}

func (c *Client) AddressBalance(ctx context.Context, owner string, coinType string) (uint64, error) {
	balance, err := c.Balance(ctx, owner, coinType)
	if err != nil {
		return 0, err
	}
	return balance.AddressBalance, nil
}

func (c *Client) ResolveGaslessStablecoinExpiration(ctx context.Context, chainDigest string) (*TransactionExpiration, error) {
	var expiration *TransactionExpiration
	if err := c.call(ctx, func(ctx context.Context, endpoint string) error {
		resolved, err := c.resolveGaslessStablecoinExpirationFromEndpoint(ctx, endpoint, chainDigest)
		if err != nil {
			return err
		}
		expiration = resolved
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to resolve gasless stablecoin expiration: %w", err)
	}
	if expiration == nil {
		return nil, errors.New("failed to resolve gasless stablecoin expiration")
	}
	return expiration, nil
}

func (c *Client) call(ctx context.Context, action func(context.Context, string) error) error {
	if c == nil {
		return errors.New("nil Sui client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	candidates := c.endpointCandidates()
	if len(candidates) == 0 {
		return errors.New("no Sui gRPC endpoints")
	}
	selected, err := utils.DoWithEndpoint(ctx, candidates, func(ctx context.Context, endpoint string) error {
		return action(ctx, endpoint)
	})
	if err != nil {
		return err
	}
	c.setActiveEndpoint(selected)
	return nil
}

func (c *Client) endpointCandidates() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return utils.EndpointCandidates(append([]string{c.url}, c.endpoints...))
}

func (c *Client) setActiveEndpoint(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.url = endpoint
	c.endpoints = utils.EndpointCandidates(append([]string{endpoint}, c.endpoints...))
}

func (c *Client) excludeAddressBalanceCoinObjects(ctx context.Context, endpoint string, coinObjects []OwnedCoinObject) ([]OwnedCoinObject, error) {
	filtered := make([]OwnedCoinObject, 0, len(coinObjects))
	settlementTransactions := make(map[string]bool)
	for _, coinObject := range coinObjects {
		objectID := coinObject.ObjectRef.ObjectID.String()
		if _, ok := c.addressBalanceCoinObjectCache.Load(objectID); ok {
			continue
		}

		previousTransaction := strings.TrimSpace(coinObject.PreviousTransaction)
		if previousTransaction == "" {
			filtered = append(filtered, coinObject)
			continue
		}

		isSettlement, ok := settlementTransactions[previousTransaction]
		if !ok {
			var err error
			isSettlement, err = c.isAddressBalanceSettlementTransaction(ctx, endpoint, previousTransaction)
			if err != nil {
				return nil, err
			}
			settlementTransactions[previousTransaction] = isSettlement
		}
		if isSettlement {
			c.addressBalanceCoinObjectCache.Store(objectID, struct{}{})
			continue
		}
		filtered = append(filtered, coinObject)
	}
	return filtered, nil
}

func (c *Client) listOwnedCoinObjectsFromEndpoint(ctx context.Context, endpoint string, owner string, coinType string) ([]OwnedCoinObject, error) {
	var coinObjects []OwnedCoinObject
	var pageToken []byte

	for {
		page, err := c.grpc.ListOwnedObjects(ctx, endpoint, listOwnedCoinObjectsRequest(owner, coinType, pageToken))
		if err != nil {
			return nil, err
		}
		for i, object := range page.GetObjects() {
			coinObject, err := ownedCoinObjectFromGRPC(object, coinType)
			if err != nil {
				return nil, fmt.Errorf("invalid coin object %d: %w", len(coinObjects)+i, err)
			}
			coinObjects = append(coinObjects, coinObject)
		}
		if len(page.GetNextPageToken()) == 0 {
			break
		}
		pageToken = page.GetNextPageToken()
	}

	return coinObjects, nil
}

func (c *Client) isAddressBalanceSettlementTransaction(ctx context.Context, endpoint string, digest string) (bool, error) {
	result, err := c.grpc.GetTransaction(ctx, endpoint, digest)
	if err != nil {
		return false, err
	}
	if result.GetTransaction() == nil {
		return false, nil
	}
	data := transactionBlockDataFromGRPC(result.GetTransaction().GetTransaction())
	if data == nil || data.Sender == nil || NormalizeAddress(data.Sender.String()) != NormalizeAddress("0x0") {
		return false, nil
	}
	for _, command := range TransactionCommands(data.Transaction) {
		if command.Kind != CommandKindMoveCall || command.MoveCall == nil {
			continue
		}
		moveCall := command.MoveCall
		if NormalizeAddress(moveCall.Package) == NormalizeAddress("0x2") && moveCall.Module == "accumulator_settlement" {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) resolveGaslessStablecoinExpirationFromEndpoint(ctx context.Context, endpoint string, chainDigest string) (*TransactionExpiration, error) {
	info, err := c.grpc.GetServiceInfo(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	epoch := info.GetEpoch()
	if epoch == ^uint64(0) {
		return nil, errors.New("Sui epoch is too large")
	}
	if strings.TrimSpace(chainDigest) == "" {
		chainDigest = info.GetChainId()
	}

	if strings.TrimSpace(chainDigest) == "" {
		checkpoint, err := c.grpc.GetCheckpoint(ctx, endpoint, 0)
		if err != nil {
			return nil, err
		}
		chainDigest = checkpoint.GetCheckpoint().GetDigest()
	}

	nonce, err := randomUint32()
	if err != nil {
		return nil, err
	}
	return TransactionExpirationValidDuring(chainDigest, epoch, epoch+1, nonce)
}

func endpointCandidatesForNetwork(network string, endpoints []string) ([]string, error) {
	endpointInput := append([]string{}, endpoints...)
	if info := GetNetworkInfo(network); info != nil {
		endpointInput = append(endpointInput, info.DefaultURLs...)
	}
	candidates := utils.EndpointCandidates(endpointInput)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no Sui gRPC endpoints for network %q", network)
	}
	return candidates, nil
}

func decodeTransactionBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrEmptyTransaction
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction encoding: %w", err)
	}
	if len(decoded) == 0 {
		return nil, ErrEmptyTransaction
	}
	return decoded, nil
}

func decodeSignatureBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrEmptySignature
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}
	if len(decoded) == 0 {
		return nil, ErrEmptySignature
	}
	return decoded, nil
}

func decodeSignaturesBase64(values []string) ([][]byte, error) {
	signatures := make([][]byte, 0, len(values))
	for _, value := range values {
		signature, err := decodeSignatureBase64(value)
		if err != nil {
			return nil, err
		}
		signatures = append(signatures, signature)
	}
	if len(signatures) == 0 {
		return nil, ErrEmptySignature
	}
	return signatures, nil
}

func dryRunTransactionBlockFromGRPC(txBytes []byte, response *rpcv2.SimulateTransactionResponse) (*DryRunTransactionBlock, error) {
	input, err := transactionBlockDataFromBytes(txBytes)
	if err != nil {
		return nil, err
	}
	transaction := response.GetTransaction()
	return &DryRunTransactionBlock{
		Input:          *input,
		Effects:        transactionEffectsFromGRPC(transaction.GetEffects()),
		BalanceChanges: balanceChangesFromGRPC(transaction.GetBalanceChanges()),
	}, nil
}

func executeTransactionBlockFromGRPC(transaction *rpcv2.ExecutedTransaction) (*ExecuteTransactionBlock, error) {
	if transaction == nil {
		return nil, errors.New("missing Sui transaction")
	}
	var digest Digest
	if rawDigest := strings.TrimSpace(transaction.GetDigest()); rawDigest != "" {
		parsed, err := ParseDigest(rawDigest)
		if err != nil {
			return nil, err
		}
		digest = parsed
	}
	var checkpoint *string
	if transaction.Checkpoint != nil {
		value := strconv.FormatUint(transaction.GetCheckpoint(), 10)
		checkpoint = &value
	}
	var timestampMs *string
	if timestamp := transaction.GetTimestamp(); timestamp != nil {
		value := strconv.FormatInt(timestamp.AsTime().UnixMilli(), 10)
		timestampMs = &value
	}
	return &ExecuteTransactionBlock{
		Digest:         digest,
		Effects:        ptr(transactionEffectsFromGRPC(transaction.GetEffects())),
		BalanceChanges: balanceChangesFromGRPC(transaction.GetBalanceChanges()),
		TimestampMs:    timestampMs,
		Checkpoint:     checkpoint,
	}, nil
}

func transactionEffectsFromGRPC(effects *rpcv2.TransactionEffects) TransactionEffects {
	result := TransactionEffects{}
	if effects == nil {
		return result
	}
	if status := effects.GetStatus(); status != nil {
		result.Status = &TransactionExecutionStatus{Status: "failure"}
		if status.GetSuccess() {
			result.Status.Status = "success"
		}
		if err := status.GetError(); err != nil {
			description := strings.TrimSpace(err.GetDescription())
			if description == "" {
				description = err.String()
			}
			result.Status.Error = &description
		}
	}
	if effects.Epoch != nil {
		result.ExecutedEpoch = strconv.FormatUint(effects.GetEpoch(), 10)
	}
	if gasUsed := effects.GetGasUsed(); gasUsed != nil {
		result.GasUsed = &GasUsedResult{
			ComputationCost:         strconv.FormatUint(gasUsed.GetComputationCost(), 10),
			StorageCost:             strconv.FormatUint(gasUsed.GetStorageCost(), 10),
			StorageRebate:           strconv.FormatUint(gasUsed.GetStorageRebate(), 10),
			NonRefundableStorageFee: strconv.FormatUint(gasUsed.GetNonRefundableStorageFee(), 10),
		}
	}
	if digest := strings.TrimSpace(effects.GetTransactionDigest()); digest != "" {
		if parsed, err := ParseDigest(digest); err == nil {
			result.TransactionDigest = parsed
		}
	}
	for _, dependency := range effects.GetDependencies() {
		if parsed, err := ParseDigest(dependency); err == nil {
			result.Dependencies = append(result.Dependencies, parsed)
		}
	}
	for _, changed := range effects.GetChangedObjects() {
		applyChangedObject(&result, changed)
	}
	if gasObject := changedObjectOwnerResult(effects.GetGasObject()); gasObject != nil {
		result.GasObject = gasObject
	}
	return result
}

func applyChangedObject(result *TransactionEffects, changed *rpcv2.ChangedObject) {
	if result == nil || changed == nil {
		return
	}
	switch changed.GetOutputState() {
	case rpcv2.ChangedObject_OUTPUT_OBJECT_STATE_DOES_NOT_EXIST:
		if ref := changedInputRef(changed); ref != nil {
			result.Deleted = append(result.Deleted, *ref)
		}
	case rpcv2.ChangedObject_OUTPUT_OBJECT_STATE_OBJECT_WRITE, rpcv2.ChangedObject_OUTPUT_OBJECT_STATE_PACKAGE_WRITE:
		owner := changedObjectOwnerResult(changed)
		if owner == nil {
			return
		}
		if changed.GetIdOperation() == rpcv2.ChangedObject_CREATED ||
			changed.GetInputState() == rpcv2.ChangedObject_INPUT_OBJECT_STATE_DOES_NOT_EXIST {
			result.Created = append(result.Created, *owner)
			return
		}
		result.Mutated = append(result.Mutated, *owner)
	}
}

func changedObjectOwnerResult(changed *rpcv2.ChangedObject) *ObjectOwnerResult {
	if changed == nil {
		return nil
	}
	ref := changedOutputRef(changed)
	if ref == nil {
		ref = changedInputRef(changed)
	}
	if ref == nil {
		return nil
	}
	return &ObjectOwnerResult{
		Owner:     ownerFromGRPC(changed.GetOutputOwner()),
		Reference: ref,
	}
}

func changedOutputRef(changed *rpcv2.ChangedObject) *ObjectRefResult {
	if changed == nil || strings.TrimSpace(changed.GetObjectId()) == "" || strings.TrimSpace(changed.GetOutputDigest()) == "" {
		return nil
	}
	return objectRefResult(changed.GetObjectId(), changed.GetOutputVersion(), changed.GetOutputDigest())
}

func changedInputRef(changed *rpcv2.ChangedObject) *ObjectRefResult {
	if changed == nil || strings.TrimSpace(changed.GetObjectId()) == "" || strings.TrimSpace(changed.GetInputDigest()) == "" {
		return nil
	}
	return objectRefResult(changed.GetObjectId(), changed.GetInputVersion(), changed.GetInputDigest())
}

func objectRefResult(objectID string, version uint64, digest string) *ObjectRefResult {
	parsedObjectID, err := ParseAddress(objectID)
	if err != nil {
		return nil
	}
	parsedDigest, err := ParseDigest(digest)
	if err != nil {
		return nil
	}
	return &ObjectRefResult{
		ObjectID: parsedObjectID,
		Version:  ObjectVersion(strconv.FormatUint(version, 10)),
		Digest:   parsedDigest,
	}
}

func balanceChangesFromGRPC(changes []*rpcv2.BalanceChange) []BalanceChange {
	result := make([]BalanceChange, 0, len(changes))
	for _, change := range changes {
		if change == nil {
			continue
		}
		result = append(result, BalanceChange{
			Owner:    map[string]interface{}{"AddressOwner": change.GetAddress()},
			CoinType: change.GetCoinType(),
			Amount:   change.GetAmount(),
		})
	}
	return result
}

func ownedCoinObjectFromGRPC(object *rpcv2.Object, expectedCoinType string) (OwnedCoinObject, error) {
	if object == nil {
		return OwnedCoinObject{}, errors.New("nil object")
	}
	if objectType := strings.TrimSpace(object.GetObjectType()); objectType != "" {
		if NormalizeType(objectType) != NormalizeType(coinObjectType(expectedCoinType)) {
			return OwnedCoinObject{}, fmt.Errorf("coin object type %q does not match %q", objectType, expectedCoinType)
		}
	}
	objectID, err := ParseAddress(object.GetObjectId())
	if err != nil {
		return OwnedCoinObject{}, err
	}
	digest, err := ParseDigest(object.GetDigest())
	if err != nil {
		return OwnedCoinObject{}, err
	}
	return OwnedCoinObject{
		ObjectRef: ObjectRef{
			ObjectID: objectID,
			Version:  object.GetVersion(),
			Digest:   digest,
		},
		CoinType:            expectedCoinType,
		Balance:             object.GetBalance(),
		PreviousTransaction: strings.TrimSpace(object.GetPreviousTransaction()),
	}, nil
}

func transactionBlockDataFromBytes(txBytes []byte) (*TransactionBlockData, error) {
	txData, err := bcs.Unmarshal[gaslessStablecoinTransactionData](txBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction data: %w", err)
	}
	if txData.V1 == nil {
		return nil, errors.New("transaction missing V1 data")
	}
	return transactionBlockDataFromV1(txData.V1), nil
}

func transactionBlockDataFromGRPC(transaction *rpcv2.Transaction) *TransactionBlockData {
	if transaction == nil {
		return nil
	}
	if bcsData := transaction.GetBcs(); bcsData != nil && len(bcsData.GetValue()) > 0 {
		if data, err := transactionBlockDataFromBytes(bcsData.GetValue()); err == nil {
			return data
		}
	}
	sender := strings.TrimSpace(transaction.GetSender())
	var parsedSender *Address
	if sender != "" {
		if address, err := ParseAddress(sender); err == nil {
			parsedSender = &address
		}
	}
	return &TransactionBlockData{
		MessageVersion: "v1",
		Sender:         parsedSender,
		Transaction:    transactionKindFromGRPC(transaction.GetKind()),
		GasData:        gasDataFromGRPC(transaction.GetGasPayment()),
	}
}

func transactionBlockDataFromV1(v1 *gaslessStablecoinTransactionDataV1) *TransactionBlockData {
	sender := v1.Sender
	return &TransactionBlockData{
		MessageVersion: "v1",
		Sender:         &sender,
		Transaction:    transactionKindFromProgrammable(v1.Kind.ProgrammableTransaction),
		GasData:        gasDataFromBCS(v1.GasData),
	}
}

func transactionKindFromProgrammable(programmable *gaslessStablecoinProgrammableTransaction) *TransactionKind {
	if programmable == nil {
		return &TransactionKind{}
	}
	commands := make([]json.RawMessage, 0, len(programmable.Commands))
	for _, command := range programmable.Commands {
		commands = append(commands, commandRawMessage(command))
	}
	return &TransactionKind{
		Kind:     TransactionKindProgrammable,
		Commands: commands,
	}
}

func transactionKindFromGRPC(kind *rpcv2.TransactionKind) *TransactionKind {
	if kind == nil {
		return nil
	}
	programmable := kind.GetProgrammableTransaction()
	if programmable == nil {
		return &TransactionKind{Kind: kind.GetKind().String()}
	}
	commands := make([]json.RawMessage, 0, len(programmable.GetCommands()))
	for _, command := range programmable.GetCommands() {
		commands = append(commands, commandRawMessageFromGRPC(command))
	}
	return &TransactionKind{
		Kind:     TransactionKindProgrammable,
		Commands: commands,
	}
}

func commandRawMessage(command Command) json.RawMessage {
	var value map[string]interface{}
	switch {
	case command.MoveCall != nil:
		typeArguments := make([]string, 0, len(command.MoveCall.TypeArguments))
		for _, typeArgument := range command.MoveCall.TypeArguments {
			typeArguments = append(typeArguments, typeArgument.String())
		}
		value = map[string]interface{}{
			CommandKindMoveCall: map[string]interface{}{
				"package":        command.MoveCall.Package.String(),
				"module":         command.MoveCall.Module,
				"function":       command.MoveCall.Function,
				"type_arguments": typeArguments,
				"arguments":      command.MoveCall.Arguments,
			},
		}
	case command.TransferObjects != nil:
		value = map[string]interface{}{CommandKindTransferObjects: command.TransferObjects}
	case command.SplitCoins != nil:
		value = map[string]interface{}{CommandKindSplitCoins: command.SplitCoins}
	case command.MergeCoins != nil:
		value = map[string]interface{}{CommandKindMergeCoins: command.MergeCoins}
	case command.Publish != nil:
		value = map[string]interface{}{CommandKindPublish: command.Publish}
	case command.MakeMoveVec != nil:
		value = map[string]interface{}{CommandKindMakeMoveVec: command.MakeMoveVec}
	case command.Upgrade != nil:
		value = map[string]interface{}{CommandKindUpgrade: command.Upgrade}
	default:
		value = map[string]interface{}{"Unknown": map[string]interface{}{}}
	}
	raw, _ := json.Marshal(value)
	return raw
}

func commandRawMessageFromGRPC(command *rpcv2.Command) json.RawMessage {
	var value map[string]interface{}
	switch {
	case command.GetMoveCall() != nil:
		moveCall := command.GetMoveCall()
		value = map[string]interface{}{
			CommandKindMoveCall: map[string]interface{}{
				"package":        moveCall.GetPackage(),
				"module":         moveCall.GetModule(),
				"function":       moveCall.GetFunction(),
				"type_arguments": moveCall.GetTypeArguments(),
			},
		}
	case command.GetTransferObjects() != nil:
		value = map[string]interface{}{CommandKindTransferObjects: map[string]interface{}{}}
	case command.GetSplitCoins() != nil:
		value = map[string]interface{}{CommandKindSplitCoins: map[string]interface{}{}}
	case command.GetMergeCoins() != nil:
		value = map[string]interface{}{CommandKindMergeCoins: map[string]interface{}{}}
	case command.GetPublish() != nil:
		value = map[string]interface{}{CommandKindPublish: map[string]interface{}{}}
	case command.GetMakeMoveVector() != nil:
		value = map[string]interface{}{CommandKindMakeMoveVec: map[string]interface{}{}}
	case command.GetUpgrade() != nil:
		value = map[string]interface{}{CommandKindUpgrade: map[string]interface{}{}}
	default:
		value = map[string]interface{}{"Unknown": map[string]interface{}{}}
	}
	raw, _ := json.Marshal(value)
	return raw
}

func gasDataFromBCS(data gaslessStablecoinGasData) *GasData {
	payment := make([]ObjectRefResult, 0, len(data.Payment))
	for _, ref := range data.Payment {
		payment = append(payment, ObjectRefResult{
			ObjectID: ref.ObjectID,
			Version:  ObjectVersion(strconv.FormatUint(ref.Version, 10)),
			Digest:   ref.Digest,
		})
	}
	owner := data.Owner
	return &GasData{
		Payment: payment,
		Owner:   &owner,
		Price:   strconv.FormatUint(data.Price, 10),
		Budget:  strconv.FormatUint(data.Budget, 10),
	}
}

func gasDataFromGRPC(data *rpcv2.GasPayment) *GasData {
	if data == nil {
		return nil
	}
	payment := make([]ObjectRefResult, 0, len(data.GetObjects()))
	for _, ref := range data.GetObjects() {
		if converted := objectRefResult(ref.GetObjectId(), ref.GetVersion(), ref.GetDigest()); converted != nil {
			payment = append(payment, *converted)
		}
	}
	var owner *Address
	if rawOwner := strings.TrimSpace(data.GetOwner()); rawOwner != "" {
		if parsed, err := ParseAddress(rawOwner); err == nil {
			owner = &parsed
		}
	}
	return &GasData{
		Payment: payment,
		Owner:   owner,
		Price:   strconv.FormatUint(data.GetPrice(), 10),
		Budget:  strconv.FormatUint(data.GetBudget(), 10),
	}
}

func ownerFromGRPC(owner *rpcv2.Owner) interface{} {
	if owner == nil {
		return nil
	}
	switch owner.GetKind() {
	case rpcv2.Owner_ADDRESS:
		return map[string]interface{}{"AddressOwner": owner.GetAddress()}
	case rpcv2.Owner_OBJECT:
		return map[string]interface{}{"ObjectOwner": owner.GetAddress()}
	case rpcv2.Owner_SHARED:
		return map[string]interface{}{"Shared": map[string]interface{}{"initial_shared_version": strconv.FormatUint(owner.GetVersion(), 10)}}
	case rpcv2.Owner_IMMUTABLE:
		return "Immutable"
	default:
		return owner.GetAddress()
	}
}

func listOwnedCoinObjectsRequest(owner string, coinType string, pageToken []byte) *rpcv2.ListOwnedObjectsRequest {
	objectType := coinObjectType(coinType)
	return &rpcv2.ListOwnedObjectsRequest{
		Owner:      &owner,
		PageSize:   ptr(uint32(suiCoinObjectPageLimit)),
		PageToken:  pageToken,
		ObjectType: &objectType,
		ReadMask: &fieldmaskpb.FieldMask{Paths: []string{
			"object_id",
			"version",
			"digest",
			"object_type",
			"balance",
			"previous_transaction",
		}},
	}
}

func coinObjectType(coinType string) string {
	return fmt.Sprintf("%s<%s>", NormalizeType("0x2::coin::Coin"), NormalizeType(coinType))
}

func ptr[T any](value T) *T {
	return &value
}
